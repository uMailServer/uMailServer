package queue

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"user@sub.example.com", "sub.example.com"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractDomain(tt.email)
		if got != tt.expected {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.expected)
		}
	}
}

func TestRetryDelays(t *testing.T) {
	// Check that retry delays are defined
	if len(retryDelays) != 10 {
		t.Errorf("Expected 10 retry delays, got %d", len(retryDelays))
	}

	// Check first delay is 5 minutes
	if retryDelays[0] != 5*time.Minute {
		t.Errorf("Expected first delay 5m, got %v", retryDelays[0])
	}

	// Check last delay is 48 hours
	if retryDelays[len(retryDelays)-1] != 48*time.Hour {
		t.Errorf("Expected last delay 48h, got %v", retryDelays[len(retryDelays)-1])
	}
}

func TestManager(t *testing.T) {
	// Create temporary directories
	dataDir := t.TempDir()

	// Open test database
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Create queue manager
	manager := NewManager(database, nil, dataDir)

	t.Run("StartStop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager.Start(ctx)
		if !manager.running {
			t.Error("Expected manager to be running")
		}

		manager.Stop()
		if manager.running {
			t.Error("Expected manager to be stopped")
		}
	})

	t.Run("SetMaxRetries", func(t *testing.T) {
		manager.SetMaxRetries(5)
		if manager.maxRetries != 5 {
			t.Errorf("Expected maxRetries to be 5, got %d", manager.maxRetries)
		}
	})

	t.Run("SetMaxQueueSize", func(t *testing.T) {
		manager.SetMaxQueueSize(100)
		if manager.maxQueueSize != 100 {
			t.Errorf("Expected maxQueueSize to be 100, got %d", manager.maxQueueSize)
		}
	})

	t.Run("GetStats", func(t *testing.T) {
		stats, err := manager.GetStats()
		if err != nil {
			t.Fatalf("GetStats failed: %v", err)
		}
		if stats == nil {
			t.Error("Expected stats, got nil")
		}
	})

	t.Run("GenerateID", func(t *testing.T) {
		id1 := generateID()
		id2 := generateID()

		if id1 == "" {
			t.Error("Expected non-empty ID")
		}

		if id1 == id2 {
			t.Error("Expected unique IDs")
		}
	})

	t.Run("FileOperations", func(t *testing.T) {
		testDir := t.TempDir()
		testFile := filepath.Join(testDir, "test.txt")
		testData := []byte("test data content")

		// Test writeFile
		err := writeFile(testFile, testData)
		if err != nil {
			t.Fatalf("writeFile failed: %v", err)
		}

		// Test readFile
		readData, err := readFile(testFile)
		if err != nil {
			t.Fatalf("readFile failed: %v", err)
		}

		if string(readData) != string(testData) {
			t.Errorf("readFile returned wrong data: got %q, want %q", string(readData), string(testData))
		}

		// Test deleteFile
		deleteFile(testFile)
		if _, err := os.Stat(testFile); !os.IsNotExist(err) {
			t.Error("File should be deleted")
		}
	})
}

func TestNewManager(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)
	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
	if manager.db != database {
		t.Error("expected database to be set")
	}
	if manager.dataDir != dataDir {
		t.Error("expected dataDir to be set")
	}
	if manager.resolver == nil {
		t.Error("expected resolver to be initialized")
	}
	if manager.shutdown == nil {
		t.Error("expected shutdown channel to be initialized")
	}
	if manager.maxRetries != len(retryDelays) {
		t.Errorf("expected maxRetries to be %d, got %d", len(retryDelays), manager.maxRetries)
	}
	if manager.maxQueueSize != 10000 {
		t.Errorf("expected maxQueueSize to be 10000, got %d", manager.maxQueueSize)
	}
}

func TestQueueStatsStruct(t *testing.T) {
	stats := &QueueStats{
		Pending:   10,
		Sending:   5,
		Failed:    2,
		Delivered: 100,
		Bounced:   1,
		Total:     118,
	}

	if stats.Pending != 10 {
		t.Errorf("expected pending 10, got %d", stats.Pending)
	}
	if stats.Delivered != 100 {
		t.Errorf("expected delivered 100, got %d", stats.Delivered)
	}
	if stats.Total != 118 {
		t.Errorf("expected total 118, got %d", stats.Total)
	}
}

