package api

// Regression tests for the mail.go markAsRead read-modify-write race:
// markAsRead read the metadata, appended \Seen to its stale copy, and wrote
// the whole metadata back — silently discarding any flags change made by a
// concurrent writer (an IMAP/JMAP keyword, another read session) inside the
// window. The fault-injection seam (markAsReadSeam) held the handler between
// its read and write to prove the loss deterministically; the fix routes the
// flag mutation through the atomic UpdateMessageMetadataFunc (storage-level
// single-transaction RMW), so concurrent changes always coexist.

import (
	"sync"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func markAsReadRaceServer(t *testing.T) (*MailHandler, *storage.Database, *storage.MessageStore, uint32) {
	t.Helper()
	db, err := storage.OpenDatabase(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	t.Cleanup(func() { msgStore.Close() })

	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	body := []byte("Subject: race\r\n\r\nbody\r\n")
	blobID, err := msgStore.StoreMessage("user@example.com", body)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	uid, err := db.GetNextUID("user@example.com", "INBOX")
	if err != nil {
		t.Fatalf("GetNextUID: %v", err)
	}
	if err := db.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID: blobID,
		UID:       uid,
		Flags:     []string{},
		Subject:   "race",
		Size:      int64(len(body)),
	}); err != nil {
		t.Fatalf("StoreMessageMetadata: %v", err)
	}
	return &MailHandler{mailDB: db, msgStore: msgStore}, db, msgStore, uid
}

func flagsOf(t *testing.T, db *storage.Database, uid uint32) []string {
	t.Helper()
	meta, err := db.GetMessageMetadata("user@example.com", "INBOX", uid)
	if err != nil || meta == nil {
		t.Fatalf("GetMessageMetadata: err=%v meta=%v", err, meta)
	}
	return meta.Flags
}

func hasStr(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// TestMarkAsReadCoexistsWithConcurrentFlagWriters is the durable regression:
// several concurrent atomic flag writers plus the mark-as-read must ALL land,
// whatever the interleaving (bbolt serializes the single-transaction RMWs and
// each writer re-reads current state). The scenario loop makes a revert to
// the racy Get->Put form fail with high probability while the fixed code is
// deterministically green.
func TestMarkAsReadCoexistsWithConcurrentFlagWriters(t *testing.T) {
	for scenario := 0; scenario < 10; scenario++ {
		handler, db, _, uid := markAsReadRaceServer(t)

		seedMeta, err := db.GetMessageMetadata("user@example.com", "INBOX", uid)
		if err != nil || seedMeta == nil {
			t.Fatalf("scenario %d: seed read: %v", scenario, err)
		}

		labels := []string{"$label1", "$label2", "$label3", "$label4"}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, label := range labels {
			wg.Add(1)
			go func(label string) {
				defer wg.Done()
				<-start
				_ = db.UpdateMessageMetadataFunc("user@example.com", "INBOX", uid, func(m *storage.MessageMetadata) error {
					m.Flags = append(m.Flags, label)
					return nil
				})
			}(label)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			handler.markAsRead("user@example.com", "INBOX", seedMeta.MessageID)
		}()
		close(start)
		wg.Wait()

		flags := flagsOf(t, db, uid)
		if !hasStr(flags, "\\Seen") {
			t.Fatalf("scenario %d: mark-read lost - flags = %v", scenario, flags)
		}
		for _, label := range labels {
			if !hasStr(flags, label) {
				t.Fatalf("scenario %d: concurrent flag %s lost - flags = %v", scenario, label, flags)
			}
		}
	}
}
