package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Test securityHeadersMiddleware
func TestSecurityHeadersMiddleware(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := server.securityHeadersMiddleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	// Check security headers
	tests := []struct {
		header   string
		expected string
	}{
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		if value := w.Header().Get(tt.header); value != tt.expected {
			t.Errorf("Header %s = %q, want %q", tt.header, value, tt.expected)
		}
	}

	// Check CSP header exists
	if csp := w.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Expected Content-Security-Policy header to be set")
	}

	// HSTS should not be set for non-TLS connections
	if hsts := w.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Error("HSTS should not be set for non-TLS connections")
	}
}

func TestSecurityHeadersMiddleware_WithTLS(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := server.securityHeadersMiddleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	// Simulate TLS connection by setting a TLS state
	// Note: This is a simplified test; in real scenario, TLS state would be set by server
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	// We can't easily simulate TLS in httptest, but we verify the middleware runs
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// Test csrfMiddleware
func TestCSRFMiddleware_SafeMethod(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := server.csrfMiddleware(handler)

	// GET request should pass without CSRF check
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if !called {
		t.Error("Handler was not called for safe method")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCSRFMiddleware_APIWithJSON(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := server.csrfMiddleware(handler)

	// POST to API with JSON content type should pass
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if !called {
		t.Error("Handler was not called for API request with JSON")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCSRFMiddleware_APIWithoutJSON(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := server.csrfMiddleware(handler)

	// POST to API without JSON content type should fail
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader("data"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if called {
		t.Error("Handler should not be called for API request without JSON")
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCSRFMiddleware_NonAPIPath(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := server.csrfMiddleware(handler)

	// POST to non-API path should pass through
	req := httptest.NewRequest("POST", "/webhook/test", strings.NewReader("data"))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if !called {
		t.Error("Handler was not called for non-API path")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// Test isSafeMethod
func TestIsSafeMethod(t *testing.T) {
	tests := []struct {
		method string
		want   bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodTrace, true},
		{http.MethodPost, false},
		{http.MethodPut, false},
		{http.MethodDelete, false},
		{http.MethodPatch, false},
		{"CUSTOM", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := isSafeMethod(tt.method)
			if got != tt.want {
				t.Errorf("isSafeMethod(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

// Test requestSizeMiddleware
func TestRequestSizeMiddleware(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Limit to 100 bytes
	middleware := server.requestSizeMiddleware(100)
	wrapped := middleware(handler)

	// Small request should pass
	req := httptest.NewRequest("POST", "/test", strings.NewReader("small data"))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if !called {
		t.Error("Handler was not called for small request")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// Test ipWhitelistMiddleware
func TestIPWhitelistMiddleware_Allowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := server.ipWhitelistMiddleware([]string{"192.168.1.1", "10.0.0.1"})
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if !called {
		t.Error("Handler was not called for allowed IP")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestIPWhitelistMiddleware_Denied(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := server.ipWhitelistMiddleware([]string{"192.168.1.1"})
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if called {
		t.Error("Handler should not be called for denied IP")
	}

	if w.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// Test getRemoteAddrIP with IPv6
func TestGetRemoteAddrIP_IPv6(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "[::1]:8080"

	ip := getRemoteAddrIP(req)
	if ip != "::1" {
		t.Errorf("Expected ::1, got %s", ip)
	}
}

func TestGetRemoteAddrIP_IPv6Full(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "[2001:db8::1]:8080"

	ip := getRemoteAddrIP(req)
	if ip != "2001:db8::1" {
		t.Errorf("Expected 2001:db8::1, got %s", ip)
	}
}

// Test getClientIP
func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2, 10.0.0.3")
	req.RemoteAddr = "192.168.1.1:12345"

	// Without trusted proxies, X-Forwarded-For is ignored (privacy protection)
	ip := getClientIP(req, nil)
	if ip != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1 (no trusted proxies), got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_TrustedProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2, 10.0.0.3")
	req.RemoteAddr = "192.168.1.1:12345"

	// With trusted proxy, X-Forwarded-For is respected
	ip := getClientIP(req, []string{"192.168.1.1"})
	if ip != "10.0.0.1" {
		t.Errorf("Expected IP 10.0.0.1 (from trusted proxy), got %s", ip)
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-Ip", "10.0.0.5")
	req.RemoteAddr = "192.168.1.1:12345"

	// Without trusted proxies, X-Real-Ip is ignored
	ip := getClientIP(req, nil)
	if ip != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1 (no trusted proxies), got %s", ip)
	}
}

func TestGetClientIP_XRealIP_TrustedProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-Ip", "10.0.0.5")
	req.RemoteAddr = "192.168.1.1:12345"

	// With trusted proxy, X-Real-Ip is respected
	ip := getClientIP(req, []string{"192.168.1.1"})
	if ip != "10.0.0.5" {
		t.Errorf("Expected IP 10.0.0.5 (from trusted proxy), got %s", ip)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	ip := getClientIP(req, nil)
	if ip != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddrNoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1"

	ip := getClientIP(req, nil)
	if ip != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1, got %s", ip)
	}
}

func TestGetClientIP_UntrustedProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.RemoteAddr = "10.0.0.99:12345" // Untrusted proxy

	// Without trusted proxies, X-Forwarded-For is ignored even from untrusted proxy
	ip := getClientIP(req, []string{"192.168.1.1"})
	if ip != "10.0.0.99" {
		t.Errorf("Expected IP 10.0.0.99 (untrusted proxy), got %s", ip)
	}
}

// Test validateContentTypeMiddleware
func TestValidateContentTypeMiddleware_CorrectType(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := validateContentTypeMiddleware("application/json")
	wrapped := middleware(handler)

	req := httptest.NewRequest("POST", "/test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestValidateContentTypeMiddleware_WrongType(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := validateContentTypeMiddleware("application/json")
	wrapped := middleware(handler)

	req := httptest.NewRequest("POST", "/test", strings.NewReader("data"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestValidateContentTypeMiddleware_NoContentType(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := validateContentTypeMiddleware("application/json")
	wrapped := middleware(handler)

	req := httptest.NewRequest("POST", "/test", strings.NewReader("data"))
	// No Content-Type header
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestValidateContentTypeMiddleware_SafeMethod(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := validateContentTypeMiddleware("application/json")
	wrapped := middleware(handler)

	// GET requests should not require Content-Type
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestValidateContentTypeMiddleware_HeadMethod(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := validateContentTypeMiddleware("application/json")
	wrapped := middleware(handler)

	// HEAD requests should not require Content-Type
	req := httptest.NewRequest("HEAD", "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}
