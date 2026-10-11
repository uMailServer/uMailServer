package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// F6647: addresses are case-insensitive system-wide; REST login must accept a
// mixed-case spelling of a stored (lowercase) address.
func TestR193_LoginCaseInsensitive(t *testing.T) {
	server, current, _ := setupAccountPasswordTest(t)
	for _, addr := range []string{"user@example.com", "USER@Example.COM", " User@EXAMPLE.com "} {
		body, _ := json.Marshal(map[string]string{"email": addr, "password": current})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		server.handleLogin(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("login %q: %d %s", addr, rec.Code, rec.Body.String())
		}
	}
	body, _ := json.Marshal(map[string]string{"email": "USER@Example.COM", "password": "wrong-password-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.11:1234"
	rec := httptest.NewRecorder()
	server.handleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password mixed case: %d", rec.Code)
	}
}

func deleteSelf(server *Server, password string, withBody bool) *httptest.ResponseRecorder {
	var req *http.Request
	if withBody {
		b, _ := json.Marshal(map[string]string{"password": password})
		req = httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/user@example.com", bytes.NewReader(b))
	} else {
		req = httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/user@example.com", nil)
	}
	ctx := context.WithValue(req.Context(), "user", "user@example.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	rec := httptest.NewRecorder()
	server.deleteAccount(rec, req.WithContext(ctx), "user@example.com")
	return rec
}

// A non-admin deleting their own account must re-authenticate.
func TestR193_SelfDeleteRequiresPassword(t *testing.T) {
	server, current, database := setupAccountPasswordTest(t)
	exists := func() bool { a, err := database.GetAccount("example.com", "user"); return err == nil && a != nil }

	if rec := deleteSelf(server, "", false); rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("no password: %d", rec.Code)
	}
	if !exists() {
		t.Fatal("account deleted without password")
	}
	if rec := deleteSelf(server, "wrong-password-xyz", true); rec.Code != http.StatusForbidden {
		t.Errorf("wrong password: %d", rec.Code)
	}
	if !exists() {
		t.Fatal("account deleted with wrong password")
	}
	if rec := deleteSelf(server, current, true); rec.Code != http.StatusNoContent {
		t.Errorf("right password: %d %s", rec.Code, rec.Body.String())
	}
	if exists() {
		t.Error("account still exists after authenticated self-delete")
	}
}

// Autoconfig must only answer for hosted domains.
func TestR193_AutoconfigHostedOnly(t *testing.T) {
	server, _, database := setupAccountPasswordTest(t)
	if err := database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	get := func(email string) int {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress="+email, nil)
		rec := httptest.NewRecorder()
		server.handleAutoconfig(rec, req)
		return rec.Code
	}
	if c := get("u@example.com"); c != http.StatusOK {
		t.Errorf("hosted domain: %d", c)
	}
	if c := get("u@evil.example.org"); c != http.StatusNotFound {
		t.Errorf("foreign domain: %d, want 404", c)
	}
}

// Every 405 carries Allow and HEAD works on GET handlers (routed through the
// real router).
func TestR193_RouterAllowAndHead(t *testing.T) {
	server, _, _ := setupAccountPasswordTest(t)
	tok, err := server.newSessionToken("user@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.20:1"
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}
	cases := []struct{ method, path, allow string }{
		{"PATCH", "/api/v1/search", "GET"},
		{"POST", "/api/v1/threads", "GET"},
		{"GET", "/api/v1/push/subscribe", "POST"},
		{"GET", "/api/v1/push/unsubscribe", "DELETE"},
		{"POST", "/api/v1/vacation", "GET"},
		{"PATCH", "/api/v1/filters", "GET"},
		{"GET", "/api/v1/filters/reorder", "POST"},
		{"GET", "/api/v1/mail/send", "POST"},
		{"PATCH", "/api/v1/mail/inbox", "GET"},
		{"PATCH", "/api/v1/auth/refresh", "POST"},
		{"GET", "/api/v1/auth/login", "POST"},
		{"GET", "/api/v1/account/password", "POST"},
		{"POST", "/api/v1/account/totp", "GET"},
		{"PATCH", "/api/v1/account/forwarding", "GET"},
		{"GET", "/api/v1/auth/logout", "POST"},
	}
	for _, c := range cases {
		rec := do(c.method, c.path)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got %d, want 405", c.method, c.path, rec.Code)
			continue
		}
		got := rec.Header().Get("Allow")
		if got == "" {
			t.Errorf("%s %s: 405 without Allow", c.method, c.path)
		} else if !strings.Contains(got, c.allow) {
			t.Errorf("%s %s: Allow=%q, want it to contain %q", c.method, c.path, got, c.allow)
		}
	}
	// HEAD on GET-only handlers is not 405 and has no body.
	for _, p := range []string{"/api/v1/search?q=x", "/api/v1/threads", "/api/v1/push/vapid-public-key", "/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=a@example.com"} {
		rec := do("HEAD", p)
		if rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("HEAD %s: 405", p)
		}
	}
	_ = time.Second
}