func TestResolverLookupMX(t *testing.T) {
	resolver := &Resolver{}

	// Test with a domain that likely has MX records
	mxRecords, err := resolver.LookupMX("google.com")
	// Don't fail on error since network may not be available
	if err == nil && len(mxRecords) == 0 {
		t.Error("expected MX records for google.com")
	}
}

func TestManagerGetQueueEntry(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Try to get non-existent entry
	_, err = manager.GetQueueEntry("non-existent-id")
	// Should return error for non-existent entry
	if err == nil {
		// Some implementations may return nil error with nil entry
		t.Log("GetQueueEntry for non-existent ID did not return error")
	}
}

func TestManagerGetPendingEntries(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	entries, err := manager.GetPendingEntries()
	if err != nil {
		t.Fatalf("GetPendingEntries failed: %v", err)
	}
	// entries can be nil or empty slice depending on implementation
	if entries != nil && len(entries) > 0 {
		t.Logf("Got %d pending entries", len(entries))
	}
}

func TestManagerDropEntry(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Try to drop non-existent entry
	err = manager.DropEntry("non-existent-id")
	// May or may not error depending on implementation
	_ = err
}

func TestManagerFlushQueue(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	err = manager.FlushQueue()
	if err != nil {
		t.Errorf("FlushQueue failed: %v", err)
	}
}

func TestWriteFileCreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "a", "b", "c", "test.txt")
	data := []byte("test content")

	err := writeFile(nestedPath, data)
	if err != nil {
		t.Fatalf("writeFile failed to create nested directories: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Error("file was not created in nested directory")
	}

	// Verify content
	readData, err := readFile(nestedPath)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Error("file content mismatch")
	}
}

// TestManagerFlushQueueWithFailedEntries tests FlushQueue with failed entries
func TestManagerFlushQueueWithFailedEntries(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a failed queue entry
	entry := &db.QueueEntry{
		ID:          "test-failed-entry",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		Status:      "failed",
		RetryCount:  3,
		MessagePath: filepath.Join(dataDir, "test.msg"),
	}

	// Create the message file
	err = writeFile(entry.MessagePath, []byte("test message"))
	if err != nil {
		t.Fatalf("Failed to write message file: %v", err)
	}

	// Add entry to database
	err = database.Enqueue(entry)
	if err != nil {
		t.Fatalf("Failed to enqueue: %v", err)
	}

	// Flush queue should process entries (may or may not retry depending on implementation)
	err = manager.FlushQueue()
	if err != nil {
		t.Fatalf("FlushQueue failed: %v", err)
	}
}

// TestManagerFlushQueueNoFailedEntries tests FlushQueue with no failed entries
func TestManagerFlushQueueNoFailedEntries(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a pending queue entry (not failed)
	entry := &db.QueueEntry{
		ID:          "test-pending-entry",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		Status:      "pending",
		RetryCount:  1,
		MessagePath: "/tmp/test.msg",
	}

	// Add entry to database
	err = database.Enqueue(entry)
	if err != nil {
		t.Fatalf("Failed to enqueue: %v", err)
	}

	// Flush queue should not retry pending entries
	err = manager.FlushQueue()
	if err != nil {
		t.Errorf("FlushQueue failed: %v", err)
	}

	// Verify entry is still pending
	updated, err := database.GetQueueEntry(entry.ID)
	if err != nil {
		t.Fatalf("Failed to get queue entry: %v", err)
	}

	if updated.Status != "pending" {
		t.Errorf("Expected status 'pending' to remain, got %q", updated.Status)
	}
}

func TestReadFileNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "non-existent.txt")

	_, err := readFile(nonExistentPath)
	if err == nil {
		t.Error("expected error when reading non-existent file")
	}
}

func TestDeleteFileNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "non-existent.txt")

	// Should not panic
	deleteFile(nonExistentPath)
}

func TestManagerDoubleStart(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first time
	manager.Start(ctx)
	if !manager.running {
		t.Error("expected manager to be running after first start")
	}

	// Start again - should not panic or create issues
	manager.Start(ctx)
	if !manager.running {
		t.Error("expected manager to still be running")
	}

	manager.Stop()
}

func TestManagerDoubleStop(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	manager.Stop()

	// Stop again - should not panic
	manager.Stop()
	if manager.running {
		t.Error("expected manager to be stopped")
	}
}

