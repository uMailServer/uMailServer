package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const adminRevokedSecret = "audit-4846-secret"

func adminRevokedSetup(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminRevokedSecret, TokenExpiry: time.Hour})
	as := NewAdminServer(s, AdminConfig{Addr: "127.0.0.1:0", JWTSecret: adminRevokedSecret})
	return s, as.router()
}

func adminRevokedToken(t *testing.T, sub string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": true, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(adminRevokedSecret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return str
}

func adminRevokedDo(h http.Handler, method, path, token string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func adminRevokedLogout(t *testing.T, s *Server, token string) {
	t.Helper()
	if c := adminRevokedDo(s, http.MethodPost, "/api/v1/auth/logout", token); c != http.StatusOK {
		t.Fatalf("INVALID logout status=%d", c)
	}
	if c := adminRevokedDo(s, http.MethodGet, "/api/v1/domains", token); c != http.StatusUnauthorized {
		t.Fatalf("INVALID: main router still accepts the logged-out token (status=%d)", c)
	}
}

// Control: a live admin token is accepted on the admin port, and the main
// router rejects it after logout.
func TestAdminServerRevokedToken_Control(t *testing.T) {
	s, admin := adminRevokedSetup(t)
	tok := adminRevokedToken(t, "root@ex.com")
	if c := adminRevokedDo(admin, http.MethodGet, "/api/v1/domains", tok); c != http.StatusOK {
		t.Fatalf("INVALID control: admin router status=%d", c)
	}
	adminRevokedLogout(t, s, tok)
}

// Defect: the admin port must reject a token revoked by logout.
func TestAdminServerRevokedToken_RevokedRejected(t *testing.T) {
	s, admin := adminRevokedSetup(t)
	tok := adminRevokedToken(t, "root@ex.com")
	adminRevokedLogout(t, s, tok)
	c := adminRevokedDo(admin, http.MethodGet, "/api/v1/domains", tok)
	t.Logf("EXPECTED: status=401 | ACTUAL: status=%d", c)
	if c != http.StatusUnauthorized {
		t.Fatalf("DEFECT F4846: admin port accepted a revoked token (status=%d)", c)
	}
}

// Edge: revocation of one token does not affect another live admin token,
// and a revoked token is rejected on every admin route, not only domains.
func TestAdminServerRevokedToken_EdgeOtherTokenAndRoutes(t *testing.T) {
	s, admin := adminRevokedSetup(t)
	revoked := adminRevokedToken(t, "root@ex.com")
	live := adminRevokedToken(t, "ops@ex.com")
	adminRevokedLogout(t, s, revoked)
	if c := adminRevokedDo(admin, http.MethodGet, "/api/v1/domains", live); c != http.StatusOK {
		t.Fatalf("live token rejected: %d", c)
	}
	for _, p := range []string{"/api/v1/accounts", "/api/v1/stats", "/api/v1/admin/queue"} {
		if c := adminRevokedDo(admin, http.MethodGet, p, revoked); c != http.StatusUnauthorized {
			t.Fatalf("revoked token on %s: status=%d", p, c)
		}
	}
}

// Edge: revocation store failure fails closed on the admin port too.
func TestAdminServerRevokedToken_EdgeClosedDBFailsClosed(t *testing.T) {
	s, admin := adminRevokedSetup(t)
	tok := adminRevokedToken(t, "root@ex.com")
	s.db.Close()
	if c := adminRevokedDo(admin, http.MethodGet, "/api/v1/domains", tok); c != http.StatusUnauthorized {
		t.Fatalf("closed revocation store: status=%d, want 401", c)
	}
}
