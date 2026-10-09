package api

// Regression tests promoted from the audit proofs in .temp_files (F5026, F5027).

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

func a5026Setup(t *testing.T, maxAttempts int) *Server {
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
	return NewServer(database, nil, Config{JWTSecret: "audit-5026", TokenExpiry: time.Hour, MaxLoginAttempts: maxAttempts})
}

// a5026Login posts a login from ip and returns the status, or -1 if the
// handler panicked.
func a5026Login(s *Server, ip, email, pw string) (code int) {
	defer func() {
		if recover() != nil {
			code = -1
		}
	}()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(fmt.Sprintf(`{"email":%q,"password":%q}`, email, pw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-client/1.0")
	req.RemoteAddr = ip + ":4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

// a5026Run sends n failed logins (distinct accounts so the per-account
// limiter never fires) then one more and returns all statuses.
func a5026Run(s *Server, n int) []int {
	var codes []int
	for i := 0; i <= n; i++ {
		codes = append(codes, a5026Login(s, "198.51.100.7", fmt.Sprintf("u%d@ex.com", i), "wrong"))
	}
	return codes
}

// Control: the default limit (5) never panics.
func TestLoginIPLimit_DefaultNoPanic(t *testing.T) {
	s := a5026Setup(t, 5)
	for _, c := range a5026Run(s, 8) {
		if c == -1 {
			t.Fatalf("INVALID control: panic at default limit")
		}
	}
}

// Defect: max_login_attempts below 5 makes the backoff shift negative, so the
// login handler panics instead of returning 429.
func TestLoginIPLimit_LowLimitLocksWithoutPanic(t *testing.T) {
	s := a5026Setup(t, 3)
	codes := a5026Run(s, 3)
	t.Logf("EXPECTED: [401 401 401 429] | ACTUAL: %v (-1 = panic)", codes)
	for _, c := range codes {
		if c == -1 {
			t.Fatalf("regression F5026: login handler panicked with max_login_attempts=3: %v", codes)
		}
	}
	if codes[3] != http.StatusTooManyRequests {
		t.Fatalf("regression F5026: no lockout after 3 failures: %v", codes)
	}
}

// Control: two wrong passwords then the right one succeeds.
func TestLoginIPLimit_TwoFailuresThenSuccess(t *testing.T) {
	s := a5026Setup(t, 5)
	for i := 0; i < 2; i++ {
		a5026Login(s, "198.51.100.8", "bob@ex.com", "wrong")
	}
	if c := a5026Login(s, "198.51.100.8", "bob@ex.com", "Passw0rd!x"); c != http.StatusOK {
		t.Fatalf("INVALID control: status=%d", c)
	}
}

// Defect: with max_login_attempts=5, three wrong passwords (each counted
// twice) lock the IP, and so do six successful logins from one shared IP.
func TestLoginIPLimit_CountsFailuresOnceNotSuccesses(t *testing.T) {
	s := a5026Setup(t, 5)
	var fails []int
	for i := 0; i < 3; i++ {
		fails = append(fails, a5026Login(s, "198.51.100.9", fmt.Sprintf("x%d@ex.com", i), "wrong"))
	}
	afterThree := a5026Login(s, "198.51.100.9", "bob@ex.com", "Passw0rd!x")

	s2 := a5026Setup(t, 5)
	var ok []int
	for i := 0; i < 6; i++ {
		ok = append(ok, a5026Login(s2, "198.51.100.10", "bob@ex.com", "Passw0rd!x"))
	}
	t.Logf("EXPECTED: 4th login after 3 failures 200; 6 successful logins all 200 | ACTUAL: %v -> %d; %v", fails, afterThree, ok)
	if afterThree != http.StatusOK || ok[5] != http.StatusOK {
		t.Fatalf("regression F5027: IP limiter locks out before max_login_attempts failures (after 3 failures=%d, 6th success=%d)", afterThree, ok[5])
	}
}

// Edge F5026: max_login_attempts=1 locks after one failure without panicking.
func TestLoginIPLimit_MaxOne(t *testing.T) {
	s := a5026Setup(t, 1)
	codes := a5026Run(s, 1)
	if codes[0] != http.StatusUnauthorized || codes[1] != http.StatusTooManyRequests {
		t.Fatalf("codes=%v want [401 429]", codes)
	}
}

// Edge F5026: a count far beyond a large limit still yields a capped lockout
// (no shift overflow into a zero/negative backoff).
func TestLoginIPLimit_HugeCountCapped(t *testing.T) {
	s := a5026Setup(t, 100)
	s.loginMu.Lock()
	s.loginAttempts = map[string]*loginAttempt{"203.0.113.1": {count: 400, lastSeen: time.Now()}}
	s.loginMu.Unlock()
	if s.checkLoginRateLimit("203.0.113.1") {
		t.Fatalf("over-limit IP allowed")
	}
	s.loginMu.Lock()
	until := s.loginAttempts["203.0.113.1"].lockoutUntil
	s.loginMu.Unlock()
	if d := time.Until(until); d < 59*time.Minute || d > 61*time.Minute {
		t.Fatalf("lockout=%v want ~60m", d)
	}
}

// Edge F5027: the default limit still locks after exactly 5 failures, and a
// successful login in between does not reset or add to the failure count.
func TestLoginIPLimit_ExactLimit(t *testing.T) {
	s := a5026Setup(t, 5)
	ip := "198.51.100.11"
	var codes []int
	for i := 0; i < 4; i++ {
		codes = append(codes, a5026Login(s, ip, fmt.Sprintf("y%d@ex.com", i), "wrong"))
	}
	codes = append(codes, a5026Login(s, ip, "bob@ex.com", "Passw0rd!x"))
	codes = append(codes, a5026Login(s, ip, "y9@ex.com", "wrong"))
	codes = append(codes, a5026Login(s, ip, "bob@ex.com", "Passw0rd!x"))
	want := []int{401, 401, 401, 401, 200, 401, 429}
	if fmt.Sprint(codes) != fmt.Sprint(want) {
		t.Fatalf("codes=%v want %v", codes, want)
	}
}
