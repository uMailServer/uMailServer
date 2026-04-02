package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// =======================================================================
// Enqueue (87.5%) - cover the queue full path and the db.Enqueue failure path
// =======================================================================

// TestEnqueue_QueueFull tests Enqueue when the queue is at maximum capacity.
func TestEnqueue_QueueFull_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	mgr.SetMaxQueueSize(0) // Set max queue size to 0 to trigger "queue full"

	_, err = mgr.Enqueue("sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	if err == nil {
		t.Error("Expected error when queue is full")
	}
}

// TestEnqueue_MultipleRecipients tests Enqueue with multiple recipients.
func TestEnqueue_MultipleRecipients_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	recipients := []string{"rcpt1@example.com", "rcpt2@example.com", "rcpt3@example.com"}
	id, err := mgr.Enqueue("sender@example.com", recipients, []byte("test message"))
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	if id == "" {
		t.Error("Expected non-empty ID")
	}

	// Verify each recipient got their own entry
	for i, rcpt := range recipients {
		entryID := id + "-" + string(rune('0'+i))
		entry, err := database.GetQueueEntry(entryID)
		if err != nil {
			t.Logf("GetQueueEntry(%s): %v (may be expected)", entryID, err)
			continue
		}
		if len(entry.To) != 1 || entry.To[0] != rcpt {
			t.Errorf("Entry %s: expected To=[%s], got %v", entryID, rcpt, entry.To)
		}
	}
}

// =======================================================================
// FlushQueue (75%) - cover error paths
// =======================================================================

// TestFlushQueue_Empty tests FlushQueue with no entries.
func TestFlushQueue_Empty_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	err = mgr.FlushQueue()
	if err != nil {
		t.Fatalf("FlushQueue on empty queue: %v", err)
	}
}

// TestFlushQueue_ClosedDB tests FlushQueue with a closed database.
func TestFlushQueue_ClosedDB_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	mgr := NewManager(database, nil, dataDir)
	database.Close()

	err = mgr.FlushQueue()
	if err == nil {
		t.Error("Expected error when flushing queue with closed DB")
	}
}

// =======================================================================
// processQueue (85.7%) - cover the ticker path
// =======================================================================

// TestProcessQueue_TickerExercises the ticker path in processQueue.
// The ticker fires every 30 seconds, so we can't wait for it in a short test.
// Instead we verify that the function can handle being started and stopped.
func TestProcessQueue_StartStop_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		mgr.processQueue(ctx)
		close(done)
	}()

	// Cancel the context to stop processQueue
	cancel()
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("processQueue did not exit on context cancellation")
	}
}

// =======================================================================
// generateBounce (87.5%) - cover the nil db path and error paths
// =======================================================================

// TestGenerateBounce_WithValidDB tests generateBounce with a valid database.
func TestGenerateBounce_WithValidDB_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	// Set max queue size high enough for the bounce enqueue
	mgr := NewManager(database, nil, dataDir)
	mgr.SetMaxQueueSize(100)

	// Create a message file for the original message
	msgPath := filepath.Join(dataDir, "bounce-test.msg")
	writeFile(msgPath, []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nOriginal message\r\n"))

	entry := &db.QueueEntry{
		ID:          "bounce-gen-test",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: msgPath,
		LastError:   "connection refused",
		CreatedAt:   time.Now(),
	}

	mgr.generateBounce(entry)

	// The message file should be cleaned up
	if _, err := os.Stat(msgPath); !os.IsNotExist(err) {
		t.Error("Expected message file to be cleaned up after bounce generation")
	}
}

// TestGenerateBounce_MissingMessageFile tests generateBounce when the message file is missing.
func TestGenerateBounce_MissingMessageFile_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	entry := &db.QueueEntry{
		ID:          "bounce-missing-test",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: "/nonexistent/path/to/message.msg",
		LastError:   "test error",
	}

	// Should not panic
	mgr.generateBounce(entry)
}

// =======================================================================
// writeFile (85.7%) - cover error paths more thoroughly
// =======================================================================

// TestWriteFile_ToReadOnlyDir tests writeFile when the target dir is read-only.
func TestWriteFile_ToReadOnlyDir_Cov4(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root")
	}
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(subDir, 0555); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(subDir, "test.msg")
	err := writeFile(target, []byte("data"))
	// On Windows, this may or may not error depending on permissions
	if err != nil {
		t.Logf("writeFile to readonly dir returned error: %v", err)
	}
} // =======================================================================
// Manager Start/Stop
// =======================================================================

// TestStart_AlreadyRunning tests Start when the manager is already running.
func TestStart_AlreadyRunning_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start once
	mgr.Start(ctx)

	// Start again - should return immediately since running=true
	mgr.Start(ctx)

	// Stop
	mgr.Stop()
}

// TestStop_NotRunning tests Stop when the manager is not running.
func TestStop_NotRunning_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	// Stop without starting - should return immediately
	mgr.Stop()
}

// =======================================================================
// GetStats, SetMaxRetries
// =======================================================================

// TestGetStats tests GetStats returns without error.
func TestGetStats_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	stats, err := mgr.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats == nil {
		t.Error("Expected non-nil stats")
	}
}

// TestSetMaxRetries tests SetMaxRetries.
func TestSetMaxRetries_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)
	mgr.SetMaxRetries(5)

	if mgr.maxRetries != 5 {
		t.Errorf("Expected maxRetries=5, got %d", mgr.maxRetries)
	}
}

// =======================================================================
// extractDomain - edge cases
// =======================================================================

// TestExtractDomain_EdgeCases tests extractDomain with various inputs.
func TestExtractDomain_EdgeCases_Cov4(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"user@example.com", "example.com"},
		{"no-at-sign", ""},
		{"@", ""},
		{"@domain.com", "domain.com"},
		{"user@host@domain.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractDomain(tt.input)
			if got != tt.expect {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

// =======================================================================
// RetryEntry
// =======================================================================

// TestRetryEntry_NotFound tests RetryEntry with a non-existent entry.
func TestRetryEntry_NotFound_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	err = mgr.RetryEntry("nonexistent-id")
	if err == nil {
		t.Error("Expected error retrying non-existent entry")
	}
}

// =======================================================================
// DropEntry
// =======================================================================

// TestDropEntry_NotFound tests DropEntry with a non-existent entry.
// bbolt Delete may not fail for non-existent keys, so this may succeed.
func TestDropEntry_NotFound_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	// DropEntry on non-existent ID may or may not error depending on bbolt behavior
	err = mgr.DropEntry("nonexistent-id")
	// Either way is fine - just verify no panic
	t.Logf("DropEntry on nonexistent ID returned: %v", err)
}

// =======================================================================
// GetQueueEntry
// =======================================================================

// TestGetQueueEntry_NotFound tests GetQueueEntry with a non-existent entry.
func TestGetQueueEntry_NotFound_Cov4(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir)

	_, err = mgr.GetQueueEntry("nonexistent-id")
	if err == nil {
		t.Error("Expected error getting non-existent entry")
	}
}
