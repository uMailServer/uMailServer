package api

// Regression tests promoted from the audit proofs in .temp_files (F5029).

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const a5029Secret = "audit-5029-secret"

func a5029Setup(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	for _, a := range []*db.AccountData{
		{Email: "root@ex.com", LocalPart: "root", Domain: "ex.com", IsActive: true, IsAdmin: true},
		{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com", IsActive: true},
	} {
		if err := database.CreateAccount(a); err != nil {
			t.Fatalf("INVALID create: %v", err)
		}
	}
	return NewServer(database, nil, Config{JWTSecret: a5029Secret, TokenExpiry: time.Hour})
}

func a5029Token(t *testing.T, sub string, admin bool) string {
	t.Helper()
	tk := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": admin, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "jti": sub,
	})
	tk.Header["kid"] = "default"
	s, err := tk.SignedString([]byte(a5029Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return s
}

func a5029Mutate(t *testing.T, s *Server, lp string, f func(*db.AccountData)) {
	t.Helper()
	a, err := s.db.GetAccount("ex.com", lp)
	if err != nil {
		t.Fatalf("INVALID get: %v", err)
	}
	f(a)
	if err := s.db.UpdateAccount(a); err != nil {
		t.Fatalf("INVALID update: %v", err)
	}
}

// Control: an active admin's and an active user's sessions work.
func TestSessionAccountState_ActiveControl(t *testing.T) {
	s := a5029Setup(t)
	if c := a5028Do(s, http.MethodGet, "/api/v1/domains", a5029Token(t, "root@ex.com", true), ""); c != http.StatusOK {
		t.Fatalf("INVALID control admin=%d", c)
	}
	if c := a5028Do(s, http.MethodGet, "/api/v1/account/totp", a5029Token(t, "bob@ex.com", false), ""); c != http.StatusOK {
		t.Fatalf("INVALID control user=%d", c)
	}
}

// Defect: a session outlives the account state it was issued for. After an
// admin is demoted the old token still passes adminMiddleware, and after an
// account is deactivated its token still authenticates.
func TestSessionAccountState_DemotedAndDeactivated(t *testing.T) {
	s := a5029Setup(t)
	adm := a5029Token(t, "root@ex.com", true)
	usr := a5029Token(t, "bob@ex.com", false)
	a5029Mutate(t, s, "root", func(a *db.AccountData) { a.IsAdmin = false })
	a5029Mutate(t, s, "bob", func(a *db.AccountData) { a.IsActive = false })
	demoted := a5028Do(s, http.MethodGet, "/api/v1/domains", adm, "")
	inactive := a5028Do(s, http.MethodGet, "/api/v1/account/totp", usr, "")
	t.Logf("EXPECTED: demoted admin 403, deactivated user 401 | ACTUAL: demoted=%d deactivated=%d", demoted, inactive)
	if demoted != http.StatusForbidden || inactive != http.StatusUnauthorized {
		t.Fatalf("regression F5029: session ignores current account state (demoted admin=%d, deactivated user=%d)", demoted, inactive)
	}
}

// Edge: reactivating the account restores the same session; re-promoting
// restores admin access without a new token.
func TestSessionAccountState_Restore(t *testing.T) {
	s := a5029Setup(t)
	adm := a5029Token(t, "root@ex.com", true)
	usr := a5029Token(t, "bob@ex.com", false)
	a5029Mutate(t, s, "bob", func(a *db.AccountData) { a.IsActive = false })
	a5029Mutate(t, s, "root", func(a *db.AccountData) { a.IsAdmin = false })
	a5029Mutate(t, s, "bob", func(a *db.AccountData) { a.IsActive = true })
	a5029Mutate(t, s, "root", func(a *db.AccountData) { a.IsAdmin = true })
	if c := a5028Do(s, http.MethodGet, "/api/v1/account/totp", usr, ""); c != http.StatusOK {
		t.Fatalf("reactivated=%d", c)
	}
	if c := a5028Do(s, http.MethodGet, "/api/v1/domains", adm, ""); c != http.StatusOK {
		t.Fatalf("re-promoted=%d", c)
	}
}

// Edge: a forged-looking claim cannot exceed the account's role: a
// non-admin account's token minted with admin=true is still refused admin
// routes (the account record wins), and the SSE endpoint also refuses a
// deactivated account.
func TestSessionAccountState_AccountRecordWins(t *testing.T) {
	s := a5029Setup(t)
	if c := a5028Do(s, http.MethodGet, "/api/v1/domains", a5029Token(t, "bob@ex.com", true), ""); c != http.StatusForbidden {
		t.Fatalf("stale admin claim on non-admin account=%d want 403", c)
	}
	usr := a5029Token(t, "bob@ex.com", false)
	a5029Mutate(t, s, "bob", func(a *db.AccountData) { a.IsActive = false })
	if c := a5028Do(s, http.MethodGet, "/api/v1/auth/refresh", usr, ""); c != http.StatusUnauthorized {
		t.Fatalf("deactivated refresh=%d want 401", c)
	}
}
