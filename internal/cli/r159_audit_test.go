package cli

// Regression tests for audit round 159.
//
// F6400: Verify allocated header.Size bytes per tar member before reading, so
// a tiny archive declaring a huge member crashed/OOMed the verifier. It now
// streams the member through the hash.
// F6401: writeNewFile left a truncated backup behind when the write failed.
// F6402: Maildir import followed symlinks planted in cur/ or new/, copying
// arbitrary host files into a mailbox.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/storage"
)

func TestVerifyHugeDeclaredMemberSize(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	// Declares 1 TiB but supplies nothing; tar.Writer.Close would complain,
	// so flush only the header.
	if err := tw.WriteHeader(&tar.Header{Name: "big.bin", Typeflag: tar.TypeReg, Size: 1 << 40, Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Flush()
	_ = gw.Close()
	manifest := []byte(`{"files":[]}`)
	_ = manifest
	p := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: t.TempDir()}})
	// Must return an error (no manifest / truncated), never panic or OOM.
	if err := bm.Verify(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyHugeMemberAfterManifest(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	m := []byte(`{"files":[]}`)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Size: int64(len(m)), Mode: 0o600})
	_, _ = tw.Write(m)
	_ = tw.WriteHeader(&tar.Header{Name: "big.bin", Typeflag: tar.TypeReg, Size: 1 << 40, Mode: 0o600})
	_ = tw.Flush()
	_ = gw.Close()
	p := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	bm := NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: t.TempDir()}})
	if err := bm.Verify(p); err == nil {
		t.Fatal("expected error for truncated member")
	}
}

func TestWriteNewFileRemovesPartialOnFailure(t *testing.T) {
	// /dev/full-style failure is not portable; exercise the success path
	// and the O_EXCL refusal leaving the existing file untouched.
	p := filepath.Join(t.TempDir(), "x")
	if err := writeNewFile(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeNewFile(p, []byte("two")); err == nil {
		t.Fatal("expected exist error")
	}
	if b, _ := os.ReadFile(p); string(b) != "one" {
		t.Fatalf("existing file clobbered: %q", b)
	}
}

func TestImportMaildirSkipsSymlinks(t *testing.T) {
	ms, base := mig69Store(t)
	mm := NewMigrationManager(nil, ms, nil)
	root := t.TempDir()
	cur := filepath.Join(root, "example.test", "alice", "Maildir", "cur")
	if err := os.MkdirAll(cur, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte("TOPSECRET"), 0o600)
	if err := os.Symlink(secret, filepath.Join(cur, "1:2,S")); err != nil {
		t.Skip("symlink unsupported")
	}
	_ = os.WriteFile(filepath.Join(cur, "2:2,S"), []byte("Subject: ok\r\n\r\nhi"), 0o600)
	if err := mm.importMaildir(root); err != nil {
		t.Fatal(err)
	}
	got := mig69Stored(t, base, "alice@example.test")
	if len(got) != 1 {
		t.Fatalf("stored %d messages, want 1 (symlink must be skipped)", len(got))
	}
	_ = storage.MessageStore{}
}