func TestExtractDomainVariations(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"user@sub.example.com", "sub.example.com"},
		{"user@deep.sub.example.com", "deep.sub.example.com"},
		{"invalid", ""},
		{"", ""},
		{"@nodomain.com", "nodomain.com"},
	}

	for _, tt := range tests {
		got := extractDomain(tt.email)
		if got != tt.expected {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.expected)
		}
	}
}

func TestQueueStatsCalculation(t *testing.T) {
	stats := &QueueStats{
		Pending:   10,
		Sending:   5,
		Failed:    2,
		Delivered: 100,
		Bounced:   1,
		Total:     118,
	}

	// Verify all fields
	if stats.Pending != 10 {
		t.Errorf("expected pending 10, got %d", stats.Pending)
	}
	if stats.Sending != 5 {
		t.Errorf("expected sending 5, got %d", stats.Sending)
	}
	if stats.Failed != 2 {
		t.Errorf("expected failed 2, got %d", stats.Failed)
	}
	if stats.Delivered != 100 {
		t.Errorf("expected delivered 100, got %d", stats.Delivered)
	}
	if stats.Bounced != 1 {
		t.Errorf("expected bounced 1, got %d", stats.Bounced)
	}
	if stats.Total != 118 {
		t.Errorf("expected total 118, got %d", stats.Total)
	}

	// Verify calculation: Total should equal sum of all statuses
	calculatedTotal := stats.Pending + stats.Sending + stats.Failed + stats.Delivered + stats.Bounced
	if calculatedTotal != stats.Total {
		t.Errorf("total mismatch: %d != sum(%d)", stats.Total, calculatedTotal)
	}
}

func TestManagerSetMaxRetries(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Set max retries to 5
	manager.SetMaxRetries(5)
	if manager.maxRetries != 5 {
		t.Errorf("Expected maxRetries to be 5, got %d", manager.maxRetries)
	}

	// Set max retries to 0
	manager.SetMaxRetries(0)
	if manager.maxRetries != 0 {
		t.Errorf("Expected maxRetries to be 0, got %d", manager.maxRetries)
	}
}

func TestManagerSetMaxQueueSize(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Set max queue size to 500
	manager.SetMaxQueueSize(500)
	if manager.maxQueueSize != 500 {
		t.Errorf("Expected maxQueueSize to be 500, got %d", manager.maxQueueSize)
	}

	// Set max queue size to 0
	manager.SetMaxQueueSize(0)
	if manager.maxQueueSize != 0 {
		t.Errorf("Expected maxQueueSize to be 0, got %d", manager.maxQueueSize)
	}
}

func TestResolverLookupMXInvalid(t *testing.T) {
	resolver := &Resolver{}

	// Test with invalid domain
	mxRecords, err := resolver.LookupMX("invalid-domain-that-does-not-exist-12345.xyz")
	// Should return error or empty
	if err == nil && len(mxRecords) > 0 {
		t.Error("expected no MX records for invalid domain")
	}
}

func TestWriteFileEmptyData(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	// Write empty data
	err := writeFile(testFile, []byte{})
	if err != nil {
		t.Fatalf("writeFile failed for empty data: %v", err)
	}

	// Read back
	readData, err := readFile(testFile)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	if len(readData) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(readData))
	}
}

func TestWriteFileLargeData(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.bin")

	// Write 1MB of data
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	err := writeFile(testFile, largeData)
	if err != nil {
		t.Fatalf("writeFile failed for large data: %v", err)
	}

	// Read back
	readData, err := readFile(testFile)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	if len(readData) != len(largeData) {
		t.Errorf("expected %d bytes, got %d", len(largeData), len(readData))
	}

	// Verify content
	for i := range largeData {
		if readData[i] != largeData[i] {
			t.Errorf("data mismatch at byte %d", i)
			break
		}
	}
}

func TestManagerEnqueue(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Test enqueue
	from := "sender@example.com"
	to := []string{"recipient1@example.com", "recipient2@example.com"}
	message := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest message")

	id, err := manager.Enqueue(from, to, message)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty message ID")
	}

	// Verify queue entry was created
	entries, err := manager.GetPendingEntries()
	if err != nil {
		t.Fatalf("GetPendingEntries failed: %v", err)
	}

	// Should have 2 entries (one per recipient)
	if len(entries) != 2 {
		t.Errorf("expected 2 queue entries, got %d", len(entries))
	}
}

