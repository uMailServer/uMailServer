package mcp

// Regression tests for F5591 (CORS preflight omitted Authorization; a
// multi-origin allowlist was sent as one comma-joined Allow-Origin value) and
// F5592 (cross-origin requests from a web page were counted against the
// per-IP rate limit shared by local clients; Origin was never validated).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func originRequest(s *Server, method, origin, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.RemoteAddr = "127.0.0.1:50000"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rr := httptest.NewRecorder()
	s.HandleHTTP(rr, req)
	return rr
}

func TestCORSPreflightAllowsAuthorizationAndReflectsListedOrigin(t *testing.T) {
	s := NewServer(nil)
	s.SetAuthToken("tok")
	s.SetCorsOrigin("https://a.example,https://b.example")

	pre := originRequest(s, http.MethodOptions, "https://b.example", "")
	if got := pre.Header().Get("Access-Control-Allow-Origin"); got != "https://b.example" {
		t.Errorf("Allow-Origin = %q, want the listed request origin", got)
	}
	if !strings.Contains(strings.ToLower(pre.Header().Get("Access-Control-Allow-Headers")), "authorization") {
		t.Errorf("Allow-Headers = %q, want Authorization", pre.Header().Get("Access-Control-Allow-Headers"))
	}
	if got := originRequest(s, http.MethodOptions, "https://evil.example", "").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unlisted origin got Allow-Origin %q", got)
	}
	if rr := originRequest(s, http.MethodPost, "https://a.example", "tok"); rr.Code != http.StatusOK {
		t.Errorf("listed origin POST: status %d, want 200", rr.Code)
	}
}

func TestDisallowedOriginRefusedWithoutConsumingRateLimit(t *testing.T) {
	s := NewServer(nil)
	s.SetAuthToken("tok")
	s.SetRateLimit(3)

	for i := 0; i < 5; i++ {
		if rr := originRequest(s, http.MethodPost, "https://evil.example", ""); rr.Code != http.StatusForbidden {
			t.Fatalf("cross-origin request %d: status %d, want 403", i, rr.Code)
		}
	}
	if rr := originRequest(s, http.MethodPost, "http://rebind.example:3000", "tok"); rr.Code != http.StatusForbidden {
		t.Errorf("disallowed origin with valid token: status %d, want 403", rr.Code)
	}
	if rr := originRequest(s, http.MethodPost, "", "tok"); rr.Code != http.StatusOK {
		t.Errorf("local client after cross-origin requests: status %d, want 200", rr.Code)
	}
}
