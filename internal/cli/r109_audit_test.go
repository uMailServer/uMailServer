package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

// F5912: symlinks under the data dir must not break the backup.
func TestBackupSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	_ = os.MkdirAll(filepath.Join(data, "dkim"), 0o750)
	_ = os.WriteFile(filepath.Join(data, "dkim", "k.pem"), []byte("key"), 0o600)
	target := filepath.Join(root, "outside")
	_ = os.WriteFile(target, bytes.Repeat([]byte("z"), 4000), 0o600)
	if err := os.Symlink(target, filepath.Join(data, "dkim", "ln")); err != nil {
		t.Skip("no symlinks")
	}
	bm := NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: data, Hostname: "h"}})
	if err := bm.Backup(filepath.Join(root, "out")); err != nil {
		t.Fatalf("backup: %v", err)
	}
}

// F5913: a manifest claiming an enormous size must be rejected, not allocated.
func TestRestoreHugeManifestRejected(t *testing.T) {
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: 1 << 40, Typeflag: tar.TypeReg})
	var gb bytes.Buffer
	gw := gzip.NewWriter(&gb)
	_, _ = gw.Write(tb.Bytes())
	_ = gw.Close()
	root := t.TempDir()
	p := filepath.Join(root, "b.tar.gz")
	_ = os.WriteFile(p, gb.Bytes(), 0o600)
	bm := NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: filepath.Join(root, "data")}})
	err := bm.Restore(p)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("want out-of-range error, got %v", err)
	}
}
