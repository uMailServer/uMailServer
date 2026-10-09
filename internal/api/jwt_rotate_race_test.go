package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const jwtRotateRaceSecret = "audit-4847-secret"

func jwtRotateRaceSetup(t *testing.T) (*Server, string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("INVALID create domain: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw-123456"), bcrypt.MinCost)
	if err := database.CreateAccount(&db.AccountData{Email: "u@ex.com", LocalPart: "u", Domain: "ex.com", PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatalf("INVALID create account: %v", err)
	}
	s := NewServer(database, nil, Config{JWTSecret: jwtRotateRaceSecret, TokenExpiry: time.Hour})
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "root@ex.com", "admin": true, "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(jwtRotateRaceSecret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	// Warm the lazily-built router serially so only the map access is concurrent.
	if c := jwtRotateRaceDo(s, http.MethodGet, "/api/v1/domains", str); c != http.StatusOK {
		t.Fatalf("INVALID warm-up status=%d", c)
	}
	return s, str
}

func jwtRotateRaceLogin(h http.Handler) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"u@ex.com","password":"pw-123456"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:4001"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func jwtRotateRaceDo(h http.Handler, method, path, token string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// Control: serial rotate, then the pre-rotation token still authenticates
// (old kid retained) and a freshly issued login-style token uses the new kid.
func TestJWTRotateRace_Control(t *testing.T) {
	s, tok := jwtRotateRaceSetup(t)
	if c := jwtRotateRaceDo(s, http.MethodPost, "/api/v1/admin/jwt/rotate", tok); c != http.StatusOK {
		t.Fatalf("INVALID rotate status=%d", c)
	}
	if c := jwtRotateRaceDo(s, http.MethodGet, "/api/v1/domains", tok); c != http.StatusOK {
		t.Fatalf("INVALID old token after rotate status=%d", c)
	}
}

// Defect: rotation concurrent with a login (which reads currentKid/jwtSecrets
// to sign). Both goroutines
// are released by one barrier and joined by a WaitGroup; there is no
// happens-before between them, so the race detector reports the unsynchronised
// jwtSecrets/currentKid accesses regardless of scheduling.
func TestJWTRotateRace_ConcurrentRotate(t *testing.T) {
	s, tok := jwtRotateRaceSetup(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		codes[0] = jwtRotateRaceLogin(s)
	}()
	go func() {
		defer wg.Done()
		<-start
		codes[1] = jwtRotateRaceDo(s, http.MethodPost, "/api/v1/admin/jwt/rotate", tok)
	}()
	close(start)
	wg.Wait()
	if c := jwtRotateRaceLogin(s); c != http.StatusOK {
		t.Fatalf("login after rotate: %d", c)
	}
	t.Logf("EXPECTED: no data race, statuses 200/200 | ACTUAL: statuses %d/%d (see race report if any)", codes[0], codes[1])
	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("unexpected statuses %v", codes)
	}
}

// Edge: repeated rotations interleaved with status reads and SSE-style token
// validation, then the token still works after pruning keeps the newest keys.
func TestJWTRotateRace_EdgeRepeatedRotateStatusAndLogin(t *testing.T) {
	s, tok := jwtRotateRaceSetup(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); <-start; jwtRotateRaceDo(s, http.MethodPost, "/api/v1/admin/jwt/rotate", tok) }()
		go func() { defer wg.Done(); <-start; jwtRotateRaceDo(s, http.MethodGet, "/api/v1/admin/jwt/status", tok) }()
		go func() { defer wg.Done(); <-start; _ = jwtRotateRaceLogin(s) }()
	}
	close(start)
	wg.Wait()
	if c := jwtRotateRaceDo(s, http.MethodGet, "/api/v1/domains", tok); c != http.StatusOK {
		t.Fatalf("default-kid token after 3 rotations: %d", c)
	}
}
