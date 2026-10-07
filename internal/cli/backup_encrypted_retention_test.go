package cli

// Regression tests for the encrypted-archive blind spot in
// CleanupOldBackups and ListBackups (round 24 of the post-merge hunt).
// Both functions filtered with filepath.Ext(name) != ".gz", which treats
// ".tar.gz.enc" as a non-archive (Ext(".tar.gz.enc") == ".enc"), so
// encrypted backups were never retention-cleaned and were invisible to
// listings. Pinned contract: cleanup deletes expired .tar.gz and
// .tar.gz.enc archives alike (and nothing else); ListBackups reports both
// archive kinds.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditCleanupDeletesExpiredEncryptedArchives(t *testing.T) {
	tempDir := t.TempDir()
	bm := auditCleanupManager()

	oldEnc := filepath.Join(tempDir, "umailserver_backup_20240101_120000.tar.gz.enc")
	oldPlain := filepath.Join(tempDir, "umailserver_backup_20240102_120000.tar.gz")
	freshEnc := filepath.Join(tempDir, "umailserver_backup_20990101_120000.tar.gz.enc")
	unrelated := filepath.Join(tempDir, "notes.txt")

	auditWriteFile(t, oldEnc, 64, 48*time.Hour)
	auditWriteFile(t, oldPlain, 64, 48*time.Hour)
	auditWriteFile(t, freshEnc, 64, 0)
	auditWriteFile(t, unrelated, 64, 48*time.Hour)

	deleted, err := bm.CleanupOldBackups(tempDir, 1)
	if err != nil {
		t.Fatalf("CleanupOldBackups failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deletions, got %d", deleted)
	}
	for _, want := range []string{oldEnc, oldPlain} {
		if _, statErr := os.Stat(want); !os.IsNotExist(statErr) {
			t.Errorf("PROBLEM CONFIRMED: expired encrypted archive survived retention cleanup: %s", filepath.Base(want))
		}
	}
	for _, keep := range []string{freshEnc, unrelated} {
		if _, statErr := os.Stat(keep); os.IsNotExist(statErr) {
			t.Errorf("PROBLEM CONFIRMED: cleanup removed a file it must keep: %s", filepath.Base(keep))
		}
	}
}

func TestAuditListBackupsIncludesEncryptedArchives(t *testing.T) {
	tempDir := t.TempDir()
	bm := auditCleanupManager()

	plain := filepath.Join(tempDir, "umailserver_backup_20240101_120000.tar.gz")
	enc := filepath.Join(tempDir, "umailserver_backup_20240102_120000.tar.gz.enc")
	auditWriteFile(t, plain, 32, 0)
	auditWriteFile(t, enc, 48, 0)
	auditWriteFile(t, filepath.Join(tempDir, "not_a_backup.txt"), 8, 0)

	backups, err := bm.ListBackups(tempDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("PROBLEM CONFIRMED: expected 2 listed archives, got %d", len(backups))
	}
	names := map[string]bool{}
	for _, b := range backups {
		names[b.Filename] = true
	}
	if !names[filepath.Base(plain)] || !names[filepath.Base(enc)] {
		t.Errorf("PROBLEM CONFIRMED: ListBackups missed an archive; listed %v", names)
	}
}
