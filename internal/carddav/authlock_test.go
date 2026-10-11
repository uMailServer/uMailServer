package carddav

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func authAttempt(s *Server, user, pass, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("OPTIONS", "/dav/", nil)
	req.RemoteAddr = ip + ":1234"
	req.SetBasicAuth(user, pass)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

// F6350: repeated failed Basic logins must lock out the (user, IP) pair.
func TestBasicAuthLockout(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	calls := 0
	s.SetAuthFunc(func(u, p string) (bool, error) { calls++; return p == "good", nil })
	now := time.Unix(1000, 0)
	s.authGuard.now = func() time.Time { return now }

	for i := 0; i < 20; i++ {
		user := "Alice@Example.com"
		if i%2 == 1 {
			user = " alice@example.com" // case/space variants share the counter
		}
		authAttempt(s, user, "bad", "10.0.0.1")
	}
	w := authAttempt(s, "alice@example.com", "good", "10.0.0.1")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("expected 429 + Retry-After during lockout, got %d", w.Code)
	}
	before := calls
	authAttempt(s, "alice@example.com", "good", "10.0.0.1")
	if calls != before {
		t.Fatal("authFunc must not be consulted while locked")
	}
	// other IP unaffected
	if w := authAttempt(s, "alice@example.com", "good", "10.0.0.2"); w.Code == http.StatusTooManyRequests || w.Code == http.StatusUnauthorized {
		t.Fatalf("other IP should not be locked, got %d", w.Code)
	}
	// lock expires; success clears state
	now = now.Add(time.Hour)
	if w := authAttempt(s, "alice@example.com", "good", "10.0.0.1"); w.Code == http.StatusTooManyRequests || w.Code == http.StatusUnauthorized {
		t.Fatalf("lock should have expired, got %d", w.Code)
	}
	for i := 0; i < 4; i++ {
		authAttempt(s, "alice@example.com", "bad", "10.0.0.1")
	}
	if w := authAttempt(s, "alice@example.com", "good", "10.0.0.1"); w.Code == http.StatusTooManyRequests {
		t.Fatal("success must reset the failure count")
	}
}

func TestAuthGuardBounded(t *testing.T) {
	g := newAuthGuard()
	for i := 0; i < maxAuthGuardEntries*2; i++ {
		g.fail(string(rune('a'+i%26))+time.Duration(i).String(), "1.1.1.1")
	}
	if len(g.m) > maxAuthGuardEntries {
		t.Fatalf("tracker unbounded: %d", len(g.m))
	}
}
