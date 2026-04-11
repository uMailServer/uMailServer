package api

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
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
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver"
	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/mcp"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/search"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/websocket"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// loginAttempt tracks failed login attempts per IP
type loginAttempt struct {
	count    int
	lastSeen time.Time
}

// apiRateAttempt tracks API requests per IP for rate limiting
type apiRateAttempt struct {
	count       int
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
	mailDB     *storage.Database
	queueMgr   *queue.Manager
	httpServer *http.Server
	healthMon  HealthMonitor

	// Interface abstractions for testability
	vacationMgr  VacationManager
	filterMgr    FilterManager
	pushSvc      PushService
	rateLimitMgr RateLimitManager

	// File system abstraction for embed.FS
	webmailFS FileSystem
	adminFS   FileSystem

	// Mail handler for user email operations
	mailHandler *MailHandler

	// Audit logger for security events
	auditLogger *audit.Logger

	// HTTP router (cached)
	router http.Handler

	// HTTP server lifecycle guard (protects httpServer field)
	serverMu sync.Mutex

	// Login rate limiting
	loginMu       sync.Mutex
	loginAttempts map[string]*loginAttempt

	// API rate limiting (HTTPRequestsPerMinute)
	apiRateMu       sync.Mutex
	apiRateAttempts map[string]*apiRateAttempt
	apiRateLimit    int // requests per minute, 0 = disabled

	// Mock errors for testing (used to test error paths)
	vacationGetError     error
	vacationSetError     error
	vacationDeleteError  error
	filterSaveError      error
	filterGetError       error
	pushSubscribeError   error
	pushUnsubscribeError error
	pushSendError        error
	queueMgrStatsError   error

	// Token blacklist for revoked tokens (supports logout before expiry)
	tokenBlacklist   map[string]time.Time
	tokenBlacklistMu sync.RWMutex

	// JWT secret versioning for rotation support
	jwtSecrets map[string]string // kid -> secret
	currentKid string            // active key ID

	// Draining state for zero-downtime deployment
	draining atomic.Bool
}

// Config holds API server configuration
type Config struct {
	Addr              string
	JWTSecret         string                  // Legacy single secret (used if JWTSecretVersions not set)
	JWTSecretVersions map[string]string       // kid -> secret, for key rotation
	TokenExpiry       time.Duration
	CorsOrigins       []string
	TrustedProxies    []string // IPs that are allowed to set X-Forwarded-For
	AuditLog          AuditLogConfig
	PasswordHasher    string                 // "bcrypt" (default) or "argon2id"
}

// AuditLogConfig holds audit logging configuration
type AuditLogConfig struct {
	Path        string // Path to audit log file, empty = disabled
	MaxSizeMB   int    // Max file size before rotation
	MaxBackups  int    // Number of backup files to keep
	MaxAgeDays  int    // Max age of backup files in days
}