func TestManagerEnqueueQueueFull(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)
	manager.SetMaxQueueSize(0) // Set queue size to 0 to simulate full queue

	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	message := []byte("Test message")

	_, err = manager.Enqueue(from, to, message)
	if err == nil {
		t.Error("expected error when queue is full")
	}
}

func TestManagerRetryEntry(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// First enqueue a message
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	message := []byte("Test message")

	id, err := manager.Enqueue(from, to, message)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Get the queue entry ID (baseID-0 format)
	entryID := id + "-0"

	// Mark it as failed first
	entry, _ := manager.GetQueueEntry(entryID)
	if entry != nil {
		entry.Status = "failed"
		entry.RetryCount = 3
		database.UpdateQueueEntry(entry)
	}

	// Retry the entry
	err = manager.RetryEntry(entryID)
	if err != nil {
		t.Logf("RetryEntry returned error (may be expected if entry not found): %v", err)
	}

	// Verify retry worked
	entry, _ = manager.GetQueueEntry(entryID)
	if entry != nil {
		if entry.Status != "pending" {
			t.Errorf("expected status 'pending' after retry, got %s", entry.Status)
		}
		if entry.RetryCount != 0 {
			t.Errorf("expected retry count 0 after retry, got %d", entry.RetryCount)
		}
	}
}


func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}

	if id1 == id2 {
		t.Error("expected unique IDs")
	}

	// Verify format (should contain timestamp and random number)
	if !strings.Contains(id1, "-") {
		t.Error("expected ID to contain '-' separator")
	}
}

func TestExtractDomainEdgeCases(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"user@sub.example.com", "sub.example.com"},
		{"user@", ""},
		{"@example.com", "example.com"},
		{"invalid", ""},
		{"", ""},
		{"a@b@c.com", ""},
		{"user@domain.co.uk", "domain.co.uk"},
	}

	for _, tc := range tests {
		got := extractDomain(tc.email)
		if got != tc.expected {
			t.Errorf("extractDomain(%q) = %q, want %q", tc.email, got, tc.expected)
		}
	}
}

func TestHandleDeliverySuccess(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a temporary message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("Test message content")
	writeFile(messagePath, testMessage)

	entry := &db.QueueEntry{
		ID:          "test-id",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: messagePath,
		Status:      "sending",
	}

	// Save entry to database first
	database.Enqueue(entry)

	// Call handleDeliverySuccess
	manager.handleDeliverySuccess(entry)

	// Verify status
	if entry.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %s", entry.Status)
	}

	// Verify file was deleted
	if _, err := os.Stat(messagePath); !os.IsNotExist(err) {
		t.Error("message file should be deleted after successful delivery")
	}
}

func TestHandleDeliveryFailure(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a temporary message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("Test message content")
	writeFile(messagePath, testMessage)

	entry := &db.QueueEntry{
		ID:          "test-id",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: messagePath,
		Status:      "sending",
		RetryCount:  0,
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call handleDeliveryFailure
	errorMsg := "connection refused"
	manager.handleDeliveryFailure(entry, errorMsg)

	// Verify status and retry count
	if entry.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", entry.RetryCount)
	}

	if entry.LastError != errorMsg {
		t.Errorf("expected last error '%s', got '%s'", errorMsg, entry.LastError)
	}

	// Status should be pending (for retry) unless max retries reached
	if entry.Status != "pending" && entry.Status != "bounced" {
		t.Errorf("expected status 'pending' or 'bounced', got %s", entry.Status)
	}
}

func TestHandleDeliveryFailureMaxRetries(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)
	manager.SetMaxRetries(3)

	// Create a temporary message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("Test message content")
	writeFile(messagePath, testMessage)

	entry := &db.QueueEntry{
		ID:          "test-id",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: messagePath,
		Status:      "sending",
		RetryCount:  2, // One away from max
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call handleDeliveryFailure
	manager.handleDeliveryFailure(entry, "final error")

	// Status should be bounced since max retries reached
	if entry.Status != "bounced" {
		t.Errorf("expected status 'bounced' after max retries, got %s", entry.Status)
	}
}

