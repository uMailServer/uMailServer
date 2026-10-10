package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func jwtRotRandHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// newJWTRotationTestServer wires JMAP and the HTTP API through startJMAP and
// startAPI (the production order). secret "" leaves jwt_secret unset. When
// withAPI is false only JMAP is started.
func newJWTRotationTestServer(t *testing.T, secret string, disableLegacy, withAPI bool) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	cfg.Security.JWTSecret = secret
	cfg.Security.DisableLegacyJWT = disableLegacy
	cfg.JMAP.Enabled = true
	cfg.JMAP.Bind = "127.0.0.1"
	cfg.JMAP.Port = 0
	cfg.HTTP.Bind = "127.0.0.1"
	cfg.HTTP.Port = 0
	cfg.Admin.Enabled = false
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	pw := "pw-" + jwtRotRandHex(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), 4)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if err := srv.database.CreateAccount(&db.AccountData{Email: "alice@test.com", LocalPart: "alice", Domain: "test.com", PasswordHash: string(hash), IsActive: true, IsAdmin: true}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	srv.startJMAP()
	if withAPI {
		srv.startAPI()
	}
	return srv, pw
}

func jwtRotDo(h http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func jwtRotLogin(t *testing.T, srv *Server, pw string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": pw})
	rec := jwtRotDo(srv.apiServer, http.MethodPost, "/api/v1/auth/login", "", body)
	var out struct {
		Token string `json:"token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Token == "" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	return out.Token
}

func jwtRotRotate(t *testing.T, srv *Server, tok string) {
	t.Helper()
	if rec := jwtRotDo(srv.apiServer, http.MethodPost, "/api/v1/admin/jwt/rotate", tok, []byte("{}")); rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
}

func jwtRotAPI(srv *Server, tok string) int {
	return jwtRotDo(srv.apiServer, http.MethodGet, "/api/v1/admin/jwt/status", tok, nil).Code
}

func jwtRotJMAP(srv *Server, tok string) int {
	return jwtRotDo(srv.jmapServer, http.MethodGet, "/jmap/session", tok, nil).Code
}

func jwtRotForge(t *testing.T, key []byte) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "alice@test.com", "admin": true, "jti": jwtRotRandHex(t),
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// TestJMAPAcceptsRotatedKeyTokens: after POST /api/v1/admin/jwt/rotate, the
// tokens the API issues are signed with the new kid; JMAP must accept them
// (F5440). Pre-rotation tokens keep working when legacy keys are allowed.
func TestJMAPAcceptsRotatedKeyTokens(t *testing.T) {
	srv, pw := newJWTRotationTestServer(t, "jwt-rot-test-"+jwtRotRandHex(t), false, true)
	before := jwtRotLogin(t, srv, pw)
	jwtRotRotate(t, srv, before)
	after := jwtRotLogin(t, srv, pw)
	if c := jwtRotAPI(srv, after); c != http.StatusOK {
		t.Fatalf("API post-rotation token: %d", c)
	}
	if c := jwtRotJMAP(srv, after); c != http.StatusOK {
		t.Errorf("JMAP post-rotation token: got %d, want 200", c)
	}
	if c := jwtRotJMAP(srv, before); c != http.StatusOK {
		t.Errorf("JMAP pre-rotation token (legacy allowed): got %d, want 200", c)
	}
}

// TestJMAPRejectsEmptyKeyTokenWhenSecretUnset: with jwt_secret unset (valid
// config), a token HMAC'd with an empty key must not authenticate on JMAP
// (F5441), with or without the API resolver installed.
func TestJMAPRejectsEmptyKeyTokenWhenSecretUnset(t *testing.T) {
	forged := jwtRotForge(t, []byte{})

	srv, pw := newJWTRotationTestServer(t, "", false, true)
	if c := jwtRotJMAP(srv, forged); c != http.StatusUnauthorized {
		t.Errorf("JMAP with API resolver, empty-key token: got %d, want 401", c)
	}
	if c := jwtRotJMAP(srv, jwtRotLogin(t, srv, pw)); c != http.StatusOK {
		t.Errorf("JMAP with API resolver, real login token: got %d, want 200", c)
	}

	jmapOnly, _ := newJWTRotationTestServer(t, "", false, false)
	if c := jwtRotJMAP(jmapOnly, forged); c != http.StatusUnauthorized {
		t.Errorf("JMAP without API resolver, empty-key token: got %d, want 401", c)
	}
}

// TestDisableLegacyJWTRetiresLegacyKeyAfterRotation: with DisableLegacyJWT,
// a token signed with the legacy secret stops working on the API and JMAP
// once the signing key has been rotated (F5442), but not before.
func TestDisableLegacyJWTRetiresLegacyKeyAfterRotation(t *testing.T) {
	srv, pw := newJWTRotationTestServer(t, "jwt-rot-test-"+jwtRotRandHex(t), true, true)
	legacy := jwtRotLogin(t, srv, pw)
	if c := jwtRotAPI(srv, legacy); c != http.StatusOK {
		t.Fatalf("API pre-rotation token: got %d, want 200", c)
	}
	jwtRotRotate(t, srv, legacy)
	if c := jwtRotAPI(srv, legacy); c != http.StatusUnauthorized {
		t.Errorf("API legacy token after rotation: got %d, want 401", c)
	}
	if c := jwtRotJMAP(srv, legacy); c != http.StatusUnauthorized {
		t.Errorf("JMAP legacy token after rotation: got %d, want 401", c)
	}
	after := jwtRotLogin(t, srv, pw)
	if c := jwtRotAPI(srv, after); c != http.StatusOK {
		t.Errorf("API post-rotation token: got %d, want 200", c)
	}
	if c := jwtRotJMAP(srv, after); c != http.StatusOK {
		t.Errorf("JMAP post-rotation token: got %d, want 200", c)
	}
}
