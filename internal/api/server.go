package api

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/mcp"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/search"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/websocket"
	"golang.org/x/crypto/bcrypt"
)

// Simple placeholder HTML for webmail - in production this would be the built React app
var webmailHTML = []byte(`<!DOCTYPE html>
<html>
<head>
    <title>uMailServer Webmail</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        h1 { color: #2563eb; }
    </style>
</head>
<body>
    <h1>uMailServer Webmail</h1>
    <p>Webmail is loading...</p>
</body>
</html>`)

// Server represents the admin API server
type Server struct {
	db         *db.DB
	logger     *slog.Logger
	config     Config
	mcpServer  *mcp.Server
	sseServer  *websocket.SSEServer
	searchSvc  *search.Service
	msgStore   *storage.MessageStore
	queueMgr   *queue.Manager
	httpServer *http.Server
}

// Config holds API server configuration
type Config struct {
	Addr        string
	JWTSecret   string
	TokenExpiry time.Duration
	CorsOrigins []string
}

// NewServer creates a new admin API server
func NewServer(database *db.DB, logger *slog.Logger, config Config) *Server {
	if config.TokenExpiry == 0 {
		config.TokenExpiry = 24 * time.Hour
	}

	sseServer := websocket.NewSSEServer(logger)
	if len(config.CorsOrigins) > 0 {
		sseServer.SetCorsOrigin(strings.Join(config.CorsOrigins, ","))
	}
	sseServer.SetAuthFunc(func(token string) (user string, isAdmin bool, err error) {
		parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(config.JWTSecret), nil
		})
		if err != nil || !parsed.Valid {
			return "", false, fmt.Errorf("invalid token")
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			return "", false, fmt.Errorf("invalid claims")
		}
		u, _ := claims["sub"].(string)
		a, _ := claims["admin"].(bool)
		return u, a, nil
	})

	return &Server{
		db:        database,
		logger:    logger,
		config:    config,
		mcpServer: mcp.NewServer(database),
		sseServer: sseServer,
	}
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router().ServeHTTP(w, r)
}

// router sets up the HTTP routes
func (s *Server) router() http.Handler {
	mux := http.NewServeMux()

	// Webmail (static files)
	mux.HandleFunc("/", s.handleWebmail)
	mux.HandleFunc("/webmail", s.handleWebmail)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	// Metrics endpoint
	mux.HandleFunc("/metrics", metrics.Get().HTTPHandler)

	// SSE endpoint for real-time updates
	mux.HandleFunc("/api/v1/events", s.sseServer.Handler())

	// MCP endpoint (protected by auth)
	mux.HandleFunc("/mcp", s.authMiddleware(http.HandlerFunc(s.mcpServer.HandleHTTP)).ServeHTTP)

	// Authentication
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/refresh", s.handleRefresh)

	// Protected routes
	api := http.NewServeMux()

	// Domains
	api.HandleFunc("/api/v1/domains", s.handleDomains)
	api.HandleFunc("/api/v1/domains/", s.handleDomainDetail)

	// Accounts
	api.HandleFunc("/api/v1/accounts", s.handleAccounts)
	api.HandleFunc("/api/v1/accounts/", s.handleAccountDetail)

	// Queue
	api.HandleFunc("/api/v1/queue", s.handleQueue)
	api.HandleFunc("/api/v1/queue/", s.handleQueueDetail)

	// Metrics
	api.HandleFunc("/api/v1/metrics", s.handleMetrics)

	// Stats
	api.HandleFunc("/api/v1/stats", s.handleStats)

	// Search
	api.HandleFunc("/api/v1/search", s.handleSearch)

	// Wrap with middleware
	return s.corsMiddleware(s.authMiddleware(api))
}

// SetSearchService injects the search service into the API server
func (s *Server) SetSearchService(svc *search.Service) {
	s.searchSvc = svc
}

