package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// F6362: identical messages share one content-addressed blob; removing one
// must not delete the body of the other.

func newBlobRefFixture(t *testing.T) (*Database, *MessageStore) {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ms, err := NewMessageStore(filepath.Join(t.TempDir(), "messages"))
	if err != nil {
		t.Fatal(err)
	}
	return db, ms
}

func addRef(t *testing.T, db *Database, user, mbox string, uid uint32, id string) {
	t.Helper()
	if err := db.CreateMailbox(user, mbox); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, mbox, uid, &MessageMetadata{UID: uid, MessageID: id, Size: 10}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteMessage_SharedBlobSurvivesFirstDelete(t *testing.T) {
	db, ms := newBlobRefFixture(t)
	ms.SetBlobReferenceChecker(db.BlobReferenced)
	user := "u@example.com"
	body := []byte("Subject: same\r\n\r\nidentical")
	id, err := ms.StoreMessage(user, body)
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := ms.StoreMessage(user, body)
	if id != id2 {
		t.Fatal("expected identical content to share an id")
	}
	addRef(t, db, user, "INBOX", 1, id)
	addRef(t, db, user, "Archive", 1, id)

	// Drop the first reference, then release the blob.
	if err := db.DeleteMessage(user, "INBOX", 1); err != nil {
		t.Fatal(err)
	}
	if err := ms.DeleteMessage(user, id); !errors.Is(err, ErrBlobReferenced) {
		t.Fatalf("DeleteMessage with a live reference = %v, want ErrBlobReferenced", err)
	}
	if got, err := ms.ReadMessage(user, id); err != nil || string(got) != string(body) {
		t.Fatalf("surviving message body lost: %q %v", got, err)
	}
	// Last reference gone: now the file really goes.
	if err := db.DeleteMessage(user, "Archive", 1); err != nil {
		t.Fatal(err)
	}
	if err := ms.DeleteMessage(user, id); err != nil {
		t.Fatalf("final release: %v", err)
	}
	if ms.MessageExists(user, id) {
		t.Fatal("blob should be removed after last reference")
	}
}

func TestDeleteMessage_UsersAreIndependent(t *testing.T) {
	db, ms := newBlobRefFixture(t)
	ms.SetBlobReferenceChecker(db.BlobReferenced)
	body := []byte("same bytes")
	id, _ := ms.StoreMessage("a@x.com", body)
	_, _ = ms.StoreMessage("b@x.com", body)
	addRef(t, db, "b@x.com", "INBOX", 1, id)
	if err := ms.DeleteMessage("a@x.com", id); err != nil {
		t.Fatalf("other user's reference must not pin the blob: %v", err)
	}
	if !ms.MessageExists("b@x.com", id) {
		t.Fatal("b's blob must survive")
	}
}

// Pre-existing data (rows written before the checker was wired) is counted
// from metadata: no migration step exists to lose references.
func TestBlobReferenced_PreExistingRowsCounted(t *testing.T) {
	db, ms := newBlobRefFixture(t)
	user := "u@example.com"
	id, _ := ms.StoreMessage(user, []byte("legacy"))
	addRef(t, db, user, "INBOX", 7, id)
	addRef(t, db, user, "Sent", 3, id)
	ms.SetBlobReferenceChecker(db.BlobReferenced) // "startup"
	if err := ms.DeleteMessage(user, id); !errors.Is(err, ErrBlobReferenced) {
		t.Fatalf("got %v", err)
	}
	refs, err := db.BlobReferenced(user, []string{id, "nope"})
	if err != nil || !refs[id] || refs["nope"] {
		t.Fatalf("refs=%v err=%v", refs, err)
	}
}

func TestBlobReferenced_PendingUploadHoldsBlob(t *testing.T) {
	db, ms := newBlobRefFixture(t)
	ms.SetBlobReferenceChecker(db.BlobReferenced)
	user := "u@example.com"
	id, _ := ms.StoreMessage(user, []byte("upload"))
	if err := db.Bolt().Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(pendingUploadsBucketName))
		if err != nil {
			return err
		}
		return b.Put([]byte(user+"\x00"+id), []byte(`{}`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.DeleteMessage(user, id); !errors.Is(err, ErrBlobReferenced) {
		t.Fatalf("pending upload must pin blob, got %v", err)
	}
}

func TestDeleteMessage_CheckerErrorKeepsBlob(t *testing.T) {
	_, ms := newBlobRefFixture(t)
	ms.SetBlobReferenceChecker(func(string, []string) (map[string]bool, error) {
		return nil, errors.New("boom")
	})
	id, _ := ms.StoreMessage("u@x.com", []byte("keep me"))
	if err := ms.DeleteMessage("u@x.com", id); !errors.Is(err, ErrBlobReferenced) {
		t.Fatalf("got %v", err)
	}
	if !ms.MessageExists("u@x.com", id) {
		t.Fatal("blob deleted despite failed reference check")
	}
}

// Gated ordering: a writer that dedupes onto an existing blob and holds the
// guard until its row is visible must not lose the blob to a concurrent
// release that started in the gap.
func TestUserGuard_ReleaseWaitsForConcurrentReference(t *testing.T) {
	db, ms := newBlobRefFixture(t)
	ms.SetBlobReferenceChecker(db.BlobReferenced)
	user := "u@example.com"
	body := []byte("race body")
	id, _ := ms.StoreMessage(user, body)
	addRef(t, db, user, "INBOX", 1, id)

	stored := make(chan struct{})
	releaseGo := make(chan struct{})
	writerDone := make(chan struct{})
	go func() { // appender of identical content
		defer close(writerDone)
		g := ms.UserGuard(user)
		g.Lock()
		defer g.Unlock()
		_, _ = ms.StoreMessage(user, body) // dedupe hit
		close(stored)
		<-releaseGo // expunger is now queued behind the guard
		addRef(t, db, user, "Archive", 1, id)
	}()
	<-stored
	releaseDone := make(chan error, 1)
	started := make(chan struct{})
	go func() { // expunger of the first copy
		close(started)
		g := ms.UserGuard(user)
		g.Lock()
		defer g.Unlock()
		_ = db.DeleteMessage(user, "INBOX", 1)
		releaseDone <- ms.DeleteMessage(user, id)
	}()
	<-started
	select {
	case <-releaseDone:
		t.Fatal("release ran while the appender held the guard")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseGo)
	<-writerDone
	if err := <-releaseDone; !errors.Is(err, ErrBlobReferenced) {
		t.Fatalf("release should see the new reference, got %v", err)
	}
	if got, err := ms.ReadMessage(user, id); err != nil || string(got) != string(body) {
		t.Fatalf("body lost: %v", err)
	}
}

func TestDeleteMessageUnchecked_RemovesRegardless(t *testing.T) {
	db, ms := newBlobRefFixture(t)
	ms.SetBlobReferenceChecker(db.BlobReferenced)
	id, _ := ms.StoreMessage("u@x.com", []byte("x"))
	addRef(t, db, "u@x.com", "INBOX", 1, id)
	if err := ms.DeleteMessageUnchecked("u@x.com", id); err != nil {
		t.Fatal(err)
	}
}
