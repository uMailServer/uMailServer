package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// F5912: a symlink in the maildir made the backup fail (tar.ErrWriteTooLong).
func TestBackupFullSkipsSymlinks(t *testing.T) {
	m, base := newTraversalFixture(t)
	outside := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(outside, bytes.Repeat([]byte("x"), 5000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "messages", "alice", "link")); err != nil {
		t.Skip("symlinks unsupported")
	}
	dest := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := m.BackupFull(dest, BackupOptions{}); err != nil {
		t.Fatalf("BackupFull with symlink: %v", err)
	}
	if archiveContains(t, dest, "xxxxxxxx") {
		t.Fatal("symlink target content leaked into archive")
	}
}

func buildR109Archive(t *testing.T, trunc bool) string {
	t.Helper()
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	_ = tw.WriteHeader(&tar.Header{Name: "alice/", Mode: 0o000, Typeflag: tar.TypeDir})
	data := bytes.Repeat([]byte("m"), 3000)
	_ = tw.WriteHeader(&tar.Header{Name: "alice/a.eml", Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(data)
	_ = tw.Close()
	raw := tb.Bytes()
	if trunc {
		raw = raw[:len(raw)-1500]
	}
	var gb bytes.Buffer
	gw := gzip.NewWriter(&gb)
	_, _ = gw.Write(raw)
	_ = gw.Close()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, gb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// F5911: a directory header with mode 0 must not block extracting its members.
func TestRestoreZeroModeDir(t *testing.T) {
	m := NewManager(t.TempDir(), nil, nil)
	if err := m.Restore(buildR109Archive(t, false), RestoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

// F5914: a truncated archive must not leave partially restored files.
func TestRestoreTruncatedLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil, nil)
	if err := m.Restore(buildR109Archive(t, true), RestoreOptions{Overwrite: true}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(dir, "messages", "alice", "a.eml")); err == nil {
		t.Fatal("partial file restored from truncated archive")
	}
}
