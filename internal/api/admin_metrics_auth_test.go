package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const adminMetricsSecret = "audit-4848-secret"

func adminMetricsRouter(t *testing.T) http.Handler {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})
	return NewAdminServer(s, AdminConfig{Addr: "127.0.0.1:0", JWTSecret: adminMetricsSecret}).router()
}

func adminMetricsToken(t *testing.T, admin bool) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "x@ex.com", "admin": admin, "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(adminMetricsSecret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return str
}

func adminMetricsGet(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Control: the same metrics handler under /api/v1/metrics on the admin port
// requires authentication, and an admin token gets it.
func TestAdminServerMetricsAuth_Control(t *testing.T) {
	h := adminMetricsRouter(t)
	if rec := adminMetricsGet(h, "/api/v1/metrics", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("INVALID control: /api/v1/metrics unauth status=%d", rec.Code)
	}
	if rec := adminMetricsGet(h, "/api/v1/metrics", adminMetricsToken(t, true)); rec.Code != http.StatusOK {
		t.Fatalf("INVALID control: /api/v1/metrics admin status=%d", rec.Code)
	}
}

// Defect: /metrics on the admin port serves the same stats with no credentials.
func TestAdminServerMetricsAuth_MetricsRequiresAuth(t *testing.T) {
	h := adminMetricsRouter(t)
	rec := adminMetricsGet(h, "/metrics", "")
	t.Logf("EXPECTED: status=401 | ACTUAL: status=%d body=%.120s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DEFECT F4848: unauthenticated /metrics on admin port (status=%d)", rec.Code)
	}
}

// Edge: a non-admin token is forbidden; an admin token still reads /metrics;
// a garbage token is unauthorized.
func TestAdminServerMetricsAuth_EdgeRoles(t *testing.T) {
	h := adminMetricsRouter(t)
	if rec := adminMetricsGet(h, "/metrics", adminMetricsToken(t, false)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin /metrics status=%d", rec.Code)
	}
	if rec := adminMetricsGet(h, "/metrics", adminMetricsToken(t, true)); rec.Code != http.StatusOK {
		t.Fatalf("admin /metrics status=%d", rec.Code)
	}
	if rec := adminMetricsGet(h, "/metrics", "not.a.jwt"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("garbage token /metrics status=%d", rec.Code)
	}
}
