package backup

// Regression tests for path traversal in the backup path construction:
// BackupUser, BackupMailbox, ListUserBackups, and Restore's TargetUser
// joined caller-supplied user/mailbox values into filesystem paths without
// validation, so user="../victim" backed up another user's mail (cross-user
// read via the backup archive), user=".." listed foreign backup directories,
// and TargetUser="../victim" restored into another user's maildir.
//
// Contract basis: the repo's own MessageStore.validatePathComponent rejects
// "", "..", and any path separator in user-supplied path components; backup
// user/mailbox arguments are the same class of identifier.

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func newTraversalFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	base := t.TempDir()
	for _, user := range []string{"alice", "victim"} {
		dir := filepath.Join(base, "messages", user)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "secret.eml"), []byte("Subject: "+user+"\r\n\r\n"+user+"\r\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return NewManager(base, nil, nil), base
}

func archiveContains(t *testing.T, path, needle string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	buf := make([]byte, 4096)
	for {
		header, err := tr.Next()
		if err != nil {
			return false
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		n, _ := tr.Read(buf)
		if n > 0 && containsBytes(buf[:n], []byte(needle)) {
			return true
		}
	}
}

func containsBytes(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestBackupUserRejectsTraversalUser(t *testing.T) {
	m, _ := newTraversalFixture(t)
	dest := filepath.Join(t.TempDir(), "out.tar.gz")

	// Control: the legitimate user backs up fine.
	if err := m.BackupUser("alice", dest, BackupOptions{}); err != nil {
		t.Fatalf("CONTROL FAILED (harness): BackupUser(alice) = %v", err)
	}

	// Traversal: "../victim" must be rejected, not archived.
	err := m.BackupUser("../victim", dest, BackupOptions{})
	if err == nil {
		if archiveContains(t, dest, "victim") {
			t.Fatalf("FAIL: BackupUser(\"../victim\") archived another user's mail (cross-user read)")
		}
		t.Fatalf("FAIL: BackupUser(\"../victim\") succeeded without validation")
	}
}

func TestBackupMailboxRejectsTraversalMailbox(t *testing.T) {
	m, base := newTraversalFixture(t)
	dest := filepath.Join(t.TempDir(), "out.tar.gz")

	if err := m.BackupMailbox("alice", "../victim", dest, BackupOptions{}); err == nil {
		if archiveContains(t, dest, "victim") {
			t.Fatalf("FAIL: BackupMailbox(alice, \"../victim\") archived another user's mail")
		}
		t.Fatalf("FAIL: BackupMailbox with traversal mailbox succeeded without validation")
	}
	_ = base
}

func TestListUserBackupsRejectsTraversalUser(t *testing.T) {
	m, base := newTraversalFixture(t)
	// Seed a foreign "backup" file one level up to make the leak observable.
	foreignDir := filepath.Join(base, "backups")
	if err := os.MkdirAll(foreignDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foreignDir, "all-users.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	infos, err := m.ListUserBackups("..")
	if err == nil && len(infos) > 0 {
		t.Fatalf("FAIL: ListUserBackups(\"..\") listed %d entries outside the user's backup dir", len(infos))
	}
}

func TestRestoreRejectsTraversalTargetUser(t *testing.T) {
	m, base := newTraversalFixture(t)
	src := filepath.Join(t.TempDir(), "alice.tar.gz")
	if err := m.BackupUser("alice", src, BackupOptions{}); err != nil {
		t.Fatalf("CONTROL FAILED (harness): BackupUser(alice) = %v", err)
	}

	err := m.Restore(src, RestoreOptions{Mode: RestoreModeDifferent, TargetUser: "../victim"})
	if err == nil {
		if _, statErr := os.Stat(filepath.Join(base, "messages", "victim", "alice", "secret.eml")); statErr == nil {
			t.Fatalf("FAIL: Restore restored into another user's maildir via TargetUser traversal")
		}
		t.Fatalf("FAIL: Restore with traversal TargetUser succeeded without validation")
	}
	// The victim's own mail must be untouched.
	data, readErr := os.ReadFile(filepath.Join(base, "messages", "victim", "secret.eml"))
	if readErr != nil || string(data) != "Subject: victim\r\n\r\nvictim\r\n" {
		t.Fatalf("FAIL: victim maildir was modified during restore: %v", readErr)
	}
}
