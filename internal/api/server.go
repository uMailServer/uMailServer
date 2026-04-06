package api

import (
	"context"
	"crypto/md5"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver"
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

// loginAttempt tracks failed login attempts per IP
type loginAttempt struct {
	count    int
	lastSeen time.Time
}

// apiRateAttempt tracks API requests per IP for rate limiting
type apiRateAttempt struct {
	count    int
	windowStart time.Time
}

// HealthMonitor interface for health checks
type HealthMonitor interface {
	HTTPHandler() http.HandlerFunc
}

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
	healthMon  HealthMonitor

	// Interface abstractions for testability
	vacationMgr VacationManager
	filterMgr   FilterManager
	pushSvc     PushService

	// File system abstraction for embed.FS
	webmailFS FileSystem
	adminFS   FileSystem

	// Login rate limiting
	loginMu      sync.Mutex
	loginAttempts map[string]*loginAttempt

	// API rate limiting (HTTPRequestsPerMinute)
	apiRateMu       sync.Mutex
	apiRateAttempts map[string]*apiRateAttempt
	apiRateLimit    int // requests per minute, 0 = disabled

	// Mock errors for testing (used to test error paths)
	vacationGetError       error
	vacationSetError       error
	vacationDeleteError    error
	filterSaveError        error
	filterGetError         error
	pushSubscribeError     error
	pushUnsubscribeError   error
	pushSendError          error
	queueMgrStatsError     error
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
	if logger == nil {
		logger = slog.Default()
	}
	if config.JWTSecret == "" {
		logger.Warn("JWTSecret is empty, generating random secret - tokens will not survive restarts")
		config.JWTSecret = fmt.Sprintf("%d", time.Now().UnixNano())
	}
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
		db:         database,
		logger:     logger,
		config:     config,
		mcpServer:  mcp.NewServer(database),
		sseServer:  sseServer,
		webmailFS:  newEmbedFSSub(umailserver.WebmailFS, "webmail/dist"),
		adminFS:    newEmbedFSSub(umailserver.AdminFS, "web/admin/dist"),
	}
}

// NewServerWithInterfaces creates a new admin API server with injectable interfaces for testing
func NewServerWithInterfaces(
	database *db.DB,
	logger *slog.Logger,
	config Config,
	vacationMgr VacationManager,
	filterMgr FilterManager,
	pushSvc PushService,
	webmailFS FileSystem,
	adminFS FileSystem,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if config.JWTSecret == "" {
		logger.Warn("JWTSecret is empty, generating random secret - tokens will not survive restarts")
		config.JWTSecret = fmt.Sprintf("%d", time.Now().UnixNano())
	}
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

	// Use provided FS or default to embedded
	if webmailFS == nil {
		webmailFS = NewEmbedFSAdapter(umailserver.WebmailFS)
	}
	if adminFS == nil {
		adminFS = NewEmbedFSAdapter(umailserver.AdminFS)
	}

	return &Server{
		db:           database,
		logger:       logger,
		config:       config,
		mcpServer:    mcp.NewServer(database),
		sseServer:    sseServer,
		vacationMgr:  vacationMgr,
		filterMgr:    filterMgr,
		pushSvc:      pushSvc,
		webmailFS:    webmailFS,
		adminFS:      adminFS,
	}
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router().ServeHTTP(w, r)
}

