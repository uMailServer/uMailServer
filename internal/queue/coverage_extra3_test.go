package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// TestWriteFileDirError tests writeFile when the parent directory cannot be created.
func TestWriteFileDirError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "afile")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := writeFile(filepath.Join(filePath, "sub", "test.msg"), []byte("data"))
	if err == nil {
		t.Error("expected error when writing to path inside a file")
	}
}

// TestWriteFileNormal tests the normal path of writeFile.
func TestWriteFileNormal(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "test.msg")
	data := []byte("hello world")

	if err := writeFile(path, data); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

// TestWriteFileAtomicRename verifies writeFile uses atomic rename.
func TestWriteFileAtomicRename(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "atomic.msg")

	if err := writeFile(path, []byte("atomic data")); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temporary file should have been renamed")
	}
}

// TestWriteFile_RenameError tests writeFile when os.Rename fails.
// On Windows, we create a file at the target path and lock it to prevent overwrite.
func TestWriteFile_RenameError(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "blocked.msg")

	// Create the target as a directory so rename from file to directory fails
	os.MkdirAll(targetPath, 0755)

	err := writeFile(targetPath, []byte("data"))
	if err == nil {
		t.Error("expected error when rename target is a directory")
	}
}

// TestProcessQueueContextCancel tests that processQueue exits on context cancellation.
func TestProcessQueueContextCancel(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mgr.processQueue(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("processQueue did not exit on context cancellation")
	}
}

// TestProcessQueueShutdownChannel3 tests that processQueue exits when the
// shutdown channel is closed (exercises the <-m.shutdown case).
func TestProcessQueueShutdownChannel3(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	// Use a fresh shutdown channel we control
	shutdown := make(chan struct{})
	mgr.shutdown = shutdown

	done := make(chan struct{})
	go func() {
		mgr.processQueue(context.Background())
		close(done)
	}()

	// Close shutdown channel to trigger the <-m.shutdown branch
	close(shutdown)
	select {
	case <-done:
		// OK - processQueue exited via shutdown channel
	case <-time.After(2 * time.Second):
		t.Error("processQueue did not exit on shutdown channel close")
	}
}

// TestProcessPendingEntriesBasic tests processPendingEntries doesn't panic.
func TestProcessPendingEntriesBasic(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	entry := &db.QueueEntry{
		ID:          "pending-basic-test",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: "/nonexistent/path.msg",
		Status:      "pending",
		NextRetry:   time.Now().Add(-1 * time.Hour),
	}
	database.Enqueue(entry)

	mgr.processPendingEntries()
}

// TestReadFileMissing tests readFile with a missing file.
func TestReadFileMissing(t *testing.T) {
	_, err := readFile("/nonexistent/path/file.msg")
	if err == nil {
		t.Error("expected error reading non-existent file")
	}
}

// TestDeleteFileMissing tests that deleteFile does not error on missing files.
func TestDeleteFileMissing(t *testing.T) {
	deleteFile("/nonexistent/path/file.msg")
}

// TestGenerateQueueID tests that generateID returns a non-empty string.
func TestGenerateQueueID(t *testing.T) {
	id := generateID()
	if id == "" {
		t.Error("expected non-empty ID")
	}
}

// TestFlushQueue_WithPendingEntry_Cov3 tests FlushQueue with a pending entry.
// GetPendingEntries only returns entries with status "pending", so the
// if entry.Status == "failed" branch is never reached, but FlushQueue
// completes without error.
func TestFlushQueue_WithPendingEntry_Cov3(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	// Create a message file
	msgPath := filepath.Join(dataDir, "flush-test.msg")
	writeFile(msgPath, []byte("test message"))

	// Enqueue a pending entry with NextRetry in the past
	entry := &db.QueueEntry{
		ID:          "flush-pending-test",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: msgPath,
		Status:      "pending",
		RetryCount:  0,
		NextRetry:   time.Now().Add(-1 * time.Hour),
	}
	if err := database.Enqueue(entry); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// FlushQueue should find the entry via GetPendingEntries,
	// but since it's "pending" (not "failed"), it won't call RetryEntry
	err = mgr.FlushQueue()
	if err != nil {
		t.Fatalf("FlushQueue failed: %v", err)
	}

	// The entry should remain as "pending" since it's not "failed"
	updated, err := database.GetQueueEntry(entry.ID)
	if err != nil {
		t.Fatalf("GetQueueEntry failed: %v", err)
	}
	if updated.Status != "pending" {
		t.Errorf("expected status to remain 'pending', got %q", updated.Status)
	}
}

// TestEnqueue_WriteFileFailure tests Enqueue when writeFile fails for the message
// (exercises the error return after creating queue dir).
func TestEnqueue_WriteFileFailure(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	// Create a file where the queue directory would go to prevent writeFile
	queueDir := filepath.Join(dataDir, "queue")
	os.WriteFile(queueDir, []byte("blocker"), 0644)

	_, err = mgr.Enqueue("sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	if err == nil {
		t.Error("expected error when writeFile fails due to blocked queue dir")
	}
}

// TestGenerateBounce_EnqueueError tests generateBounce when the subsequent
// Enqueue call fails (exercises the fmt.Printf branch at line 444).
func TestGenerateBounce_EnqueueError(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	// Set max queue size to 0 so Enqueue inside generateBounce will fail
	mgr.SetMaxQueueSize(0)

	msgPath := filepath.Join(dataDir, "bounce-enqueue-err.msg")
	writeFile(msgPath, []byte("Original message body"))

	entry := &db.QueueEntry{
		ID:          "bounce-enqueue-err-test",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: msgPath,
		LastError:   "test delivery failure",
		CreatedAt:   time.Now(),
	}

	// generateBounce should not panic even when Enqueue fails
	mgr.generateBounce(entry)

	// File should still be cleaned up
	if _, err := os.Stat(msgPath); !os.IsNotExist(err) {
		t.Error("expected message file to be cleaned up after bounce")
	}
}
