package api

// Regression tests promoted from the audit proofs in .temp_files (F5025).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const a5025Secret = "audit-5025-secret"

func a5025Setup(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!x"), 4)
	if err := database.CreateAccount(&db.AccountData{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com",
		PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatalf("INVALID create account: %v", err)
	}
	return NewServer(database, nil, Config{JWTSecret: a5025Secret, TokenExpiry: time.Hour})
}

var a5025IP int

func a5025Login(t *testing.T, s *Server) string {
	t.Helper()
	a5025IP++
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"bob@ex.com","password":"Passw0rd!x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-client/1.0")
	req.RemoteAddr = fmt.Sprintf("10.0.%d.%d:4000", a5025IP/250, a5025IP%250+1)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out struct {
		Token string `json:"token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Token == "" {
		t.Fatalf("INVALID login status=%d body=%s", rec.Code, rec.Body.String())
	}
	return out.Token
}

func a5025Req(s *Server, method, path, bearer string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

// a5025TwoLoginsSameSecond logs in twice within one wall-clock second
// (bounded retry, no sleeps) and returns both session tokens.
func a5025TwoLoginsSameSecond(t *testing.T, s *Server) (string, string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		before := time.Now().Unix()
		a := a5025Login(t, s)
		b := a5025Login(t, s)
		if time.Now().Unix() == before {
			return a, b
		}
	}
	t.Fatalf("INVALID: could not land two logins in one second")
	return "", ""
}

// Control: logging out a session leaves a session from a different second usable.
func TestLoginJTI_DistinctSecondsControl(t *testing.T) {
	s := a5025Setup(t)
	a := a5025Login(t, s)
	b := a5025Token2(t, s, a)
	if c := a5025Req(s, http.MethodPost, "/api/v1/auth/logout", b); c != http.StatusOK {
		t.Fatalf("INVALID control logout status=%d", c)
	}
	if c := a5025Req(s, http.MethodGet, "/api/v1/account/totp", a); c != http.StatusOK {
		t.Fatalf("INVALID control: unrelated session rejected (%d)", c)
	}
}

// a5025Token2 logs in until the token differs from a (a later second).
func a5025Token2(t *testing.T, s *Server, a string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b := a5025Login(t, s); b != a {
			return b
		}
	}
	t.Fatalf("INVALID: no distinct token")
	return ""
}

// Defect: two logins in the same second get byte-identical tokens, so logging
// out one device also logs out the other.
func TestLoginJTI_SameSecondLogoutKeepsOtherSession(t *testing.T) {
	s := a5025Setup(t)
	a, b := a5025TwoLoginsSameSecond(t, s)
	if c := a5025Req(s, http.MethodPost, "/api/v1/auth/logout", b); c != http.StatusOK {
		t.Fatalf("INVALID logout status=%d", c)
	}
	c := a5025Req(s, http.MethodGet, "/api/v1/account/totp", a)
	t.Logf("EXPECTED: distinct tokens, other session still 200 | ACTUAL: identical=%v status=%d", a == b, c)
	if a == b || c != http.StatusOK {
		t.Fatalf("regression F5025: logout of one login revoked a separate same-second login (identical=%v status=%d)", a == b, c)
	}
}

// Edge: three logins in one second are pairwise distinct; logging out one
// leaves the other two usable while the logged-out one stays revoked.
func TestLoginJTI_ThreeSameSecondLogins(t *testing.T) {
	s := a5025Setup(t)
	var toks []string
	for i := 0; i < 20; i++ {
		before := time.Now().Unix()
		toks = []string{a5025Login(t, s), a5025Login(t, s), a5025Login(t, s)}
		if time.Now().Unix() == before {
			break
		}
		toks = nil
	}
	if toks == nil {
		t.Fatalf("INVALID: no same-second triple")
	}
	if toks[0] == toks[1] || toks[1] == toks[2] || toks[0] == toks[2] {
		t.Fatalf("tokens not distinct")
	}
	if c := a5025Req(s, http.MethodPost, "/api/v1/auth/logout", toks[1]); c != http.StatusOK {
		t.Fatalf("logout=%d", c)
	}
	for i, want := range []int{200, 401, 200} {
		if c := a5025Req(s, http.MethodGet, "/api/v1/account/totp", toks[i]); c != want {
			t.Fatalf("token %d status=%d want %d", i, c, want)
		}
	}
}

// Edge: the login token carries a 128-bit jti, and a refresh of it yields a
// different jti.
func TestLoginJTI_ClaimShapeAndRefresh(t *testing.T) {
	s := a5025Setup(t)
	tok := a5025Login(t, s)
	claims := func(tok string) map[string]interface{} {
		p, err := base64.RawURLEncoding.DecodeString(strings.Split(tok, ".")[1])
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]interface{}{}
		_ = json.Unmarshal(p, &m)
		return m
	}
	j, _ := claims(tok)["jti"].(string)
	raw, err := base64.RawURLEncoding.DecodeString(j)
	if err != nil || len(raw) != 16 {
		t.Fatalf("login jti=%q len=%d", j, len(raw))
	}
	rec := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", tok)
	var out struct {
		Token string `json:"token"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("refresh=%d", rec.Code)
	}
	if j2, _ := claims(out.Token)["jti"].(string); j2 == "" || j2 == j {
		t.Fatalf("refresh jti=%q old=%q", j2, j)
	}
}
