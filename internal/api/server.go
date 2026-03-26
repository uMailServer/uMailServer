package api

import (
	_ "embed"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/mcp"
)

//go:embed static/index.html
var webmailHTML []byte

// Server represents the admin API server
type Server struct {
	db        *db.DB
	logger    *slog.Logger
	config    Config
	mcpServer *mcp.Server
}

// Config holds API server configuration
type Config struct {
	Addr        string
	JWTSecret   string
	TokenExpiry time.Duration
}

// NewServer creates a new admin API server
func NewServer(db *db.DB, logger *slog.Logger, config Config) *Server {
	if config.TokenExpiry == 0 {
		config.TokenExpiry = 24 * time.Hour
	}

	return &Server{
		db:        db,
		logger:    logger,
		config:    config,
		mcpServer: mcp.NewServer(db),
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

	// MCP endpoint
	mux.HandleFunc("/mcp", s.mcpServer.HandleHTTP)

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

	// Wrap with middleware
	return s.corsMiddleware(s.authMiddleware(api))
}

// Start starts the API server
func (s *Server) Start(addr string) error {
	s.config.Addr = addr

	server := &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.logger.Info("Admin API server starting", "addr", addr)
	return server.ListenAndServe()
}

// Middleware

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
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

	// Check password (TODO: Use proper password hashing)
	if account.PasswordHash != req.Password {
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
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

	// TODO: Return actual metrics
	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"smtp": map[string]int{
			"connections": 0,
			"messages":    0,
		},
		"imap": map[string]int{
			"connections": 0,
			"sessions":    0,
		},
		"queue": map[string]int{
			"pending": 0,
			"failed":  0,
		},
	})
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

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"domains":    len(domains),
		"accounts":   0, // TODO: Count accounts
		"messages":   0, // TODO: Count messages
		"queue_size": 0, // TODO: Count queue
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

	account := &db.AccountData{
		Email:        req.Email,
		LocalPart:    user,
		Domain:       domain,
		PasswordHash: req.Password, // TODO: Hash password
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
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
		IsActive bool   `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password != "" {
		account.PasswordHash = req.Password // TODO: Hash password
	}
	account.IsAdmin = req.IsAdmin
	account.IsActive = req.IsActive
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
	// TODO: Implement queue listing
	s.sendJSON(w, http.StatusOK, []map[string]interface{}{})
}

func (s *Server) getQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	// TODO: Implement queue entry retrieval
	s.sendError(w, http.StatusNotFound, "queue entry not found")
}

func (s *Server) retryQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	// TODO: Implement queue retry
	s.sendJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

func (s *Server) dropQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	// TODO: Implement queue drop
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
	return map[string]interface{}{
		"name":         d.Name,
		"max_accounts": d.MaxAccounts,
		"is_active":    d.IsActive,
		"created_at":   d.CreatedAt,
		"updated_at":   d.UpdatedAt,
	}
}

func accountToJSON(a *db.AccountData) map[string]interface{} {
	return map[string]interface{}{
		"email":       a.Email,
		"is_admin":    a.IsAdmin,
		"is_active":   a.IsActive,
		"quota_used":  a.QuotaUsed,
		"quota_limit": a.QuotaLimit,
		"created_at":  a.CreatedAt,
		"updated_at":  a.UpdatedAt,
		"last_login":  a.LastLoginAt,
	}
}

func parseEmail(email string) (user, domain string) {
	at := strings.LastIndex(email, "@")
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}
