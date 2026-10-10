package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const r143Password = "seed-current-password"

func r143Server(t *testing.T, cfg Config) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/r143.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if cfg.TokenExpiry == 0 {
		cfg.TokenExpiry = time.Hour
	}
	cfg.JWTSecret = "r143-test-secret-key-for-jwt-signing"
	return NewServer(database, nil, cfg), database
}

func r143Account(t *testing.T, s *Server, d *db.DB, email string, admin bool) {
	t.Helper()
	user, domain := parseEmail(email)
	h, err := s.hashPassword(r143Password)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&db.AccountData{
		Email: email, LocalPart: user, Domain: domain, PasswordHash: h,
		IsActive: true, IsAdmin: admin, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// r143Login performs a real login (API client UA so the token is in the body).
func r143Login(t *testing.T, s *Server, email string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": r143Password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	tok, _ := out["token"].(string)
	if tok == "" {
		t.Fatalf("no token in %s", rec.Body.String())
	}
	return tok
}

// r143Status runs the auth middleware with token and reports the status.
func r143Status(s *Server, token string) int {
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func r143Do(s *Server, handler http.HandlerFunc, token, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	s.authMiddleware(handler).ServeHTTP(rec, req)
	return rec
}

// F6250 (High): a password change must invalidate every other session of the
// account; the acting session continues with the replacement token.
func TestR143PasswordChangeRevokesOtherSessions(t *testing.T) {
	s, d := r143Server(t, Config{})
	r143Account(t, s, d, "user@example.com", false)
	acting := r143Login(t, s, "user@example.com")
	other := r143Login(t, s, "user@example.com")

	rec := r143Do(s, s.handleAccountPassword, acting, http.MethodPost, "/api/v1/account/password",
		map[string]string{"current_password": r143Password, "new_password": "another-password-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	replacement, _ := out["token"].(string)
	if replacement == "" {
		t.Fatalf("no replacement token: %s", rec.Body.String())
	}
	if got := r143Status(s, other); got != http.StatusUnauthorized {
		t.Errorf("other session still valid after password change: %d", got)
	}
	if got := r143Status(s, acting); got != http.StatusUnauthorized {
		t.Errorf("pre-change token of the acting session still valid: %d", got)
	}
	if got := r143Status(s, replacement); got != http.StatusOK {
		t.Errorf("replacement token rejected: %d", got)
	}
	// Refresh keeps working from the replacement and the result stays valid.
	rr := r143Do(s, s.handleRefresh, replacement, http.MethodPost, "/api/v1/auth/refresh", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("refresh: %d", rr.Code)
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if got := r143Status(s, out["token"].(string)); got != http.StatusOK {
		t.Errorf("refreshed token rejected: %d", got)
	}
}

// F6250 (High): admin reset of another user's password revokes that user's
// sessions; F6252: disabling / demoting revokes too, so re-enabling or
// re-promoting cannot revive old tokens.
func TestR143AdminActionsRevokeTargetSessions(t *testing.T) {
	s, d := r143Server(t, Config{})
	r143Account(t, s, d, "admin@example.com", true)
	r143Account(t, s, d, "victim@example.com", false)
	adminTok := r143Login(t, s, "admin@example.com")
	update := func(body map[string]interface{}) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/victim@example.com", func() *bytes.Reader {
			b, _ := json.Marshal(body)
			return bytes.NewReader(b)
		}())
		req.Header.Set("Authorization", "Bearer "+adminTok)
		rec := httptest.NewRecorder()
		s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.updateAccount(w, r, "victim@example.com")
		})).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
	}

	// reset
	tok := r143Login(t, s, "victim@example.com")
	update(map[string]interface{}{"password": "Reset-Password-99!"})
	if got := r143Status(s, tok); got != http.StatusUnauthorized {
		t.Errorf("session survived admin password reset: %d", got)
	}
	if got := r143Status(s, adminTok); got != http.StatusOK {
		t.Errorf("admin's own session was revoked: %d", got)
	}

	// disable then re-enable must not revive an old token
	s.config.TokenExpiry = time.Hour
	h, _ := s.hashPassword(r143Password)
	if _, err := d.MutateAccount("example.com", "victim", func(a *db.AccountData) error { a.PasswordHash = h; return nil }); err != nil {
		t.Fatal(err)
	}
	tok = r143Login(t, s, "victim@example.com")
	update(map[string]interface{}{"is_active": false})
	update(map[string]interface{}{"is_active": true})
	if got := r143Status(s, tok); got != http.StatusUnauthorized {
		t.Errorf("token revived by disable+enable: %d", got)
	}

	// demotion revokes
	if _, err := d.MutateAccount("example.com", "victim", func(a *db.AccountData) error { a.IsAdmin = true; return nil }); err != nil {
		t.Fatal(err)
	}
	tok = r143Login(t, s, "victim@example.com")
	// demotion needs admin re-auth
	update(map[string]interface{}{"is_admin": false, "current_admin_password": r143Password})
	if _, err := d.MutateAccount("example.com", "victim", func(a *db.AccountData) error { a.IsAdmin = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := r143Status(s, tok); got != http.StatusUnauthorized {
		t.Errorf("token revived by demote+promote: %d", got)
	}
}

// F6251 (High): tokens of a deleted account must stop working, and must not
// be inherited by a re-created account with the same address.
func TestR143DeletedAccountTokensRejected(t *testing.T) {
	s, d := r143Server(t, Config{})
	r143Account(t, s, d, "gone@example.com", false)
	tok := r143Login(t, s, "gone@example.com")
	if got := r143Status(s, tok); got != http.StatusOK {
		t.Fatalf("baseline: %d", got)
	}
	if err := d.DeleteAccount("example.com", "gone"); err != nil {
		t.Fatal(err)
	}
	if got := r143Status(s, tok); got != http.StatusUnauthorized {
		t.Errorf("deleted account token accepted: %d", got)
	}
	// re-create the address: the old token predates the new account.
	time.Sleep(2 * time.Millisecond)
	h, _ := s.hashPassword(r143Password)
	if err := d.CreateAccount(&db.AccountData{Email: "gone@example.com", LocalPart: "gone", Domain: "example.com",
		PasswordHash: h, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := r143Status(s, tok); got != http.StatusUnauthorized {
		t.Errorf("recreated account inherited an old session: %d", got)
	}
}

// F6258: the admin listener trusted token claims alone.
func TestR143AdminServerHonoursAccountState(t *testing.T) {
	s, d := r143Server(t, Config{})
	r143Account(t, s, d, "root@example.com", true)
	tok := r143Login(t, s, "root@example.com")
	as := NewAdminServer(s, AdminConfig{JWTSecret: s.config.JWTSecret})
	probe := as.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	call := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		probe.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := call(); got != http.StatusOK {
		t.Fatalf("baseline: %d", got)
	}
	if err := d.DeleteAccount("example.com", "root"); err != nil {
		t.Fatal(err)
	}
	if got := call(); got != http.StatusUnauthorized {
		t.Errorf("deleted admin still accepted on admin listener: %d", got)
	}
}

// F6256 (High, race): self-service password change wrote back a snapshot
// read before the slow hashes, re-enabling an account an admin disabled in
// between.
func TestR143PasswordChangeDoesNotReenableDisabledAccount(t *testing.T) {
	s, d := r143Server(t, Config{})
	r143Account(t, s, d, "user@example.com", false)
	beforeCredentialWrite = func() {
		_, _ = d.MutateAccount("example.com", "user", func(a *db.AccountData) error { a.IsActive = false; return nil })
	}
	defer func() { beforeCredentialWrite = nil }()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/password", bytes.NewReader([]byte(
		`{"current_password":"`+r143Password+`","new_password":"another-password-1"}`)))
	req = req.WithContext(context.WithValue(req.Context(), "user", "user@example.com"))
	rec := httptest.NewRecorder()
	s.handleAccountPassword(rec, req)
	a, _ := d.GetAccount("example.com", "user")
	if a.IsActive {
		t.Fatal("password change re-enabled a concurrently disabled account")
	}
}

// F6255 (High, race): the login-time rehash wrote the whole stale snapshot,
// replacing a password changed during the hash with the old password's rehash.
func TestR143LoginRehashDoesNotClobberConcurrentPasswordChange(t *testing.T) {
	s, d := r143Server(t, Config{PasswordHasher: "argon2id"})
	// legacy bcrypt hash => login triggers a rehash
	h, err := bcryptHashForTest(r143Password)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&db.AccountData{Email: "user@example.com", LocalPart: "user", Domain: "example.com",
		PasswordHash: h, IsActive: true, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	newHash, _ := bcryptHashForTest("changed-meanwhile-1")
	beforeCredentialWrite = func() {
		_, _ = d.MutateAccount("example.com", "user", func(a *db.AccountData) error { a.PasswordHash = newHash; return nil })
	}
	defer func() { beforeCredentialWrite = nil }()
	_ = r143Login(t, s, "user@example.com")
	a, _ := d.GetAccount("example.com", "user")
	if a.PasswordHash != newHash {
		t.Fatal("login rehash overwrote a concurrent password change")
	}
}

// F6254: list endpoints page with ?limit/?offset, stable order, and expose
// X-Total-Count while keeping the array body.
func TestR143ListPagination(t *testing.T) {
	s, d := r143Server(t, Config{})
	if err := d.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = d.CreateAlias(&db.AliasData{Alias: "a" + strconv.Itoa(i) + "@example.com", Target: "t@example.com", Domain: "example.com", IsActive: true})
		r143Account(t, s, d, "u"+strconv.Itoa(i)+"@example.com", false)
	}
	get := func(fn func(http.ResponseWriter, *http.Request), q string) (*httptest.ResponseRecorder, []map[string]interface{}) {
		req := httptest.NewRequest(http.MethodGet, "/x"+q, nil)
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), "user", "u0@example.com"), "isAdmin", true))
		rec := httptest.NewRecorder()
		fn(rec, req)
		var out []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	for name, fn := range map[string]func(http.ResponseWriter, *http.Request){
		"accounts": s.listAccounts, "aliases": s.listAliases,
	} {
		rec, all := get(fn, "")
		if rec.Header().Get("X-Total-Count") != "5" || len(all) != 5 {
			t.Fatalf("%s: total=%q len=%d", name, rec.Header().Get("X-Total-Count"), len(all))
		}
		rec, page := get(fn, "?limit=2&offset=3")
		if len(page) != 2 || rec.Header().Get("X-Total-Count") != "5" {
			t.Fatalf("%s: page len=%d total=%q", name, len(page), rec.Header().Get("X-Total-Count"))
		}
		if page[0]["email"] != all[3]["email"] && page[0]["alias"] != all[3]["alias"] {
			t.Errorf("%s: window not stable: %v vs %v", name, page[0], all[3])
		}
		if rec, _ := get(fn, "?limit=0"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: limit=0 => %d", name, rec.Code)
		}
		if rec, _ := get(fn, "?limit=1001"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: limit=1001 => %d", name, rec.Code)
		}
		if rec, _ := get(fn, "?offset=-1"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: offset=-1 => %d", name, rec.Code)
		}
		if _, p := get(fn, "?offset=99"); len(p) != 0 {
			t.Errorf("%s: offset past end returned %d", name, len(p))
		}
	}
	// default limit is 100
	rec, _ := get(s.listDomains, "")
	if rec.Header().Get("X-Total-Count") != "1" {
		t.Errorf("domains total=%q", rec.Header().Get("X-Total-Count"))
	}
}

func bcryptHashForTest(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	return string(b), err
}

// seedSessionAccount stores an active account so tokens minted for it by test
// fixtures stay valid: since F6251 a token whose account does not exist is
// rejected.
func seedSessionAccount(t *testing.T, d *db.DB, email string, admin bool) {
	t.Helper()
	user, domain := parseEmail(email)
	if err := d.CreateAccount(&db.AccountData{Email: email, LocalPart: user, Domain: domain, IsActive: true, IsAdmin: admin,
		CreatedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("seed %s: %v", email, err)
	}
}