// NewServer creates a new admin API server
func NewServer(database *db.DB, logger *slog.Logger, config Config) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if config.JWTSecret == "" {
		logger.Warn("JWTSecret is empty, generating random secret - tokens will not survive restarts")
		config.JWTSecret = generateSecureJWTSecret()
	}
	if config.TokenExpiry == 0 {
		config.TokenExpiry = 24 * time.Hour
	}

	// Initialize JWT secret versioning
	jwtSecrets := make(map[string]string)
	currentKid := "default"
	if len(config.JWTSecretVersions) > 0 {
		// Use configured versions
		for kid, secret := range config.JWTSecretVersions {
			jwtSecrets[kid] = secret
		}
		// Set currentKid to first key in map if not set
		for kid := range config.JWTSecretVersions {
			currentKid = kid
			break
		}
	} else {
		// Migrate legacy single secret to versioned format
		jwtSecrets[currentKid] = config.JWTSecret
	}

	sseServer := websocket.NewSSEServer(logger)
	if len(config.CorsOrigins) > 0 {
		sseServer.SetCorsOrigin(strings.Join(config.CorsOrigins, ","))
	}

	// Capture jwtSecrets and currentKid for closure
	secrets := jwtSecrets
	kid := currentKid
	sseServer.SetAuthFunc(func(token string) (user string, isAdmin bool, err error) {
		parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			// Try kid-based secret lookup first
			if t.Header["kid"] != nil {
				if kidSecret, ok := secrets[t.Header["kid"].(string)]; ok {
					return []byte(kidSecret), nil
				}
			}
			// Fall back to current kid
			if secret, ok := secrets[kid]; ok {
				return []byte(secret), nil
			}
			// Last resort: try legacy JWTSecret
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

	// Initialize audit logger
	auditLogger, err := audit.NewLogger(
		config.AuditLog.Path,
		config.AuditLog.MaxSizeMB,
		config.AuditLog.MaxBackups,
		config.AuditLog.MaxAgeDays,
	)
	if err != nil {
		logger.Warn("failed to initialize audit logger", "error", err)
	}

	return &Server{
		db:          database,
		logger:      logger,
		config:      config,
		mcpServer:   mcp.NewServer(database),
		sseServer:   sseServer,
		webmailFS:   newEmbedFSSub(umailserver.WebmailFS, "webmail/dist"),
		adminFS:     newEmbedFSSub(umailserver.AdminFS, "web/admin/dist"),
		auditLogger: auditLogger,
		tokenBlacklist: make(map[string]time.Time),
		jwtSecrets: jwtSecrets,
		currentKid:  currentKid,
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

	// Initialize JWT secret versioning
	jwtSecrets := make(map[string]string)
	currentKid := "default"
	if len(config.JWTSecretVersions) > 0 {
		for kid, secret := range config.JWTSecretVersions {
			jwtSecrets[kid] = secret
		}
		for kid := range config.JWTSecretVersions {
			currentKid = kid
			break
		}
	} else {
		jwtSecrets[currentKid] = config.JWTSecret
	}

	sseServer := websocket.NewSSEServer(logger)
	if len(config.CorsOrigins) > 0 {
		sseServer.SetCorsOrigin(strings.Join(config.CorsOrigins, ","))
	}

	// Capture jwtSecrets and currentKid for closure
	secrets := jwtSecrets
	kid := currentKid
	sseServer.SetAuthFunc(func(token string) (user string, isAdmin bool, err error) {
		parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			if t.Header["kid"] != nil {
				if kidSecret, ok := secrets[t.Header["kid"].(string)]; ok {
					return []byte(kidSecret), nil
				}
			}
			if secret, ok := secrets[kid]; ok {
				return []byte(secret), nil
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
		db:          database,
		logger:      logger,
		config:      config,
		mcpServer:   mcp.NewServer(database),
		sseServer:   sseServer,
		vacationMgr: vacationMgr,
		filterMgr:   filterMgr,
		pushSvc:     pushSvc,
		webmailFS:   webmailFS,
		adminFS:     adminFS,
		tokenBlacklist: make(map[string]time.Time),
		jwtSecrets: jwtSecrets,
		currentKid:  currentKid,
	}
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.router == nil {
		s.initRouter()
	}
	s.router.ServeHTTP(w, r)
}

// RevokeToken adds a token to the blacklist (for logout)
func (s *Server) RevokeToken(tokenHash string) {
	s.tokenBlacklistMu.Lock()
	defer s.tokenBlacklistMu.Unlock()
	// Blacklist expires when the token would have expired (1 hour from now for typical tokens)
	s.tokenBlacklist[tokenHash] = time.Now().Add(time.Hour)
}

// IsTokenRevoked checks if a token is in the blacklist
func (s *Server) IsTokenRevoked(tokenHash string) bool {
	s.tokenBlacklistMu.RLock()
	defer s.tokenBlacklistMu.RUnlock()
	if expiry, ok := s.tokenBlacklist[tokenHash]; ok {
		if time.Now().After(expiry) {
			return false // Expired, remove from blacklist
		}
		return true
	}
	return false
}

// CleanupExpiredTokens removes expired entries from the blacklist
func (s *Server) CleanupExpiredTokens() {
	s.tokenBlacklistMu.Lock()
	defer s.tokenBlacklistMu.Unlock()
	now := time.Now()
	for token, expiry := range s.tokenBlacklist {
		if now.After(expiry) {
			delete(s.tokenBlacklist, token)
		}
	}
}

// initRouter sets up the HTTP routes (called once on first request)
func (s *Server) initRouter() {
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

	// Kubernetes readiness probe - returns 200 if ready to accept traffic
	mux.HandleFunc("/health/ready", s.handleReady)

	// Metrics endpoint (admin only)
	mux.HandleFunc("/metrics", s.authMiddleware(s.adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.Get().HTTPHandler(w, r)
	}))).ServeHTTP)

	// SSE endpoint for real-time updates
	mux.HandleFunc("/api/v1/events", s.sseServer.Handler())

	// MCP endpoint (protected by auth)
	mux.Handle("/mcp", s.authMiddleware(http.HandlerFunc(s.mcpServer.HandleHTTP)))

	// Authentication
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/refresh", s.handleRefresh)
	mux.Handle("/api/v1/auth/logout", s.authMiddleware(http.HandlerFunc(s.handleLogout)))

	// Protected routes
	api := http.NewServeMux()

	// Domains (admin only)
	api.HandleFunc("/api/v1/domains", s.adminMiddleware(http.HandlerFunc(s.handleDomains)).ServeHTTP)
	api.HandleFunc("/api/v1/domains/", s.adminMiddleware(http.HandlerFunc(s.handleDomainDetail)).ServeHTTP)

	// Accounts (admin only)
	api.HandleFunc("/api/v1/accounts", s.adminMiddleware(http.HandlerFunc(s.handleAccounts)).ServeHTTP)
	api.HandleFunc("/api/v1/accounts/", s.adminMiddleware(http.HandlerFunc(s.handleAccountDetail)).ServeHTTP)

	// Aliases (admin only)
	api.HandleFunc("/api/v1/aliases", s.adminMiddleware(http.HandlerFunc(s.handleAliases)).ServeHTTP)
	api.HandleFunc("/api/v1/aliases/", s.adminMiddleware(http.HandlerFunc(s.handleAliasDetail)).ServeHTTP)

	// Queue (admin only)
	api.HandleFunc("/api/v1/queue", s.adminMiddleware(http.HandlerFunc(s.handleQueue)).ServeHTTP)
	api.HandleFunc("/api/v1/queue/", s.adminMiddleware(http.HandlerFunc(s.handleQueueDetail)).ServeHTTP)

	// Metrics (admin only)
	api.HandleFunc("/api/v1/metrics", s.adminMiddleware(http.HandlerFunc(s.handleMetrics)).ServeHTTP)

	// Stats (admin only)
	api.HandleFunc("/api/v1/stats", s.adminMiddleware(http.HandlerFunc(s.handleStats)).ServeHTTP)

	// Rate limits (admin only)
	api.HandleFunc("/api/v1/admin/ratelimits/config", s.adminMiddleware(http.HandlerFunc(s.handleRateLimitConfig)).ServeHTTP)
	api.HandleFunc("/api/v1/admin/ratelimits/ip/", s.adminMiddleware(http.HandlerFunc(s.handleRateLimitIPStats)).ServeHTTP)
	api.HandleFunc("/api/v1/admin/ratelimits/user/", s.adminMiddleware(http.HandlerFunc(s.handleRateLimitUserStats)).ServeHTTP)

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

	// JWT secret rotation
	api.HandleFunc("/api/v1/admin/jwt/rotate", s.adminMiddleware(http.HandlerFunc(s.handleJWTRotate)).ServeHTTP)
	api.HandleFunc("/api/v1/admin/jwt/status", s.adminMiddleware(http.HandlerFunc(s.handleJWTStatus)).ServeHTTP)

	// Email filters
	api.HandleFunc("/api/v1/filters", s.handleFilters)
	api.HandleFunc("/api/v1/filters/reorder", s.handleFilterReorder)
	api.HandleFunc("/api/v1/filters/", s.handleFilterPath)

	// Queue admin routes
	api.HandleFunc("/api/v1/admin/queue", s.adminMiddleware(http.HandlerFunc(s.handleQueue)).ServeHTTP)
	api.HandleFunc("/api/v1/admin/queue/", s.adminMiddleware(http.HandlerFunc(s.handleQueueDetail)).ServeHTTP)

	// Mail (user-facing, uses same auth as API)
	// Ensure mailHandler is initialized
	if s.mailHandler == nil {
		s.mailHandler = NewMailHandler()
		s.mailHandler.SetStorage(s.msgStore, s.mailDB)
	}

	api.HandleFunc("/api/v1/mail/inbox", s.mailHandler.handleMailList)
	api.HandleFunc("/api/v1/mail/sent", http.HandlerFunc(s.mailHandler.handleMailList).ServeHTTP)
	api.HandleFunc("/api/v1/mail/drafts", http.HandlerFunc(s.mailHandler.handleMailList).ServeHTTP)
	api.HandleFunc("/api/v1/mail/trash", http.HandlerFunc(s.mailHandler.handleMailList).ServeHTTP)
	api.HandleFunc("/api/v1/mail/spam", http.HandlerFunc(s.mailHandler.handleMailList).ServeHTTP)
	api.HandleFunc("/api/v1/mail/send", http.HandlerFunc(s.mailHandler.handleMailSend).ServeHTTP)
	api.HandleFunc("/api/v1/mail/delete", http.HandlerFunc(s.mailHandler.handleMailDelete).ServeHTTP)

	// Wrap API with auth middleware and mount to main mux
	apiHandler := s.rateLimitMiddleware(s.limitBodyMiddleware(s.corsMiddleware(s.authMiddleware(api))))
	mux.Handle("/api/v1/", apiHandler)

	s.router = mux
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

// SetRateLimitManager injects the rate limit manager into the API server
func (s *Server) SetRateLimitManager(mgr RateLimitManager) {
	s.rateLimitMgr = mgr
}

// Start starts the API server
func (s *Server) Start(addr string) error {
	s.config.Addr = addr

	s.serverMu.Lock()
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	s.serverMu.Unlock()

	s.logger.Info("Admin API server starting", "addr", addr)
	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the API server
func (s *Server) Stop() error {
	s.serverMu.Lock()
	httpServer := s.httpServer
	s.serverMu.Unlock()
	if httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	}
	return nil
}

// StartDrain initiates graceful draining mode.
// After this is called, /health/ready returns 503 and new requests are rejected.
// Returns a function that waits for all active requests to complete.
// Call this before Stop() for zero-downtime deployments.
func (s *Server) StartDrain() func() {
	s.draining.Store(true)
	return func() {
		// Wait for in-flight requests to complete
		// This is a simple implementation - for production, track active connections
		// The actual connection tracking would use middleware that increments/decrements a counter
	}
}

// DrainWait waits for all active requests to complete.
// timeout is the maximum time to wait before forcing close.
func (s *Server) DrainWait(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for s.activeRequests() > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
}

// activeRequests returns the number of currently active requests
// This is a placeholder - real implementation would use atomic counter
func (s *Server) activeRequests() int {
	return 0
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
			// Try kid-based secret lookup first
			if token.Header["kid"] != nil {
				if kidSecret, ok := s.jwtSecrets[token.Header["kid"].(string)]; ok {
					return []byte(kidSecret), nil
				}
			}
			// Fall back to current kid
			if secret, ok := s.jwtSecrets[s.currentKid]; ok {
				return []byte(secret), nil
			}
			// Last resort: try legacy JWTSecret
			return []byte(s.config.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			s.sendError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		// Check if token is revoked (logout)
		tokenHash := fmt.Sprintf("%x", md5.Sum([]byte(tokenStr)))
		if s.IsTokenRevoked(tokenHash) {
			s.sendError(w, http.StatusUnauthorized, "token has been revoked")
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

// handleHealth returns server health status
//
//	@Summary Get server health
//	@Description Returns the current health status of the server including database, queue, and storage checks
//	@Tags Health
//	@Produce json
//	@Success 200 {object} map[string]interface{} "Server is healthy"
//	@Success 503 {object} map[string]interface{} "Server is unhealthy"
//	@Router /health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	result := map[string]interface{}{
		"status": "healthy",
	}

	// Check if server is draining (graceful shutdown in progress)
	if s.draining.Load() {
		result["draining"] = true
		result["status"] = "draining"
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
			result["database"] = "unavailable"
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
			result["queue"] = "unavailable"
		} else if _, err := s.queueMgr.GetStats(); err != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["queue"] = "unavailable"
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

// handleReady is the Kubernetes readiness probe endpoint
// Returns 200 if the server is ready to accept traffic, 503 if draining
//
//	@Summary Get server readiness for zero-downtime deployment
//	@Description Returns whether the server is ready to accept traffic. Used for kubernetes readiness probes.
//	@Tags Health
//	@Produce json
//	@Success 200 {object} map[string]interface{} "Server is ready"
//	@Success 503 {object} map[string]interface{} "Server is not ready"
//	@Router /health/ready [get]
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// If draining, report not ready
	if s.draining.Load() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not ready",
			"reason": "server is draining for graceful shutdown",
		})
		return
	}

	// Check database connectivity
	if s.db != nil {
		if _, err := s.db.ListDomains(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "not ready",
				"reason": "database unavailable",
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
	})
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

// handleLogin authenticates a user and returns a JWT token
//
//	@Summary User login
//	@Description Authenticates a user with email and password, returns JWT token
//	@Tags Auth
//	@Accept json
//	@Produce json
//	@Param email body string true "User email"
//	@Param password body string true "User password"
//	@Param totp_code body string false "TOTP code if MFA is enabled"
//	@Success 200 {object} map[string]interface{} "Login successful"
//	@Failure 400 {object} map[string]interface{} "Invalid request"
//	@Failure 401 {object} map[string]interface{} "Invalid credentials"
//	@Failure 429 {object} map[string]interface{} "Too many login attempts"
//	@Router /api/v1/login [post]
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
		s.auditLogger.LogLoginFailure(req.Email, ip, "account_not_found")
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Check password using configured hasher
	matches, needsRehash := s.verifyPassword(req.Password, account.PasswordHash)
	if !matches {
		s.recordLoginFailure(ip)
		s.auditLogger.LogLoginFailure(req.Email, ip, "invalid_password")
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Rehash password if using older algorithm and argon2id is preferred
	if needsRehash {
		newHash, err := s.hashPassword(req.Password)
		if err == nil {
			account.PasswordHash = newHash
			s.db.UpdateAccount(account)
		}
	}

	// Check TOTP if enabled
	if account.TOTPEnabled {
		if req.TOTPCode == "" {
			s.auditLogger.LogLoginFailure(req.Email, ip, "totp_required")
			s.sendError(w, http.StatusUnauthorized, "TOTP code required")
			return
		}
		if !auth.ValidateTOTP(account.TOTPSecret, req.TOTPCode) {
			s.auditLogger.LogLoginFailure(req.Email, ip, "invalid_totp")
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
	// Set key ID header for secret rotation support
	token.Header["kid"] = s.currentKid

	tokenString, err := token.SignedString([]byte(s.jwtSecrets[s.currentKid]))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})

	// Initialize demo emails for the user on first login
	InitDemoEmails(account.Email)

	// Audit successful login
	s.auditLogger.LogLoginSuccess(account.Email, ip)
}

// handleLogout revokes the current token (adds it to blacklist)
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		s.sendError(w, http.StatusUnauthorized, "invalid authorization header")
		return
	}

	tokenStr := parts[1]

	// Revoke the token by adding it to blacklist
	tokenHash := fmt.Sprintf("%x", md5.Sum([]byte(tokenStr)))
	s.RevokeToken(tokenHash)

	// Audit logout
	user := r.Context().Value("user")
	if user != nil {
		s.auditLogger.LogLogout(user.(string), audit.ExtractIP(r))
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"message": "logged out successfully",
	})
}

