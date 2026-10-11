package imap

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// F6362: two identical messages share one blob; EXPUNGE of one must keep the
// other readable and must release quota only with the last reference.
func TestExpunge_IdenticalMessagesShareBlobSafely(t *testing.T) {
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	var used atomic.Int64
	ms.SetQuotaAdjustFunc(func(_ string, d int64) error { used.Add(d); return nil })
	user := "u@example.com"
	body := []byte("Subject: dup\r\n\r\nsame body\r\n")
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(user, mb); err != nil {
			t.Fatal(err)
		}
		if err := ms.AppendMessage(user, mb, nil, time.Now(), body); err != nil {
			t.Fatal(err)
		}
	}
	if used.Load() != int64(len(body)) {
		t.Fatalf("quota charged %d, want one blob (%d)", used.Load(), len(body))
	}
	if err := ms.StoreFlags(user, "INBOX", "1", []string{"\\Deleted"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	if err := ms.Expunge(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	uids, _ := ms.db.GetMessageUIDs(user, "Archive")
	meta, _ := ms.db.GetMessageMetadata(user, "Archive", uids[0])
	if got, err := ms.msgStore.ReadMessage(user, meta.MessageID); err != nil || string(got) != string(body) {
		t.Fatalf("surviving message lost its body: %v", err)
	}
	if used.Load() != int64(len(body)) {
		t.Fatalf("quota released while a reference remains: %d", used.Load())
	}
	// Direct unconditional-looking delete is refused while referenced.
	if err := ms.msgStore.DeleteMessage(user, meta.MessageID); !errors.Is(err, storage.ErrBlobReferenced) {
		t.Fatalf("DeleteMessage = %v, want ErrBlobReferenced", err)
	}
	if err := ms.StoreFlags(user, "Archive", "1", []string{"\\Deleted"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	if err := ms.Expunge(user, "Archive"); err != nil {
		t.Fatal(err)
	}
	if ms.msgStore.MessageExists(user, meta.MessageID) || used.Load() != 0 {
		t.Fatalf("last reference gone: exists=%v used=%d", ms.msgStore.MessageExists(user, meta.MessageID), used.Load())
	}
}
