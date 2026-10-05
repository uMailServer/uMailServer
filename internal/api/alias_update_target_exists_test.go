package api

import (
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// F4814 retains the state/error boundary covered by the audit proof.
func TestAliasUpdateRejectsMissingTarget(t *testing.T) {
	s, d := apiUpdateFixtureServer(t)
	if err := d.CreateAlias(&db.AliasData{Alias: "alias", Domain: "example.test", Target: "user@example.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	rec := apiUpdateFixtureCall(t, "PUT", "/api/v1/aliases/alias@example.test", `{"target":"user@example.test"}`, s.handleAliasDetail)
	apiUpdateFixtureWant(t, "CONTROL existing target", rec.Code, 200)
	rec = apiUpdateFixtureCall(t, "PUT", "/api/v1/aliases/alias@example.test", `{"target":"missing@example.test"}`, s.handleAliasDetail)
	apiUpdateFixtureWant(t, "missing target rejected", rec.Code, 400)
	a, err := d.GetAlias("example.test", "alias")
	if err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "target unchanged", a.Target, "user@example.test")
	if err := d.CreateAccount(&db.AccountData{Email: "other@example.test", LocalPart: "other", Domain: "example.test", PasswordHash: "fixture", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	rec = apiUpdateFixtureCall(t, "PUT", "/api/v1/aliases/alias@example.test", `{"target":"other@example.test"}`, s.handleAliasDetail)
	apiUpdateFixtureWant(t, "valid alternative target", rec.Code, 200)
	rec = apiUpdateFixtureCall(t, "PUT", "/api/v1/aliases/alias@example.test", `{"is_active":false}`, s.handleAliasDetail)
	apiUpdateFixtureWant(t, "omitted target", rec.Code, 200)
	a, err = d.GetAlias("example.test", "alias")
	if err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "omitted target retained", a.Target, "other@example.test")
}