// handleRefresh refreshes the JWT token
//
//	@Summary Refresh token
//	@Description Returns a new JWT token with extended expiry
//	@Tags Auth
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {object} map[string]interface{} "Token refreshed"
//	@Failure 401 {object} map[string]interface{} "Unauthorized"
//	@Router /api/v1/refresh [post]
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
	token.Header["kid"] = s.currentKid

	tokenString, err := token.SignedString([]byte(s.jwtSecrets[s.currentKid]))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})
}

// handleDomains lists and creates domains
//
//	@Summary List domains
//	@Description Returns a list of all domains
//	@Tags Domains
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {array} map[string]interface{} "List of domains"
//	@Router /api/v1/domains [get]
//	@Summary Create domain
//	@Description Creates a new domain
//	@Tags Domains
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Success 201 {object} map[string]interface{} "Domain created"
//	@Router /api/v1/domains [post]
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

// handleAccounts lists and creates accounts
//
//	@Summary List accounts
//	@Description Returns a list of all accounts for a domain
//	@Tags Accounts
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {array} map[string]interface{} "List of accounts"
//	@Router /api/v1/accounts [get]
//	@Summary Create account
//	@Description Creates a new email account
//	@Tags Accounts
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Success 201 {object} map[string]interface{} "Account created"
//	@Router /api/v1/accounts [post]
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

