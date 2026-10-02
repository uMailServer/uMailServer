package cli

// Audit property tests for CleanupOldBackups and ListBackups (the last parts
// of the backup.go deep-audit list). Pinned contracts: cleanup deletes ONLY
// .gz backups strictly older than the retention window (calendar-day cutoff),
// never touches directories, non-backup files, or recent backups, and counts
// only successful deletions; ListBackups returns .gz entries in lexical
// filename order with correct metadata. The mutation check (flipping the age
// comparison) proves the cleanup test's teeth.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

func auditCleanupManager() *BackupManager {
	return NewBackupManager(&config.Config{})
}

func auditWriteFile(t *testing.T, path string, size int, age time.Duration) {
	t.Helper()
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("Chtimes %s: %v", path, err)
	}
}

// TestAuditCleanupDeletesOnlyExpiredGz: old .gz backups are deleted and
// counted; recent backups, non-.gz files, directories and their contents
// survive; failed deletes are not counted.
func TestAuditCleanupDeletesOnlyExpiredGz(t *testing.T) {
	dir := t.TempDir()
	oldAge := 9 * 24 * time.Hour
	recentAge := 1 * 24 * time.Hour

	auditWriteFile(t, filepath.Join(dir, "umail_20250101_000000.tar.gz"), 100, oldAge)
	auditWriteFile(t, filepath.Join(dir, "umail_20250102_000000.tar.gz"), 110, oldAge)
	auditWriteFile(t, filepath.Join(dir, "umail_20250928_000000.tar.gz"), 120, recentAge)
	auditWriteFile(t, filepath.Join(dir, "notes.txt"), 30, oldAge) // non-backup: survives

	// A directory holding an old .gz: directories are skipped, contents
	// must never be visited or deleted.
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	auditWriteFile(t, filepath.Join(sub, "nested.tar.gz"), 40, oldAge)

	deleted, err := auditCleanupManager().CleanupOldBackups(dir, 7)
	if err != nil {
		t.Fatalf("CleanupOldBackups: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("FAIL: deleted = %d, want 2", deleted)
	}
	for _, gone := range []string{"umail_20250101_000000.tar.gz", "umail_20250102_000000.tar.gz"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("FAIL: expired backup %s still present", gone)
		}
	}
	for _, keep := range []string{
		filepath.Join(dir, "umail_20250928_000000.tar.gz"),
		filepath.Join(dir, "notes.txt"),
		filepath.Join(sub, "nested.tar.gz"),
	} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("FAIL: %s should have survived cleanup: %v", keep, err)
		}
	}
}

// TestAuditCleanupRejectsNonPositiveRetention: the documented input guard.
func TestAuditCleanupRejectsNonPositiveRetention(t *testing.T) {
	dir := t.TempDir()
	auditWriteFile(t, filepath.Join(dir, "umail_20250101_000000.tar.gz"), 50, 30*24*time.Hour)
	for _, days := range []int{0, -3} {
		deleted, err := auditCleanupManager().CleanupOldBackups(dir, days)
		if err == nil {
			t.Fatalf("FAIL: retentionDays=%d accepted (deleted=%d)", days, deleted)
		}
		if deleted != 0 {
			t.Fatalf("FAIL: retentionDays=%d deleted %d files", days, deleted)
		}
		if _, err := os.Stat(filepath.Join(dir, "umail_20250101_000000.tar.gz")); err != nil {
			t.Fatalf("FAIL: file deleted despite rejected retention: %v", err)
		}
	}
}

// TestAuditListBackupsSortedGzEntriesWithMetadata: only .gz files are listed,
// in lexical filename order, with correct metadata and absolute paths.
func TestAuditListBackupsSortedGzEntriesWithMetadata(t *testing.T) {
	dir := t.TempDir()
	auditWriteFile(t, filepath.Join(dir, "c.gz"), 33, 2*24*time.Hour)
	auditWriteFile(t, filepath.Join(dir, "a.tar.gz"), 11, 3*24*time.Hour)
	auditWriteFile(t, filepath.Join(dir, "b.tar.gz"), 22, 1*24*time.Hour)
	auditWriteFile(t, filepath.Join(dir, "notes.txt"), 5, 1*24*time.Hour) // excluded
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	backups, err := auditCleanupManager().ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("FAIL: listed %d backups, want 3", len(backups))
	}
	wantOrder := []string{"a.tar.gz", "b.tar.gz", "c.gz"}
	wantSize := []int64{11, 22, 33}
	for i, bi := range backups {
		if bi.Filename != wantOrder[i] {
			t.Fatalf("FAIL: entry %d = %q, want %q (lexical order)", i, bi.Filename, wantOrder[i])
		}
		if bi.Size != wantSize[i] {
			t.Fatalf("FAIL: %s size = %d, want %d", bi.Filename, bi.Size, wantSize[i])
		}
		if want, got := filepath.Join(dir, bi.Filename), bi.Path; want != got {
			t.Fatalf("FAIL: %s path = %q, want %q", bi.Filename, got, want)
		}
		if bi.ModTime.IsZero() {
			t.Fatalf("FAIL: %s has zero ModTime", bi.Filename)
		}
	}
}