// router sets up the HTTP routes
func (s *Server) router() http.Handler {
	mux := http.NewServeMux()

	// Webmail (static files) - user interface
	mux.HandleFunc("/", s.handleWebmail)
	mux.HandleFunc("/webmail/", s.handleWebmail)

	// Admin panel (static files) - admin interface
	mux.HandleFunc("/admin/", s.handleAdmin)

	// Mozilla-style autoconfig
	mux.HandleFunc("/.well-known/autoconfig/mail/config-v1.1.xml", s.handleAutoconfig)

	// Microsoft Autodiscover
	mux.HandleFunc("/autodiscover/autodiscover.xml", s.handleAutodiscover)

	// Health check - use health monitor if available
	if s.healthMon != nil {
		mux.HandleFunc("/health", s.healthMon.HTTPHandler())
	} else {
		mux.HandleFunc("/health", s.handleHealth)
	}

	// Metrics endpoint (admin only)
	mux.HandleFunc("/metrics", s.authMiddleware(s.adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.Get().HTTPHandler(w, r)
	}))).ServeHTTP)

	// SSE endpoint for real-time updates
	mux.HandleFunc("/api/v1/events", s.sseServer.Handler())

	// MCP endpoint (protected by auth)
	mux.HandleFunc("/mcp", s.authMiddleware(http.HandlerFunc(s.mcpServer.HandleHTTP)).ServeHTTP)

	// Authentication
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/refresh", s.handleRefresh)

	// Protected routes
	api := http.NewServeMux()

	// Domains (admin only)
	api.HandleFunc("/api/v1/domains", s.adminMiddleware(http.HandlerFunc(s.handleDomains)).ServeHTTP)
	api.HandleFunc("/api/v1/domains/", s.adminMiddleware(http.HandlerFunc(s.handleDomainDetail)).ServeHTTP)

	// Accounts (admin only)
	api.HandleFunc("/api/v1/accounts", s.adminMiddleware(http.HandlerFunc(s.handleAccounts)).ServeHTTP)
	api.HandleFunc("/api/v1/accounts/", s.adminMiddleware(http.HandlerFunc(s.handleAccountDetail)).ServeHTTP)

	// Queue (admin only)
	api.HandleFunc("/api/v1/queue", s.adminMiddleware(http.HandlerFunc(s.handleQueue)).ServeHTTP)
	api.HandleFunc("/api/v1/queue/", s.adminMiddleware(http.HandlerFunc(s.handleQueueDetail)).ServeHTTP)

	// Metrics (admin only)
	api.HandleFunc("/api/v1/metrics", s.adminMiddleware(http.HandlerFunc(s.handleMetrics)).ServeHTTP)

	// Stats (admin only)
	api.HandleFunc("/api/v1/stats", s.adminMiddleware(http.HandlerFunc(s.handleStats)).ServeHTTP)

	// Search
	api.HandleFunc("/api/v1/search", s.handleSearch)

	// Threads
	api.HandleFunc("/api/v1/threads", s.handleThreads)
	api.HandleFunc("/api/v1/threads/search", s.handleThreadSearch)
	api.HandleFunc("/api/v1/threads/", s.handleThreadPath)

	// Vacation auto-reply
	api.HandleFunc("/api/v1/vacation", s.handleVacation)

	// Admin routes
	api.HandleFunc("/api/v1/admin/vacations", s.adminMiddleware(http.HandlerFunc(s.handleAdminVacations)).ServeHTTP)

	// Push notifications
	api.HandleFunc("/api/v1/push/vapid-public-key", s.handlePushVAPID)
	api.HandleFunc("/api/v1/push/subscribe", s.handlePushSubscribe)
	api.HandleFunc("/api/v1/push/unsubscribe", s.handlePushUnsubscribe)
	api.HandleFunc("/api/v1/push/subscriptions", s.handlePushSubscriptions)
	api.HandleFunc("/api/v1/push/test", s.handlePushTest)
	api.HandleFunc("/api/v1/admin/push/stats", s.adminMiddleware(http.HandlerFunc(s.handleAdminPushStats)).ServeHTTP)

	// Email filters
	api.HandleFunc("/api/v1/filters", s.handleFilters)
	api.HandleFunc("/api/v1/filters/reorder", s.handleFilterReorder)
	api.HandleFunc("/api/v1/filters/", s.handleFilterPath)

	// Queue admin routes
	api.HandleFunc("/api/v1/admin/queue", s.adminMiddleware(http.HandlerFunc(s.handleQueue)).ServeHTTP)
	api.HandleFunc("/api/v1/admin/queue/", s.adminMiddleware(http.HandlerFunc(s.handleQueueDetail)).ServeHTTP)

	// Wrap API with auth middleware and mount to main mux
	apiHandler := s.rateLimitMiddleware(s.limitBodyMiddleware(s.corsMiddleware(s.authMiddleware(api))))
	mux.Handle("/api/v1/", apiHandler)

	return mux
}

// limitBodyMiddleware restricts request body size to prevent DoS.
func (s *Server) limitBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 4<<20) // 4 MB
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware enforces API rate limiting per IP.
// Exempts health check and authentication endpoints.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for health and auth endpoints
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		// Get client IP
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		if !s.checkAPIRateLimit(ip) {
			s.logger.Warn("API rate limit exceeded",
				slog.String("ip", ip),
				slog.String("path", r.URL.Path),
			)
			s.sendError(w, http.StatusTooManyRequests, "rate limit exceeded, try again later")
			return
		}

		next.ServeHTTP(w, r)
	})
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

