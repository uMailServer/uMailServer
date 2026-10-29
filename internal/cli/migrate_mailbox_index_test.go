package cli

// F7788: `migrate` stored message blobs only; no mailbox index row referenced
// them, so IMAP, JMAP and the web API never listed the imported mail.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/storage"
)

type testMailboxIndex struct {
	store *imap.BboltMailstore
	db    *storage.Database
}

func (m testMailboxIndex) CreateMailbox(u, b string) error { return m.store.CreateMailbox(u, b) }
func (m testMailboxIndex) AppendMessage(u, b string, f []string, d time.Time, data []byte) error {
	return m.store.AppendMessage(u, b, f, d, data)
}
func (m testMailboxIndex) HasMessage(u, b, id string) bool {
	uids, _ := m.db.GetMessageUIDs(u, b)
	for _, uid := range uids {
		if meta, err := m.db.GetMessageMetadata(u, b, uid); err == nil && meta.MessageID == id {
			return true
		}
	}
	return false
}

func newIndexedManager(t *testing.T) (*MigrationManager, *storage.Database) {
	t.Helper()
	dir := t.TempDir()
	ms, err := storage.NewMessageStore(filepath.Join(dir, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	sdb, err := storage.OpenDatabase(filepath.Join(dir, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sdb.Close() })
	mm := NewMigrationManager(nil, ms, nil)
	mm.SetMailboxIndex(testMailboxIndex{store: imap.NewBboltMailstoreWithInterfaces(sdb, ms), db: sdb})
	return mm, sdb
}

func TestMigrateMBOXFilesMessagesIntoTheMailboxIndex(t *testing.T) {
	mm, sdb := newIndexedManager(t)
	p := filepath.Join(t.TempDir(), "alice@example.test.mbox")
	content := "From a@x Mon Jan  1 00:00:00 2024\nSubject: one\nDate: Mon, 01 Jan 2024 10:00:00 +0000\n\nhello\n\n" +
		"From b@x Mon Jan  1 00:00:00 2024\nSubject: two\n\nworld\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ { // the second run must not duplicate
		if err := mm.MigrateFromMBOX(p); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		uids, err := sdb.GetMessageUIDs("alice@example.test", "INBOX")
		if err != nil || len(uids) != 2 {
			t.Fatalf("run %d: INBOX holds %d messages (err %v), want 2", run, len(uids), err)
		}
	}
	uids, _ := sdb.GetMessageUIDs("alice@example.test", "INBOX")
	meta, err := sdb.GetMessageMetadata("alice@example.test", "INBOX", uids[0])
	if err != nil || meta.Subject != "one" || !meta.InternalDate.Equal(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("metadata = %+v (err %v)", meta, err)
	}
}

func TestStoreImportedKeepsFolderAndFlags(t *testing.T) {
	mm, sdb := newIndexedManager(t)
	msg := []byte("Subject: kept\r\n\r\nbody\r\n")
	if _, err := mm.storeImported("bob@example.test", "Archive", []string{"\\Seen", "\\Recent"}, time.Time{}, msg); err != nil {
		t.Fatal(err)
	}
	uids, _ := sdb.GetMessageUIDs("bob@example.test", "Archive")
	if len(uids) != 1 {
		t.Fatalf("Archive holds %d messages, want 1", len(uids))
	}
	meta, _ := sdb.GetMessageMetadata("bob@example.test", "Archive", uids[0])
	seen := false
	for _, f := range meta.Flags {
		if f == "\\Seen" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("flags = %v, want \\Seen kept", meta.Flags)
	}
	if inbox, _ := sdb.GetMessageUIDs("bob@example.test", "INBOX"); len(inbox) != 0 {
		t.Fatalf("INBOX holds %d messages, want 0", len(inbox))
	}
}
