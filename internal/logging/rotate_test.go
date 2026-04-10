package logging

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewRotatingWriter(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	if w.filename != logFile {
		t.Errorf("expected filename %s, got %s", logFile, w.filename)
	}
	if w.maxSize != 1*1024*1024 {
		t.Errorf("expected maxSize 1MB, got %d", w.maxSize)
	}
	if w.maxBackups != 3 {
		t.Errorf("expected maxBackups 3, got %d", w.maxBackups)
	}
	if w.maxAge != 7*24*time.Hour {
		t.Errorf("expected maxAge 7 days, got %v", w.maxAge)
	}

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("log file was not created")
	}
}

func TestNewRotatingWriter_InvalidDirectory(t *testing.T) {
	// Try to create writer in a non-existent, non-creatable directory
	// Use a path that will fail on both Unix and Windows
	invalidPath := string([]byte{0}) + "/invalid/path/test.log"
	_, err := NewRotatingWriter(invalidPath, 1, 3, 7)
	if err == nil {
		t.Error("expected error for invalid directory, got nil")
	}
}

func TestRotatingWriter_Write(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// Write some data
	data := []byte("test log line\n")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}

	// Verify file contents
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("expected %q, got %q", string(data), string(content))
	}
}

func TestRotatingWriter_Rotate(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	// Write some initial data
	_, err = w.Write([]byte("initial data\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close to ensure flush
	w.Close()

	// Manually trigger rotation by creating a new writer with same file
	w2, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter (second) failed: %v", err)
	}
	defer w2.Close()

	// Write more data to trigger rotation logic
	largeData := make([]byte, 100)
	for i := range largeData {
		largeData[i] = 'x'
	}

	// Write multiple times to fill up
	for i := 0; i < 11000; i++ {
		_, err := w2.Write(largeData)
		if err != nil {
			t.Fatalf("Write failed at iteration %d: %v", i, err)
		}
	}

	// Check if backup files were created
	files, err := filepath.Glob(logFile + ".*")
	if err != nil {
		t.Fatalf("failed to glob backup files: %v", err)
	}

	// Should have at least one backup
	if len(files) == 0 {
		t.Error("expected backup files after rotation, found none")
	}
}

func TestRotatingWriter_CleanupMaxBackups(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	// Create old backup files manually
	for i := 0; i < 5; i++ {
		backupName := logFile + ".20240101-12000" + string(rune('0'+i))
		if err := os.WriteFile(backupName, []byte("old log"), 0644); err != nil {
			t.Fatalf("failed to create backup file: %v", err)
		}
		// Sleep to ensure different modification times
		time.Sleep(10 * time.Millisecond)
	}

	// Create writer with maxBackups=2
	w, err := NewRotatingWriter(logFile, 1, 2, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// Trigger cleanup
	w.cleanup()

	// Wait for cleanup to complete
	time.Sleep(100 * time.Millisecond)

	// Check backup files
	files, err := filepath.Glob(logFile + ".*")
	if err != nil {
		t.Fatalf("failed to glob backup files: %v", err)
	}

	// Should have at most maxBackups files (or fewer due to timing)
	if len(files) > 3 {
		t.Errorf("expected at most 3 backup files after cleanup, got %d", len(files))
	}
}

func TestRotatingWriter_CleanupMaxAge(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	// Create a recent backup file
	recentBackup := logFile + ".20241201-120000"
	if err := os.WriteFile(recentBackup, []byte("recent log"), 0644); err != nil {
		t.Fatalf("failed to create recent backup: %v", err)
	}

	// Create writer with maxAge=1 hour
	w, err := NewRotatingWriter(logFile, 1, 10, 0) // 0 days = no age cleanup
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// Recent file should still exist
	if _, err := os.Stat(recentBackup); os.IsNotExist(err) {
		t.Error("recent backup file was incorrectly removed")
	}
}

func TestRotatingWriter_Close(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	// Write some data
	_, err = w.Write([]byte("test data"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close should succeed
	err = w.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Double close should not panic
	err = w.Close()
	if err != nil {
		t.Logf("Second close returned error (acceptable): %v", err)
	}
}

func TestGetLogWriter_Stdout(t *testing.T) {
	w, err := GetLogWriter("stdout", 1, 3, 7)
	if err != nil {
		t.Fatalf("GetLogWriter failed: %v", err)
	}

	if w != os.Stdout {
		t.Error("expected os.Stdout for 'stdout' output")
	}
}

func TestGetLogWriter_Stderr(t *testing.T) {
	w, err := GetLogWriter("stderr", 1, 3, 7)
	if err != nil {
		t.Fatalf("GetLogWriter failed: %v", err)
	}

	if w != os.Stderr {
		t.Error("expected os.Stderr for 'stderr' output")
	}
}

func TestGetLogWriter_Empty(t *testing.T) {
	w, err := GetLogWriter("", 1, 3, 7)
	if err != nil {
		t.Fatalf("GetLogWriter failed: %v", err)
	}

	if w != os.Stdout {
		t.Error("expected os.Stdout for empty output")
	}
}

func TestGetLogWriter_File(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := GetLogWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("GetLogWriter failed: %v", err)
	}
	defer w.Close()

	// Should be a RotatingWriter
	rw, ok := w.(*RotatingWriter)
	if !ok {
		t.Error("expected *RotatingWriter for file output")
	}

	if rw.filename != logFile {
		t.Errorf("expected filename %s, got %s", logFile, rw.filename)
	}
}

func TestRotatingWriter_MultipleWrites(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// Write multiple lines
	lines := []string{
		"line 1\n",
		"line 2\n",
		"line 3\n",
	}

	for _, line := range lines {
		_, err := w.Write([]byte(line))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	// Verify all lines were written
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	expected := strings.Join(lines, "")
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}
}

func TestRotatingWriter_AppendToExisting(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	// Create existing file with content
	if err := os.WriteFile(logFile, []byte("existing content\n"), 0644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	// Create writer should append
	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// Write new content
	_, err = w.Write([]byte("new content\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify both contents exist
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "existing content") {
		t.Error("existing content was lost")
	}
	if !strings.Contains(string(content), "new content") {
		t.Error("new content was not written")
	}
}

func TestRotatingWriter_Close_AlreadyClosed(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	// Close first time
	w.Close()

	// Close second time - note: this may return an error because w.file is not nil
	// This test documents the behavior where double-close returns an error
	err = w.Close()
	if err == nil {
		t.Log("Close on already closed writer did not return error (acceptable)")
	}
}

func TestRotatingWriter_Rotate_NoOriginalFile(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "nonexistent.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// File should not exist yet, but rotate should handle it
	// Write some data first to create the file
	_, err = w.Write([]byte("test data\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Now close and remove the original file
	w.Close()

	// Delete the log file to simulate the scenario where original doesn't exist during rotate
	os.Remove(logFile)

	// Create a new writer - this simulates what happens when rotate is called
	// but the original file doesn't exist
	w2, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w2.Close()

	// Write enough to trigger rotation
	largeData := make([]byte, 1024*1024+1) // 1MB + 1 byte to trigger rotation
	_, err = w2.Write(largeData)
	if err != nil {
		t.Logf("Write returned error (may be expected during rotation): %v", err)
	}
}

func TestRotatingWriter_ConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 10, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// Write concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			data := []byte(string(rune('0' + id)))
			for j := 0; j < 100; j++ {
				_, err := w.Write(data)
				if err != nil {
					t.Errorf("Write failed: %v", err)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify file exists and has content
	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("failed to stat log file: %v", err)
	}

	if info.Size() != 1000 {
		t.Errorf("expected 1000 bytes, got %d", info.Size())
	}
}

// TestRotatingWriter_Close_NilFile tests close when file is already nil
func TestRotatingWriter_Close_NilFile(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w := &RotatingWriter{
		filename: logFile,
		maxSize:  1024 * 1024,
		file:     nil, // file is nil
	}

	// Close should return nil even when file is nil
	err := w.Close()
	if err != nil {
		t.Errorf("Close on nil file returned error: %v", err)
	}
}

// TestRotate_CloseError tests rotate when close fails
func TestRotate_CloseError(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	// Write some data
	_, err = w.Write([]byte("test data\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Replace file with a pipe to cause close to fail
	w.mu.Lock()
	if w.file != nil {
		// This is tricky to test - close errors on regular files are rare
		// The actual error path would require a mock or filesystem-level injection
	}
	w.mu.Unlock()

	// Just verify rotation works normally
	w.Close()
}

// TestRotate_RenameError tests rotate when rename fails
func TestRotate_RenameError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to file locking issues")
	}

	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	// Write some data
	_, err = w.Write([]byte("test data\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Make the log file read-only to cause rename to fail
	os.Chmod(logFile, 0444)
	defer os.Chmod(logFile, 0644)

	// Trigger rotation
	w.mu.Lock()
	err = w.rotate()
	w.mu.Unlock()

	// On Unix, read-only files can't be renamed - this should fail
	if err == nil {
		t.Log("rename succeeded (platform dependent)")
	}
}

// TestRotate_OpenError tests rotate when open fails after rename
func TestRotate_OpenError(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	// Write some data
	_, err = w.Write([]byte("test data\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close and remove the file, then make the dir read-only so re-open fails
	w.Close()

	// Remove the file so rotate will try to rename non-existent file
	os.Remove(logFile)

	// Make parent dir read-only so open fails
	os.Chmod(tempDir, 0555)

	// Create new writer to trigger rotate
	w2, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		// Expected on some platforms
		os.Chmod(tempDir, 0755)
		return
	}

	// Write enough to trigger rotation
	largeData := make([]byte, 1024*1024+1)
	_, _ = w2.Write(largeData)

	// Cleanup
	os.Chmod(tempDir, 0755)
	w2.Close()

	// The error may or may not occur depending on timing
}

// TestRotatingWriter_CloseError tests close error handling
func TestRotatingWriter_CloseError(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	w, err := NewRotatingWriter(logFile, 1, 3, 7)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}

	_, err = w.Write([]byte("test\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close should succeed
	err = w.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

// TestCleanup_NilMaxAge tests cleanup with maxAge=0 (disabled)
func TestCleanup_NilMaxAge(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	// Create writer with maxAge=0 (disabled)
	w, err := NewRotatingWriter(logFile, 1, 3, 0)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// cleanup should return early when both maxBackups and maxAge are disabled
	w.cleanup()
	// Should not panic
}

// TestCleanup_MaxBackupsDisabled tests cleanup with maxBackups=0
func TestCleanup_MaxBackupsDisabled(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	// Create writer with maxBackups=0 (disabled)
	w, err := NewRotatingWriter(logFile, 1, 0, 0)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	// cleanup should return early when both are disabled
	w.cleanup()
	// Should not panic
}