// Start starts the API server
func (s *Server) Start(addr string) error {
	s.config.Addr = addr

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.logger.Info("Admin API server starting", "addr", addr)
	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the API server
func (s *Server) Stop() error {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// Middleware

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := ""
		if len(s.config.CorsOrigins) > 0 {
			for _, o := range s.config.CorsOrigins {
				if o == origin || o == "*" {
					allowed = o
					break
				}
			}
		} else {
			allowed = "*"
		}
		if allowed != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health and login endpoints
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.sendError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			s.sendError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		tokenStr := parts[1]

		// Validate token
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(s.config.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			s.sendError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		// Get claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			s.sendError(w, http.StatusUnauthorized, "invalid token claims")
			return
		}

		// Add claims to context
		ctx := context.WithValue(r.Context(), "user", claims["sub"])
		ctx = context.WithValue(ctx, "isAdmin", claims["admin"])

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

func (s *Server) handleWebmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(webmailHTML)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Parse email
	user, domain := parseEmail(req.Email)

	// Get account
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Check password using bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(req.Password)); err != nil {
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Check TOTP if enabled
	if account.TOTPEnabled {
		if req.TOTPCode == "" {
			s.sendError(w, http.StatusUnauthorized, "TOTP code required")
			return
		}
		if !auth.ValidateTOTP(account.TOTPSecret, req.TOTPCode) {
			s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
			return
		}
	}

	// Generate JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   account.Email,
		"admin": account.IsAdmin,
		"exp":   time.Now().Add(s.config.TokenExpiry).Unix(),
		"iat":   time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// The auth middleware already validated the token
	user := r.Context().Value("user")
	isAdmin := r.Context().Value("isAdmin")

	// Generate new token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user,
		"admin": isAdmin,
		"exp":   time.Now().Add(s.config.TokenExpiry).Unix(),
		"iat":   time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})
}

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listDomains(w, r)
	case http.MethodPost:
		s.createDomain(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDomainDetail(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimPrefix(r.URL.Path, "/api/v1/domains/")

	switch r.Method {
	case http.MethodGet:
		s.getDomain(w, r, domain)
	case http.MethodPut:
		s.updateDomain(w, r, domain)
	case http.MethodDelete:
		s.deleteDomain(w, r, domain)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAccounts(w, r)
	case http.MethodPost:
		s.createAccount(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAccountDetail(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")

	switch r.Method {
	case http.MethodGet:
		s.getAccount(w, r, email)
	case http.MethodPut:
		s.updateAccount(w, r, email)
	case http.MethodDelete:
		s.deleteAccount(w, r, email)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listQueue(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleQueueDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/queue/")

	switch r.Method {
	case http.MethodGet:
		s.getQueueEntry(w, r, id)
	case http.MethodPost:
		s.retryQueueEntry(w, r, id)
	case http.MethodDelete:
		s.dropQueueEntry(w, r, id)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get actual metrics from metrics collector
	stats := metrics.Get().GetStats()
	s.sendJSON(w, http.StatusOK, stats)
}


// SetQueueManager injects the queue manager for stats
func (s *Server) SetQueueManager(qm *queue.Manager) {
	s.queueMgr = qm
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	domains, err := s.db.ListDomains()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	// Count accounts across all domains
	accounts := 0
	for _, d := range domains {
		accts, _ := s.db.ListAccountsByDomain(d.Name)
		accounts += len(accts)
	}

	queueSize := 0
	if s.queueMgr != nil {
		if stats, err := s.queueMgr.GetStats(); err == nil {
			queueSize = stats.Pending + stats.Sending + stats.Failed
		}
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"domains":    len(domains),
		"accounts":   accounts,
		"messages":   0, // Would need to scan maildirs
		"queue_size": queueSize,
	})
}

// Domain handlers

func (s *Server) listDomains(w http.ResponseWriter, r *http.Request) {
	domains, err := s.db.ListDomains()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list domains")
		return
	}

	var result []map[string]interface{}
	for _, d := range domains {
		result = append(result, domainToJSON(d))
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) createDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		MaxAccounts int    `json:"max_accounts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		s.sendError(w, http.StatusBadRequest, "domain name is required")
		return
	}

	domain := &db.DomainData{
		Name:        req.Name,
		MaxAccounts: req.MaxAccounts,
		IsActive:    true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Generate DKIM key pair for the domain
	privKey, _, err := auth.GenerateDKIMKeyPair(2048)
	if err == nil {
		domain.DKIMSelector = "default"
		domain.DKIMPublicKey = auth.GetPublicKeyForDNS(privKey)
		privKeyBytes := x509.MarshalPKCS1PrivateKey(privKey)
		domain.DKIMPrivateKey = string(pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privKeyBytes,
		}))
	}

	if err := s.db.CreateDomain(domain); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to create domain")
		return
	}

	s.sendJSON(w, http.StatusCreated, domainToJSON(domain))
}

func (s *Server) getDomain(w http.ResponseWriter, r *http.Request, name string) {
	domain, err := s.db.GetDomain(name)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "domain not found")
		return
	}

	s.sendJSON(w, http.StatusOK, domainToJSON(domain))
}

func (s *Server) updateDomain(w http.ResponseWriter, r *http.Request, name string) {
	domain, err := s.db.GetDomain(name)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "domain not found")
		return
	}

	var req struct {
		MaxAccounts int  `json:"max_accounts"`
		IsActive    bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	domain.MaxAccounts = req.MaxAccounts
	domain.IsActive = req.IsActive
	domain.UpdatedAt = time.Now()

	if err := s.db.UpdateDomain(domain); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update domain")
		return
	}

	s.sendJSON(w, http.StatusOK, domainToJSON(domain))
}

func (s *Server) deleteDomain(w http.ResponseWriter, r *http.Request, name string) {
	if err := s.db.DeleteDomain(name); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to delete domain")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Account handlers

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")

	var accounts []*db.AccountData
	var err error

	if domain != "" {
		accounts, err = s.db.ListAccountsByDomain(domain)
	} else {
		// Get all accounts from all domains
		domains, err := s.db.ListDomains()
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to list accounts")
			return
		}
		for _, d := range domains {
			domainAccounts, _ := s.db.ListAccountsByDomain(d.Name)
			accounts = append(accounts, domainAccounts...)
		}
	}

	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}

	var result []map[string]interface{}
	for _, a := range accounts {
		result = append(result, accountToJSON(a))
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		s.sendError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, domain := parseEmail(req.Email)

	// Hash password with bcrypt
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	account := &db.AccountData{
		Email:        req.Email,
		LocalPart:    user,
		Domain:       domain,
		PasswordHash: string(hashedPassword),
		IsAdmin:      req.IsAdmin,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.CreateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	s.sendJSON(w, http.StatusCreated, accountToJSON(account))
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	s.sendJSON(w, http.StatusOK, accountToJSON(account))
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	var req struct {
		Password         string `json:"password"`
		IsAdmin          bool   `json:"is_admin"`
		IsActive         bool   `json:"is_active"`
		ForwardTo        string `json:"forward_to"`
		ForwardKeepCopy  bool   `json:"forward_keep_copy"`
		QuotaLimit       int64  `json:"quota_limit"`
		VacationSettings string `json:"vacation_settings"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password != "" {
		// Hash new password with bcrypt
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		account.PasswordHash = string(hashedPassword)
	}
	account.IsAdmin = req.IsAdmin
	account.IsActive = req.IsActive
	account.ForwardTo = req.ForwardTo
	account.ForwardKeepCopy = req.ForwardKeepCopy
	account.QuotaLimit = req.QuotaLimit
	account.VacationSettings = req.VacationSettings
	account.UpdatedAt = time.Now()

	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update account")
		return
	}

	s.sendJSON(w, http.StatusOK, accountToJSON(account))
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	if err := s.db.DeleteAccount(domain, user); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to delete account")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Queue handlers

func (s *Server) listQueue(w http.ResponseWriter, r *http.Request) {
	// Get pending queue entries from database
	entries, err := s.db.GetPendingQueue(time.Now().Add(24 * time.Hour))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list queue")
		return
	}

	var result []map[string]interface{}
	for _, e := range entries {
		result = append(result, map[string]interface{}{
			"id":          e.ID,
			"from":        e.From,
			"to":          e.To,
			"status":      e.Status,
			"retry_count": e.RetryCount,
			"last_error":  e.LastError,
			"created_at":  e.CreatedAt,
			"next_retry":  e.NextRetry,
		})
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) getQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := s.db.GetQueueEntry(id)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "queue entry not found")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"id":          entry.ID,
		"from":        entry.From,
		"to":          entry.To,
		"status":      entry.Status,
		"retry_count": entry.RetryCount,
		"last_error":  entry.LastError,
		"created_at":  entry.CreatedAt,
		"next_retry":  entry.NextRetry,
	})
}

func (s *Server) retryQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := s.db.GetQueueEntry(id)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "queue entry not found")
		return
	}

	// Reset retry count and status
	entry.Status = "pending"
	entry.RetryCount = 0
	entry.LastError = ""
	entry.NextRetry = time.Now()

	if err := s.db.UpdateQueueEntry(entry); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to retry queue entry")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

