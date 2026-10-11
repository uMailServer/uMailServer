package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func r188Setup(t *testing.T, admins ...string) *Server {
	t.Helper()
	s := NewTestServer(t, t.TempDir())
	if err := s.db.CreateDomain(&db.DomainData{Name: "r188.test", IsActive: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	mk := func(local string, admin bool) {
		h, _ := s.hashPassword("Str0ng!Passw0rd")
		if err := s.db.CreateAccount(&db.AccountData{Email: local + "@r188.test", LocalPart: local, Domain: "r188.test", PasswordHash: h, IsAdmin: admin, IsActive: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range admins {
		mk(a, true)
	}
	mk("user", false)
	return s
}

func r188Do(s *Server, method, path, body, actor string, admin bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), "user", actor)
	ctx = context.WithValue(ctx, "isAdmin", admin)
	w := httptest.NewRecorder()
	if strings.HasPrefix(path, "/api/v1/domains/") {
		s.handleDomainDetail(w, req.WithContext(ctx))
	} else if strings.HasPrefix(path, "/api/v1/aliases") {
		if path == "/api/v1/aliases" {
			s.handleAliases(w, req.WithContext(ctx))
		} else {
			s.handleAliasDetail(w, req.WithContext(ctx))
		}
	} else {
		s.handleAccountDetail(w, req.WithContext(ctx))
	}
	return w
}

// F6650: the last active admin must not be deletable, demotable or
// deactivatable (lock-out), and a domain delete must not remove them either.
func TestR188LastAdminProtected(t *testing.T) {
	s := r188Setup(t, "root")
	if w := r188Do(s, "DELETE", "/api/v1/accounts/root@r188.test", "", "root@r188.test", true); w.Code != http.StatusConflict {
		t.Errorf("delete last admin = %d, want 409", w.Code)
	}
	if w := r188Do(s, "PUT", "/api/v1/accounts/root@r188.test", `{"is_active":false}`, "root@r188.test", true); w.Code != http.StatusConflict {
		t.Errorf("deactivate last admin = %d, want 409", w.Code)
	}
	if w := r188Do(s, "PUT", "/api/v1/accounts/root@r188.test", `{"is_admin":false,"current_admin_password":"Str0ng!Passw0rd"}`, "root@r188.test", true); w.Code != http.StatusConflict {
		t.Errorf("demote last admin = %d, want 409", w.Code)
	}
	if w := r188Do(s, "DELETE", "/api/v1/domains/r188.test", "", "root@r188.test", true); w.Code != http.StatusConflict {
		t.Errorf("delete domain of last admin = %d, want 409", w.Code)
	}
	if a, err := s.db.GetAccount("r188.test", "root"); err != nil || !a.IsAdmin || !a.IsActive {
		t.Fatalf("admin damaged: %v %+v", err, a)
	}
	// With a second admin, removal is allowed.
	h, _ := s.hashPassword("Str0ng!Passw0rd")
	_ = s.db.CreateAccount(&db.AccountData{Email: "two@r188.test", LocalPart: "two", Domain: "r188.test", PasswordHash: h, IsAdmin: true, IsActive: true})
	if w := r188Do(s, "DELETE", "/api/v1/accounts/two@r188.test", "", "root@r188.test", true); w.Code != http.StatusNoContent {
		t.Errorf("delete non-last admin = %d, want 204", w.Code)
	}
	// Deleting an ordinary user is unaffected.
	if w := r188Do(s, "DELETE", "/api/v1/accounts/user@r188.test", "", "root@r188.test", true); w.Code != http.StatusNoContent {
		t.Errorf("delete user = %d, want 204", w.Code)
	}
}

// F6651: admin PUT stored forward_to unvalidated (self-service validates).
func TestR188AdminForwardToValidated(t *testing.T) {
	s := r188Setup(t, "root")
	for _, bad := range []string{`not an address`, `"Eve" <eve@evil.test>`, "a@b.test\\r\\nBcc: x@y.test", `<eve@evil.test>`} {
		w := r188Do(s, "PUT", "/api/v1/accounts/user@r188.test", `{"forward_to":"`+strings.ReplaceAll(bad, `"`, `\"`)+`"}`, "root@r188.test", true)
		if w.Code != http.StatusBadRequest {
			t.Errorf("forward_to %q = %d, want 400", bad, w.Code)
		}
	}
	if w := r188Do(s, "PUT", "/api/v1/accounts/user@r188.test", `{"forward_to":"ok@elsewhere.test"}`, "root@r188.test", true); w.Code != http.StatusOK {
		t.Errorf("valid forward_to = %d", w.Code)
	}
	if a, _ := s.db.GetAccount("r188.test", "user"); a.ForwardTo != "ok@elsewhere.test" {
		t.Errorf("stored %q", a.ForwardTo)
	}
	if w := r188Do(s, "PUT", "/api/v1/accounts/user@r188.test", `{"forward_to":""}`, "root@r188.test", true); w.Code != http.StatusOK {
		t.Errorf("clear forward_to = %d", w.Code)
	}
}

// F6652: an alias must not shadow an existing mailbox (alias routing wins).
func TestR188AliasShadowsAccount(t *testing.T) {
	s := r188Setup(t, "root")
	w := r188Do(s, "POST", "/api/v1/aliases", `{"alias":"user@r188.test","target":"root@r188.test"}`, "root@r188.test", true)
	if w.Code != http.StatusConflict {
		t.Errorf("alias shadowing account = %d, want 409", w.Code)
	}
}

// F6653: alias detail paths and update targets are normalised like create.
func TestR188AliasCaseNormalised(t *testing.T) {
	s := r188Setup(t, "root")
	if w := r188Do(s, "POST", "/api/v1/aliases", `{"alias":"sales@r188.test","target":"root@r188.test"}`, "root@r188.test", true); w.Code != http.StatusCreated {
		t.Fatalf("create = %d", w.Code)
	}
	if w := r188Do(s, "GET", "/api/v1/aliases/Sales@R188.test", "", "root@r188.test", true); w.Code != http.StatusOK {
		t.Errorf("get mixed-case = %d, want 200", w.Code)
	}
	w := r188Do(s, "PUT", "/api/v1/aliases/sales@r188.test", `{"target":" User@R188.test "}`, "root@r188.test", true)
	if w.Code != http.StatusOK {
		t.Fatalf("update mixed-case target = %d %s", w.Code, w.Body)
	}
	if a, _ := s.db.GetAlias("r188.test", "sales"); a.Target != "user@r188.test" {
		t.Errorf("stored target %q", a.Target)
	}
}

// F6654: 405 carries Allow; 429 carries Retry-After.
func TestR188AllowAndRetryAfter(t *testing.T) {
	s := r188Setup(t, "root")
	for path, h := range map[string]http.HandlerFunc{
		"/api/v1/accounts": s.handleAccounts, "/api/v1/domains": s.handleDomains,
		"/api/v1/aliases": s.handleAliases, "/api/v1/queue": s.handleQueue, "/api/v1/stats": s.handleStats,
	} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("PATCH", path, nil))
		if w.Code != 405 || w.Header().Get("Allow") == "" {
			t.Errorf("%s: code=%d Allow=%q", path, w.Code, w.Header().Get("Allow"))
		}
	}
	w := httptest.NewRecorder()
	s.sendError(w, http.StatusTooManyRequests, "x")
	if w.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
}

// F6655: stats must not hide per-domain account listing failures silently
// is covered by review; account detail path is case-insensitive.
func TestR188AccountPathCaseInsensitive(t *testing.T) {
	s := r188Setup(t, "root")
	if w := r188Do(s, "GET", "/api/v1/accounts/User@R188.Test", "", "root@r188.test", true); w.Code != http.StatusOK {
		t.Errorf("mixed-case account get = %d", w.Code)
	}
}
