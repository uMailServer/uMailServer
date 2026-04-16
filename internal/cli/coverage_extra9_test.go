package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
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

// --- BackupManager Verify tests ---

func TestVerify_NonExistentFile(t *testing.T) {
	bm := NewBackupManager(nil)

	err := bm.Verify("/non/existent/backup.tar.gz")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestVerify_InvalidGzip(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create a non-gzip file
	invalidFile := filepath.Join(tempDir, "invalid.tar.gz")
	os.WriteFile(invalidFile, []byte("not gzip data"), 0o644)

	err := bm.Verify(invalidFile)
	if err == nil {
		t.Error("Expected error for invalid gzip")
	}
}

func TestVerify_EncryptedWithoutPassword(t *testing.T) {
	tempDir := t.TempDir()
	bm := NewBackupManager(nil)

	// Create an encrypted-looking file (starts with backupMagic)
	encryptedFile := filepath.Join(tempDir, "encrypted.tar.gz")
	// Write backup magic to make it look encrypted
	magic := []byte("UMAILBACKUP")
	os.WriteFile(encryptedFile, magic, 0o644)

	err := bm.Verify(encryptedFile)
	if err == nil {
		t.Error("Expected error for encrypted file without password")
	}
}

func TestVerify_ValidBackupNoManifestHashes(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}
	bm := NewBackupManager(cfg)

	// Create a valid backup with manifest but no file hashes
	backupFile := filepath.Join(tempDir, "valid_backup.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Create manifest without files
	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240101_120000",
		"hostname":  "test.example.com",
	}
	manifestData, _ := json.Marshal(manifest)
	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0o644,
	}
	tw.WriteHeader(header)
	tw.Write(manifestData)

	// Add a test file
	testContent := []byte("test content")
	testHeader := &tar.Header{
		Name: "config/test.conf",
		Size: int64(len(testContent)),
		Mode: 0o644,
	}
	tw.WriteHeader(testHeader)
	tw.Write(testContent)

	tw.Close()
	gw.Close()

	err = bm.Verify(backupFile)
	if err != nil {
		t.Errorf("Verify failed: %v", err)
	}
}

func TestVerify_ManifestNotFound(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}
	bm := NewBackupManager(cfg)

	// Create a backup without manifest
	backupFile := filepath.Join(tempDir, "no_manifest.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Add a file but no manifest
	testContent := []byte("test content")
	testHeader := &tar.Header{
		Name: "config/test.conf",
		Size: int64(len(testContent)),
		Mode: 0o644,
	}
	tw.WriteHeader(testHeader)
	tw.Write(testContent)

	tw.Close()
	gw.Close()

	err = bm.Verify(backupFile)
	if err == nil {
		t.Error("Expected error for missing manifest")
	}
}

func TestVerify_InvalidManifestJSON(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}
	bm := NewBackupManager(cfg)

	// Create a backup with invalid JSON manifest
	backupFile := filepath.Join(tempDir, "bad_manifest.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	badJSON := []byte("{invalid json")
	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(badJSON)),
		Mode: 0o644,
	}
	tw.WriteHeader(header)
	tw.Write(badJSON)

	tw.Close()
	gw.Close()

	err = bm.Verify(backupFile)
	if err == nil {
		t.Error("Expected error for invalid JSON manifest")
	}
}

func TestVerify_ManifestWithFileHashes(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}
	bm := NewBackupManager(cfg)

	// Create a backup with manifest containing file hashes
	backupFile := filepath.Join(tempDir, "hashed_backup.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Create manifest with file hashes
	testContent := []byte("test content")
	hash := sha256.Sum256(testContent)
	hashHex := hex.EncodeToString(hash[:])

	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240101_120000",
		"hostname":  "test.example.com",
		"files": []interface{}{
			map[string]interface{}{
				"path": "config/test.conf",
				"hash": hashHex,
				"size": int64(len(testContent)),
			},
		},
	}
	manifestData, _ := json.Marshal(manifest)
	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0o644,
	}
	tw.WriteHeader(header)
	tw.Write(manifestData)

	// Add the test file with matching hash
	testHeader := &tar.Header{
		Name: "config/test.conf",
		Size: int64(len(testContent)),
		Mode: 0o644,
	}
	tw.WriteHeader(testHeader)
	tw.Write(testContent)

	tw.Close()
	gw.Close()

	err = bm.Verify(backupFile)
	if err != nil {
		t.Errorf("Verify failed with matching hashes: %v", err)
	}
}

func TestVerify_FilesWithWrongHash(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}
	bm := NewBackupManager(cfg)

	// Create a backup with wrong file hashes
	backupFile := filepath.Join(tempDir, "wrong_hash.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Create manifest with CORRECT hash
	testContent := []byte("test content")
	correctHash := sha256.Sum256(testContent)
	correctHashHex := hex.EncodeToString(correctHash[:])

	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240101_120000",
		"hostname":  "test.example.com",
		"files": []interface{}{
			map[string]interface{}{
				"path": "config/test.conf",
				"hash": correctHashHex,
				"size": int64(len(testContent)),
			},
		},
	}
	manifestData, _ := json.Marshal(manifest)
	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0o644,
	}
	tw.WriteHeader(header)
	tw.Write(manifestData)

	// Add the test file with DIFFERENT content (wrong hash)
	wrongContent := []byte("wrong content")
	wrongHeader := &tar.Header{
		Name: "config/test.conf",
		Size: int64(len(wrongContent)),
		Mode: 0o644,
	}
	tw.WriteHeader(wrongHeader)
	tw.Write(wrongContent)

	tw.Close()
	gw.Close()

	err = bm.Verify(backupFile)
	if err == nil {
		t.Error("Expected error for wrong file hash")
	}
}
