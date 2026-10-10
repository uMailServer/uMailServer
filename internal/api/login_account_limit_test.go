package api

// Regression tests for F5135: checkAccountLoginRateLimit incremented the
// per-account counter on every attempt and recordAccountLoginFailure
// incremented it again, so each failed login counted twice and three wrong
// passwords locked the account (documented budget: 5 failures per 5 min).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func f5135Server(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!x"), 4)
	if err := database.CreateAccount(&db.AccountData{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com",
		PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return NewServer(database, nil, Config{JWTSecret: "f5135-account-limit", TokenExpiry: time.Hour})
}

// f5135Login uses a distinct client IP per call so only the per-account
// limiter can refuse the request.
func f5135Login(s *Server, n int, pw string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(fmt.Sprintf(`{"email":"bob@ex.com","password":%q}`, pw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "f5135-client/1.0")
	// F6134: the budget is per (IP, account); n < 100 reuses one client IP
	// (the per-IP limiter has its own tests), larger n is a different IP.
	if n >= 100 {
		req.RemoteAddr = fmt.Sprintf("198.51.100.%d:4000", n)
	} else {
		req.RemoteAddr = "198.51.100.1:4000"
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

func TestAccountLoginLimit_FourFailuresThenSuccess(t *testing.T) {
	s := f5135Server(t)
	for i := 0; i < 4; i++ {
		if c := f5135Login(s, i, "wrong"); c != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i+1, c)
		}
	}
	if c := f5135Login(s, 10, "Passw0rd!x"); c != http.StatusOK {
		t.Fatalf("F5135: correct password after 4 failures = %d, want 200", c)
	}
	s.accountLoginMu.Lock()
	_, left := s.accountLoginAttempts[accountLoginKey("198.51.100.1", "bob@ex.com")]
	s.accountLoginMu.Unlock()
	if left {
		t.Fatal("successful login did not clear the account budget")
	}
}

func TestAccountLoginLimit_FiveFailuresLock(t *testing.T) {
	s := f5135Server(t)
	for i := 0; i < 5; i++ {
		if c := f5135Login(s, i, "wrong"); c != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i+1, c)
		}
	}
	if c := f5135Login(s, 10, "Passw0rd!x"); c != http.StatusTooManyRequests {
		t.Fatalf("correct password after 5 failures = %d, want 429", c)
	}
}

func TestAccountLoginLimit_SuccessesDoNotCount(t *testing.T) {
	s := f5135Server(t)
	for i := 0; i < 8; i++ {
		if c := f5135Login(s, i, "Passw0rd!x"); c != http.StatusOK {
			t.Fatalf("successful login %d = %d, want 200", i+1, c)
		}
	}
}

func TestAccountLoginLimit_ExpiredWindowResets(t *testing.T) {
	s := f5135Server(t)
	s.accountLoginMu.Lock()
	s.accountLoginAttempts = map[string]*loginAttempt{
		accountLoginKey("198.51.100.1", "bob@ex.com"): {count: 5, lastSeen: time.Now().Add(-6 * time.Minute)},
	}
	s.accountLoginMu.Unlock()
	if c := f5135Login(s, 0, "wrong"); c != http.StatusUnauthorized {
		t.Fatalf("first failure in a new window = %d, want 401", c)
	}
	s.accountLoginMu.Lock()
	count := s.accountLoginAttempts[accountLoginKey("198.51.100.1", "bob@ex.com")].count
	s.accountLoginMu.Unlock()
	if count != 1 {
		t.Fatalf("count after expired window + 1 failure = %d, want 1", count)
	}
}
