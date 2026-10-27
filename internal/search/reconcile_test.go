package search

// F7776: only deliveries and IMAP expunges updated the index, so a message
// moved, copied or appended any other way vanished from search.

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const reconcileUser = "u@x.test"

type reconcileFixture struct {
	db    *storage.Database
	store *storage.MessageStore
	svc   *Service
}

func newReconcileFixture(t *testing.T) *reconcileFixture {
	t.Helper()
	db, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, mb := range []string{"INBOX", "Archive", "Sent"} {
		if err := db.CreateMailbox(reconcileUser, mb); err != nil {
			t.Fatal(err)
		}
	}
	return &reconcileFixture{db: db, store: store, svc: NewService(db, store, nil)}
}

func (f *reconcileFixture) put(t *testing.T, mailbox string, uid uint32, body string) {
	t.Helper()
	id, err := f.store.StoreMessage(reconcileUser, []byte("Subject: s\r\n\r\n"+body+"\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.StoreMessageMetadata(reconcileUser, mailbox, uid, &storage.MessageMetadata{
		UID: uid, MessageID: id, Subject: "s", InternalDate: time.Unix(1700000000, 0)}); err != nil {
		t.Fatal(err)
	}
}

func (f *reconcileFixture) folders(t *testing.T, q string) []string {
	t.Helper()
	res, err := f.svc.Search(MessageSearchOptions{User: reconcileUser, Query: q})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, r := range res {
		out = append(out, r.Folder)
	}
	sort.Strings(out)
	return out
}

func TestSearchFollowsMailboxChanges(t *testing.T) {
	f := newReconcileFixture(t)
	f.put(t, "INBOX", 1, "zebracorn report")
	f.put(t, "INBOX", 2, "llamabeam notes")
	if err := f.svc.IndexMessage(reconcileUser, "INBOX", 1); err != nil {
		t.Fatal(err)
	}

	// Move 1 to Archive, copy 2 to Archive, append a Sent copy, and delete
	// nothing through RemoveMessage.
	f.put(t, "Archive", 1, "zebracorn report")
	if err := f.db.DeleteMessage(reconcileUser, "INBOX", 1); err != nil {
		t.Fatal(err)
	}
	f.put(t, "Archive", 2, "llamabeam notes")
	f.put(t, "Sent", 1, "quokkaflare minutes")

	for q, want := range map[string][]string{
		"zebracorn":   {"Archive"},
		"llamabeam":   {"Archive", "INBOX"},
		"quokkaflare": {"Sent"},
	} {
		if got := f.folders(t, q); len(got) != len(want) || (len(want) > 0 && got[0] != want[0]) || (len(want) > 1 && got[1] != want[1]) {
			t.Errorf("%s: found in %v, want %v", q, got, want)
		}
	}

	// A message deleted without RemoveMessage leaves the index too.
	if err := f.db.DeleteMessage(reconcileUser, "Sent", 1); err != nil {
		t.Fatal(err)
	}
	if got := f.folders(t, "quokkaflare"); len(got) != 0 {
		t.Errorf("deleted message still found in %v", got)
	}
	f.svc.mu.RLock()
	n := f.svc.indexes[reconcileUser].DocCount()
	f.svc.mu.RUnlock()
	if n != 3 {
		t.Errorf("index holds %d documents, want 3", n)
	}
}

func TestSearchReconcileConcurrent(t *testing.T) {
	f := newReconcileFixture(t)
	for uid := uint32(1); uid <= 20; uid++ {
		f.put(t, "INBOX", uid, "sharedword")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.svc.Search(MessageSearchOptions{User: reconcileUser, Query: "sharedword", Limit: 100}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := f.folders(t, "sharedword"); len(got) != 20 {
		t.Errorf("found %d, want 20", len(got))
	}
}
