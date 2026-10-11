package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

type r204Purger struct {
	fail  bool
	calls []string
}

func (p *r204Purger) PurgeAccount(email string) error {
	p.calls = append(p.calls, email)
	if p.fail {
		return errors.New("injected purge failure")
	}
	return nil
}

func r204Server(t *testing.T) (*Server, *db.DB, *r204Purger) {
	database, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	srv := NewServer(database, nil, Config{})
	p := &r204Purger{}
	srv.SetAccountPurger(p)
	_ = database.CreateDomain(&db.DomainData{Name: "a.test", IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "adm", Domain: "a.test", PasswordHash: "x", IsAdmin: true, IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "adm2", Domain: "a.test", PasswordHash: "x", IsAdmin: true, IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "bob", Domain: "a.test", PasswordHash: "x", IsActive: true})
	_ = database.CreateDomain(&db.DomainData{Name: "b.test", IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "bob", Domain: "b.test", PasswordHash: "x", IsActive: true})
	return srv, database, p
}

// F6811: a failed purge must stay retryable although the row is gone.
func TestR204_AccountDeletePurgeFailureIsRetryable(t *testing.T) {
	srv, _, p := r204Server(t)
	p.fail = true
	rec := httptest.NewRecorder()
	srv.deleteAccount(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "bob@a.test")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("first delete: %d", rec.Code)
	}
	// Retry while the purge still fails: must keep reporting failure, not 404.
	rec = httptest.NewRecorder()
	srv.deleteAccount(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "bob@a.test")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("retry with failing purge: %d", rec.Code)
	}
	p.fail = false
	n := len(p.calls)
	rec = httptest.NewRecorder()
	srv.deleteAccount(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "bob@a.test")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("retry after recovery: %d", rec.Code)
	}
	if len(p.calls) != n+1 {
		t.Fatalf("purge not re-run on retry: %v", p.calls)
	}
}

func TestR204_DomainDeletePurgeFailureReported(t *testing.T) {
	srv, _, p := r204Server(t)
	p.fail = true
	rec := httptest.NewRecorder()
	srv.deleteDomain(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "b.test")
	if rec.Code == http.StatusNoContent {
		t.Fatalf("domain delete hid a purge failure")
	}
	if !strings.Contains(rec.Body.String(), "bob@b.test") {
		t.Fatalf("leftover accounts not named: %s", rec.Body.String())
	}
	// Per-account retry now works.
	p.fail = false
	rec = httptest.NewRecorder()
	srv.deleteAccount(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "bob@b.test")
	if rec.Code != http.StatusNotFound || p.calls[len(p.calls)-1] != "bob@b.test" {
		t.Fatalf("retry: %d %v", rec.Code, p.calls)
	}
}
