package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// Regression tests for F5370-F5373 (domain update, account creation limits,
// self-service forwarding address normalisation).

func newDomainLimitsServer(t *testing.T, domains ...*db.DomainData) (*Server, *db.DB) {
	t.Helper()
	d, err := db.Open(t.TempDir() + "/accounts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	for _, dom := range domains {
		if err := d.CreateDomain(dom); err != nil {
			t.Fatal(err)
		}
	}
	return NewServer(d, nil, Config{}), d
}

func domainLimitsAdmin(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), "user", "admin@example.test")
	ctx = context.WithValue(ctx, "isAdmin", true)
	return r.WithContext(ctx)
}

func putDomain(s *Server, name, body string) int {
	rec := httptest.NewRecorder()
	s.handleDomainDetail(rec, domainLimitsAdmin(httptest.NewRequest(http.MethodPut, "/api/v1/domains/"+name, strings.NewReader(body))))
	return rec.Code
}

func postAccount(t *testing.T, s *Server, email string, admin bool) int {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"email": email, "password": strings.Repeat("Rr9!", 3), "is_admin": admin})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.handleAccounts(rec, domainLimitsAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(string(body)))))
	return rec.Code
}

func mustDomain(t *testing.T, d *db.DB, name string) *db.DomainData {
	t.Helper()
	dom, err := d.GetDomain(name)
	if err != nil {
		t.Fatal(err)
	}
	return dom
}

// F5370: a partial domain update changes only the fields it names.
func TestDomainUpdatePreservesOmittedFields(t *testing.T) {
	s, d := newDomainLimitsServer(t,
		&db.DomainData{Name: "busy.test", MaxAccounts: 50, IsActive: true},
		&db.DomainData{Name: "empty.test", MaxAccounts: 25, IsActive: true},
	)
	if err := d.CreateAccount(&db.AccountData{Email: "u@busy.test", LocalPart: "u", Domain: "busy.test", PasswordHash: "fixture", IsActive: true}); err != nil {
		t.Fatal(err)
	}

	// Admin panel toggle body.
	if c := putDomain(s, "busy.test", `{"is_active":true}`); c != 200 {
		t.Fatalf("toggle status=%d", c)
	}
	if got := mustDomain(t, d, "busy.test"); got.MaxAccounts != 50 || !got.IsActive {
		t.Fatalf("toggle wiped fields: %+v", got)
	}
	// max_accounts-only update must not deactivate (or 409 on a busy domain).
	if c := putDomain(s, "busy.test", `{"max_accounts":40}`); c != 200 {
		t.Fatalf("max-only on busy status=%d", c)
	}
	if got := mustDomain(t, d, "busy.test"); got.MaxAccounts != 40 || !got.IsActive {
		t.Fatalf("max-only on busy: %+v", got)
	}
	if c := putDomain(s, "empty.test", `{"max_accounts":30}`); c != 200 {
		t.Fatalf("max-only on empty status=%d", c)
	}
	if got := mustDomain(t, d, "empty.test"); got.MaxAccounts != 30 || !got.IsActive {
		t.Fatalf("max-only on empty: %+v", got)
	}
	// Empty body changes nothing.
	if c := putDomain(s, "empty.test", `{}`); c != 200 {
		t.Fatalf("empty body status=%d", c)
	}
	if got := mustDomain(t, d, "empty.test"); got.MaxAccounts != 30 || !got.IsActive {
		t.Fatalf("empty body: %+v", got)
	}
	// Explicit values still apply: deactivate keeps max, explicit 0 clears.
	if c := putDomain(s, "empty.test", `{"is_active":false}`); c != 200 {
		t.Fatalf("deactivate status=%d", c)
	}
	if got := mustDomain(t, d, "empty.test"); got.MaxAccounts != 30 || got.IsActive {
		t.Fatalf("deactivate: %+v", got)
	}
	if c := putDomain(s, "empty.test", `{"max_accounts":0}`); c != 200 {
		t.Fatalf("explicit zero status=%d", c)
	}
	if got := mustDomain(t, d, "empty.test"); got.MaxAccounts != 0 {
		t.Fatalf("explicit zero: %+v", got)
	}
	// Rejected inputs leave the stored domain untouched.
	if c := putDomain(s, "busy.test", `{"max_accounts":-1}`); c != 400 {
		t.Fatalf("negative status=%d", c)
	}
	if c := putDomain(s, "busy.test", `{"is_active":false}`); c != 409 {
		t.Fatalf("deactivate busy status=%d", c)
	}
	if got := mustDomain(t, d, "busy.test"); got.MaxAccounts != 40 || !got.IsActive {
		t.Fatalf("rejected update changed domain: %+v", got)
	}
}

