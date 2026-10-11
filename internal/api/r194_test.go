package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

type r194Purger struct{ got []string }

func (p *r194Purger) PurgeAccount(email string) error { p.got = append(p.got, email); return nil }

// F6649c/F6710: account and domain deletion cascade into mailbox data and
// the installed hook; the last active admin cannot be removed.
func TestR194_DeleteCascadesAndLastAdmin(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ms, _ := storage.NewMessageStore(filepath.Join(dir, "msgs"))
	mdb, err := storage.OpenDatabase(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()
	srv := NewServer(database, nil, Config{})
	srv.SetMsgStore(ms)
	srv.SetMailDB(mdb)
	hook := &r194Purger{}
	srv.SetAccountPurger(hook)

	_ = database.CreateDomain(&db.DomainData{Name: "a.test", IsActive: true})
	_ = database.CreateDomain(&db.DomainData{Name: "b.test", IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "adm", Domain: "a.test", PasswordHash: "x", IsAdmin: true, IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "bob", Domain: "a.test", PasswordHash: "x", IsActive: true})
	_ = database.CreateAccount(&db.AccountData{LocalPart: "carl", Domain: "b.test", PasswordHash: "x", IsActive: true})
	for _, u := range []string{"bob@a.test", "carl@b.test"} {
		_, _ = ms.StoreMessage(u, []byte("Subject: s\r\n\r\n"+u))
		_ = mdb.CreateMailbox(u, "Work")
	}

	rec := httptest.NewRecorder()
	srv.deleteAccount(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "adm@a.test")
	if rec.Code != http.StatusConflict {
		t.Fatalf("last admin delete: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.deleteAccount(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "bob@a.test")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete bob: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := ms.UserUsage("bob@a.test"); n != 0 {
		t.Errorf("bob blobs survive: %d", n)
	}
	if mbs, _ := mdb.ListMailboxes("bob@a.test"); len(mbs) > 1 {
		t.Errorf("bob metadata survives: %v", mbs)
	}
	if n, _ := ms.UserUsage("carl@b.test"); n == 0 {
		t.Errorf("control: carl blobs removed")
	}
	rec = httptest.NewRecorder()
	srv.deleteDomain(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "a.test")
	if rec.Code != http.StatusConflict {
		t.Fatalf("domain with last admin: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.deleteDomain(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "b.test")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete b.test: %d", rec.Code)
	}
	if n, _ := ms.UserUsage("carl@b.test"); n != 0 {
		t.Errorf("domain delete left carl's blobs: %d", n)
	}
	if len(hook.got) != 2 || hook.got[0] != "bob@a.test" || hook.got[1] != "carl@b.test" {
		t.Errorf("hook calls %v", hook.got)
	}
}
