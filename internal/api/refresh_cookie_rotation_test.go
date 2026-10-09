package api

// Regression tests promoted from the audit proofs in .temp_files (F4935-F4939).

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const a4937Secret = "audit-4937-secret"

func a4937Setup(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewServer(database, nil, Config{JWTSecret: a4937Secret, TokenExpiry: time.Hour})
}

func a4937Token(t *testing.T, sub string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": false, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(a4937Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return str
}

func a4937Refresh(s *Server, cookieTok, bearerTok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if cookieTok != "" {
		req.AddCookie(&http.Cookie{Name: "jwt", Value: cookieTok})
	}
	if bearerTok != "" {
		req.Header.Set("Authorization", "Bearer "+bearerTok)
	}
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func a4937Revoked(s *Server, tok string) bool {
	return s.IsTokenRevoked(fmt.Sprintf("%x", sha256.Sum256([]byte(tok))))
}

// Control: a Bearer-authenticated refresh revokes the old Bearer token.
func TestRefreshCookieRotation_Control(t *testing.T) {
	s := a4937Setup(t)
	old := a4937Token(t, "bob@ex.com")
	if rec := a4937Refresh(s, "", old); rec.Code != http.StatusOK {
		t.Fatalf("INVALID control: refresh status=%d", rec.Code)
	}
	if !a4937Revoked(s, old) {
		t.Fatalf("INVALID control: bearer token not revoked")
	}
	if rec := a4937Refresh(s, "", old); rec.Code != http.StatusUnauthorized {
		t.Fatalf("INVALID control: old bearer still accepted (%d)", rec.Code)
	}
}

// Defect: a cookie-authenticated (browser) refresh leaves the old cookie
// token valid, so refresh mints a second live session instead of rotating.
func TestRefreshCookieRotation_CookieRefreshRevokesOld(t *testing.T) {
	s := a4937Setup(t)
	old := a4937Token(t, "bob@ex.com")
	if rec := a4937Refresh(s, old, ""); rec.Code != http.StatusOK {
		t.Fatalf("INVALID refresh status=%d", rec.Code)
	}
	again := a4937Refresh(s, old, "")
	t.Logf("EXPECTED: old cookie token revoked, reuse status=401 | ACTUAL: revoked=%v reuse status=%d", a4937Revoked(s, old), again.Code)
	if again.Code != http.StatusUnauthorized {
		t.Fatalf("DEFECT F4937: old cookie token still valid after refresh (status=%d)", again.Code)
	}
}

// Edge: cookie refresh hands the browser a new HttpOnly cookie that works.
func TestRefreshCookieRotation_CookieRefreshSetsNewCookie(t *testing.T) {
	s := a4937Setup(t)
	old := a4937Token(t, "bob@ex.com")
	rec := a4937Refresh(s, old, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status=%d", rec.Code)
	}
	var newTok string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jwt" {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("new cookie lacks HttpOnly/SameSite=Strict")
			}
			newTok = c.Value
		}
	}
	if newTok == "" || newTok == old {
		t.Fatalf("no new jwt cookie set (got %q)", newTok)
	}
	if rec2 := a4937Refresh(s, newTok, ""); rec2.Code != http.StatusOK {
		t.Fatalf("new cookie token rejected: %d", rec2.Code)
	}
}

// Edge: cookie and a different bearer token both presented: both are revoked.
func TestRefreshCookieRotation_CookieAndBearerBothRevoked(t *testing.T) {
	s := a4937Setup(t)
	ck := a4937Token(t, "bob@ex.com")
	br := a4937Token(t, "carol@ex.com")
	if rec := a4937Refresh(s, ck, br); rec.Code != http.StatusOK {
		t.Fatalf("refresh status=%d", rec.Code)
	}
	if !a4937Revoked(s, ck) || !a4937Revoked(s, br) {
		t.Fatalf("revoked cookie=%v bearer=%v, want both", a4937Revoked(s, ck), a4937Revoked(s, br))
	}
}
