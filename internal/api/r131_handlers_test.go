package api

// Round 131 (F6130-F6139) regression tests for account/alias/domain
// handlers: sentinel error mapping, DKIM failure, login lockout isolation,
// TOTP key case, validation parity and response shapes.

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func r131Server(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!x"), 4)
	if err := database.CreateAccount(&db.AccountData{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com",
		PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	return NewServer(database, nil, Config{JWTSecret: "r131-secret", TokenExpiry: time.Hour})
}

// r131Do calls h as an authenticated admin (or user) with the given body.
func r131Do(h func(http.ResponseWriter, *http.Request), method, path, body, user string, admin bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = context.WithValue(ctx, "isAdmin", admin)
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	return rec
}

func TestR131_F6130_DeleteMissingIs404(t *testing.T) {
	s := r131Server(t)
	cases := []struct {
		name string
		h    func(http.ResponseWriter, *http.Request)
		path string
	}{
		{"account", s.handleAccountDetail, "/api/v1/accounts/ghost@ex.com"},
		{"alias", s.handleAliasDetail, "/api/v1/aliases/ghost@ex.com"},
		{"domain", s.handleDomainDetail, "/api/v1/domains/ghost.com"},
	}
	for _, c := range cases {
		if rec := r131Do(c.h, http.MethodDelete, c.path, "", "admin@ex.com", true); rec.Code != http.StatusNotFound {
			t.Errorf("%s: DELETE missing = %d, want 404", c.name, rec.Code)
		}
	}
}

func TestR131_F6131_AliasCreateErrorMapping(t *testing.T) {
	s := r131Server(t)
	// alias shadowing an existing mailbox -> 409
	rec := r131Do(s.handleAliases, http.MethodPost, "/api/v1/aliases", `{"alias":"bob@ex.com","target":"bob@ex.com"}`, "admin@ex.com", true)
	if rec.Code != http.StatusConflict {
		t.Errorf("alias shadowing mailbox = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// invalid local part -> 400
	rec = r131Do(s.handleAliases, http.MethodPost, "/api/v1/aliases", `{"alias":"a/b@ex.com","target":"bob@ex.com"}`, "admin@ex.com", true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("alias with '/' = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestR131_F6132_DbErrStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{db.ErrAccountNotFound, 404}, {db.ErrAliasNotFound, 404}, {db.ErrDomainNotFound, 404},
		{db.ErrAliasConflict, 409}, {db.ErrAccountExists, 409}, {db.ErrDomainAccountLimit, 409},
		{db.ErrInvalidName, 400}, {fmt.Errorf("wrap: %w", db.ErrAccountNotFound), 404},
		{errors.New("disk"), 500},
	} {
		if got, _ := dbErrStatus(tc.err, "x"); got != tc.want {
			t.Errorf("dbErrStatus(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestR131_F6133_DKIMFailureFailsCreate(t *testing.T) {
	s := r131Server(t)
	old := generateDKIMKeyPair
	generateDKIMKeyPair = func(int) (*rsa.PrivateKey, []byte, error) { return nil, nil, errors.New("entropy") }
	defer func() { generateDKIMKeyPair = old }()
	rec := r131Do(s.handleDomains, http.MethodPost, "/api/v1/domains", `{"name":"new.com"}`, "admin@ex.com", true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("create with DKIM failure = %d, want 500", rec.Code)
	}
	if _, err := s.db.GetDomain("new.com"); err == nil {
		t.Error("domain created without DKIM key")
	}
}

func TestR131_F6134_LockoutNotRemotelyTriggerable(t *testing.T) {
	s := r131Server(t)
	login := func(ip, pw string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(fmt.Sprintf(`{"email":"bob@ex.com","password":%q}`, pw)))
		req.Header.Set("User-Agent", "r131")
		req.RemoteAddr = ip + ":4000"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 20; i++ {
		login(fmt.Sprintf("203.0.113.%d", i+1), "wrong") // attacker rotating IPs
	}
	if c := login("198.51.100.7", "Passw0rd!x"); c != http.StatusOK {
		t.Fatalf("victim from own IP = %d, want 200", c)
	}
	// the same attacker IP is still limited on the account
	for i := 0; i < 5; i++ {
		login("203.0.113.200", "wrong")
	}
	if c := login("203.0.113.200", "Passw0rd!x"); c != http.StatusTooManyRequests {
		t.Fatalf("locked pair = %d, want 429", c)
	}
}

func TestR131_F6135_TOTPKeyCaseInsensitive(t *testing.T) {
	s := r131Server(t)
	for i := 0; i < maxTOTPFailures; i++ {
		s.recordTOTPFailure(fmt.Sprintf("%s@Ex.com", []string{"Bob", "BOB", "bob", "bOb", "boB"}[i]))
	}
	if !s.isTOTPLockedOut("bob@ex.com") {
		t.Error("case variants did not share the TOTP budget")
	}
	s.clearTOTPFailures("BOB@EX.COM")
	if s.isTOTPLockedOut("bob@ex.com") {
		t.Error("clear with different case left the lockout")
	}
}

func TestR131_F6136_CreateAccountNormalisesCase(t *testing.T) {
	s := r131Server(t)
	rec := r131Do(s.handleAccounts, http.MethodPost, "/api/v1/accounts", `{"email":"Carol@EX.com","password":"Passw0rd!x"}`, "admin@ex.com", true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", rec.Code, rec.Body.String())
	}
	if a, err := s.db.GetAccount("ex.com", "carol"); err != nil || a.Email != "carol@ex.com" {
		t.Errorf("stored account not normalised: %v %v", a, err)
	}
}

func TestR131_F6137_EmptyListsAreArrays(t *testing.T) {
	s := r131Server(t)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"aliases":  r131Do(s.handleAliases, http.MethodGet, "/api/v1/aliases", "", "admin@ex.com", true),
		"accounts": r131Do(s.handleAccounts, http.MethodGet, "/api/v1/accounts?domain=none.com", "", "admin@ex.com", true),
	} {
		if strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Errorf("%s empty list = %q, want []", name, rec.Body.String())
		}
	}
	empty := r131Server(t)
	_ = empty.db.DeleteDomain("ex.com")
	rec := r131Do(empty.handleDomains, http.MethodGet, "/api/v1/domains", "", "admin@ex.com", true)
	var v []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v == nil {
		t.Errorf("domains empty = %q", rec.Body.String())
	}
}

func TestR131_F6138_SelfChangePasswordPolicy(t *testing.T) {
	s := r131Server(t)
	s.config.PasswordHasher = "argon2id" // no 72-byte ceiling: only the 128-char policy applies
	pw := strings.Repeat("Aa1!", 50)
	body := fmt.Sprintf(`{"current_password":"Passw0rd!x","new_password":%q}`, pw)
	rec := r131Do(s.handleAccountPassword, http.MethodPost, "/api/v1/account/password", body, "bob@ex.com", false)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("over-long new password = %d, want 400", rec.Code)
	}
}

func TestR131_F6139_PutCannotBypassCurrentPassword(t *testing.T) {
	s := r131Server(t)
	rec := r131Do(s.handleAccountDetail, http.MethodPut, "/api/v1/accounts/bob@ex.com", `{"password":"Newpassw0rd!"}`, "bob@ex.com", false)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin PUT password = %d, want 403", rec.Code)
	}
}
