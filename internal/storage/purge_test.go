package storage

import (
	"path/filepath"
	"testing"
)

// F6649c: purging a user removes blobs and metadata, nobody else's.
func TestPurgeUserRemovesDataOnlyForUser(t *testing.T) {
	dir := t.TempDir()
	ms, err := NewMessageStore(filepath.Join(dir, "msgs"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenDatabase(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, u := range []string{"bob@x.test", "eve@x.test"} {
		if _, err := ms.StoreMessage(u, []byte("Subject: hi\r\n\r\nbody "+u)); err != nil {
			t.Fatal(err)
		}
		_ = db.CreateMailbox(u, "INBOX")
		_ = db.CreateMailbox(u, "Work")
		_ = db.SetACL(u, "INBOX", "bob@x.test", ACLRead, u)
	}
	if err := db.PurgeUser("bob@x.test"); err != nil {
		t.Fatal(err)
	}
	if err := ms.PurgeUser("bob@x.test"); err != nil {
		t.Fatal(err)
	}
	if n, _ := ms.UserUsage("bob@x.test"); n != 0 {
		t.Errorf("bob blobs left: %d", n)
	}
	if mbs, _ := db.ListMailboxes("bob@x.test"); len(mbs) > 1 { // INBOX is virtual
		t.Errorf("bob mailboxes left: %v", mbs)
	}
	if n, _ := ms.UserUsage("eve@x.test"); n == 0 {
		t.Errorf("control: eve blobs removed")
	}
	if mbs, _ := db.ListMailboxes("eve@x.test"); len(mbs) != 2 {
		t.Errorf("control: eve mailboxes %v", mbs)
	}
	if r, _ := db.GetACL("eve@x.test", "INBOX", "bob@x.test"); r != 0 {
		t.Errorf("ACL granted to purged user survives: %v", r)
	}
	if err := ms.PurgeUser("nobody@x.test"); err != nil { // idempotent
		t.Errorf("purge of unknown user: %v", err)
	}
	if err := ms.PurgeUser("../x"); err == nil {
		t.Errorf("path-traversal user accepted")
	}
}
