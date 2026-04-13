package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AdminConfig holds configuration for the admin server
type AdminConfig struct {
	Addr              string            // e.g., "127.0.0.1:8443"
	JWTSecret         string            // Legacy single secret
	JWTSecretVersions map[string]string // kid -> secret, for key rotation
	AuditLog          AuditLogConfig
}

// AdminServer is a lightweight HTTP server for admin panel access.
// It serves on a separate port bound to localhost only.
// It embeds the main Server to reuse its handlers.
type AdminServer struct {
	*Server    // Embed main server to reuse handlers
	config     AdminConfig
	httpServer *http.Server
}

// NewAdminServer creates a new admin-only HTTP server
// It shares the main Server's handlers but runs on a separate port
func NewAdminServer(server *Server, cfg AdminConfig) *AdminServer {
	s := &AdminServer{
		Server: server,
		config: cfg,
	}
	return s
}

// Start starts the admin HTTP server
func (s *AdminServer) Start() error {
	s.httpServer = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.logger.Info("Admin API server starting", "addr", s.config.Addr)
	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the admin server
func (s *AdminServer) Stop() error {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// router sets up the admin-only HTTP routes
func (s *AdminServer) router() http.Handler {
	mux := http.NewServeMux()

	// Admin panel static files
	mux.HandleFunc("/admin/", s.handleAdmin)

	// Health check - delegate to embedded server's handler
	mux.HandleFunc("/health", s.Server.handleHealth)

	// Metrics - delegate to embedded server's handler
	mux.HandleFunc("/metrics", s.Server.handleMetrics)

	// Admin API routes (all require admin auth)
	api := http.NewServeMux()

	// Domains (admin only)
	api.HandleFunc("/api/v1/domains", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleDomains))))
	api.HandleFunc("/api/v1/domains/", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleDomainDetail))))

	// Accounts (admin only)
	api.HandleFunc("/api/v1/accounts", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleAccounts))))
	api.HandleFunc("/api/v1/accounts/", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleAccountDetail))))

	// Queue (admin only)
	api.HandleFunc("/api/v1/admin/queue", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleQueue))))
	api.HandleFunc("/api/v1/admin/queue/", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleQueueDetail))))

	// Queue alias (no /admin prefix)
	api.HandleFunc("/api/v1/queue", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleQueue))))
	api.HandleFunc("/api/v1/queue/", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleQueueDetail))))

	// Rate limits (admin only)
	api.HandleFunc("/api/v1/admin/ratelimits/config", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleRateLimitConfig))))
	api.HandleFunc("/api/v1/admin/ratelimits/ip/", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleRateLimitIPStats))))
	api.HandleFunc("/api/v1/admin/ratelimits/user/", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleRateLimitUserStats))))

	// Metrics (admin only)
	api.HandleFunc("/api/v1/metrics", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleMetrics))))
	api.HandleFunc("/api/v1/stats", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleStats))))

	// Vacation (admin only)
	api.HandleFunc("/api/v1/admin/vacations", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleAdminVacations))))

	// Push stats (admin only)
	api.HandleFunc("/api/v1/admin/push/stats", s.withAuth(s.adminMiddleware(http.HandlerFunc(s.handleAdminPushStats))))

	// All API routes
	mux.Handle("/api/v1/", api)

	return mux
}

// withAuth middleware requires valid JWT authentication
func (s *AdminServer) withAuth(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || len(authHeader) < 7 {
			writeError(w, "unauthorized", "Missing authorization header", http.StatusUnauthorized)
			return
		}
		tokenStr := authHeader[7:]

		// Parse JWT - use versioned secrets from embedded Server
		parsed, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			// Try kid-based secret lookup first
			if t.Header["kid"] != nil {
				if kidSecret, ok := s.Server.jwtSecrets[t.Header["kid"].(string)]; ok {
					return []byte(kidSecret), nil
				}
			}
			// Fall back to current kid
			if secret, ok := s.Server.jwtSecrets[s.Server.currentKid]; ok {
				return []byte(secret), nil
			}
			// Last resort: try legacy JWTSecret
			return []byte(s.config.JWTSecret), nil
		})
		if err != nil || !parsed.Valid {
			writeError(w, "unauthorized", "Invalid token", http.StatusUnauthorized)
			return
		}

		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			writeError(w, "unauthorized", "Invalid claims", http.StatusUnauthorized)
			return
		}

		user, _ := claims["sub"].(string)
		isAdmin, _ := claims["admin"].(bool)

		// Validate that we got valid values
		if user == "" {
			writeError(w, "unauthorized", "Invalid token: missing subject", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "user", user)
		ctx = context.WithValue(ctx, "isAdmin", isAdmin)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// adminMiddleware ensures user is an admin
func (s *AdminServer) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAdmin, ok := r.Context().Value("isAdmin").(bool)
		if !ok || !isAdmin {
			writeError(w, "forbidden", "Admin access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, errCode, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]interface{}{
		"error":   errCode,
		"message": message,
		"code":    status,
	})
	w.WriteHeader(status)
	w.Write(body)
}

// handleAdmin serves the admin panel static files
func (s *AdminServer) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if s.Server.adminFS == nil {
		http.Error(w, "Admin filesystem not configured", http.StatusInternalServerError)
		return
	}

	path := r.URL.Path
	if path == "/admin" || path == "/admin/" {
		path = "/admin/"
	}

	// Remove /admin prefix to get file path
	filePath := strings.TrimPrefix(path, "/admin/")
	if filePath == "" {
		filePath = "index.html"
	}

	// Try to serve the file
	data, err := s.Server.adminFS.Open(filePath)
	if err != nil {
		// Try index.html for SPA routing
		data, err = s.Server.adminFS.Open("index.html")
		if err != nil {
			http.Error(w, "Admin panel not found", http.StatusNotFound)
			return
		}
	}
	defer data.Close()

	contentType := getContentType(filePath)
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
}

// getContentType returns MIME type for static files
func getContentType(path string) string {
	switch {
	case strings.HasSuffix(path, ".html"):
		return "text/html"
	case strings.HasSuffix(path, ".js"):
		return "application/javascript"
	case strings.HasSuffix(path, ".css"):
		return "text/css"
	case strings.HasSuffix(path, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(path, ".png"):
		return "image/png"
	case strings.HasSuffix(path, ".ico"):
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
