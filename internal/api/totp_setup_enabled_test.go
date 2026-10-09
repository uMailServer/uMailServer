package api

// Regression tests promoted from the audit proofs in .temp_files (F5028).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const a5028Secret = "audit-5028-secret"

// a5028Setup seeds bob@ex.com with TOTP enabled on a known secret.
func a5028Setup(t *testing.T) (*Server, string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	totp, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("INVALID secret: %v", err)
	}
	enc, err := auth.EncryptTOTPSecret(totp, a5028Secret)
	if err != nil {
		t.Fatalf("INVALID encrypt: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!x"), 4)
	if err := database.CreateAccount(&db.AccountData{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com",
		PasswordHash: string(hash), IsActive: true, TOTPEnabled: true, TOTPSecret: enc}); err != nil {
		t.Fatalf("INVALID create account: %v", err)
	}
	return NewServer(database, nil, Config{JWTSecret: a5028Secret, TokenExpiry: time.Hour}), totp
}

func a5028Session(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "bob@ex.com", "admin": false, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "jti": "a5028",
	})
	tok.Header["kid"] = "default"
	s, err := tok.SignedString([]byte(a5028Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return s
}

func a5028Do(s *Server, method, path, bearer, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

func a5028Login(t *testing.T, s *Server, code string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(fmt.Sprintf(`{"email":"bob@ex.com","password":"Passw0rd!x","totp_code":%q}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-client/1.0")
	req.RemoteAddr = "198.51.100.20:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

// Control: the enrolled authenticator logs in.
func TestTOTPSetupEnabled_EnrolledLogin(t *testing.T) {
	s, totp := a5028Setup(t)
	if c := a5028Login(t, s, totpCodeAt(t, totp, time.Now())); c != http.StatusOK {
		t.Fatalf("INVALID control: login status=%d", c)
	}
}

// Defect: calling setup while 2FA is enabled silently replaces the live,
// verified secret with an unverified one; the enrolled authenticator stops
// working while the account still demands TOTP.
func TestTOTPSetupEnabled_SetupKeepsEnrolledSecret(t *testing.T) {
	s, totp := a5028Setup(t)
	setup := a5028Do(s, http.MethodPost, "/api/v1/account/totp/setup", a5028Session(t), "{}")
	login := a5028Login(t, s, totpCodeAt(t, totp, time.Now()))
	t.Logf("EXPECTED: setup refused (409) and enrolled code logs in (200) | ACTUAL: setup=%d login=%d", setup, login)
	if login != http.StatusOK {
		t.Fatalf("regression F5028: re-running TOTP setup while enabled broke the enrolled authenticator (setup=%d login=%d)", setup, login)
	}
}

// Edge: setup while enabled returns 409 and leaves the stored secret intact.
func TestTOTPSetupEnabled_Conflict(t *testing.T) {
	s, _ := a5028Setup(t)
	before, _ := s.db.GetAccount("ex.com", "bob")
	if c := a5028Do(s, http.MethodPost, "/api/v1/account/totp/setup", a5028Session(t), "{}"); c != http.StatusConflict {
		t.Fatalf("setup=%d want 409", c)
	}
	after, _ := s.db.GetAccount("ex.com", "bob")
	if after.TOTPSecret != before.TOTPSecret || !after.TOTPEnabled {
		t.Fatalf("secret changed or 2FA disabled")
	}
}

// Edge: disable then setup is allowed, and a pending (not yet verified)
// setup can still be regenerated.
func TestTOTPSetupEnabled_DisableThenSetup(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	if c := a5028Do(s, http.MethodPost, "/api/v1/account/totp/disable", tok, "{}"); c != http.StatusOK {
		t.Fatalf("disable=%d", c)
	}
	for i := 0; i < 2; i++ {
		if c := a5028Do(s, http.MethodPost, "/api/v1/account/totp/setup", tok, "{}"); c != http.StatusOK {
			t.Fatalf("setup #%d=%d want 200", i+1, c)
		}
	}
}

// Edge: the admin route cannot overwrite a non-admin user's enabled secret either.
func TestTOTPSetupEnabled_AdminPath(t *testing.T) {
	s, totp := a5028Setup(t)
	tk := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "root@ex.com", "admin": true, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "jti": "adm",
	})
	tk.Header["kid"] = "default"
	adm, _ := tk.SignedString([]byte(a5028Secret))
	if c := a5028Do(s, http.MethodPost, "/api/v1/accounts/bob@ex.com/totp/setup", adm, "{}"); c != http.StatusConflict {
		t.Fatalf("admin setup=%d want 409", c)
	}
	if c := a5028Login(t, s, totpCodeAt(t, totp, time.Now())); c != http.StatusOK {
		t.Fatalf("enrolled login=%d", c)
	}
}