// F5372: accounts can only be created on a hosted domain.
func TestCreateAccountRequiresExistingDomain(t *testing.T) {
	s, d := newDomainLimitsServer(t, &db.DomainData{Name: "example.test", IsActive: true})
	for _, admin := range []bool{true, false} {
		if c := postAccount(t, s, "boss@exmaple.test", admin); c != http.StatusBadRequest {
			t.Fatalf("unknown domain (admin=%v) status=%d, want 400", admin, c)
		}
	}
	if a, _ := d.GetAccount("exmaple.test", "boss"); a != nil {
		t.Fatalf("account stored on unknown domain: %+v", a)
	}
	if c := postAccount(t, s, "boss@example.test", true); c != http.StatusCreated {
		t.Fatalf("hosted domain status=%d", c)
	}
}

// F5373: domain max_accounts is enforced on creation (0 = unlimited).
func TestCreateAccountEnforcesDomainMaxAccounts(t *testing.T) {
	s, d := newDomainLimitsServer(t,
		&db.DomainData{Name: "limited.test", MaxAccounts: 2, IsActive: true},
		&db.DomainData{Name: "open.test", IsActive: true},
	)
	if c := postAccount(t, s, "a@limited.test", false); c != 201 {
		t.Fatalf("1st status=%d", c)
	}
	if c := postAccount(t, s, "b@limited.test", false); c != 201 {
		t.Fatalf("2nd (at limit) status=%d", c)
	}
	if c := postAccount(t, s, "c@limited.test", false); c != http.StatusConflict {
		t.Fatalf("3rd (over limit) status=%d, want 409", c)
	}
	if accts, _ := d.ListAccountsByDomain("limited.test"); len(accts) != 2 {
		t.Fatalf("accounts=%d, want 2", len(accts))
	}
	// Freeing a slot or raising the limit allows creation again.
	if err := d.DeleteAccount("limited.test", "a"); err != nil {
		t.Fatal(err)
	}
	if c := postAccount(t, s, "c@limited.test", false); c != 201 {
		t.Fatalf("after delete status=%d", c)
	}
	if c := putDomain(s, "limited.test", `{"max_accounts":3}`); c != 200 {
		t.Fatalf("raise limit status=%d", c)
	}
	if c := postAccount(t, s, "d@limited.test", false); c != 201 {
		t.Fatalf("after raise status=%d", c)
	}
	// max_accounts 0 means unlimited.
	for _, u := range []string{"x", "y", "z"} {
		if c := postAccount(t, s, u+"@open.test", false); c != 201 {
			t.Fatalf("unlimited domain %s status=%d", u, c)
		}
	}
}

// F5371: self-service forwarding stores the bare addr-spec.
func TestForwardingStoresBareAddress(t *testing.T) {
	s, d := newDomainLimitsServer(t, &db.DomainData{Name: "example.test", IsActive: true})
	if err := d.CreateAccount(&db.AccountData{Email: "user@example.test", LocalPart: "user", Domain: "example.test", PasswordHash: "fixture", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	put := func(body string) (int, map[string]interface{}) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/account/forwarding", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleForwarding(rec, req.WithContext(context.WithValue(req.Context(), "user", "user@example.test")))
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out) // error bodies are checked via the status code
		return rec.Code, out
	}
	stored := func() *db.AccountData {
		a, err := d.GetAccount("example.test", "user")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	for _, in := range []string{`Dest <dest@example.org>`, `<dest@example.org>`, `dest@example.org (Dest)`, ` dest@example.org `} {
		c, out := put(`{"forward_to":"` + in + `","keep_copy":true}`)
		if a := stored(); c != 200 || a.ForwardTo != "dest@example.org" || !a.ForwardKeepCopy || out["forward_to"] != "dest@example.org" {
			t.Fatalf("input %q: status=%d stored=%q keep=%v resp=%v", in, c, a.ForwardTo, a.ForwardKeepCopy, out["forward_to"])
		}
	}
	if c, _ := put(`{"forward_to":"not an address"}`); c != 400 {
		t.Fatalf("invalid status=%d", c)
	}
	if a := stored(); a.ForwardTo != "dest@example.org" {
		t.Fatalf("rejected input changed forwarding: %q", a.ForwardTo)
	}
	if c, _ := put(`{"forward_to":""}`); c != 200 || stored().ForwardTo != "" {
		t.Fatalf("disable: status=%d stored=%q", c, stored().ForwardTo)
	}
}
