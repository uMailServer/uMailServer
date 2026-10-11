package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F6520: cookie-authenticated state-changing requests must come from the
// same host (or an allowed CORS origin); Bearer-header auth is unaffected.
func TestR172CSRFOriginHostCheck(t *testing.T) {
	server := NewTestServer(t, t.TempDir())
	server.config.CorsOrigins = []string{"https://app.example.com"}
	hit := false
	h := server.csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))

	cases := []struct {
		name    string
		cookie  bool
		bearer  bool
		origin  string
		referer string
		want    int
	}{
		{"cookie cross-origin", true, false, "https://evil.example", "", http.StatusForbidden},
		{"cookie null origin", true, false, "null", "", http.StatusForbidden},
		{"cookie evil referer only", true, false, "", "https://evil.example/x", http.StatusForbidden},
		{"cookie same host", true, false, "https://mail.example.com", "", http.StatusOK},
		{"cookie same host referer", true, false, "", "https://mail.example.com/webmail/", http.StatusOK},
		{"cookie allowed cors origin", true, false, "https://app.example.com", "", http.StatusOK},
		{"cookie no origin or referer", true, false, "", "", http.StatusOK},
		{"bearer cross-origin unaffected", false, true, "https://evil.example", "", http.StatusOK},
		{"no auth cross-origin unaffected", false, false, "https://evil.example", "", http.StatusOK},
	}
	for _, c := range cases {
		hit = false
		req := httptest.NewRequest("POST", "/api/v1/mail/send", strings.NewReader(`{}`))
		req.Host = "mail.example.com"
		req.Header.Set("Content-Type", "application/json")
		if c.cookie {
			req.AddCookie(&http.Cookie{Name: "jwt", Value: "x"})
		}
		if c.bearer {
			req.Header.Set("Authorization", "Bearer x")
		}
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if c.referer != "" {
			req.Header.Set("Referer", c.referer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != c.want || hit != (c.want == http.StatusOK) {
			t.Errorf("%s: status=%d handler=%v, want %d", c.name, w.Code, hit, c.want)
		}
	}
}

func TestR172APINoStoreAndNosniff(t *testing.T) {
	server := NewTestServer(t, t.TempDir())
	h := server.securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/mail/inbox", nil))
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers: %v", w.Header())
	}
}

// Routes are wired through the full middleware chain: an unauthenticated
// call is rejected, a cookie-authenticated cross-origin write is a 403.
func TestR172NewMailRoutesWired(t *testing.T) {
	server := NewTestServer(t, t.TempDir())
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/mail/message?id=x"},
		{"POST", "/api/v1/mail/flags"},
		{"POST", "/api/v1/mail/draft"},
		{"PUT", "/api/v1/mail/draft"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d want 401", c.method, c.path, w.Code)
		}
		req = httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example")
		req.AddCookie(&http.Cookie{Name: "jwt", Value: "x"})
		w = httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if c.method != "GET" && w.Code != http.StatusForbidden {
			t.Errorf("%s %s cross-origin: %d want 403", c.method, c.path, w.Code)
		}
	}
}
