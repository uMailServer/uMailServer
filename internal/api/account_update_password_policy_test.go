package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// Regression tests for F5280 (partial admin update wiped omitted fields),
// F5281 (admin password reset skipped the password policy) and F5282
// (policy-valid passwords longer than bcrypt's 72-byte limit returned 500).

// Fixture credentials are derived at runtime (project convention).
func r528Current() string        { return strings.Repeat("Cur9!", 3) }
func r528Compliant(n int) string { return strings.Repeat("Aa1!", n/4) + strings.Repeat("x", n%4) }

func r528Setup(t *testing.T, cfg Config) (*Server, *db.DB) {
	t.Helper()
	d, err := db.Open(t.TempDir() + "/accounts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := d.CreateDomain(&db.DomainData{Name: "example.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(r528Current()), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []*db.AccountData{
		{Email: "admin@example.test", LocalPart: "admin", Domain: "example.test", PasswordHash: string(h), IsActive: true, IsAdmin: true},
		{Email: "admin2@example.test", LocalPart: "admin2", Domain: "example.test", PasswordHash: string(h), IsActive: true, IsAdmin: true, QuotaLimit: 5},
		{Email: "user@example.test", LocalPart: "user", Domain: "example.test", PasswordHash: string(h), IsActive: true,
			ForwardTo: "elsewhere@example.org", ForwardKeepCopy: true, QuotaLimit: 1 << 20, VacationSettings: `{"enabled":true}`},
	} {
		if err := d.CreateAccount(a); err != nil {
			t.Fatal(err)
		}
	}
	return NewServer(d, nil, cfg), d
}

func r528Call(h http.HandlerFunc, method, path, user string, admin bool, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = context.WithValue(ctx, "isAdmin", admin)
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	return rec
}

func r528Get(t *testing.T, d *db.DB, local string) *db.AccountData {
	t.Helper()
	a, err := d.GetAccount("example.test", local)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAccountUpdatePreservesOmittedFields(t *testing.T) {
	s, d := r528Setup(t, Config{})
	put := func(target, body string) int {
		return r528Call(s.handleAccountDetail, http.MethodPut, "/api/v1/accounts/"+target, "admin@example.test", true, body).Code
	}

	// The admin panel's edit dialog body: only is_admin/is_active/password.
	if c := put("user@example.test", `{"is_admin":false,"is_active":true,"password":"`+r528Compliant(12)+`"}`); c != http.StatusOK {
		t.Fatalf("panel edit status=%d", c)
	}
	a := r528Get(t, d, "user")
	if a.ForwardTo != "elsewhere@example.org" || !a.ForwardKeepCopy || a.QuotaLimit != 1<<20 || a.VacationSettings != `{"enabled":true}` {
		t.Fatalf("omitted fields changed: forward=%q keep=%v quota=%d vacation=%q", a.ForwardTo, a.ForwardKeepCopy, a.QuotaLimit, a.VacationSettings)
	}

	// A rejected negative quota leaves the account untouched.
	if c := put("user@example.test", `{"quota_limit":-1}`); c != http.StatusBadRequest {
		t.Fatalf("negative quota status=%d", c)
	}
	if r528Get(t, d, "user").QuotaLimit != 1<<20 {
		t.Fatal("negative quota changed the account")
	}

	// Explicit zero values still clear each field.
	if c := put("user@example.test", `{"forward_to":"","forward_keep_copy":false,"quota_limit":0,"vacation_settings":""}`); c != http.StatusOK {
		t.Fatalf("explicit clear status=%d", c)
	}
	a = r528Get(t, d, "user")
	if a.ForwardTo != "" || a.ForwardKeepCopy || a.QuotaLimit != 0 || a.VacationSettings != "" || !a.IsActive {
		t.Fatalf("explicit clear not applied: forward=%q keep=%v quota=%d vacation=%q active=%v", a.ForwardTo, a.ForwardKeepCopy, a.QuotaLimit, a.VacationSettings, a.IsActive)
	}

	// Omitted is_admin keeps an admin target's role without re-auth.
	if c := put("admin2@example.test", `{"quota_limit":9}`); c != http.StatusOK {
		t.Fatalf("quota-only update of an admin status=%d", c)
	}
	if a := r528Get(t, d, "admin2"); !a.IsAdmin || a.QuotaLimit != 9 {
		t.Fatalf("admin target after quota-only update: admin=%v quota=%d", a.IsAdmin, a.QuotaLimit)
	}

	// An explicit role change still requires current_admin_password.
	if c := put("admin2@example.test", `{"is_admin":false}`); c != http.StatusForbidden {
		t.Fatalf("demotion without re-auth status=%d", c)
	}
	if !r528Get(t, d, "admin2").IsAdmin {
		t.Fatal("demotion without re-auth was applied")
	}
}

func TestAccountAdminResetEnforcesPasswordPolicy(t *testing.T) {
	s, d := r528Setup(t, Config{})
	put := func(body string) int {
		return r528Call(s.handleAccountDetail, http.MethodPut, "/api/v1/accounts/user@example.test", "admin@example.test", true, body).Code
	}
	verifies := func(pw string) bool {
		ok, _ := s.verifyPassword(pw, r528Get(t, d, "user").PasswordHash)
		return ok
	}

	for _, weak := range []string{"x", strings.Repeat("a", 12), r528Compliant(73)} {
		if c := put(fmt.Sprintf(`{"password":%q}`, weak)); c != http.StatusBadRequest {
			t.Fatalf("reset to %d-byte non-compliant password status=%d want 400", len(weak), c)
		}
	}
	if !verifies(r528Current()) {
		t.Fatal("rejected reset changed the stored password")
	}

	// Omitted password: no validation, password unchanged.
	if c := put(`{"is_active":true}`); c != http.StatusOK || !verifies(r528Current()) {
		t.Fatalf("update without password status=%d", c)
	}

	next := r528Compliant(72)
	if c := put(fmt.Sprintf(`{"password":%q}`, next)); c != http.StatusOK || !verifies(next) {
		t.Fatalf("compliant 72-byte reset status=%d", c)
	}
}

func TestPasswordLongerThanBcryptLimitIsRejected(t *testing.T) {
	create := func(s *Server, local, pw string) int {
		return r528Call(s.handleAccounts, http.MethodPost, "/api/v1/accounts", "admin@example.test", true,
			fmt.Sprintf(`{"email":"%s@example.test","password":%q}`, local, pw)).Code
	}

	s, d := r528Setup(t, Config{})
	if c := create(s, "at72", r528Compliant(72)); c != http.StatusCreated {
		t.Fatalf("72-byte create status=%d want 201", c)
	}
	for _, n := range []int{73, 100, 128} {
		if c := create(s, fmt.Sprintf("n%d", n), r528Compliant(n)); c != http.StatusBadRequest {
			t.Fatalf("%d-byte create status=%d want 400", n, c)
		}
	}

	// Self-service keeps its 8-character minimum and gets the same ceiling.
	self := func(next string) int {
		return r528Call(s.handleAccountPassword, http.MethodPost, "/api/v1/account/password", "user@example.test", false,
			fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, r528Current(), next)).Code
	}
	if c := self(r528Compliant(100)); c != http.StatusBadRequest {
		t.Fatalf("100-byte self-service status=%d want 400", c)
	}
	if ok, _ := s.verifyPassword(r528Current(), r528Get(t, d, "user").PasswordHash); !ok {
		t.Fatal("rejected self-service change altered the password")
	}
	if c := self(strings.Repeat("a", 72)); c != http.StatusOK {
		t.Fatalf("72-byte self-service status=%d want 200", c)
	}

	// argon2id has no 72-byte limit: the policy's 128 characters apply.
	s2, _ := r528Setup(t, Config{PasswordHasher: "argon2id"})
	if c := create(s2, "argon", r528Compliant(100)); c != http.StatusCreated {
		t.Fatalf("argon2id 100-byte create status=%d want 201", c)
	}
}