func r193Do(server *Server, method, path, bearer, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "jwt", Value: cookie})
	}
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.30:1"
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

// F6704: logout with both a cookie and a Bearer token must end both sessions
// (refresh already rotates both).
func TestR193_LogoutRevokesCookieAndBearer(t *testing.T) {
	server, _, _ := setupAccountPasswordTest(t)
	cookieTok, _ := server.newSessionToken("user@example.com", false)
	bearerTok, _ := server.newSessionToken("user@example.com", false)
	if rec := r193Do(server, "POST", "/api/v1/auth/logout", bearerTok, cookieTok); rec.Code != http.StatusOK {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := r193Do(server, "GET", "/api/v1/threads", "", cookieTok); rec.Code != http.StatusUnauthorized {
		t.Errorf("cookie token still valid after logout: %d", rec.Code)
	}
	if rec := r193Do(server, "GET", "/api/v1/threads", bearerTok, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("bearer token still valid after logout: %d", rec.Code)
	}
	// Control: an unrelated session of the same user is untouched.
	other, _ := server.newSessionToken("user@example.com", false)
	if rec := r193Do(server, "GET", "/api/v1/threads", other, ""); rec.Code == http.StatusUnauthorized {
		t.Errorf("unrelated token revoked: %d", rec.Code)
	}
}

// F6705: one token must be spendable on /auth/refresh exactly once even when
// two refreshes interleave between the revocation check and the revocation.
func TestR193_RefreshSingleSpend(t *testing.T) {
	server, _, _ := setupAccountPasswordTest(t)
	tok, _ := server.newSessionToken("user@example.com", false)

	reached := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	beforeRefreshRevoke = func() {
		if first.CompareAndSwap(false, true) {
			close(reached)
			<-release
		}
	}
	defer func() { beforeRefreshRevoke = nil }()

	aDone := make(chan int, 1)
	go func() { aDone <- r193Do(server, "POST", "/api/v1/auth/refresh", tok, "").Code }()
	<-reached
	bDone := make(chan int, 1)
	go func() { bDone <- r193Do(server, "POST", "/api/v1/auth/refresh", tok, "").Code }()
	var b int
	select { // pre-fix B completes here; post-fix B waits for A
	case b = <-bDone:
		close(release)
	case <-time.After(300 * time.Millisecond):
		close(release)
		b = <-bDone
	}
	a := <-aDone
	ok := 0
	for _, c := range []int{a, b} {
		if c == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("refresh spent one token %d times (a=%d b=%d)", ok, a, b)
	}
}

// F6706: TOTP setup must change only the TOTP secret, not write back a stale
// whole-row snapshot.
func TestR193_TOTPSetupKeepsConcurrentChanges(t *testing.T) {
	server, _, database := setupAccountPasswordTest(t)
	beforeCredentialWrite = func() {
		_, _ = database.MutateAccount("example.com", "user", func(a *db.AccountData) error {
			a.PasswordHash = "CHANGED-HASH"
			return nil
		})
	}
	defer func() { beforeCredentialWrite = nil }()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/totp/setup", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", "user@example.com"))
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	a, _ := database.GetAccount("example.com", "user")
	if a.PasswordHash != "CHANGED-HASH" {
		t.Errorf("setup reverted a concurrent password change")
	}
	if a.TOTPSecret == "" {
		t.Errorf("secret not stored")
	}
}