// handleAliases lists and creates aliases
//
//	@Summary List aliases
//	@Description Returns a list of all email aliases
//	@Tags Aliases
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {array} map[string]interface{} "List of aliases"
//	@Router /api/v1/aliases [get]
//	@Summary Create alias
//	@Description Creates a new email alias
//	@Tags Aliases
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Success 201 {object} map[string]interface{} "Alias created"
//	@Router /api/v1/aliases [post]
func (s *Server) handleAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAliases(w, r)
	case http.MethodPost:
		s.createAlias(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAliasDetail(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/aliases/")

	switch r.Method {
	case http.MethodGet:
		s.getAlias(w, r, suffix)
	case http.MethodPut:
		s.updateAlias(w, r, suffix)
	case http.MethodDelete:
		s.deleteAlias(w, r, suffix)
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

	// Authorization: user can setup their own TOTP; admin can setup for non-admin users
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")
	isAdmin, _ := authIsAdmin.(bool)
	authenticatedUser, _ := authUser.(string)

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Allow if: user is setting up their own TOTP, OR admin is setting up for non-admin users
	if authenticatedUser != email && (!isAdmin || account.IsAdmin) {
		s.sendError(w, http.StatusForbidden, "forbidden: cannot setup TOTP for this user")
		return
	}
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

	uri := auth.GenerateTOTPUri(secret, email, "uMailServer", auth.TOTPAlgorithmSHA1)

	// Audit TOTP setup initiated
	s.auditLogger.LogTOTPEnable(authenticatedUser, email, audit.ExtractIP(r))

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

	// Authorization: user can verify their own TOTP; admin can verify for non-admin users
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")
	isAdmin, _ := authIsAdmin.(bool)
	authenticatedUser, _ := authUser.(string)

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Allow if: user is verifying their own TOTP, OR admin is verifying for non-admin users
	if authenticatedUser != email && (!isAdmin || account.IsAdmin) {
		s.sendError(w, http.StatusForbidden, "forbidden: cannot verify TOTP for this user")
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

	// Audit TOTP verification complete (2FA now enabled)
	s.auditLogger.LogTOTPEnable(authenticatedUser, email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
	})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Authorization: user can disable their own TOTP; admin can disable non-admin users
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")
	isAdmin, _ := authIsAdmin.(bool)
	authenticatedUser, _ := authUser.(string)

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Allow if: user is disabling their own TOTP, OR admin is disabling a non-admin user's TOTP
	if authenticatedUser != email && (!isAdmin || account.IsAdmin) {
		s.sendError(w, http.StatusForbidden, "forbidden: cannot disable TOTP for this user")
		return
	}

	account.TOTPSecret = ""
	account.TOTPEnabled = false
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to disable TOTP")
		return
	}

	// Audit TOTP disable
	s.auditLogger.LogTOTPDisable(authenticatedUser, email, audit.ExtractIP(r))

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

// SetMailDB sets the mail database for email operations
func (s *Server) SetMailDB(db *storage.Database) {
	s.mailDB = db
	s.initMailHandler()
}

// SetMsgStore sets the message store for email operations
func (s *Server) SetMsgStore(msgStore *storage.MessageStore) {
	s.msgStore = msgStore
	s.initMailHandler()
}

// initMailHandler initializes the mail handler with storage backends
func (s *Server) initMailHandler() {
	if s.mailHandler == nil && (s.msgStore != nil || s.mailDB != nil) {
		s.mailHandler = NewMailHandler()
		s.mailHandler.SetStorage(s.msgStore, s.mailDB)
	} else if s.mailHandler != nil {
		s.mailHandler.SetStorage(s.msgStore, s.mailDB)
	}
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

	// Validate domain name format
	if err := validateDomainName(req.Name); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate max accounts if provided
	if req.MaxAccounts < 0 {
		s.sendError(w, http.StatusBadRequest, "max_accounts must be non-negative")
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

	// Validate email format
	if err := validateEmailFormat(req.Email); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate password strength
	if err := validatePassword(req.Password); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, domain := parseEmail(req.Email)

	// Hash password with configured hasher
	hashedPassword, err := s.hashPassword(req.Password)
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

	// Audit account creation
	actor := "system"
	if authUser := r.Context().Value("user"); authUser != nil {
		actor = authUser.(string)
	}
	s.auditLogger.LogAccountCreate(actor, req.Email, audit.ExtractIP(r))

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

	// Authorization check: prevent privilege escalation
	// Only enforce when context is properly set (i.e., through HTTP middleware)
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")

	// Parse request body first to check IsAdmin modification
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

	// Only enforce authorization when context is properly set (authenticated request)
	if authUser != nil && authIsAdmin != nil {
		isAdmin, _ := authIsAdmin.(bool)
		authenticatedUser, _ := authUser.(string)

		// Prevent self-modification of IsAdmin flag
		if authenticatedUser == email && req.IsAdmin != account.IsAdmin {
			s.sendError(w, http.StatusForbidden, "cannot modify your own admin status")
			return
		}

		// Prevent non-admin from setting IsAdmin to true
		if !isAdmin && req.IsAdmin {
			s.sendError(w, http.StatusForbidden, "only admins can grant admin privileges")
			return
		}
	}

	if req.Password != "" {
		// Hash new password with configured hasher
		hashedPassword, err := s.hashPassword(req.Password)
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

	// Audit account deletion
	actor := "system"
	if authUser := r.Context().Value("user"); authUser != nil {
		actor = authUser.(string)
	}
	s.auditLogger.LogAccountDelete(actor, email, audit.ExtractIP(r))

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

// Alias handlers

func (s *Server) listAliases(w http.ResponseWriter, r *http.Request) {
	aliases, err := s.db.ListAliases()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list aliases")
		return
	}

	var result []map[string]interface{}
	for _, a := range aliases {
		result = append(result, aliasToJSON(a))
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) createAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias    string `json:"alias"`    // alias@domain
		Target   string `json:"target"`  // user@domain
		IsActive bool   `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate alias format (must be alias@domain)
	if req.Alias == "" {
		s.sendError(w, http.StatusBadRequest, "alias address required")
		return
	}
	aliasUser, aliasDomain := parseEmail(req.Alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address format")
		return
	}

	// Validate target format (must be user@domain)
	if req.Target == "" {
		s.sendError(w, http.StatusBadRequest, "target address required")
		return
	}
	targetUser, targetDomain := parseEmail(req.Target)
	if targetUser == "" || targetDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid target address format")
		return
	}

	// Verify domain exists
	if _, err := s.db.GetDomain(aliasDomain); err != nil {
		s.sendError(w, http.StatusBadRequest, "domain not found")
		return
	}

	// Verify target account exists
	if _, err := s.db.GetAccount(targetDomain, targetUser); err != nil {
		s.sendError(w, http.StatusBadRequest, "target account not found")
		return
	}

	alias := &db.AliasData{
		Alias:    aliasUser,
		Domain:   aliasDomain,
		Target:   req.Target,
		IsActive: req.IsActive,
	}

	if err := s.db.CreateAlias(alias); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to create alias")
		return
	}

	s.sendJSON(w, http.StatusCreated, aliasToJSON(alias))
}

func (s *Server) getAlias(w http.ResponseWriter, r *http.Request, alias string) {
	aliasUser, aliasDomain := parseEmail(alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address")
		return
	}

	data, err := s.db.GetAlias(aliasDomain, aliasUser)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "alias not found")
		return
	}

	s.sendJSON(w, http.StatusOK, aliasToJSON(data))
}

func (s *Server) updateAlias(w http.ResponseWriter, r *http.Request, alias string) {
	aliasUser, aliasDomain := parseEmail(alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address")
		return
	}

	data, err := s.db.GetAlias(aliasDomain, aliasUser)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "alias not found")
		return
	}

	var req struct {
		Target   string `json:"target"`
		IsActive *bool  `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Target != "" {
		targetUser, targetDomain := parseEmail(req.Target)
		if targetUser == "" || targetDomain == "" {
			s.sendError(w, http.StatusBadRequest, "invalid target address format")
			return
		}
		data.Target = req.Target
	}

	if req.IsActive != nil {
		data.IsActive = *req.IsActive
	}

	if err := s.db.UpdateAlias(data); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update alias")
		return
	}

	s.sendJSON(w, http.StatusOK, aliasToJSON(data))
}

func (s *Server) deleteAlias(w http.ResponseWriter, r *http.Request, alias string) {
	aliasUser, aliasDomain := parseEmail(alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address")
		return
	}

	if err := s.db.DeleteAlias(aliasDomain, aliasUser); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to delete alias")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func aliasToJSON(a *db.AliasData) map[string]interface{} {
	return map[string]interface{}{
		"alias":      a.Alias + "@" + a.Domain,
		"target":     a.Target,
		"domain":     a.Domain,
		"is_active":  a.IsActive,
		"created_at": a.CreatedAt,
	}
}

func parseEmail(email string) (user, domain string) {
	at := strings.LastIndex(email, "@")
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// validateDomainName validates domain name format and checks for path traversal
func validateDomainName(name string) error {
	if name == "" {
		return fmt.Errorf("domain name cannot be empty")
	}
	// Check for path traversal sequences and invalid characters
	if strings.Contains(name, "..") {
		return fmt.Errorf("domain name contains invalid sequence")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("domain name contains invalid characters")
	}
	// Check length
	if len(name) > 253 {
		return fmt.Errorf("domain name exceeds maximum length")
	}
	// Basic format check - should have at least one dot for multi-level domains
	// Single-label domains (like "localhost") are allowed but not ideal
	return nil
}

// validateEmailFormat validates email address format
func validateEmailFormat(email string) error {
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}
	// Check for path traversal sequences and invalid characters
	if strings.Contains(email, "..") {
		return fmt.Errorf("email contains invalid sequence")
	}
	if strings.ContainsAny(email, "/\\") {
		return fmt.Errorf("email contains invalid characters")
	}
	// Must have exactly one @
	at := strings.Count(email, "@")
	if at != 1 {
		return fmt.Errorf("email must contain exactly one @ character")
	}
	user, domain := parseEmail(email)
	if user == "" || domain == "" {
		return fmt.Errorf("email format is invalid")
	}
	if len(user) > 64 {
		return fmt.Errorf("email local part exceeds maximum length")
	}
	if len(domain) > 253 {
		return fmt.Errorf("email domain exceeds maximum length")
	}
	return nil
}

// validatePassword checks password strength
func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password exceeds maximum length")
	}
	return nil
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

	// Validate query length
	if len(query) > 500 {
		s.sendError(w, http.StatusBadRequest, "query too long (max 500 characters)")
		return
	}

	folder := r.URL.Query().Get("folder")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			if l > 100 {
				l = 100 // Cap at 100 to prevent resource exhaustion
			}
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

// handleJWTRotate handles POST /api/v1/admin/jwt/rotate to rotate JWT secret
// It generates a new key ID and secret, keeping old secrets for backward compatibility
func (s *Server) handleJWTRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Generate new key ID and secret
	newKid := fmt.Sprintf("k%d", time.Now().UnixNano())
	newSecret := generateSecureJWTSecret()

	// Add new secret to versions map (keeping old ones for backward compatibility)
	s.jwtSecrets[newKid] = newSecret
	s.currentKid = newKid

	s.logger.Info("JWT secret rotated", "newKid", newKid)

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "rotated",
		"newKid":     newKid,
		"message":    "JWT secret rotated successfully. Old tokens remain valid until they expire.",
		"activeKids": len(s.jwtSecrets),
	})
}

