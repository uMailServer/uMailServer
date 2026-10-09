package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func inactiveLoginSetup(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("INVALID create domain: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw-123456"), bcrypt.MinCost)
	for _, a := range []struct {
		local  string
		active bool
	}{{"live", true}, {"off", false}} {
		acc := &db.AccountData{Email: a.local + "@ex.com", LocalPart: a.local, Domain: "ex.com", PasswordHash: string(hash), IsActive: a.active}
		if err := database.CreateAccount(acc); err != nil {
			t.Fatalf("INVALID create account: %v", err)
		}
	}
	return NewServer(database, nil, Config{JWTSecret: "audit-4845-secret", TokenExpiry: time.Hour})
}

func inactiveLoginLogin(s *Server, email, pw string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email, "password": pw})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:5555"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// Control: an active account logs in through the real router.
func TestLoginInactiveAccount_Control(t *testing.T) {
	s := inactiveLoginSetup(t)
	rec := inactiveLoginLogin(s, "live@ex.com", "pw-123456")
	if rec.Code != http.StatusOK {
		t.Fatalf("INVALID control: active login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Defect: a deactivated account must not receive a JWT.
func TestLoginInactiveAccount_InactiveRejected(t *testing.T) {
	s := inactiveLoginSetup(t)
	rec := inactiveLoginLogin(s, "off@ex.com", "pw-123456")
	t.Logf("EXPECTED: status=401 no token | ACTUAL: status=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code == http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte(`"token"`)) {
		t.Fatalf("DEFECT F4845: inactive account obtained a session")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status %d", rec.Code)
	}
}

// Edge: a wrong password on an inactive account is still the generic 401
// (no account-state oracle), and re-activation restores login.
func TestLoginInactiveAccount_EdgeWrongPasswordAndReactivate(t *testing.T) {
	s := inactiveLoginSetup(t)
	a := inactiveLoginLogin(s, "off@ex.com", "nope")
	b := inactiveLoginLogin(s, "off@ex.com", "pw-123456")
	if a.Code != http.StatusUnauthorized || b.Code != http.StatusUnauthorized || a.Body.String() != b.Body.String() {
		t.Fatalf("inactive responses differ: %d %q vs %d %q", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
	acc, err := s.db.GetAccount("ex.com", "off")
	if err != nil {
		t.Fatal(err)
	}
	acc.IsActive = true
	if err := s.db.UpdateAccount(acc); err != nil {
		t.Fatal(err)
	}
	// Reset per-account limiter so the extra attempts above don't mask the result.
	s.clearAccountLoginFailures("off@ex.com")
	if rec := inactiveLoginLogin(s, "off@ex.com", "pw-123456"); rec.Code != http.StatusOK {
		t.Fatalf("reactivated login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Edge: the failure counts toward the per-IP limiter like any bad login.
func TestLoginInactiveAccount_EdgeCountsAsFailure(t *testing.T) {
	s := inactiveLoginSetup(t)
	inactiveLoginLogin(s, "off@ex.com", "pw-123456")
	s.loginMu.Lock()
	at := s.loginAttempts["192.0.2.10"]
	s.loginMu.Unlock()
	// F5027: one failed login now counts once (it was counted twice).
	if at == nil || at.count < 1 {
		t.Fatalf("inactive login not recorded as failure: %+v", at)
	}
}
