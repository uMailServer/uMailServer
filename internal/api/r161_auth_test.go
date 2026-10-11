package api

import (
	"github.com/golang-jwt/jwt/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// F6421: unknown-account logins must cost as much as real hash checks.
func TestDummyPasswordHashCostMatchesRealHashes(t *testing.T) {
	c, err := bcrypt.Cost([]byte(dummyPasswordHash))
	if err != nil {
		t.Fatal(err)
	}
	if c < bcrypt.DefaultCost {
		t.Fatalf("dummy hash cost %d < real cost %d: login timing reveals account existence", c, bcrypt.DefaultCost)
	}
}

// F6422: self-service password change enforces the same strength policy as
// account creation / admin reset.
func TestSelfServicePasswordChangeEnforcesPolicy(t *testing.T) {
	server, current, database := setupAccountPasswordTest(t)
	rec := postAccountPassword(server, "user@example.com", current, "alllowercase")
	if rec.Code != 400 {
		t.Fatalf("weak new password: status %d, want 400", rec.Code)
	}
	acc, _ := database.GetAccount("example.com", "user")
	if ok, _ := server.verifyPassword(current, acc.PasswordHash); !ok {
		t.Fatal("rejected change altered the password")
	}
}

// F6423: a validly signed token without exp must be rejected.
func TestAuthRejectsTokenWithoutExp(t *testing.T) {
	s := NewServer(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{JWTSecret: strings.Repeat("k", 32), TokenExpiry: time.Hour})
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "a@b.c"}).SignedString([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/api/v1/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("exp-less token accepted: %d", rr.Code)
	}
}
