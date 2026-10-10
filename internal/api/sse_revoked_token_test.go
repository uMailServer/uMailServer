package api

// Regression tests promoted from the audit proofs in .temp_files (F4935-F4939).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const a4936Secret = "audit-4936-secret"

func a4936Setup(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	seedSessionAccount(t, database, "bob@ex.com", false)
	seedSessionAccount(t, database, "root@ex.com", true)
	return NewServer(database, nil, Config{JWTSecret: a4936Secret, TokenExpiry: time.Hour})
}

func a4936Token(t *testing.T, sub string, admin bool) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": admin, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(a4936Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return str
}

func a4936Logout(t *testing.T, s *Server, token string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("INVALID logout status=%d", rec.Code)
	}
}

// a4936Events opens /api/v1/events with an already-cancelled context so the
// SSE loop returns right after the "connected" event (no timing involved).
func a4936Events(s *Server, cookieTok string, hdr map[string]string) (int, string) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	if cookieTok != "" {
		req.AddCookie(&http.Cookie{Name: "jwt", Value: cookieTok})
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Control: a live cookie plus the same live token in X-Auth-Token connects
// as that user.
func TestSSERevokedToken_Control(t *testing.T) {
	s := a4936Setup(t)
	bob := a4936Token(t, "bob@ex.com", false)
	c, b := a4936Events(s, bob, map[string]string{"X-Auth-Token": bob})
	if c != http.StatusOK || !strings.Contains(b, `"user":"bob@ex.com"`) {
		t.Fatalf("INVALID control: status=%d body=%q", c, b)
	}
}

// Defect: a logged-out (revoked) admin token in X-Auth-Token opens an SSE
// stream as that admin, because authMiddleware checked only the cookie and
// the SSE authFunc never consults the revocation list.
func TestSSERevokedToken_RevokedXAuthToken(t *testing.T) {
	s := a4936Setup(t)
	bob := a4936Token(t, "bob@ex.com", false)
	root := a4936Token(t, "root@ex.com", true)
	a4936Logout(t, s, root)
	c, b := a4936Events(s, bob, map[string]string{"X-Auth-Token": root})
	t.Logf("EXPECTED: status=401 | ACTUAL: status=%d body=%q", c, strings.TrimSpace(b))
	if c != http.StatusUnauthorized {
		t.Fatalf("DEFECT F4936: SSE accepted a revoked token (status=%d)", c)
	}
}

// Edge: same bypass through Authorization: Bearer while the cookie authenticates.
func TestSSERevokedToken_RevokedBearer(t *testing.T) {
	s := a4936Setup(t)
	bob := a4936Token(t, "bob@ex.com", false)
	root := a4936Token(t, "root@ex.com", true)
	a4936Logout(t, s, root)
	c, b := a4936Events(s, bob, map[string]string{"Authorization": "Bearer " + root})
	if c != http.StatusUnauthorized {
		t.Fatalf("DEFECT F4936: SSE accepted a revoked bearer token (status=%d body=%q)", c, b)
	}
}

// Edge: revoking one token does not break SSE for another live token, and the
// cookie-only browser path still works.
func TestSSERevokedToken_LiveTokensUnaffected(t *testing.T) {
	s := a4936Setup(t)
	bob := a4936Token(t, "bob@ex.com", false)
	root := a4936Token(t, "root@ex.com", true)
	a4936Logout(t, s, root)
	if c, b := a4936Events(s, bob, nil); c != http.StatusOK || !strings.Contains(b, `"user":"bob@ex.com"`) {
		t.Fatalf("cookie-only SSE: status=%d body=%q", c, b)
	}
	if c, _ := a4936Events(s, "", map[string]string{"Authorization": "Bearer " + bob}); c != http.StatusOK {
		t.Fatalf("bearer-only SSE: status=%d", c)
	}
}
