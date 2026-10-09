package api

// Regression tests promoted from the audit proofs in .temp_files (F4935-F4939).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const a4935Secret = "audit-4935-secret"

func a4935Setup(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: a4935Secret, TokenExpiry: time.Hour})
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", IsActive: true}); err != nil {
		t.Fatalf("INVALID domain: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com", IsActive: true}); err != nil {
		t.Fatalf("INVALID account: %v", err)
	}
	as := NewAdminServer(s, AdminConfig{Addr: "127.0.0.1:0", JWTSecret: a4935Secret})
	return s, as.router()
}

func a4935Token(t *testing.T, sub string, admin bool) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": admin, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(a4935Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return str
}

func a4935Do(h http.Handler, method, path, token, body string) (int, string) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Control: the same admin token works on admin-port routes whose handler does
// not consult the request context (domains list).
func TestAdminServerContextKeys_Control(t *testing.T) {
	_, admin := a4935Setup(t)
	tok := a4935Token(t, "root@ex.com", true)
	if c, b := a4935Do(admin, http.MethodGet, "/api/v1/domains", tok, ""); c != http.StatusOK {
		t.Fatalf("INVALID control: domains status=%d body=%s", c, b)
	}
}

// Defect: an admin token is refused by admin-port handlers that read the
// identity from context ("isAdmin"/"user" string keys).
func TestAdminServerContextKeys_AdminVacations(t *testing.T) {
	_, admin := a4935Setup(t)
	tok := a4935Token(t, "root@ex.com", true)
	c, b := a4935Do(admin, http.MethodGet, "/api/v1/admin/vacations", tok, "")
	t.Logf("EXPECTED: status=200 | ACTUAL: status=%d body=%s", c, strings.TrimSpace(b))
	if c != http.StatusOK {
		t.Fatalf("DEFECT F4935: admin port refused admin on /admin/vacations (status=%d)", c)
	}
}

// Edge: admin updating another account on the admin port.
func TestAdminServerContextKeys_AdminUpdateAccount(t *testing.T) {
	_, admin := a4935Setup(t)
	tok := a4935Token(t, "root@ex.com", true)
	c, b := a4935Do(admin, http.MethodPut, "/api/v1/accounts/bob@ex.com", tok, `{"quota_limit":1000}`)
	t.Logf("EXPECTED: status=200 | ACTUAL: status=%d body=%s", c, strings.TrimSpace(b))
	if c != http.StatusOK {
		t.Fatalf("DEFECT F4935: admin port refused admin account update (status=%d)", c)
	}
}

// Edge: admin creating an admin account on the admin port succeeds with is_admin=true.
func TestAdminServerContextKeys_AdminCreateAdmin(t *testing.T) {
	s, admin := a4935Setup(t)
	tok := a4935Token(t, "root@ex.com", true)
	c, b := a4935Do(admin, http.MethodPost, "/api/v1/accounts", tok, `{"email":"ops@ex.com","password":"Str0ng!Passw0rd#x","is_admin":true}`)
	t.Logf("EXPECTED: status=201 | ACTUAL: status=%d body=%s", c, strings.TrimSpace(b))
	if c != http.StatusCreated {
		t.Fatalf("DEFECT F4935: admin port refused admin creating an admin account (status=%d)", c)
	}
	acc, err := s.db.GetAccount("ex.com", "ops")
	if err != nil {
		t.Fatalf("INVALID get: %v", err)
	}
	t.Logf("EXPECTED: IsAdmin=true | ACTUAL: IsAdmin=%v", acc.IsAdmin)
	if !acc.IsAdmin {
		t.Fatalf("admin-created admin account stored without admin flag")
	}
}

// Edge (after fix): a non-admin token still gets 403 on the admin port.
func TestAdminServerContextKeys_NonAdminStillForbidden(t *testing.T) {
	_, admin := a4935Setup(t)
	tok := a4935Token(t, "bob@ex.com", false)
	if c, _ := a4935Do(admin, http.MethodGet, "/api/v1/admin/vacations", tok, ""); c != http.StatusForbidden {
		t.Fatalf("non-admin status=%d, want 403", c)
	}
	if c, _ := a4935Do(admin, http.MethodPut, "/api/v1/accounts/bob@ex.com", tok, `{}`); c != http.StatusForbidden {
		t.Fatalf("non-admin update status=%d, want 403", c)
	}
}