// adminMiddleware wraps a handler to require admin role.
// Must be used after authMiddleware so that "isAdmin" is in context.
func (s *Server) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAdmin, _ := r.Context().Value("isAdmin").(bool)
		if !isAdmin {
			s.sendJSON(w, http.StatusForbidden, map[string]string{
				"error": "admin access required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkLoginRateLimit returns true if the IP is allowed to attempt login.
// Allows 5 attempts per 5-minute window per IP; blocks after that.
func (s *Server) checkLoginRateLimit(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.loginAttempts[ip]
	if !exists || now.Sub(attempt.lastSeen) > 5*time.Minute {
		s.loginAttempts[ip] = &loginAttempt{count: 1, lastSeen: now}
		return true
	}

	if attempt.count >= 5 {
		return false
	}
	attempt.count++
	attempt.lastSeen = now
	return true
}

// recordLoginFailure increments the failed login counter for an IP.
func (s *Server) recordLoginFailure(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.loginAttempts[ip]
	if !exists {
		s.loginAttempts[ip] = &loginAttempt{count: 1, lastSeen: now}
		return
	}
	attempt.count++
	attempt.lastSeen = now
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	result := map[string]interface{}{
		"status": "healthy",
	}

	// Check database
	if s.db == nil {
		status = http.StatusServiceUnavailable
		result["status"] = "unhealthy"
		result["database"] = "not initialized"
	} else {
		if _, err := s.db.ListDomains(); err != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["database"] = err.Error()
		} else {
			result["database"] = "ok"
		}
	}

	// Check queue manager
	if s.queueMgr != nil {
		// Check for mock error injection (used in tests)
		if s.queueMgrStatsError != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["queue"] = s.queueMgrStatsError.Error()
		} else if _, err := s.queueMgr.GetStats(); err != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["queue"] = err.Error()
		} else {
			result["queue"] = "ok"
		}
	}

	// Check message store
	if s.msgStore != nil {
		result["storage"] = "ok"
	}

	s.sendJSON(w, status, result)
}

