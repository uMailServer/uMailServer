package search

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func r160Service(t *testing.T) (*Service, *storage.Database) {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db, nil, nil), db
}

func r160Store(t *testing.T, db *storage.Database, user, folder string, uid uint32, subj string) {
	t.Helper()
	if err := db.CreateMailbox(user, folder); err != nil {
		_ = err // may already exist
	}
	if err := db.StoreMessageMetadata(user, folder, uid, &storage.MessageMetadata{MessageID: fmt.Sprint(uid), Subject: subj}); err != nil {
		t.Fatal(err)
	}
}

// F6410: a hit whose message no longer exists (deleted/moved without an
// index notification) must not be returned.
func TestF6410_NoStaleHitsAfterDelete(t *testing.T) {
	s, db := r160Service(t)
	r160Store(t, db, "u", "INBOX", 1, "zebra report")
	if err := s.BuildIndex("u"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMessage("u", "INBOX", 1); err != nil {
		t.Fatal(err)
	}
	res, err := s.Search(MessageSearchOptions{User: "u", Query: "zebra"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("stale hit returned: %+v", res)
	}
}

// F6411: per-user isolation.
func TestF6411_UserIsolation(t *testing.T) {
	s, db := r160Service(t)
	r160Store(t, db, "alice", "INBOX", 1, "secretword")
	r160Store(t, db, "bob", "INBOX", 1, "other")
	res, err := s.Search(MessageSearchOptions{User: "bob", Query: "secretword"})
	if err != nil || len(res) != 0 {
		t.Fatalf("leak: %v %+v", err, res)
	}
}

// F6412: oversized tokens and content must not grow the index unboundedly.
func TestF6412_IndexGrowthBounded(t *testing.T) {
	idx := NewIndex()
	idx.Add(&Document{ID: "INBOX:1", Content: strings.Repeat("a", 5<<20)})
	for tok := range idx.tokens {
		if len(tok) > 512 {
			t.Fatalf("token of %d bytes indexed", len(tok))
		}
	}
	words := strings.Repeat("w", 1) // many distinct words
	_ = words
	var b strings.Builder
	for i := 0; i < 400000; i++ {
		fmt.Fprintf(&b, "tok%d ", i)
	}
	idx2 := NewIndex()
	idx2.Add(&Document{ID: "INBOX:2", Content: b.String()})
	if n := len(idx2.tokens); n > 200000 {
		t.Fatalf("vocabulary of %d tokens from one message", n)
	}
}

// F6413: IndexMessage / RemoveMessage racing BuildIndex.
func TestF6413_ConcurrentIndexSearch(t *testing.T) {
	s, db := r160Service(t)
	for i := uint32(1); i <= 20; i++ {
		r160Store(t, db, "u", "INBOX", i, "alpha beta")
	}
	if err := s.BuildIndex("u"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				uid := uint32(1 + (i+g)%20)
				switch i % 4 {
				case 0:
					_ = s.IndexMessage("u", "INBOX", uid)
				case 1:
					s.RemoveMessage("u", "INBOX", uid)
				case 2:
					_, _ = s.Search(MessageSearchOptions{User: "u", Query: "alpha"})
				default:
					if g == 0 {
						_ = s.BuildIndex("u")
					}
				}
			}
		}(g)
	}
	wg.Wait()
}

// F6414: empty / punctuation-only queries return nothing without panicking.
func TestF6414_EmptyQuery(t *testing.T) {
	idx := NewIndex()
	idx.Add(&Document{ID: "INBOX:1", Content: "hello"})
	for _, q := range []string{"", "   ", ":::", "from:", "!!!"} {
		if r := idx.Search(q, SearchOptions{}); len(r) != 0 {
			t.Errorf("query %q returned %v", q, r)
		}
	}
}
