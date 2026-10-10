package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
)

const jmapTokenTestSecret = "jmap-token-test-secret-0123456789abcdef0123"

// newJMAPTokenTestServer builds the JMAP listener through startJMAP plus an
// API server sharing the same accounts database.
func newJMAPTokenTestServer(t *testing.T) (*Server, *api.Server) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	cfg.Security.JWTSecret = jmapTokenTestSecret
	cfg.JMAP.Enabled = true
	cfg.JMAP.Bind = "127.0.0.1"
	cfg.JMAP.Port = 0
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	for _, lp := range []string{"alice", "bob"} {
		if err := srv.database.CreateAccount(&db.AccountData{Email: lp + "@test.com", LocalPart: lp, Domain: "test.com", PasswordHash: "x", IsActive: true}); err != nil {
			t.Fatalf("CreateAccount %s: %v", lp, err)
		}
	}
	srv.startJMAP()
	if srv.jmapServer == nil {
		t.Fatal("startJMAP did not create the JMAP server")
	}
	return srv, api.NewServer(srv.database, srv.logger, api.Config{JWTSecret: jmapTokenTestSecret})
}

// jmapTestToken mirrors the token handleLogin issues.
func jmapTestToken(t *testing.T, sub, jti string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": false, "jti": jti,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	s, err := tok.SignedString([]byte(jmapTokenTestSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func jmapTokenStatus(h http.Handler, method, path, token string) int {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestJMAPRejectsLoggedOutToken: a token revoked by /api/v1/auth/logout must
// stop working on /jmap/* too (F5330), while another live token still works.
func TestJMAPRejectsLoggedOutToken(t *testing.T) {
	srv, apiSrv := newJMAPTokenTestServer(t)
	tok := jmapTestToken(t, "alice@test.com", "logout")
	live := jmapTestToken(t, "alice@test.com", "live")
	if c := jmapTokenStatus(srv.jmapServer, http.MethodGet, "/jmap/session", tok); c != http.StatusOK {
		t.Fatalf("token before logout: got %d, want 200", c)
	}
	if c := jmapTokenStatus(apiSrv, http.MethodPost, "/api/v1/auth/logout", tok); c != http.StatusOK {
		t.Fatalf("logout: got %d, want 200", c)
	}
	if c := jmapTokenStatus(srv.jmapServer, http.MethodGet, "/jmap/session", tok); c != http.StatusUnauthorized {
		t.Errorf("logged-out token on /jmap/session: got %d, want 401", c)
	}
	if c := jmapTokenStatus(srv.jmapServer, http.MethodPost, "/jmap/api", tok); c != http.StatusUnauthorized {
		t.Errorf("logged-out token on /jmap/api: got %d, want 401", c)
	}
	if c := jmapTokenStatus(srv.jmapServer, http.MethodGet, "/jmap/session", live); c != http.StatusOK {
		t.Errorf("other live token: got %d, want 200", c)
	}
}

// TestJMAPRejectsDisabledAccountToken: a token for an account disabled after
// issue must be rejected, as the HTTP API does (F5331).
func TestJMAPRejectsDisabledAccountToken(t *testing.T) {
	srv, _ := newJMAPTokenTestServer(t)
	tok := jmapTestToken(t, "bob@test.com", "bob")
	acct, err := srv.database.GetAccount("test.com", "bob")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	acct.IsActive = false
	if err := srv.database.UpdateAccount(acct); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if c := jmapTokenStatus(srv.jmapServer, http.MethodGet, "/jmap/session", tok); c != http.StatusUnauthorized {
		t.Errorf("disabled account token: got %d, want 401", c)
	}
	acct.IsActive = true
	if err := srv.database.UpdateAccount(acct); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if c := jmapTokenStatus(srv.jmapServer, http.MethodGet, "/jmap/session", tok); c != http.StatusOK {
		t.Errorf("re-enabled account token: got %d, want 200", c)
	}
}