func (s *Server) handleWebmail(w http.ResponseWriter, r *http.Request) {
	// Use injected webmail FS
	webmailFS := s.webmailFS
	if webmailFS == nil {
		webmailFS = NewEmbedFSAdapter(umailserver.WebmailFS)
	}

	// Also use admin FS for fallback (shared /assets/ paths)
	adminFS := s.adminFS
	if adminFS == nil {
		adminFS = NewEmbedFSAdapter(umailserver.AdminFS)
	}

	// Handle SPA routing - serve index.html for non-existent paths
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Try to open the file from webmail FS
	file, err := webmailFS.Open(path)
	if err != nil {
		// Fallback: check admin FS for shared assets (e.g., /assets/...)
		file, err = adminFS.Open(path)
		if err != nil {
			// If file not found, serve index.html for SPA routing
			file, err = webmailFS.Open("index.html")
			if err != nil {
				s.logger.Error("Failed to open index.html", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			path = "index.html"
		}
	}
	defer file.Close()

	// Set content type based on file extension
	if strings.HasSuffix(path, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else if strings.HasSuffix(path, ".js") {
		w.Header().Set("Content-Type", "application/javascript")
	} else if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	} else if strings.HasSuffix(path, ".svg") {
		w.Header().Set("Content-Type", "image/svg+xml")
	}

	// Serve file content
	stat, err := file.Stat()
	if err != nil {
		s.logger.Error("Failed to stat file", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, path, stat.ModTime(), file.(io.ReadSeeker))
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	// Use injected admin FS
	adminFS := s.adminFS
	if adminFS == nil {
		adminFS = NewEmbedFSAdapter(umailserver.AdminFS)
	}

	// Handle SPA routing - serve index.html for non-existent paths
	path := strings.TrimPrefix(r.URL.Path, "/admin/")
	if path == "" {
		path = "index.html"
	}

	// Try to open the file
	file, err := adminFS.Open(path)
	if err != nil {
		// If file not found, serve index.html for SPA routing
		file, err = adminFS.Open("index.html")
		if err != nil {
			s.logger.Error("Failed to open index.html", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		path = "index.html"
	}
	defer file.Close()

	// Set content type based on file extension
	if strings.HasSuffix(path, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else if strings.HasSuffix(path, ".js") {
		w.Header().Set("Content-Type", "application/javascript")
	} else if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	} else if strings.HasSuffix(path, ".svg") {
		w.Header().Set("Content-Type", "image/svg+xml")
	}

	// Serve file content
	stat, err := file.Stat()
	if err != nil {
		s.logger.Error("Failed to stat file", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, path, stat.ModTime(), file.(io.ReadSeeker))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Rate limit login attempts by IP
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if !s.checkLoginRateLimit(ip) {
		s.sendError(w, http.StatusTooManyRequests, "too many login attempts")
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
		s.recordLoginFailure(ip)
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Check password using bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(req.Password)); err != nil {
		s.recordLoginFailure(ip)
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
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")

	// Handle TOTP 2FA sub-paths
	if strings.HasSuffix(suffix, "/totp/setup") {
		email := suffix[:len(suffix)-len("/totp/setup")]
		s.handleTOTPSetup(w, r, email)
		return
	}
	if strings.HasSuffix(suffix, "/totp/disable") {
		email := suffix[:len(suffix)-len("/totp/disable")]
		s.handleTOTPDisable(w, r, email)
		return
	}
	if strings.HasSuffix(suffix, "/totp/verify") {
		email := suffix[:len(suffix)-len("/totp/verify")]
		s.handleTOTPVerify(w, r, email)
		return
	}

	// Regular account detail
	switch r.Method {
	case http.MethodGet:
		s.getAccount(w, r, suffix)
	case http.MethodPut:
		s.updateAccount(w, r, suffix)
	case http.MethodDelete:
		s.deleteAccount(w, r, suffix)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// TOTP 2FA handlers

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate TOTP secret")
		return
	}

	// Store secret but don't enable yet — user must verify first
	account.TOTPSecret = secret
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to save TOTP secret")
		return
	}

	uri := auth.GenerateTOTPUri(secret, email, "uMailServer")

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"secret": secret,
		"uri":    uri,
	})
}

func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	if account.TOTPSecret == "" {
		s.sendError(w, http.StatusBadRequest, "TOTP not set up — call /totp/setup first")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !auth.ValidateTOTP(account.TOTPSecret, req.Code) {
		s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}

	// Code verified — enable TOTP
	account.TOTPEnabled = true
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to enable TOTP")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
	})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	account.TOTPSecret = ""
	account.TOTPEnabled = false
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to disable TOTP")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": false,
	})
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

// SetHealthMonitor sets the health monitor for health endpoints
func (s *Server) SetHealthMonitor(mon HealthMonitor) {
	s.healthMon = mon
}

// SetAPIRateLimit sets the HTTP API rate limit (requests per minute, 0 = disabled)
func (s *Server) SetAPIRateLimit(limit int) {
	s.apiRateMu.Lock()
	defer s.apiRateMu.Unlock()
	s.apiRateLimit = limit
}

// checkAPIRateLimit returns true if the IP is allowed to make API requests.
// Uses sliding window based on HTTPRequestsPerMinute config.
func (s *Server) checkAPIRateLimit(ip string) bool {
	s.apiRateMu.Lock()
	defer s.apiRateMu.Unlock()

	if s.apiRateLimit <= 0 {
		return true // rate limiting disabled
	}

	if s.apiRateAttempts == nil {
		s.apiRateAttempts = make(map[string]*apiRateAttempt)
	}

	now := time.Now()
	attempt, exists := s.apiRateAttempts[ip]

	// Check if window has expired (1 minute window)
	if !exists || now.Sub(attempt.windowStart) > time.Minute {
		s.apiRateAttempts[ip] = &apiRateAttempt{count: 1, windowStart: now}
		return true
	}

	// Check if limit exceeded
	if attempt.count >= s.apiRateLimit {
		return false
	}

	attempt.count++
	return true
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
		domains, listErr := s.db.ListDomains()
		if listErr != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to list accounts")
			return
		}
		for _, d := range domains {
			domainAccounts, accErr := s.db.ListAccountsByDomain(d.Name)
			if accErr != nil {
				s.sendError(w, http.StatusInternalServerError, "failed to list accounts for domain")
				return
			}
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
		APOPHash:     fmt.Sprintf("%x", md5.Sum([]byte(req.Password))),
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
		account.APOPHash = fmt.Sprintf("%x", md5.Sum([]byte(req.Password)))
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

	userStr, ok := user.(string)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalid user context")
		return
	}

	results, err := s.searchSvc.Search(search.MessageSearchOptions{
		User:   userStr,
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