func (s *Server) dropQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.db.Dequeue(id); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to drop queue entry")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Helpers

func (s *Server) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) sendError(w http.ResponseWriter, status int, message string) {
	s.sendJSON(w, status, map[string]string{"error": message})
}

func domainToJSON(d *db.DomainData) map[string]interface{} {
	result := map[string]interface{}{
		"name":         d.Name,
		"max_accounts": d.MaxAccounts,
		"is_active":    d.IsActive,
		"created_at":   d.CreatedAt,
		"updated_at":   d.UpdatedAt,
	}
	if d.DKIMSelector != "" {
		result["dkim_selector"] = d.DKIMSelector
		result["dkim_public_key"] = d.DKIMPublicKey
	}
	return result
}

func accountToJSON(a *db.AccountData) map[string]interface{} {
	result := map[string]interface{}{
		"email":             a.Email,
		"is_admin":          a.IsAdmin,
		"is_active":         a.IsActive,
		"quota_used":        a.QuotaUsed,
		"quota_limit":       a.QuotaLimit,
		"forward_to":        a.ForwardTo,
		"forward_keep_copy": a.ForwardKeepCopy,
		"created_at":        a.CreatedAt,
		"updated_at":        a.UpdatedAt,
		"last_login":        a.LastLoginAt,
	}
	if a.VacationSettings != "" {
		result["vacation_settings"] = a.VacationSettings
	}
	return result
}

func parseEmail(email string) (user, domain string) {
	at := strings.LastIndex(email, "@")
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// Search handlers

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user := r.Context().Value("user")
	if user == nil {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		s.sendError(w, http.StatusBadRequest, "missing query parameter 'q'")
		return
	}

	folder := r.URL.Query().Get("folder")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Perform search
	if s.searchSvc == nil {
		s.sendError(w, http.StatusServiceUnavailable, "search service not available")
		return
	}

	results, err := s.searchSvc.Search(search.MessageSearchOptions{
		User:   user.(string),
		Folder: folder,
		Query:  query,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "search failed")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"query":   query,
		"folder":  folder,
		"results": results,
		"total":   len(results),
		"limit":   limit,
		"offset":  offset,
	})
}
