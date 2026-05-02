package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver"
	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/mcp"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/search"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/tracing"
	"github.com/umailserver/umailserver/internal/websocket"
)

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

	// Tracing provider for OpenTelemetry
	tracingProvider *tracing.Provider

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

	// Account-based login rate limiting
	accountLoginMu       sync.Mutex
	accountLoginAttempts map[string]*loginAttempt

	// TOTP attempt limiting
	totpMu       sync.Mutex
	totpAttempts map[string]*totpAttempt

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

	// Background task management
	stopCh   chan struct{}
	stopOnce sync.Once
}

// Config holds API server configuration
type Config struct {
	Addr              string
	JWTSecret         string            // Legacy single secret (used if JWTSecretVersions not set)
	JWTSecretVersions map[string]string // kid -> secret, for key rotation
	DisableLegacyJWT  bool              // When true, disables fallback to legacy JWTSecret after kid rotation
	TokenExpiry       time.Duration
	CorsOrigins       []string
	TrustedProxies    []string // IPs that are allowed to set X-Forwarded-For
	TOTPKey           string   // Separate encryption key for TOTP secrets (falls back to JWTSecret if empty)
	AuditLog          AuditLogConfig
	PasswordHasher    string // "bcrypt" (default) or "argon2id"
}

// AuditLogConfig holds audit logging configuration
type AuditLogConfig struct {
	Path       string // Path to audit log file, empty = disabled
	MaxSizeMB  int    // Max file size before rotation
	MaxBackups int    // Number of backup files to keep
	MaxAgeDays int    // Max age of backup files in days
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
			if kid, ok := t.Header["kid"].(string); ok && kid != "" {
				if kidSecret, ok := secrets[kid]; ok {
					return []byte(kidSecret), nil
				}
			}
			// Fall back to current kid
			if secret, ok := secrets[kid]; ok {
				return []byte(secret), nil
			}
			// Last resort: try legacy JWTSecret only if not disabled
			if !config.DisableLegacyJWT {
				return []byte(config.JWTSecret), nil
			}
			return nil, fmt.Errorf("unknown signing key")
		})
		if err != nil || !parsed.Valid {
			return "", false, fmt.Errorf("invalid token")
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			return "", false, fmt.Errorf("invalid claims")
		}
		user, _ = claims["sub"].(string)
		isAdmin, _ = claims["admin"].(bool)
		return user, isAdmin, nil
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
		db:             database,
		logger:         logger,
		config:         config,
		mcpServer:      mcp.NewServer(database),
		sseServer:      sseServer,
		webmailFS:      newEmbedFSSub(umailserver.WebmailFS, "webmail/dist"),
		adminFS:        newEmbedFSSub(umailserver.AdminFS, "web/admin/dist"),
		auditLogger:    auditLogger,
		tokenBlacklist: make(map[string]time.Time),
		jwtSecrets:     jwtSecrets,
		currentKid:     currentKid,
		stopCh:         make(chan struct{}),
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
		config.JWTSecret = generateSecureJWTSecret()
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
			if !config.DisableLegacyJWT {
				return []byte(config.JWTSecret), nil
			}
			return nil, fmt.Errorf("unknown signing key")
		})
		if err != nil || !parsed.Valid {
			return "", false, fmt.Errorf("invalid token")
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			return "", false, fmt.Errorf("invalid claims")
		}
		user, _ = claims["sub"].(string)
		isAdmin, _ = claims["admin"].(bool)
		return user, isAdmin, nil
	})

	// Use provided FS or default to embedded
	if webmailFS == nil {
		webmailFS = NewEmbedFSAdapter(umailserver.WebmailFS)
	}
	if adminFS == nil {
		adminFS = NewEmbedFSAdapter(umailserver.AdminFS)
	}

	// Initialize audit logger so SMTP/IMAP/POP3 hooks can route protocol-level
	// auth events into the same sink as HTTP/admin events.
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
		db:             database,
		logger:         logger,
		config:         config,
		mcpServer:      mcp.NewServer(database),
		sseServer:      sseServer,
		vacationMgr:    vacationMgr,
		filterMgr:      filterMgr,
		pushSvc:        pushSvc,
		webmailFS:      webmailFS,
		adminFS:        adminFS,
		auditLogger:    auditLogger,
		tokenBlacklist: make(map[string]time.Time),
		jwtSecrets:     jwtSecrets,
		currentKid:     currentKid,
		stopCh:         make(chan struct{}),
	}
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("HTTP handler panic", "panic", recovered, "path", r.URL.Path)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}()
	if s.router == nil {
		s.initRouter()
	}
	s.traceRequest(s.router).ServeHTTP(w, r)
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

	// SSE endpoint for real-time updates (requires auth)
	mux.Handle("/api/v1/events", s.authMiddleware(s.sseServer.Handler()))

	// MCP endpoint (protected by auth)
	mux.Handle("/mcp", s.authMiddleware(http.HandlerFunc(s.mcpServer.HandleHTTP)))

	// Authentication
	mux.Handle("/api/v1/auth/login", s.limitBodyMiddleware(http.HandlerFunc(s.handleLogin)))
	mux.Handle("/api/v1/auth/logout", s.rateLimitMiddleware(s.authMiddleware(http.HandlerFunc(s.handleLogout))))

	// Protected routes
	api := http.NewServeMux()

	// Refresh token (requires auth)
	api.HandleFunc("/api/v1/auth/refresh", s.handleRefresh)

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
	apiHandler := s.rateLimitMiddleware(s.limitBodyMiddleware(s.securityHeadersMiddleware(s.csrfMiddleware(s.corsMiddleware(s.authMiddleware(api))))))
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
		// Skip rate limiting for health checks (probes must not be throttled)
		if r.URL.Path == "/health" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}

		// Get client IP (respects X-Forwarded-For from trusted proxies)
		ip := getClientIP(r, s.config.TrustedProxies)

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

