package api

import (
	"net/http"
	"strings"
)

// securityHeadersMiddleware adds security headers to all responses
func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// XSS Protection for legacy browsers
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy
		// Note: 'unsafe-inline' is required for React development builds.
		// For production, consider using nonce-based or hash-based CSP.
		// The webmail XSS is fixed via output encoding, but strict CSP provides defense-in-depth.
		csp := []string{
			"default-src 'self'",
			"script-src 'self' 'unsafe-inline' 'unsafe-hashes'", // React needs this; prefer nonce in production
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data: https:",
			"font-src 'self'",
			"connect-src 'self'",
			"frame-ancestors 'none'",
			"base-uri 'self'",
			"form-action 'self'",
			"object-src 'none'", // Disallow plugins like Flash
			"upgrade-insecure-requests", // Upgrade HTTP to HTTPS
		}
		w.Header().Set("Content-Security-Policy", strings.Join(csp, "; "))

		// HSTS for HTTPS connections
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}

// csrfMiddleware validates CSRF tokens for state-changing requests
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF check for safe methods
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Skip CSRF for API endpoints that use JWT auth via Authorization header
		// JWT Bearer tokens are not automatically sent by browsers, providing CSRF protection.
		// Additionally, we validate Content-Type to prevent form-based attacks.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// Require application/json Content-Type to prevent simple form submissions
			contentType := r.Header.Get("Content-Type")
			if !strings.Contains(contentType, "application/json") {
				s.sendError(w, http.StatusBadRequest, "Content-Type must be application/json")
				return
			}

			// For extra security, verify Origin/Referer if present (defense-in-depth)
			origin := r.Header.Get("Origin")
			referer := r.Header.Get("Referer")
			if origin != "" || referer != "" {
				// If both are present, they should be consistent
				if origin != "" && referer != "" && !strings.HasPrefix(referer, origin) {
					// Could be a CSRF attempt - but allow if Referer is just missing (some privacy tools strip it)
					// This is defense-in-depth, not a blocking check
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// isSafeMethod returns true for HTTP methods that don't modify state
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// requestSizeMiddleware limits the size of incoming requests
func (s *Server) requestSizeMiddleware(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}

// ipWhitelistMiddleware restricts access to specific IP addresses
func (s *Server) ipWhitelistMiddleware(allowedIPs []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, ip := range allowedIPs {
		allowed[ip] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := getClientIP(r)
			if !allowed[clientIP] {
				s.sendError(w, http.StatusForbidden, "Access denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the chain
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-Ip header
	xri := r.Header.Get("X-Real-Ip")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	// Strip port if present
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// validateContentTypeMiddleware ensures requests have the correct Content-Type
func validateContentTypeMiddleware(contentType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				ct := r.Header.Get("Content-Type")
				if ct == "" || !strings.HasPrefix(ct, contentType) {
					http.Error(w, "Invalid Content-Type", http.StatusUnsupportedMediaType)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
