//go:build unix

package backup

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestBackupOutputsAreOwnerOnly is the regression for F5352: archives and
// decrypted output were created with os.Create (0666 minus umask), so under
// the usual 022 umask other local users could read mail backups.
func TestBackupOutputsAreOwnerOnly(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)

	base := t.TempDir()
	m := NewManager(base, nil, nil)
	src := filepath.Join(base, "messages", "alice", "INBOX", "cur", "msg1")
	if err := os.MkdirAll(filepath.Dir(src), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("private mail"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(base, "backups", "per-user", "alice")
	user := filepath.Join(out, "user.tar.gz")
	mbox := filepath.Join(out, "mbox.tar.gz")
	full := filepath.Join(out, "full.tar.gz")
	enc := filepath.Join(out, "user.tar.gz.enc")
	dec := filepath.Join(out, "dec.tar.gz")
	if err := m.BackupUser("alice", user, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := m.BackupMailbox("alice", "INBOX", mbox, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := m.BackupFull(full, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Encrypt(user, enc, "pw"); err != nil {
		t.Fatal(err)
	}
	if err := m.Decrypt(enc, dec, "pw"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{user, mbox, full, enc, dec} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("%s: mode %#o, want 0600", filepath.Base(p), got)
		}
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got&0o007 != 0 {
		t.Errorf("backup dir mode %#o is world-accessible", got)
	}
}
