package backup

// Regression test for the Verify no-op defect: Manager.Verify validated only
// the 10-byte gzip header — it opened a gzip reader and immediately closed
// it, never decompressing the stream or reading the tar structure — so a
// backup truncated mid-stream, or ANY gzip file (of random junk), verified
// as intact. Verify is the integrity API of the live backup path (wired via
// internal/server/server_api.go -> internal/api SetBackupManager), and
// green-lighting a corrupt archive surfaces data loss mid-restore. Expected
// (method contract): "Verify checks backup file integrity" — the gzip stream
// must decompress completely and the payload must be a well-formed tar with
// at least one entry (the format backupUserToPath writes).

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyAcceptsValidBackup(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)

	maildir := base + "/messages/alice"
	if err := os.MkdirAll(maildir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maildir+"/msg1.eml", []byte("Subject: t\r\n\r\nb\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := base + "/valid.tar.gz"
	if err := m.BackupUser("alice", archive, BackupOptions{}); err != nil {
		t.Fatalf("CONTROL FAILED: BackupUser: %v", err)
	}
	if _, err := m.Verify(archive); err != nil {
		t.Fatalf("CONTROL FAILED: Verify(valid archive) = %v, want nil", err)
	}
}

func TestVerifyRejectsTruncatedBackup(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)

	maildir := base + "/messages/alice"
	if err := os.MkdirAll(maildir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maildir+"/msg1.eml", []byte("Subject: t\r\n\r\nb\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := base + "/valid.tar.gz"
	if err := m.BackupUser("alice", archive, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	full, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) < 40 {
		t.Fatalf("fixture too small to truncate: %d bytes", len(full))
	}

	truncated := base + "/truncated.tar.gz"
	if err := os.WriteFile(truncated, full[:20], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Verify(truncated); err == nil {
		t.Fatalf("FAIL: Verify accepted a TRUNCATED backup (20 of %d bytes) — no structural verification performed", len(full))
	}
}

func TestVerifyRejectsNonTarGzip(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)

	// gzip of non-tar junk passes the 10-byte header check but is not a
	// backup archive.
	junk := base + "/junk.tar.gz"
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte("this is definitely not a tar archive payload")); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(junk, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Verify(junk); err == nil {
		t.Fatalf("FAIL: Verify accepted gzip of NON-TAR junk as a valid backup")
	}
}

func TestVerifyRejectsMissingFile(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)

	if _, err := m.Verify(filepath.Join(base, "does-not-exist.tar.gz")); err == nil {
		t.Fatalf("FAIL: Verify accepted a nonexistent backup file")
	}
}
