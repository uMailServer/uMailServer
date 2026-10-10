package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func seedMessage(t *testing.T, base, user, body string) {
	t.Helper()
	p := filepath.Join(base, "messages", user, "cur", "msg1")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRestore_DifferentUserStripsSourceUser is the regression for F5350:
// per-user archive entries start with the source user, so different-user
// restore extracted to messages/<target>/<source>/... where the target user's
// mailstore never looks.
func TestRestore_DifferentUserStripsSourceUser(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)
	seedMessage(t, base, "alice", "hello")
	archive := filepath.Join(base, "alice.tar.gz")
	if err := m.BackupUser("alice", archive, BackupOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := m.Restore(archive, RestoreOptions{Mode: RestoreModeDifferent, TargetUser: "bob", Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(base, "messages", "bob", "cur", "msg1")); err != nil || string(b) != "hello" {
		t.Fatalf("messages/bob/cur/msg1 = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(base, "messages", "bob", "alice")); !os.IsNotExist(err) {
		t.Fatal("restore nested the source user under the target user")
	}
}

// TestRestore_DifferentUserRejectsMultiUserArchive guards the F5350 fix: an
// archive with several users must not be merged into one target user.
func TestRestore_DifferentUserRejectsMultiUserArchive(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)
	multi := filepath.Join(base, "multi.tar.gz")
	buildArchive(t, multi, []tarEntry{{name: "alice/cur/a", body: "a"}, {name: "carol/cur/c", body: "c"}})
	if err := m.Restore(multi, RestoreOptions{Mode: RestoreModeDifferent, TargetUser: "dave", Overwrite: true}); err == nil {
		t.Fatal("multi-user archive accepted for different-user restore")
	}
	if _, err := os.Stat(filepath.Join(base, "messages", "dave", "cur", "c")); !os.IsNotExist(err) {
		t.Fatal("another user's mail was merged into the target user")
	}
}

// TestBackupFull_RestoreRoundTrip is the regression for F5351: full backups
// were prefixed with "messages/" and Restore extracts under messages/, so a
// full restore landed in messages/messages/<user>.
func TestBackupFull_RestoreRoundTrip(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)
	seedMessage(t, base, "alice", "a")
	seedMessage(t, base, "bob", "b")
	full := filepath.Join(base, "full.tar.gz")
	if err := m.BackupFull(full, BackupOptions{}); err != nil {
		t.Fatal(err)
	}

	dst := NewManager(t.TempDir(), nil, nil)
	if err := dst.Restore(full, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[string]string{"alice": "a", "bob": "b"} {
		b, err := os.ReadFile(filepath.Join(dst.dataDir, "messages", user, "cur", "msg1"))
		if err != nil || string(b) != want {
			t.Fatalf("%s: got %q, %v", user, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst.dataDir, "messages", "messages")); !os.IsNotExist(err) {
		t.Fatal("full restore created messages/messages")
	}
}
