package jmap

// Regression tests for the round-61 feature: the background orphan-blob
// sweep (StartBlobGC/sweepBlobGC, the round-60 plan) gave the previously
// uncalled GCOrphanBlobs its production caller. The sweep iterates every
// blob owner (MessageStore.BlobOwners) and removes only blobs that are
// past orphanGCGrace AND unreferenced per the installed checker — so:
// past-grace orphans disappear (UserUsage delta), fresh orphans survive
// the grace window, blobs referenced by a live Email survive even when
// aged (identical content = same blob id; the round-57 guarantee), and
// the sweep covers every user (per-user scoping).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const r61Grace = 2 * time.Hour // comfortably past the 1h orphanGCGrace

func r61Setup(t *testing.T) (*Server, *storage.MessageStore, string) {
	t.Helper()
	tmp := t.TempDir()
	db, err := storage.OpenDatabase(tmp + "/db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ms, err := storage.NewMessageStore(tmp + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ms.Close() })
	server := NewServer(db, ms, nil, Config{JWTSecret: "test-secret-r61"})
	for _, u := range []string{"alice@example.com", "bob@example.com"} {
		if err := db.CreateMailbox(u, "INBOX"); err != nil {
			t.Fatal(err)
		}
	}
	return server, ms, tmp + "/messages"
}

// r61Orphan stores content for user and backdates the blob file so it is
// older than the grace window. It returns the blob id.
func r61Orphan(t *testing.T, ms *storage.MessageStore, basePath, user, content string, age time.Duration) string {
	t.Helper()
	id, err := ms.StoreMessage(user, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(basePath, user, id[:2], id[2:4], id)
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatalf("age blob: %v", err)
	}
	return id
}

// r61Referenced stores content and imports it (a metadata row references
// the blob), returning the blob id.
func r61Referenced(t *testing.T, server *Server, ms *storage.MessageStore, user, content string) string {
	t.Helper()
	id, err := ms.StoreMessage(user, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     id,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "c1",
	}
	resp := server.handleEmailImport(user, call)
	if _, ok := resp.Args["created"].(map[string]Email)["i1"]; !ok {
		t.Fatalf("FAIL: import failed: notCreated=%v", resp.Args["notCreated"])
	}
	return id
}

func r61Usage(t *testing.T, ms *storage.MessageStore, user string) int64 {
	t.Helper()
	n, err := ms.UserUsage(user)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The plan's core cases: the aged orphan of each user is removed (per-user
// scoping; UserUsage drops by exactly the orphan's size), the fresh orphan
// survives the grace window, and the referenced blob survives even when its
// file is aged past the grace (identical content = same blob id as an aged
// orphan candidate).
func TestBlobGCSweepRemovesPastGraceOrphansAcrossUsers(t *testing.T) {
	server, ms, basePath := r61Setup(t)
	const alice, bob = "alice@example.com", "bob@example.com"

	r61Orphan(t, ms, basePath, alice, "aged orphan for alice", r61Grace)
	r61Orphan(t, ms, basePath, bob, "aged orphan for bob", r61Grace)
	freshA := r61Orphan(t, ms, basePath, alice, "fresh orphan for alice", 0)
	refA := r61Referenced(t, server, ms, alice, "referenced content for alice")

	// Age the referenced blob's file too: it is past the grace but
	// referenced, so the checker must protect it.
	p := filepath.Join(basePath, alice, refA[:2], refA[2:4], refA)
	stamp := time.Now().Add(-r61Grace)
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	beforeA := r61Usage(t, ms, alice)
	beforeB := r61Usage(t, ms, bob)

	removed, err := server.sweepBlobGC()
	if err != nil {
		t.Fatalf("FAIL: sweep error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("FAIL: sweep removed %d blobs, want 2 (both users' aged orphans)", removed)
	}

	// Per-user scoping + usage deltas.
	if got := r61Usage(t, ms, alice); got != beforeA-int64(len("aged orphan for alice")) {
		t.Fatalf("FAIL: alice usage = %d, want %d (aged orphan removed, rest kept)", got, beforeA-int64(len("aged orphan for alice")))
	}
	if got := r61Usage(t, ms, bob); got != beforeB-int64(len("aged orphan for bob")) {
		t.Fatalf("FAIL: bob usage = %d, want %d (his only blob was the aged orphan)", got, beforeB-int64(len("aged orphan for bob")))
	}

	// The fresh orphan survives the grace window.
	if _, err := ms.ReadMessage(alice, freshA); err != nil {
		t.Fatalf("FAIL: fresh orphan was removed despite the grace window: %v", err)
	}
	// The referenced blob survives despite being past the grace.
	if _, err := ms.ReadMessage(alice, refA); err != nil {
		t.Fatalf("FAIL: referenced blob was removed: %v", err)
	}
}

// Identical content maps to the same blob id: a blob referenced by a live
// Email must survive the sweep even when an orphan copy of the same content
// would be removed (the round-57 checker guarantee, now under the sweep).
func TestBlobGCSweepKeepsReferencedIdenticalContent(t *testing.T) {
	server, ms, basePath := r61Setup(t)
	const alice = "alice@example.com"
	content := "shared identical content for the round-61 sweep"

	ref := r61Referenced(t, server, ms, alice, content)
	// Age the referenced blob past the grace.
	p := filepath.Join(basePath, alice, ref[:2], ref[2:4], ref)
	stamp := time.Now().Add(-r61Grace)
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	removed, err := server.sweepBlobGC()
	if err != nil {
		t.Fatalf("FAIL: sweep error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("FAIL: sweep removed %d blobs; the referenced blob must survive", removed)
	}
	if _, err := ms.ReadMessage(alice, ref); err != nil {
		t.Fatalf("FAIL: referenced identical-content blob was removed: %v", err)
	}
}

// StartBlobGC is idempotent and Stop ends it without hanging (the upload-GC
// lifecycle contract, mirrored).
func TestBlobGCLifecycleIdempotent(t *testing.T) {
	server, _, _ := r61Setup(t)
	server.StartBlobGC()
	server.StartBlobGC() // second call must be a no-op, not a second goroutine
	server.Stop()        // must end both GC goroutines and return
}
