package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func r256Store(t *testing.T) *MessageStore {
	t.Helper()
	s, err := NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobReferenceChecker(func(string, []string) (map[string]bool, error) {
		return map[string]bool{}, nil // no metadata row yet
	})
	return s
}

func r256Age(t *testing.T, s *MessageStore, user, id string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(s.basePath, user, id[:2], id[2:4], id)
	old := time.Now().Add(-age)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	return p
}

// F7336: a dedupe hit on an old unreferenced blob must restart its orphan-GC
// grace period; otherwise a sweep running between the store and the caller's
// metadata write deletes the blob the new row is about to reference.
func TestF7336_DedupeHitSurvivesGC(t *testing.T) {
	for _, quota := range []bool{false, true} {
		s := r256Store(t)
		data := []byte("Subject: x\r\n\r\nbody")
		id, err := s.StoreMessage("u", data)
		if err != nil {
			t.Fatal(err)
		}
		p := r256Age(t, s, "u", id, 3*time.Hour) // orphan for hours
		if quota {
			_, err = s.StoreMessageWithQuota("u", data, 1<<20)
		} else {
			_, err = s.StoreMessage("u", data)
		}
		if err != nil {
			t.Fatal(err)
		}
		// Ordering gate: GC runs after the dedupe hit, before the row exists.
		n, err := s.GCOrphanBlobs("u")
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("quota=%v: GC removed a blob just re-stored (dedupe hit)", quota)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("quota=%v: blob gone: %v", quota, err)
		}
	}
}

// F7337: publishing a blob fsyncs the shard dirs, user dir and store root,
// and a dedupe hit fsyncs the leaf directory.
func TestF7337_DirectoryFsyncChain(t *testing.T) {
	s, err := NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var synced []string
	s.onDirSync = func(d string) { synced = append(synced, d) }
	id, err := s.StoreMessage("u", []byte("hello fsync"))
	if err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(s.basePath, "u")
	leaf := filepath.Join(user, id[:2], id[2:4])
	want := []string{leaf, filepath.Dir(leaf), user, s.basePath}
	if strings.Join(synced, "|") != strings.Join(want, "|") {
		t.Fatalf("synced %v, want %v", synced, want)
	}
	synced = nil
	if _, err := s.StoreMessage("u", []byte("hello fsync")); err != nil {
		t.Fatal(err)
	}
	if len(synced) != 1 || synced[0] != leaf {
		t.Fatalf("dedupe hit synced %v, want [%s]", synced, leaf)
	}
}

// F7332: only content hashes and copy-qualified ids are valid blob keys.
func TestF7332_MessageIDGrammar(t *testing.T) {
	h := strings.Repeat("ab", 32)
	good := []string{h, strings.ToUpper(h), h + "_12_SU5CT1g", h + "_1_a_b-c", h + "_0_" + "x"}
	bad := []string{"", "abcd", h[:63], h + "/x", "ab/cd/" + h, "/" + h[1:], h + "_x_abc", h + "_1_", h + "__abc", h + "_1", h + "x",
		h + "_1_a/b", h + "_1_..", h + "\x00", strings.Repeat("g", 64), h + "_1_" + strings.Repeat("a", 200)}
	for _, id := range good {
		if !isValidMessageID(id) {
			t.Errorf("rejected valid id %q", id)
		}
	}
	for _, id := range bad {
		if isValidMessageID(id) {
			t.Errorf("accepted invalid id %q", id)
		}
	}
}