// SetTracingProvider sets the OpenTelemetry tracing provider
func (s *Server) SetTracingProvider(provider *tracing.Provider) {
	s.tracingProvider = provider
}

// AuditLogger exposes the underlying audit logger so other subsystems
// (SMTP/IMAP/POP3) can record protocol-level auth events into the same sink.
func (s *Server) AuditLogger() *audit.Logger {
	return s.auditLogger
}

// Start starts the API server
func (s *Server) Start(addr string) error {
	s.config.Addr = addr

	s.serverMu.Lock()
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.serverMu.Unlock()

	// Start background token blacklist cleanup
	go s.tokenBlacklistCleanup()

	s.logger.Info("Admin API server starting", "addr", addr)
	return s.httpServer.ListenAndServe()
}

// tokenBlacklistCleanup periodically removes expired entries from the token blacklist
func (s *Server) tokenBlacklistCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.CleanupExpiredTokens()
		}
	}
}

// Stop gracefully stops the API server
func (s *Server) Stop() error {
	// Signal background tasks to stop (sync.Once ensures only one call)
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})

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

// decodeJSON decodes JSON from request body with DisallowUnknownFields
// to reject requests with unknown fields
func decodeJSON(r *http.Request, v interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

// Middleware

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := ""
		for _, o := range s.config.CorsOrigins {
			if o == origin {
				allowed = o
				break
			}
		}
		if allowed != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
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
		if r.URL.Path == "/health" || r.URL.Path == "/api/v1/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from HttpOnly cookie first (preferred for web clients)
		var tokenStr string
		if cookie, err := r.Cookie("jwt"); err == nil && cookie.Value != "" {
			tokenStr = cookie.Value
		} else {
			// Fall back to Authorization header (for API clients)
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				s.sendError(w, http.StatusUnauthorized, "missing authorization")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				s.sendError(w, http.StatusUnauthorized, "invalid authorization header format")
				return
			}
			tokenStr = parts[1]
		}

		if tokenStr == "" {
			s.sendError(w, http.StatusUnauthorized, "missing token")
			return
		}

		// Validate token
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			// Try kid-based secret lookup first
			if kid, ok := token.Header["kid"].(string); ok && kid != "" {
				if kidSecret, ok := s.jwtSecrets[kid]; ok {
					return []byte(kidSecret), nil
				}
			}
			// Fall back to current kid
			if secret, ok := s.jwtSecrets[s.currentKid]; ok {
				return []byte(secret), nil
			}
			// Last resort: try legacy JWTSecret only if not disabled
			if !s.config.DisableLegacyJWT {
				return []byte(s.config.JWTSecret), nil
			}
			return nil, fmt.Errorf("unknown signing key")
		}, jwt.WithValidMethods([]string{"HS256"}))

		if err != nil || !token.Valid {
			s.sendError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		// Check if token is revoked (logout)
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenStr)))
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
		isAdmin, ok := r.Context().Value("isAdmin").(bool)
		if !ok || !isAdmin {
			s.sendJSON(w, http.StatusForbidden, map[string]string{
				"error": "admin access required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Handlers

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
		// Close the webmail file first if it was opened
		if file != nil {
			_ = file.Close()
		}
		file, err = adminFS.Open(path)
		if err != nil {
			// If file not found, serve index.html for SPA routing
			// Close the admin file first if it was opened
			if file != nil {
				_ = file.Close()
			}
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
		// Close the first file if it was opened
		if file != nil {
			_ = file.Close()
		}
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

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get actual metrics from metrics collector
	stats := metrics.Get().GetStats()

	// Add queue stats if queue manager is available
	if s.queueMgr != nil {
		if queueStats, err := s.queueMgr.GetStats(); err == nil {
			stats["queue"] = map[string]int{
				"pending":   queueStats.Pending,
				"sending":   queueStats.Sending,
				"failed":    queueStats.Failed,
				"delivered": queueStats.Delivered,
				"bounced":   queueStats.Bounced,
				"total":     queueStats.Total,
			}
		}
	}

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

// Helpers

func (s *Server) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", "error", err)
	}
}

func (s *Server) sendError(w http.ResponseWriter, status int, message string) {
	s.sendJSON(w, status, map[string]string{"error": message})
}