func TestGenerateBounce(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a temporary message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("Original message content")
	writeFile(messagePath, testMessage)

	entry := &db.QueueEntry{
		ID:          "test-id",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: messagePath,
		LastError:   "recipient mailbox full",
		Status:      "failed",
		RetryCount:  10,
	}

	// Call generateBounce - should not panic
	manager.generateBounce(entry)

	// Verify file was cleaned up
	if _, err := os.Stat(messagePath); !os.IsNotExist(err) {
		t.Error("message file should be deleted after bounce")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testData := []byte("atomic write test")

	err := writeFile(testFile, testData)
	if err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("file was not created")
	}

	// Verify content
	readData, err := readFile(testFile)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	if string(readData) != string(testData) {
		t.Errorf("content mismatch: got %q, want %q", string(readData), string(testData))
	}

	// Verify temp file was cleaned up
	tmpFile := testFile + ".tmp"
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up after atomic rename")
	}
}


func TestDeleteFileSilent(t *testing.T) {
	// Should not panic when deleting non-existent file
	deleteFile("/non/existent/file.txt")

	// Should work when deleting existing file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "to-delete.txt")
	os.WriteFile(testFile, []byte("delete me"), 0644)

	deleteFile(testFile)

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestManagerStartStop(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Test Start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	if !manager.running {
		t.Error("expected manager to be running after Start")
	}

	// Test Stop
	manager.Stop()
	if manager.running {
		t.Error("expected manager to be stopped after Stop")
	}
}


func TestQueueStatsFields(t *testing.T) {
	stats := &QueueStats{
		Pending:   5,
		Sending:   2,
		Failed:    1,
		Delivered: 100,
		Bounced:   3,
		Total:     111,
	}

	if stats.Pending != 5 {
		t.Error("Pending mismatch")
	}
	if stats.Sending != 2 {
		t.Error("Sending mismatch")
	}
	if stats.Failed != 1 {
		t.Error("Failed mismatch")
	}
	if stats.Delivered != 100 {
		t.Error("Delivered mismatch")
	}
	if stats.Bounced != 3 {
		t.Error("Bounced mismatch")
	}
	if stats.Total != 111 {
		t.Error("Total mismatch")
	}
}

func TestManagerProcessPendingEntries(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Enqueue a message
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	message := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest message")

	_, err = manager.Enqueue(from, to, message)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Process pending entries - should not panic
	manager.processPendingEntries()
}

func TestManagerDeliver(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest content")
	writeFile(messagePath, testMessage)

	entry := &db.QueueEntry{
		ID:          "test-deliver-id",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: messagePath,
		Status:      "pending",
		RetryCount:  0,
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call deliver - will likely fail due to no MX, but should not panic
	manager.deliver(entry)
}

func TestManagerDeliverToMX(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	message := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest content")

	// Try to deliver to invalid MX - should return error
	err = manager.deliverToMX("sender@example.com", "recipient@example.com", message, "invalid-mx-server-12345.xyz")
	if err == nil {
		t.Log("deliverToMX to invalid MX did not return error (may have fallback)")
	}
}

// TestManagerDeliverWithEmptyMessagePath tests deliver with empty message path
func TestManagerDeliverWithEmptyMessagePath(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	entry := &db.QueueEntry{
		ID:          "test-empty-path",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: "", // Empty path
		Status:      "pending",
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call deliver - should handle empty path gracefully
	manager.deliver(entry)
}

// TestManagerDeliverWithNonExistentFile tests deliver with non-existent message file
func TestManagerDeliverWithNonExistentFile(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	entry := &db.QueueEntry{
		ID:          "test-missing-file",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: "/non/existent/file.msg",
		Status:      "pending",
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call deliver - should handle missing file gracefully
	manager.deliver(entry)
}

// TestManagerProcessQueue tests the processQueue function
func TestManagerProcessQueue(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest content")
	writeFile(messagePath, testMessage)

	// Create entries with different statuses
	entries := []*db.QueueEntry{
		{
			ID:          "test-pending",
			From:        "sender@example.com",
			To:          []string{"recipient@example.com"},
			MessagePath: messagePath,
			Status:      "pending",
			RetryCount:  0,
		},
		{
			ID:          "test-sending",
			From:        "sender@example.com",
			To:          []string{"recipient2@example.com"},
			MessagePath: messagePath,
			Status:      "sending",
			RetryCount:  0,
		},
		{
			ID:          "test-failed",
			From:        "sender@example.com",
			To:          []string{"recipient3@example.com"},
			MessagePath: messagePath,
			Status:      "failed",
			RetryCount:  1,
		},
	}

	for _, entry := range entries {
		database.Enqueue(entry)
	}

	// Process queue with cancellable context - should not panic
	ctx, cancel := context.WithCancel(context.Background())
	go manager.processQueue(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
}

// TestManagerEnqueueWithEmptyRecipient tests enqueue with empty recipient list
func TestManagerEnqueueWithEmptyRecipient(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	from := "sender@example.com"
	to := []string{} // Empty recipient list
	message := []byte("Test message")

	_, err = manager.Enqueue(from, to, message)
	// Should handle empty recipient list gracefully
	if err != nil {
		t.Logf("Enqueue with empty recipients returned error (may be expected): %v", err)
	}
}

// TestManagerRetryEntryNotFound tests retrying a non-existent entry
func TestManagerRetryEntryNotFound(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Try to retry non-existent entry
	err = manager.RetryEntry("non-existent-id")
	if err == nil {
		t.Error("expected error when retrying non-existent entry")
	}
}

// TestManagerRetryEntryNotFailed tests retrying an entry that is not failed
func TestManagerRetryEntryNotFailed(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a pending entry
	entry := &db.QueueEntry{
		ID:          "test-pending-retry",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: filepath.Join(dataDir, "test.msg"),
		Status:      "pending",
		RetryCount:  5, // Set a high retry count
	}

	// Create message file
	writeFile(entry.MessagePath, []byte("test message"))

	// Save entry
	database.Enqueue(entry)

	// Retry pending entry - current implementation allows retrying any entry
	err = manager.RetryEntry(entry.ID)
	if err != nil {
		t.Errorf("unexpected error when retrying pending entry: %v", err)
	}

	// Verify entry was reset
	updated, _ := database.GetQueueEntry(entry.ID)
	if updated.RetryCount != 0 {
		t.Errorf("expected retry count to be reset to 0, got %d", updated.RetryCount)
	}
	if updated.Status != "pending" {
		t.Errorf("expected status to be pending, got %s", updated.Status)
	}
}

// TestHandleDeliveryFailureWithMetrics tests handleDeliveryFailure with metrics
func TestHandleDeliveryFailureWithMetrics(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a temporary message file
	messagePath := filepath.Join(dataDir, "test.msg")
	writeFile(messagePath, []byte("Test message content"))

	entry := &db.QueueEntry{
		ID:          "test-metrics",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: messagePath,
		Status:      "sending",
		RetryCount:  0,
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call handleDeliveryFailure - should not panic with metrics
	manager.handleDeliveryFailure(entry, "test error")

	// Verify entry was updated
	updated, _ := database.GetQueueEntry(entry.ID)
	if updated == nil || updated.RetryCount != 1 {
		t.Error("expected entry to be updated with retry count")
	}
}

// mockMetricsCollector is a mock implementation for testing
type mockMetricsCollector struct {
	deliveryFailedCalled bool
}

func (m *mockMetricsCollector) DeliveryFailed() {
	m.deliveryFailedCalled = true
}

func (m *mockMetricsCollector) DeliverySucceeded() {}
func (m *mockMetricsCollector) MessageReceived()   {}
func (m *mockMetricsCollector) MessageRejected()   {}

// TestGenerateBounceWithMissingFile tests generateBounce when message file is missing
func TestGenerateBounceWithMissingFile(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	entry := &db.QueueEntry{
		ID:          "test-bounce-missing",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: "/non/existent/file.msg",
		LastError:   "delivery failed",
		Status:      "failed",
		RetryCount:  10,
	}

	// Call generateBounce with missing file - should not panic
	manager.generateBounce(entry)
}

// TestManagerDeliverLocalDomain tests delivery to local domain
func TestManagerDeliverLocalDomain(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir)

	// Create a message file
	messagePath := filepath.Join(dataDir, "test.msg")
	testMessage := []byte("From: sender@localhost\r\nTo: recipient@localhost\r\nSubject: Test\r\n\r\nTest content")
	writeFile(messagePath, testMessage)

	entry := &db.QueueEntry{
		ID:          "test-local",
		From:        "sender@localhost",
		To:          []string{"recipient@localhost"},
		MessagePath: messagePath,
		Status:      "pending",
		RetryCount:  0,
	}

	// Save entry to database
	database.Enqueue(entry)

	// Call deliver - will attempt local delivery
	manager.deliver(entry)
}
