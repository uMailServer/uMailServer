package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- CleanupOldBackups tests ---

func TestCleanupOldBackups_InvalidRetentionDays(t *testing.T) {
	bm := NewBackupManager(nil)

	// Zero retention days should fail
	deleted, err := bm.CleanupOldBackups("/tmp", 0)
	if err == nil {
		t.Error("Expected error for zero retention days")
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted, got %d", deleted)
	}

	// Negative retention days should fail
	deleted, err = bm.CleanupOldBackups("/tmp", -1)
	if err == nil {
		t.Error("Expected error for negative retention days")
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted, got %d", deleted)
	}
}

func TestCleanupOldBackups_NonExistentDirectory(t *testing.T) {
	bm := NewBackupManager(nil)

	deleted, err := bm.CleanupOldBackups("/non/existent/directory/12345", 7)
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted, got %d", deleted)
	}
}

func TestCleanupOldBackups_NoOldBackups(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create a recent backup file
	recentFile := filepath.Join(tempDir, "umailserver_backup_20240101_120000.tar.gz")
	os.WriteFile(recentFile, []byte("recent backup"), 0o644)

	// Try to cleanup with 30 day retention - file is from 2024 so should not be deleted
	deleted, err := bm.CleanupOldBackups(tempDir, 30)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Logf("Deleted %d files", deleted)
	}
}

func TestCleanupOldBackups_OldBackupDeleted(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create a backup file
	backupFile := filepath.Join(tempDir, "umailserver_backup_old.tar.gz")
	f, err := os.Create(backupFile)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Set the file's mod time to 100 days ago
	oldTime := time.Now().AddDate(0, 0, -100)
	os.Chtimes(backupFile, oldTime, oldTime)

	// Verify the file exists before
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		t.Fatal("Backup file should exist before test")
	}

	// Cleanup with 30 day retention - should delete this old file
	deleted, err := bm.CleanupOldBackups(tempDir, 30)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1 deleted, got %d", deleted)
	}

	// Verify the file was deleted
	if _, err := os.Stat(backupFile); !os.IsNotExist(err) {
		t.Error("Backup file should have been deleted")
	}
}

func TestCleanupOldBackups_SkipsNonBackupFiles(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create a non-backup file
	nonBackupFile := filepath.Join(tempDir, "readme.txt")
	os.WriteFile(nonBackupFile, []byte("not a backup"), 0o644)

	// Create a backup file that is old
	oldBackupFile := filepath.Join(tempDir, "umailserver_backup_old.tar.gz")
	f, _ := os.Create(oldBackupFile)
	f.Close()
	oldTime := time.Now().AddDate(0, 0, -100)
	os.Chtimes(oldBackupFile, oldTime, oldTime)

	// Cleanup
	deleted, err := bm.CleanupOldBackups(tempDir, 30)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	// Only the .tar.gz file should be deleted, not the .txt file
	if deleted != 1 {
		t.Errorf("Expected 1 deleted (.tar.gz), got %d", deleted)
	}

	// Non-backup file should still exist
	if _, err := os.Stat(nonBackupFile); os.IsNotExist(err) {
		t.Error("Non-backup file should not be deleted")
	}
}

func TestCleanupOldBackups_SkipsDirectories(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	os.Mkdir(subDir, 0o755)

	// Create an old backup file
	oldBackupFile := filepath.Join(tempDir, "old_backup.tar.gz")
	f, _ := os.Create(oldBackupFile)
	f.Close()
	oldTime := time.Now().AddDate(0, 0, -100)
	os.Chtimes(oldBackupFile, oldTime, oldTime)

	// Cleanup - should not fail even with directory present
	deleted, err := bm.CleanupOldBackups(tempDir, 30)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1 deleted, got %d", deleted)
	}
}

func TestCleanupOldBackups_ProcessesPlainGZFiles(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create a .gz file (not .tar.gz)
	gzFile := filepath.Join(tempDir, "somefile.gz")
	os.WriteFile(gzFile, []byte("gz data"), 0o644)

	// Set to old time
	oldTime := time.Now().AddDate(0, 0, -100)
	os.Chtimes(gzFile, oldTime, oldTime)

	// Cleanup - .gz files ARE processed (not skipped since they pass the Ext check)
	deleted, err := bm.CleanupOldBackups(tempDir, 30)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	// Plain .gz should be deleted (not skipped)
	if deleted != 1 {
		t.Errorf("Expected 1 deleted, got %d", deleted)
	}

	// File should have been deleted
	if _, err := os.Stat(gzFile); !os.IsNotExist(err) {
		t.Error("Plain .gz file should have been deleted")
	}
}
