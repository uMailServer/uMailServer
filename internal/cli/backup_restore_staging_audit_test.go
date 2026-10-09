package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

type stagingMember struct {
	name string
	data string
}

// writeStagingArchive writes a tar.gz with the given members followed by a
// manifest declaring hashes for declared. truncateAfter > 0 cuts the raw tar
// stream to that many bytes before compressing.
func writeStagingArchive(t *testing.T, path string, members []stagingMember, declared map[string]string, truncateAfter int) {
	t.Helper()
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	write := func(name string, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	hashes := []fileHash{}
	for n, c := range declared {
		s := sha256.Sum256([]byte(c))
		hashes = append(hashes, fileHash{Path: n, Hash: hex.EncodeToString(s[:]), Size: int64(len(c))})
	}
	mf, _ := json.Marshal(map[string]interface{}{"version": "1.0.0", "timestamp": "x", "hostname": "h", "files": hashes})
	write("manifest.json", mf)
	for _, m := range members {
		write(m.name, []byte(m.data))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := tb.Bytes()
	if truncateAfter > 0 {
		raw = raw[:truncateAfter]
	}
	var gb bytes.Buffer
	gw := gzip.NewWriter(&gb)
	_, _ = gw.Write(raw)
	_ = gw.Close()
	if err := os.WriteFile(path, gb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newStagingManager(t *testing.T) (*BackupManager, string) {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(data, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config", "a.conf"), []byte("cfg"), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: data, Hostname: "h"}}), root
}

// F4855: a restore that fails part-way must leave no partial restore_temp
// (nor a staging directory) behind.
func TestRestoreFailureLeavesNoPartialTree(t *testing.T) {
	bm, root := newStagingManager(t)
	bf := filepath.Join(root, "b.tar.gz")
	writeStagingArchive(t, bf, []stagingMember{{"config/a.conf", "cfg"}, {"database/umailserver.db", string(bytes.Repeat([]byte("D"), 4096))}}, nil, 512*5)
	if err := bm.Restore(bf); err == nil {
		t.Fatal("truncated archive restored without error")
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.Name() != "data" && e.Name() != "b.tar.gz" {
			t.Errorf("failed restore left %s behind", e.Name())
		}
	}
}

// F4856: restoring into a non-empty restore_temp must be refused rather than
// merged, and must not touch the existing content.
func TestRestoreRefusesNonEmptyRestoreDir(t *testing.T) {
	bm, root := newStagingManager(t)
	a := filepath.Join(root, "a.tar.gz")
	b := filepath.Join(root, "b.tar.gz")
	writeStagingArchive(t, a, []stagingMember{{"messages/old.eml", "deleted mail"}}, nil, 0)
	writeStagingArchive(t, b, []stagingMember{{"config/new.conf", "new"}}, nil, 0)
	if err := bm.Restore(a); err != nil {
		t.Fatal(err)
	}
	if err := bm.Restore(b); err == nil {
		t.Fatal("restore merged into a non-empty restore_temp")
	}
	rt := filepath.Join(root, "restore_temp")
	if _, err := os.Stat(filepath.Join(rt, "messages", "old.eml")); err != nil {
		t.Errorf("existing restore_temp content lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rt, "config", "new.conf")); err == nil {
		t.Error("second backup was merged into restore_temp")
	}
}

// F4857: a file declared in the manifest but absent from the archive must
// fail the restore.
func TestRestoreRejectsMissingManifestFile(t *testing.T) {
	bm, root := newStagingManager(t)
	bf := filepath.Join(root, "b.tar.gz")
	declared := map[string]string{"config/a.conf": "cfg", "database/umailserver.db": "DB"}
	writeStagingArchive(t, bf, []stagingMember{{"config/a.conf", "cfg"}}, declared, 0)
	if err := bm.Restore(bf); err == nil {
		t.Fatal("restore succeeded with database missing from archive")
	}
	if _, err := os.Stat(filepath.Join(root, "restore_temp")); !os.IsNotExist(err) {
		t.Errorf("incomplete restore was published: %v", err)
	}
}

// F4858: Backup must not overwrite an existing backup with the same name.
func TestBackupDoesNotOverwriteExistingFile(t *testing.T) {
	bm, root := newStagingManager(t)
	dir := filepath.Join(root, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Second)
	for i := 0; i < 12; i++ {
		n := filepath.Join(dir, fmt.Sprintf("umailserver_backup_%s.tar.gz", start.Add(time.Duration(i)*time.Second).Format("20060102_150405")))
		if err := os.WriteFile(n, []byte("EARLIER"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := bm.Backup(dir); err == nil {
		t.Fatal("Backup reported success over an existing backup file")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if b, _ := os.ReadFile(filepath.Join(dir, e.Name())); string(b) != "EARLIER" {
			t.Errorf("existing backup %s was overwritten", e.Name())
		}
	}
}