// handleJWTStatus handles GET /api/v1/admin/jwt/status to get JWT secret status
func (s *Server) handleJWTStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return status (not the actual secrets for security)
	activeKids := make([]string, 0, len(s.jwtSecrets))
	for kid := range s.jwtSecrets {
		activeKids = append(activeKids, kid)
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"currentKid": s.currentKid,
		"activeKeys": len(s.jwtSecrets),
		"activeKids": activeKids,
	})
}

// Argon2id password hashing parameters (OWASP recommended)
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 4
	argon2KeyLen  = 32
)

// hashPasswordArgon2id hashes a password using Argon2id
func hashPasswordArgon2id(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	// Format: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		hex.EncodeToString(salt), hex.EncodeToString(hash))
	return encoded, nil
}

// verifyPassword verifies a password against an Argon2id hash
func verifyPasswordArgon2id(password, encodedHash string) bool {
	// Parse the hash format
	// $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	memoryStr := parts[3] // m=65536,t=1,p=4
	saltHex := parts[4]
	hashHex := parts[5]

	// Parse memory, time, threads from memoryStr
	var memory, time, threads int
	if _, err := fmt.Sscanf(memoryStr, "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}

	// Decode salt and stored hash
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	storedHash, err := hex.DecodeString(hashHex)
	if err != nil {
		return false
	}

	// Compute hash with same parameters
	computedHash := argon2.IDKey([]byte(password), salt, uint32(time), uint32(memory), uint8(threads), uint32(len(storedHash)))

	// Constant-time comparison
	if len(computedHash) != len(storedHash) {
		return false
	}
	var result byte
	for i := range computedHash {
		result |= computedHash[i] ^ storedHash[i]
	}
	return result == 0
}

// hashPassword hashes a password using the configured hasher
func (s *Server) hashPassword(password string) (string, error) {
	if s.config.PasswordHasher == "argon2id" {
		return hashPasswordArgon2id(password)
	}
	// Default to bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword verifies a password against a stored hash
// Returns (matches, needsRehash) where needsRehash is true if the hash uses an older algorithm
func (s *Server) verifyPassword(password, encodedHash string) (bool, bool) {
	// Try bcrypt first (legacy)
	if strings.HasPrefix(encodedHash, "$2") {
		err := bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password))
		if err == nil {
			// If using bcrypt but argon2id is preferred, needs rehash
			return true, s.config.PasswordHasher == "argon2id"
		}
		return false, false
	}
	// Try argon2id
	if strings.HasPrefix(encodedHash, "$argon2id$") {
		if verifyPasswordArgon2id(password, encodedHash) {
			// Already using argon2id, no rehash needed
			return true, false
		}
		return false, false
	}
	// Unknown format
	return false, false
}

// generateSecureJWTSecret generates a cryptographically secure 32-byte hex token for JWT signing
func generateSecureJWTSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
