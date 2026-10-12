package api

// Regression tests for R221 (F6980-F6989): self-service account deletion
// and quota_limit on admin account create.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// r221Login gives the seeded admin a known password and returns a session.
func r221Login(t *testing.T, s *Server, email string) string {
	t.Helper()
	user, domain := parseEmail(email)
	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!x"), 4)
	if _, err := s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
		a.PasswordHash = string(hash)
		return nil
	}); err != nil {
		t.Fatalf("set password: %v", err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": email, "admin": true, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "jti": "r221",
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(a5028Secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return str
}

func r221Do(s *Server, method, path, bearer, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// F6980 proof: the non-admin self-delete branch in deleteAccount sits behind
// adminMiddleware, so a user can never reach it.
func TestR221_LegacySelfDeleteBranchUnreachable(t *testing.T) {
	s, _ := a5028Setup(t)
	rec := r221Do(s, http.MethodDelete, "/api/v1/accounts/bob@ex.com", a5028Session(t), `{"password":"Passw0rd!x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin-gated route returned %d, want 403", rec.Code)
	}
}

// F6981: self-service DELETE /api/v1/account with the current password.
func TestR221_SelfDelete(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	rec := r221Do(s, http.MethodDelete, "/api/v1/account", tok, `{"password":"Passw0rd!x"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("self delete=%d body=%s want 204", rec.Code, rec.Body.String())
	}
	if a, _ := s.db.GetAccount("ex.com", "bob"); a != nil {
		t.Fatalf("account still exists")
	}
	if c := r221Do(s, http.MethodGet, "/api/v1/account/totp", tok, "").Code; c != http.StatusUnauthorized {
		t.Fatalf("old session after delete=%d want 401", c)
	}
}

func TestR221_SelfDeleteRequiresPassword(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	for _, body := range []string{``, `{}`, `{"password":""}`} {
		if c := r221Do(s, http.MethodDelete, "/api/v1/account", tok, body).Code; c != http.StatusBadRequest {
			t.Fatalf("body %q=%d want 400", body, c)
		}
	}
	if c := r221Do(s, http.MethodDelete, "/api/v1/account", tok, `{"password":"wrong"}`).Code; c != http.StatusForbidden {
		t.Fatalf("wrong password=%d want 403", c)
	}
	if a, _ := s.db.GetAccount("ex.com", "bob"); a == nil {
		t.Fatalf("account deleted despite bad password")
	}
}

func TestR221_SelfDeleteThrottled(t *testing.T) {
	s, _ := a5028Setup(t)
	tok := a5028Session(t)
	for i := 0; i < 5; i++ {
		r221Do(s, http.MethodDelete, "/api/v1/account", tok, `{"password":"bad"}`)
	}
	if c := r221Do(s, http.MethodDelete, "/api/v1/account", tok, `{"password":"Passw0rd!x"}`).Code; c != http.StatusTooManyRequests {
		t.Fatalf("after budget=%d want 429", c)
	}
}

func TestR221_SelfDeleteUnauthenticatedAndMethod(t *testing.T) {
	s, _ := a5028Setup(t)
	if c := r221Do(s, http.MethodDelete, "/api/v1/account", "", `{"password":"Passw0rd!x"}`).Code; c != http.StatusUnauthorized {
		t.Fatalf("unauthenticated=%d want 401", c)
	}
	rec := r221Do(s, http.MethodGet, "/api/v1/account", a5028Session(t), "")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") == "" {
		t.Fatalf("GET=%d allow=%q want 405 with Allow", rec.Code, rec.Header().Get("Allow"))
	}
}

// F6982: the last active administrator cannot delete itself.
func TestR221_SelfDeleteLastAdminGuard(t *testing.T) {
	s, _ := a5028Setup(t)
	root := r221Login(t, s, "root@ex.com")
	rec := r221Do(s, http.MethodDelete, "/api/v1/account", root, `{"password":"Passw0rd!x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("last admin self delete=%d body=%s want 409", rec.Code, rec.Body.String())
	}
	if a, _ := s.db.GetAccount("ex.com", "root"); a == nil {
		t.Fatalf("last admin was deleted")
	}
}

// F6983: purge cascade runs through the AccountPurger.
type r221Purger struct{ got []string }

func (p *r221Purger) PurgeAccount(email string) error { p.got = append(p.got, email); return nil }

func TestR221_SelfDeletePurges(t *testing.T) {
	s, _ := a5028Setup(t)
	p := &r221Purger{}
	s.accountPurger = p
	if c := r221Do(s, http.MethodDelete, "/api/v1/account", a5028Session(t), `{"password":"Passw0rd!x"}`).Code; c != http.StatusNoContent {
		t.Fatalf("delete=%d", c)
	}
	if len(p.got) != 1 || p.got[0] != "bob@ex.com" {
		t.Fatalf("purged=%v", p.got)
	}
}

// F6984: admin create accepts quota_limit in a single call.
func TestR221_CreateAccountQuota(t *testing.T) {
	s, _ := a5028Setup(t)
	if err := s.db.CreateDomain(&db.DomainData{Name: "ex.com", IsActive: true}); err != nil {
		t.Fatalf("domain: %v", err)
	}
	root := r221Login(t, s, "root@ex.com")
	rec := r221Do(s, http.MethodPost, "/api/v1/accounts", root, `{"email":"q@ex.com","password":"Str0ngPassw0rd!x","quota_limit":1048576}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["quota_limit"] != float64(1048576) {
		t.Fatalf("response quota=%v", out["quota_limit"])
	}
	if a, _ := s.db.GetAccount("ex.com", "q"); a == nil || a.QuotaLimit != 1048576 {
		t.Fatalf("stored quota=%+v", a)
	}
	if c := r221Do(s, http.MethodPost, "/api/v1/accounts", root, `{"email":"n@ex.com","password":"Str0ngPassw0rd!x","quota_limit":-1}`).Code; c != http.StatusBadRequest {
		t.Fatalf("negative quota=%d want 400", c)
	}
}
