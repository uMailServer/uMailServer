package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func apiUpdateFixtureServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/accounts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := database.CreateDomain(&db.DomainData{Name: "example.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "user@example.test", LocalPart: "user", Domain: "example.test", PasswordHash: "fixture", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	return NewServer(database, nil, Config{}), database
}

func apiUpdateFixtureCall(t *testing.T, method, path, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), "user", "user@example.test"), "isAdmin", true))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func apiUpdateFixtureWant(t *testing.T, label string, got, want interface{}) {
	t.Helper()
	if got != want {
		t.Errorf("%s got %v want %v", label, got, want)
	}
}

// F4812 retains the state/error boundary covered by the audit proof.
func TestAccountUpdatePreservesOmittedActivation(t *testing.T) {
	s, d := apiUpdateFixtureServer(t)
	control := apiUpdateFixtureCall(t, "PUT", "/api/v1/accounts/user@example.test", `{"is_active":true,"quota_limit":3}`, s.handleAccountDetail)
	apiUpdateFixtureWant(t, "CONTROL explicit activation", control.Code, 200)
	a, err := d.GetAccount("example.test", "user")
	if err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "CONTROL retained active", a.IsActive, true)
	rec := apiUpdateFixtureCall(t, "PUT", "/api/v1/accounts/user@example.test", `{"quota_limit":12}`, s.handleAccountDetail)
	apiUpdateFixtureWant(t, "update status", rec.Code, 200)
	a, err = d.GetAccount("example.test", "user")
	if err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "omitted activation preserves state", a.IsActive, true)
	rec = apiUpdateFixtureCall(t, "PUT", "/api/v1/accounts/user@example.test", `{"is_active":false}`, s.handleAccountDetail)
	apiUpdateFixtureWant(t, "explicit false", rec.Code, 200)
	rec = apiUpdateFixtureCall(t, "PUT", "/api/v1/accounts/user@example.test", `{"quota_limit":5}`, s.handleAccountDetail)
	apiUpdateFixtureWant(t, "inactive omitted update", rec.Code, 200)
	a, err = d.GetAccount("example.test", "user")
	if err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "inactive preserved", a.IsActive, false)
	rec = apiUpdateFixtureCall(t, "PUT", "/api/v1/accounts/user@example.test", `{broken`, s.handleAccountDetail)
	apiUpdateFixtureWant(t, "malformed body", rec.Code, 400)
	a, err = d.GetAccount("example.test", "user")
	if err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "malformed no state change", a.IsActive, false)
}
