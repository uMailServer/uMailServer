package cli

// Regression test for the Verify manifest-coverage defect: Verify parsed the
// manifest in the same single pass that walked the archive, but Backup()
// writes the manifest as the LAST member — so expectedHashes was empty for
// every preceding file entry and NOTHING was verified ("0 files verified, 0
// failed" even for intact backups). Worse, the loop never checked that every
// manifest-declared file is present: a truncated or stripped archive —
// missing the database member, for example — verified as intact. RFC-style
// contract: Verify's documented purpose is to check backup file integrity,
// so every declared file must be present and hash-matched. Fixture note:
// backupDatabase reads the DB from the DataDir ROOT, not DataDir/database.

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func verifyFixtureDataDir(t *testing.T) string {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "messages", "alice"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config", "umailserver.yaml"), []byte("config: yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// backupDatabase reads the DB from the DataDir ROOT (not database/).
	if err := os.WriteFile(filepath.Join(dataDir, "umailserver.db"), []byte("pretend-bbolt-db-bytes-0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "messages", "alice", "msg1.eml"), []byte("Subject: t\r\n\r\nb\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

// stripMember copies a tar.gz archive to dst while dropping the member named
// drop, preserving every other member (including manifest.json).
func stripMember(t *testing.T, src, dst, drop string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	gr, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	gw := gzip.NewWriter(out)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for {
		header, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == drop {
			if _, err := io.Copy(io.Discard, tr); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyIntactBackupStillPasses(t *testing.T) {
	dataDir := verifyFixtureDataDir(t)
	bm := NewBackupManager(&config.Config{
		Server: config.ServerConfig{DataDir: dataDir, Hostname: "test.example.com"},
	})

	backupDir := filepath.Join(t.TempDir(), "backups")
	if err := bm.Backup(backupDir); err != nil {
		t.Fatalf("CONTROL FAILED: Backup: %v", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("CONTROL FAILED: expected one backup file, got %d (err=%v)", len(entries), err)
	}
	if err := bm.Verify(filepath.Join(backupDir, entries[0].Name())); err != nil {
		t.Fatalf("CONTROL FAILED: Verify(intact backup) = %v, want nil", err)
	}
}

func TestVerifyRejectsArchiveMissingDeclaredFile(t *testing.T) {
	dataDir := verifyFixtureDataDir(t)
	bm := NewBackupManager(&config.Config{
		Server: config.ServerConfig{DataDir: dataDir, Hostname: "test.example.com"},
	})

	backupDir := filepath.Join(t.TempDir(), "backups")
	if err := bm.Backup(backupDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one backup file, got %d (err=%v)", len(entries), err)
	}
	intact := filepath.Join(backupDir, entries[0].Name())

	// The manifest still declares database/umailserver.db; the archive no
	// longer contains it. Verify must fail on the missing declared file.
	broken := filepath.Join(t.TempDir(), "broken.tar.gz")
	stripMember(t, intact, broken, "database/umailserver.db")

	if err := bm.Verify(broken); err == nil {
		t.Fatalf("FAIL: Verify accepted an archive MISSING the manifest-declared member database/umailserver.db — verification counted only files it happened to see")
	}

	// Control on the same fixture: the intact archive still verifies.
	if err := bm.Verify(intact); err != nil {
		t.Fatalf("CONTROL FAILED: Verify(intact) = %v, want nil", err)
	}
}
