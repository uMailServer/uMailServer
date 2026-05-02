package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// generateNonce creates a random nonce for CSP
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to static nonce if random fails (should never happen)
		return "fallback-nonce-123456789"
	}
	return base64.StdEncoding.EncodeToString(b)
}

// securityHeadersMiddleware adds security headers to all responses
func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate a unique nonce for this request
		nonce := generateNonce()
		// Store nonce in context for use in HTML rendering
		ctx := context.WithValue(r.Context(), "csp-nonce", nonce)
		r = r.WithContext(ctx)

		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// XSS Protection for legacy browsers
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy with nonce (no unsafe-inline)
		// The nonce is unique per request, preventing XSS attacks
		csp := []string{
			"default-src 'self'",
			"script-src 'self' 'nonce-" + nonce + "'",
			"style-src 'self' 'unsafe-inline'", // Styles still need unsafe-inline for React
			"img-src 'self' data: https:",
			"font-src 'self'",
			"connect-src 'self'",
			"frame-ancestors 'none'",
			"base-uri 'self'",
			"form-action 'self'",
			"object-src 'none'",         // Disallow plugins like Flash
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

// csrfMiddleware validates CSRF protection for state-changing requests.
// For API endpoints using JWT Bearer tokens in the Authorization header,
// the JWT itself provides CSRF protection because browsers don't
// automatically include Authorization headers in cross-origin form submissions.
// We additionally validate Content-Type: application/json to block
// traditional form-based attacks, and verify Origin/Referer consistency
// as defense-in-depth.
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

			// For extra security, verify Origin/Referer for state-changing requests
			// Only check for POST/PUT/PATCH/DELETE (not GET/HEAD/OPTIONS)
			if !isSafeMethod(r.Method) {
				origin := r.Header.Get("Origin")
				referer := r.Header.Get("Referer")
				if origin != "" || referer != "" {
					// If both are present, they should be consistent
					if origin != "" && referer != "" && !strings.HasPrefix(referer, origin) {
						s.logger.Warn("CSRF: Origin/Referer mismatch", "origin", origin, "referer", referer, "path", r.URL.Path)
						http.Error(w, "Forbidden - CSRF check failed", http.StatusForbidden)
						return
					}
					// If origin is present but referer is missing (privacy tool stripped it), allow
					// If referer is present but origin is missing, allow (some setups don't send Origin)
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
			clientIP := getClientIP(r, s.config.TrustedProxies)
			if !allowed[clientIP] {
				s.sendError(w, http.StatusForbidden, "Access denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request
// If trustedProxies is set, only trust X-Forwarded-For from those IPs
func getClientIP(r *http.Request, trustedProxies []string) string {
	remoteIP := getRemoteAddrIP(r)

	// If trusted proxies are configured, only trust X-Forwarded-For if the request is from a trusted proxy
	if len(trustedProxies) > 0 {
		isFromTrustedProxy := false
		for _, trusted := range trustedProxies {
			if remoteIP == trusted {
				isFromTrustedProxy = true
				break
			}
		}

		if isFromTrustedProxy {
			// Only respect X-Forwarded-For from trusted proxies
			xff := r.Header.Get("X-Forwarded-For")
			if xff != "" {
				ips := strings.Split(xff, ",")
				if len(ips) > 0 {
					return strings.TrimSpace(ips[0])
				}
			}

			// Also check X-Real-Ip from trusted proxy
			xri := r.Header.Get("X-Real-Ip")
			if xri != "" {
				return xri
			}
		}
	}

	// No trusted proxies configured or request not from trusted proxy - use RemoteAddr
	return remoteIP
}

// getRemoteAddrIP extracts the IP from RemoteAddr without port
func getRemoteAddrIP(r *http.Request) string {
	ip := r.RemoteAddr
	// Handle IPv6 addresses like [::1]:8080
	if len(ip) > 1 && ip[0] == '[' {
		if idx := strings.LastIndex(ip, "]"); idx != -1 {
			ip = ip[1:idx]
		}
	} else if idx := strings.LastIndex(ip, ":"); idx != -1 {
		// Check if this is IPv4 (has no colons in the part before last colon)
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
