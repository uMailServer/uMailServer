package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// TestRestore_TruncatedArchivePreservesExistingFile is the regression for
// F5260: Restore with Overwrite truncated the existing message before reading
// the archive payload, so a truncated archive replaced live mail with a
// partial fragment. A failed restore must leave the existing file intact.
func TestRestore_TruncatedArchivePreservesExistingFile(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)

	body := make([]byte, 256*1024)
	rand.New(rand.NewSource(5260)).Read(body) // incompressible, deterministic
	archive := filepath.Join(base, "good.tar.gz")
	buildArchive(t, archive, []tarEntry{{name: "alice/cur/msg1", body: string(body)}})

	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(base, "truncated.tar.gz")
	if err := os.WriteFile(truncated, raw[:len(raw)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(base, "messages", "alice", "cur", "msg1")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("Subject: live mail\r\n\r\nthe only copy\r\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := m.Restore(truncated, RestoreOptions{Overwrite: true}); err == nil {
		t.Fatal("Restore accepted a truncated archive")
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, original) {
		t.Fatalf("failed restore clobbered existing message: got %d bytes", len(got))
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("failed restore left %d entries in target dir, want 1", len(entries))
	}

	// Control: the intact archive still replaces the file.
	if err := m.Restore(archive, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("intact restore: %v", err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, body) {
		t.Fatal("intact restore content mismatch")
	}
}

// TestRestore_RejectsGzipChecksumMismatch is the regression for F5261:
// Restore stopped at the tar end-of-archive marker and never read the gzip
// footer, so a payload corrupted without breaking deflate framing restored
// with a nil error even though Verify rejects the same archive.
func TestRestore_RejectsGzipChecksumMismatch(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base, nil, nil)

	body := []byte("Amount due: 100 EUR\r\n")
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.NoCompression)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "alice/cur/msg1", Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	raw := buf.Bytes()
	idx := bytes.Index(raw, []byte("100 EUR"))
	if idx < 0 {
		t.Fatal("payload not found in stored gzip stream")
	}
	raw[idx] = '9'
	corrupt := filepath.Join(base, "corrupt.tar.gz")
	if err := os.WriteFile(corrupt, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Verify(corrupt); err == nil {
		t.Fatal("precondition: Verify should reject the corrupted archive")
	}
	if err := m.Restore(corrupt, RestoreOptions{Overwrite: true}); err == nil {
		t.Fatal("Restore reported success for an archive with a gzip checksum mismatch")
	}
}
