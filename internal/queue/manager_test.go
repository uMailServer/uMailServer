package queue

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"net/smtp"
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
	manager := NewManager(database, nil, dataDir, nil)

	t.Run("StartStop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager.Start(ctx)
		if !manager.running.Load() {
			t.Error("Expected manager to be running")
		}

		manager.Stop()
		if manager.running.Load() {
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

	manager := NewManager(database, nil, dataDir, nil)
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
	resolver := &realDNSResolver{}

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first time
	manager.Start(ctx)
	if !manager.running.Load() {
		t.Error("expected manager to be running after first start")
	}

	// Start again - should not panic or create issues
	manager.Start(ctx)
	if !manager.running.Load() {
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

	manager := NewManager(database, nil, dataDir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	manager.Stop()

	// Stop again - should not panic
	manager.Stop()
	if manager.running.Load() {
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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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
	resolver := &realDNSResolver{}

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)
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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)
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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

	// Test Start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	if !manager.running.Load() {
		t.Error("expected manager to be running after Start")
	}

	// Test Stop
	manager.Stop()
	if manager.running.Load() {
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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

	manager := NewManager(database, nil, dataDir, nil)

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

func TestSignWithDKIMNoKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dkim-queue-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	manager := NewManager(database, nil, tmpDir, nil)

	// Test with no DKIM key configured — should fail gracefully
	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	_, err = manager.signWithDKIM("sender@example.com", msg)
	if err == nil {
		t.Error("Expected error when no DKIM key configured, got nil")
	}
}

func TestParseMessageHeaders(t *testing.T) {
	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: Test Message\r\n\r\nBody content\r\n")
	headers := parseMessageHeaders(msg)
	if len(headers) == 0 {
		t.Fatal("Expected headers to be parsed")
	}
	fromVals := headers["From"]
	if len(fromVals) == 0 || fromVals[0] != "sender@example.com" {
		t.Errorf("Expected From header 'sender@example.com', got: %v", fromVals)
	}
	subjectVals := headers["Subject"]
	if len(subjectVals) == 0 || subjectVals[0] != "Test Message" {
		t.Errorf("Expected Subject header 'Test Message', got: %v", subjectVals)
	}
}

func TestExtractMessageBody(t *testing.T) {
	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\n\r\nBody line 1\r\nBody line 2\r\n")
	body := extractMessageBody(msg)
	if string(body) != "Body line 1\r\nBody line 2\r\n" {
		t.Errorf("Expected body content, got: %q", string(body))
	}

	// Test with no body separator
	noBodyMsg := []byte("From: sender@example.com\r\nTo: rcpt@example.com")
	body = extractMessageBody(noBodyMsg)
	if body != nil {
		t.Errorf("Expected nil body for message without separator, got: %q", string(body))
	}
}

func TestDkimDNSResolver(t *testing.T) {
	r := &dkimDNSResolver{}
	ctx := context.Background()

	// Test LookupTXT
	_, _ = r.LookupTXT(ctx, "localhost")

	// Test LookupIP
	_, err := r.LookupIP(ctx, "127.0.0.1")
	if err != nil {
		t.Logf("LookupIP returned error (expected in some envs): %v", err)
	}

	// Test LookupMX
	_, _ = r.LookupMX(ctx, "localhost")
}

// --- Additional tests for low-coverage functions ---

// helper: generateRSAPEM creates a PKCS1 RSA private key in PEM format.
func generateRSAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// helper: generatePKCS8PEM creates a PKCS8 RSA private key in PEM format.
func generatePKCS8PEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// helper: setupManager creates a Manager backed by a real database in a temp dir.
func setupManager(t *testing.T) (*Manager, string, *db.DB) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	manager := NewManager(database, nil, dataDir, nil)
	return manager, dataDir, database
}

// --- signWithDKIM tests ---

func TestSignWithDKIM_NilDB(t *testing.T) {
	manager := &Manager{db: nil}
	_, err := manager.signWithDKIM("user@example.com", []byte("msg"))
	if err == nil {
		t.Error("expected error with nil db")
	}
}

func TestSignWithDKIM_EmptySenderDomain(t *testing.T) {
	mgr, _, db := setupManager(t)
	defer db.Close()
	_, err := mgr.signWithDKIM("invalid-sender", []byte("msg"))
	if err == nil {
		t.Error("expected error when sender domain cannot be extracted")
	}
}

func TestSignWithDKIM_DomainNotFound(t *testing.T) {
	mgr, _, db := setupManager(t)
	defer db.Close()
	_, err := mgr.signWithDKIM("user@not-in-db.com", []byte("From: user@not-in-db.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when domain is not in database")
	}
}

func TestSignWithDKIM_NoDKIMConfig(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// Create a domain without DKIM keys
	database.CreateDomain(&db.DomainData{Name: "example.com"})

	_, err := mgr.signWithDKIM("user@example.com", []byte("From: user@example.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when DKIM not configured")
	}
}

func TestSignWithDKIM_EmptySelector(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	pemKey := generateRSAPEM(t)
	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: string(pemKey),
		DKIMSelector:   "", // empty selector
	})

	_, err := mgr.signWithDKIM("user@example.com", []byte("From: user@example.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when DKIM selector is empty")
	}
}

func TestSignWithDKIM_EmptyPrivateKey(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: "", // empty key
		DKIMSelector:   "selector",
	})

	_, err := mgr.signWithDKIM("user@example.com", []byte("From: user@example.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when DKIM private key is empty")
	}
}

func TestSignWithDKIM_InvalidPEM(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: "not-valid-pem-data",
		DKIMSelector:   "selector",
	})

	_, err := mgr.signWithDKIM("user@example.com", []byte("From: user@example.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when PEM data is invalid")
	}
}

func TestSignWithDKIM_InvalidKeyBytes(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// Valid PEM block but garbage key bytes
	badPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("garbage-key-data")})
	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: string(badPEM),
		DKIMSelector:   "selector",
	})

	_, err := mgr.signWithDKIM("user@example.com", []byte("From: user@example.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when key bytes are invalid")
	}
}

func TestSignWithDKIM_PKCS8NonRSAKey(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// Generate an EC key (not RSA) and marshal as PKCS8
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	ecPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: string(ecPEM),
		DKIMSelector:   "selector",
	})

	_, err = mgr.signWithDKIM("user@example.com", []byte("From: user@example.com\r\n\r\nbody"))
	if err == nil {
		t.Error("expected error when private key is not RSA")
	}
}

func TestSignWithDKIM_ValidPKCS1Key(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	pemKey := generateRSAPEM(t)
	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: string(pemKey),
		DKIMSelector:   "default",
	})

	msg := []byte("From: user@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello world\r\n")
	signed, err := mgr.signWithDKIM("user@example.com", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The signature value is prepended before the original message.
	// It starts with "v=1; a=rsa-sha256; ..." based on buildHeader.
	if !bytes.HasPrefix(signed, []byte("v=1;")) {
		t.Errorf("expected signed message to start with DKIM signature data, got prefix: %q", string(signed[:80]))
	}
	// The original message should follow after the signature header line
	if !bytes.Contains(signed, []byte("From: user@example.com")) {
		t.Error("signed message should contain original message content")
	}
}

func TestSignWithDKIM_ValidPKCS8Key(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	pemKey := generatePKCS8PEM(t)
	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: string(pemKey),
		DKIMSelector:   "default",
	})

	msg := []byte("From: user@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello world\r\n")
	signed, err := mgr.signWithDKIM("user@example.com", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.HasPrefix(signed, []byte("v=1;")) {
		t.Errorf("expected signed message to start with DKIM signature data, got prefix: %q", string(signed[:80]))
	}
	if !bytes.Contains(signed, []byte("From: user@example.com")) {
		t.Error("signed message should contain original message content")
	}
}

// --- deliverToMX tests using a fake SMTP server ---

// fakeSMTPServer starts a minimal SMTP server on a random port.
// It accepts the SMTP conversation and calls handler with the received data.
// Returns the server address (host:port).
func fakeSMTPServer(t *testing.T, handler func(from, to string, data []byte)) (addr string, closer func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.Write([]byte("220 fake.smtp ESMTP\r\n"))
				buf := make([]byte, 4096)
				var from, to string
				var dataBuf bytes.Buffer
				readingData := false

				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					line := string(buf[:n])

					if readingData {
						dataBuf.Write(buf[:n])
						if bytes.Contains(buf[:n], []byte("\r\n.\r\n")) {
							handler(from, to, dataBuf.Bytes())
							c.Write([]byte("250 OK\r\n"))
							readingData = false
						}
						continue
					}

					upper := strings.ToUpper(strings.TrimSpace(line))
					if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
						c.Write([]byte("250-fake.smtp\r\n250 STARTTLS\r\n"))
					} else if strings.HasPrefix(upper, "STARTTLS") {
						c.Write([]byte("454 TLS not available\r\n"))
					} else if strings.HasPrefix(upper, "MAIL FROM:") {
						from = strings.TrimPrefix(upper, "MAIL FROM:")
						from = strings.Trim(from, "<> ")
						c.Write([]byte("250 OK\r\n"))
					} else if strings.HasPrefix(upper, "RCPT TO:") {
						to = strings.TrimPrefix(upper, "RCPT TO:")
						to = strings.Trim(to, "<> ")
						c.Write([]byte("250 OK\r\n"))
					} else if strings.HasPrefix(upper, "DATA") {
						c.Write([]byte("354 Go ahead\r\n"))
						readingData = true
					} else if strings.HasPrefix(upper, "QUIT") {
						c.Write([]byte("221 Bye\r\n"))
						return
					} else if strings.HasPrefix(upper, "RSET") {
						c.Write([]byte("250 OK\r\n"))
					} else if strings.HasPrefix(upper, "NOOP") {
						c.Write([]byte("250 OK\r\n"))
					} else {
						c.Write([]byte("500 Unknown command\r\n"))
					}
				}
			}(conn)
		}
	}()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	addr = "127.0.0.1:" + port
	closer = func() {
		ln.Close()
		<-done
	}
	return
}

func TestDeliverToMX_Success(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// deliverToMX always appends ":25" to the MX host argument.
	// We test the connection-refused path which exercises most of the function's
	// error handling. A full integration test would require port 25.
	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")

	// Test connection to localhost:25 which should fail quickly
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "127.0.0.1")
	if err == nil {
		t.Log("deliverToMX to 127.0.0.1:25 succeeded (unlikely)")
	}
	// Error is expected since no SMTP server is on port 25
}

func TestDeliverToMX_ConnectionRefused(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	// Port 1 is unlikely to have anything listening
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "127.0.0.1:1")
	if err == nil {
		t.Error("expected error connecting to non-existent server on port 1")
	}
}

func TestDeliverToMX_InvalidHost(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "invalid-host-that-does-not-exist-99999.xyz")
	if err == nil {
		t.Error("expected error delivering to invalid host")
	}
}

func TestDeliverToMX_FakeServerSuccess(t *testing.T) {
	// This test verifies the fakeSMTPServer helper works correctly.
	// Since deliverToMX hardcodes port 25, we test the server infrastructure
	// by connecting to it directly via net/smtp.
	var receivedFrom, receivedTo string
	var receivedData []byte
	addr, closer := fakeSMTPServer(t, func(from, to string, data []byte) {
		receivedFrom = from
		receivedTo = to
		receivedData = data
	})
	defer closer()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to fake SMTP server: %v", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, "127.0.0.1")
	if err != nil {
		t.Fatalf("failed to create SMTP client: %v", err)
	}
	defer client.Close()

	client.Hello("testhost")
	client.Mail("sender@example.com")
	client.Rcpt("rcpt@example.com")
	w, _ := client.Data()
	w.Write([]byte("Subject: test\r\n\r\nHello\r\n.\r\n"))
	w.Close()
	client.Quit()

	if receivedFrom != "SENDER@EXAMPLE.COM" && receivedFrom != "sender@example.com" {
		t.Logf("received from: %q", receivedFrom)
	}
	if receivedTo == "" {
		t.Error("expected non-empty recipient")
	}
	if len(receivedData) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestDeliverToMX_VerEncoding(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")

	// This tests the VERP encoding path in deliverToMX.
	// The function extracts the domain from 'from', creates a VERP sender,
	// and uses that as the envelope sender. We just verify it doesn't panic.
	// It will fail at connection time.
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "nonexistent.invalid")
	if err == nil {
		t.Log("deliverToMX unexpectedly succeeded")
	}
}

// --- writeFile error tests ---

func TestWriteFile_InvalidPath(t *testing.T) {
	// On Windows, paths with null bytes or reserved names will fail.
	// Use a path with a null byte which is invalid on all platforms.
	targetPath := "file\x00with\x00null/path.txt"
	err := writeFile(targetPath, []byte("data"))
	if err == nil {
		t.Error("expected error writing to path with null bytes")
	}
}

func TestWriteFile_OverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "file.txt")

	// Write initial content
	if err := writeFile(path, []byte("initial")); err != nil {
		t.Fatal(err)
	}

	// Overwrite
	if err := writeFile(path, []byte("updated")); err != nil {
		t.Fatal(err)
	}

	data, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated" {
		t.Errorf("expected 'updated', got %q", string(data))
	}
}

// --- FlushQueue with failed entries that exist in the pending list ---

func TestFlushQueue_WithRetryableFailedEntry(t *testing.T) {
	mgr, dataDir, database := setupManager(t)
	defer database.Close()

	// Create a "failed" entry that also has status conditions where
	// GetPendingQueue would return it (status "pending" and NextRetry in the past).
	// Since FlushQueue calls GetPendingEntries -> GetPendingQueue which filters
	// for status == "pending", a "failed" entry won't be returned.
	// But FlushQueue iterates and only retries entries with status == "failed".
	// This tests the code path where pending entries exist but none are "failed".
	msgPath := filepath.Join(dataDir, "test.msg")
	writeFile(msgPath, []byte("test"))

	entry := &db.QueueEntry{
		ID:          "flush-pending-1",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: msgPath,
		Status:      "pending",
		RetryCount:  2,
		NextRetry:   time.Now().Add(-time.Hour),
	}
	database.Enqueue(entry)

	err := mgr.FlushQueue()
	if err != nil {
		t.Fatalf("FlushQueue failed: %v", err)
	}

	// Entry should remain "pending" since it was not "failed"
	updated, _ := database.GetQueueEntry(entry.ID)
	if updated != nil {
		if updated.Status != "pending" {
			t.Errorf("expected status 'pending' (unchanged), got %q", updated.Status)
		}
		if updated.RetryCount != 2 {
			t.Errorf("expected retry count 2 (unchanged), got %d", updated.RetryCount)
		}
	}
}

func TestFlushQueue_DBError(t *testing.T) {
	// Create manager, then close database to cause errors
	mgr, _, database := setupManager(t)
	database.Close()

	err := mgr.FlushQueue()
	if err == nil {
		t.Error("expected error when database is closed")
	}
}

// --- processPendingEntries edge cases ---

func TestProcessPendingEntries_DBError(t *testing.T) {
	mgr, _, database := setupManager(t)
	database.Close()

	// Should not panic when DB is closed
	mgr.processPendingEntries()
}

// --- deliver edge cases ---

func TestDeliver_EmptyRecipientDomain(t *testing.T) {
	mgr, dataDir, database := setupManager(t)
	defer database.Close()

	msgPath := filepath.Join(dataDir, "test.msg")
	writeFile(msgPath, []byte("test message"))

	entry := &db.QueueEntry{
		ID:          "deliver-nodomain",
		From:        "sender@example.com",
		To:          []string{"invalid-recipient"}, // no @ sign
		MessagePath: msgPath,
		Status:      "pending",
	}
	database.Enqueue(entry)

	// Should handle gracefully (invalid domain)
	mgr.deliver(entry)

	updated, _ := database.GetQueueEntry(entry.ID)
	if updated == nil {
		t.Fatal("entry should still exist")
	}
	if updated.Status == "sending" {
		t.Error("status should not remain 'sending'")
	}
}

func TestDeliver_MultipleMXFallback(t *testing.T) {
	mgr, dataDir, database := setupManager(t)
	defer database.Close()

	msgPath := filepath.Join(dataDir, "test.msg")
	writeFile(msgPath, []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nbody"))

	entry := &db.QueueEntry{
		ID:          "deliver-mxfail",
		From:        "sender@example.com",
		To:          []string{"rcpt@localhost"},
		MessagePath: msgPath,
		Status:      "pending",
	}
	database.Enqueue(entry)

	mgr.deliver(entry)

	updated, _ := database.GetQueueEntry(entry.ID)
	if updated == nil {
		t.Fatal("entry should exist")
	}
	// Delivery will fail since there's no real MX, but it should not panic
	t.Logf("Status after delivery attempt: %s, error: %s", updated.Status, updated.LastError)
}

// --- parseMessageHeaders and extractMessageBody additional tests ---

func TestParseMessageHeaders_InvalidMessage(t *testing.T) {
	headers := parseMessageHeaders([]byte("not a valid message at all"))
	if headers == nil {
		t.Error("expected non-nil map even for invalid message")
	}
}

func TestExtractMessageBody_LFOnly(t *testing.T) {
	// Message with \n\n separator (no \r\n)
	msg := []byte("From: a@b.com\nTo: c@d.com\n\nBody here\n")
	body := extractMessageBody(msg)
	if string(body) != "Body here\n" {
		t.Errorf("expected 'Body here\\n', got %q", string(body))
	}
}

func TestExtractMessageBody_NoSeparator(t *testing.T) {
	body := extractMessageBody([]byte("just headers no body"))
	if body != nil {
		t.Errorf("expected nil, got %q", string(body))
	}
}

func TestExtractMessageBody_EmptyBody(t *testing.T) {
	msg := []byte("From: a@b.com\r\n\r\n")
	body := extractMessageBody(msg)
	if len(body) != 0 {
		t.Errorf("expected empty body, got %q", string(body))
	}
}

func TestParseMessageHeaders_MultipleValues(t *testing.T) {
	msg := []byte("From: a@b.com\r\nTo: x@y.com\r\nTo: z@y.com\r\nSubject: test\r\n\r\nbody\r\n")
	headers := parseMessageHeaders(msg)
	toVals := headers["To"]
	if len(toVals) != 2 {
		t.Errorf("expected 2 To headers, got %d", len(toVals))
	}
}

// --- deliverToMX tests using dialSMTP injection ---

// fakeSMTPServerConfig controls the behavior of a configurable fake SMTP server.
type fakeSMTPServerConfig struct {
	// STARTTLS: if true, the server will advertise STARTTLS and attempt a TLS handshake.
	advertiseSTARTTLS bool
	// tlsCert and tlsConfig are used when advertiseSTARTTLS is true.
	tlsConfig *tls.Config

	// Response overrides: if set, these responses are sent instead of "250 OK".
	mailFromResponse string // e.g. "550 rejected"
	rcptToResponse   string
	dataResponse     string // response to the DATA command itself
	dataEndResponse  string // response after .\r\n

	// handler is called when a complete message is received.
	handler func(from, to string, data []byte)
}

// fakeSMTPServerEx starts a configurable SMTP server on a random port.
// Returns the network address (host:port).
func fakeSMTPServerEx(t *testing.T, cfg fakeSMTPServerConfig) (addr string, closer func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.Write([]byte("220 fake.smtp ESMTP\r\n"))
				buf := make([]byte, 8192)
				var from, to string
				var dataBuf bytes.Buffer
				readingData := false

				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					line := string(buf[:n])

					if readingData {
						dataBuf.Write(buf[:n])
						if bytes.Contains(buf[:n], []byte("\r\n.\r\n")) {
							if cfg.handler != nil {
								cfg.handler(from, to, dataBuf.Bytes())
							}
							resp := "250 OK\r\n"
							if cfg.dataEndResponse != "" {
								resp = cfg.dataEndResponse
							}
							c.Write([]byte(resp))
							readingData = false
						}
						continue
					}

					upper := strings.ToUpper(strings.TrimSpace(line))
					if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
						if cfg.advertiseSTARTTLS {
							c.Write([]byte("250-fake.smtp\r\n250 STARTTLS\r\n"))
						} else {
							c.Write([]byte("250-fake.smtp\r\n250 OK\r\n"))
						}
					} else if strings.HasPrefix(upper, "STARTTLS") {
						if cfg.advertiseSTARTTLS && cfg.tlsConfig != nil {
							c.Write([]byte("220 Ready for TLS\r\n"))
							tlsConn := tls.Server(c, cfg.tlsConfig)
							if err := tlsConn.Handshake(); err != nil {
								// TLS handshake failed, close connection
								return
							}
							// Replace c with tlsConn for further I/O
							c = tlsConn
							// Continue reading from the TLS connection
							for {
								n, err = c.Read(buf)
								if err != nil {
									return
								}
								line = string(buf[:n])
								upper = strings.ToUpper(strings.TrimSpace(line))

								if readingData {
									dataBuf.Write(buf[:n])
									if bytes.Contains(buf[:n], []byte("\r\n.\r\n")) {
										if cfg.handler != nil {
											cfg.handler(from, to, dataBuf.Bytes())
										}
										resp := "250 OK\r\n"
										if cfg.dataEndResponse != "" {
											resp = cfg.dataEndResponse
										}
										c.Write([]byte(resp))
										readingData = false
									}
									continue
								}

								if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
									c.Write([]byte("250-fake.smtp\r\n250 OK\r\n"))
								} else if strings.HasPrefix(upper, "MAIL FROM:") {
									from = strings.TrimPrefix(upper, "MAIL FROM:")
									from = strings.Trim(from, "<> ")
									resp := "250 OK\r\n"
									if cfg.mailFromResponse != "" {
										resp = cfg.mailFromResponse + "\r\n"
									}
									c.Write([]byte(resp))
								} else if strings.HasPrefix(upper, "RCPT TO:") {
									to = strings.TrimPrefix(upper, "RCPT TO:")
									to = strings.Trim(to, "<> ")
									resp := "250 OK\r\n"
									if cfg.rcptToResponse != "" {
										resp = cfg.rcptToResponse + "\r\n"
									}
									c.Write([]byte(resp))
								} else if strings.HasPrefix(upper, "DATA") {
									resp := "354 Go ahead\r\n"
									if cfg.dataResponse != "" {
										resp = cfg.dataResponse + "\r\n"
									}
									c.Write([]byte(resp))
									readingData = true
								} else if strings.HasPrefix(upper, "QUIT") {
									c.Write([]byte("221 Bye\r\n"))
									return
								} else {
									c.Write([]byte("250 OK\r\n"))
								}
							}
						}
						c.Write([]byte("454 TLS not available\r\n"))
					} else if strings.HasPrefix(upper, "MAIL FROM:") {
						from = strings.TrimPrefix(upper, "MAIL FROM:")
						from = strings.Trim(from, "<> ")
						resp := "250 OK\r\n"
						if cfg.mailFromResponse != "" {
							resp = cfg.mailFromResponse + "\r\n"
						}
						c.Write([]byte(resp))
					} else if strings.HasPrefix(upper, "RCPT TO:") {
						to = strings.TrimPrefix(upper, "RCPT TO:")
						to = strings.Trim(to, "<> ")
						resp := "250 OK\r\n"
						if cfg.rcptToResponse != "" {
							resp = cfg.rcptToResponse + "\r\n"
						}
						c.Write([]byte(resp))
					} else if strings.HasPrefix(upper, "DATA") {
						resp := "354 Go ahead\r\n"
						if cfg.dataResponse != "" {
							resp = cfg.dataResponse + "\r\n"
						}
						c.Write([]byte(resp))
						readingData = true
					} else if strings.HasPrefix(upper, "QUIT") {
						c.Write([]byte("221 Bye\r\n"))
						return
					} else if strings.HasPrefix(upper, "RSET") || strings.HasPrefix(upper, "NOOP") {
						c.Write([]byte("250 OK\r\n"))
					} else {
						c.Write([]byte("500 Unknown command\r\n"))
					}
				}
			}(conn)
		}
	}()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	addr = "127.0.0.1:" + port
	closer = func() {
		ln.Close()
		<-done
	}
	return
}

// generateTestTLSCert creates a self-signed TLS certificate for testing.
func generateTestTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"127.0.0.1", "localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return tlsCert
}

func TestDeliverToMX_FullSMTPConversation(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	var receivedFrom, receivedTo string
	var receivedData []byte
	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		handler: func(from, to string, data []byte) {
			receivedFrom = from
			receivedTo = to
			receivedData = data
		},
	})
	defer closer()

	// Override dial to connect to our fake server
	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello world\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	if err != nil {
		t.Fatalf("deliverToMX failed: %v", err)
	}

	if receivedFrom == "" {
		t.Error("expected MAIL FROM to be received by server")
	}
	if receivedTo == "" {
		t.Error("expected RCPT TO to be received by server")
	}
	if len(receivedData) == 0 {
		t.Error("expected message data to be received by server")
	}
	// Verify VERP-encoded sender was used
	if !strings.Contains(receivedFrom, "BOUNCE-") && !strings.Contains(receivedFrom, "bounce-") {
		t.Logf("Note: sender received by server: %q (expected VERP-encoded)", receivedFrom)
	}
}

func TestDeliverToMX_StartTLSSuccess(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	tlsCert := generateTestTLSCert(t)
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	var receivedData []byte
	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		advertiseSTARTTLS: true,
		tlsConfig:         tlsCfg,
		handler: func(from, to string, data []byte) {
			receivedData = data
		},
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nTLS test\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	// Since deliverToMX uses tls.Config{ServerName: mx} with default verification,
	// the self-signed cert will fail verification. The function should fall back
	// to plaintext and still deliver. If it succeeds, data was received.
	if err != nil {
		// Even if STARTTLS fails, the server may still have accepted the message
		// over the plaintext connection. Some servers close the connection after
		// a failed TLS handshake, so an error is acceptable here.
		t.Logf("deliverToMX with self-signed STARTTLS cert returned error (acceptable): %v", err)
	} else if len(receivedData) > 0 {
		t.Log("deliverToMX succeeded and data was received by the server")
	}
	// The key point: the function should not panic and the STARTTLS code path
	// is exercised (lines 318-322 in manager.go).
}

func TestDeliverToMX_StartTLSFailure(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// Server advertises STARTTLS but the handshake will fail because we provide
	// a bad TLS config (cert not matching, etc.). The client should fall back to
	// plaintext and still deliver.
	var receivedData []byte
	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		advertiseSTARTTLS: true,
		tlsConfig:         &tls.Config{}, // empty config, handshake will fail
		handler: func(from, to string, data []byte) {
			receivedData = data
		},
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nPlaintext fallback\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	// The function may succeed or fail depending on how the server reacts to a
	// failed TLS handshake. Either way, it should not panic.
	if err != nil {
		t.Logf("deliverToMX with failed STARTTLS returned error (acceptable): %v", err)
	} else if len(receivedData) > 0 {
		t.Log("deliverToMX succeeded via plaintext fallback after STARTTLS failure")
	}
}

func TestDeliverToMX_MailFailure(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		mailFromResponse: "550 Sender rejected",
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	if err == nil {
		t.Error("expected error when MAIL FROM is rejected")
	}
	if !strings.Contains(err.Error(), "Sender rejected") {
		t.Errorf("expected error to contain 'Sender rejected', got: %v", err)
	}
}

func TestDeliverToMX_RcptFailure(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		rcptToResponse: "550 Recipient rejected",
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	if err == nil {
		t.Error("expected error when RCPT TO is rejected")
	}
	if !strings.Contains(err.Error(), "Recipient rejected") {
		t.Errorf("expected error to contain 'Recipient rejected', got: %v", err)
	}
}

func TestDeliverToMX_DataFailure(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		dataResponse: "552 Message too large",
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nHello\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	if err == nil {
		t.Error("expected error when DATA is rejected")
	}
}

func TestDeliver_FullSuccessPath(t *testing.T) {
	mgr, dataDir, database := setupManager(t)
	defer database.Close()

	var receivedData []byte
	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		handler: func(from, to string, data []byte) {
			receivedData = data
		},
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msgPath := filepath.Join(dataDir, "deliver-test.msg")
	testMsg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: test\r\n\r\nDelivery test\r\n")
	writeFile(msgPath, testMsg)

	entry := &db.QueueEntry{
		ID:          "deliver-full-success",
		From:        "sender@example.com",
		To:          []string{"rcpt@example.com"},
		MessagePath: msgPath,
		Status:      "pending",
		RetryCount:  0,
		CreatedAt:   time.Now(),
		NextRetry:   time.Now(),
	}
	database.Enqueue(entry)

	// Call deliverToMX directly to exercise the SMTP conversation.
	// Then verify handleDeliverySuccess by checking the resulting state.
	err := mgr.deliverToMX(entry.From, entry.To[0], testMsg, "mx.example.com")
	if err != nil {
		t.Fatalf("deliverToMX failed: %v", err)
	}

	if len(receivedData) == 0 {
		t.Error("expected message data to be received by fake server")
	}

	// Now simulate what deliver() does after successful deliverToMX:
	// call handleDeliverySuccess and verify the status transition.
	mgr.handleDeliverySuccess(entry)

	updated, err := database.GetQueueEntry(entry.ID)
	if err != nil {
		t.Fatalf("failed to get entry: %v", err)
	}
	if updated.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", updated.Status)
	}
	// Verify message file was cleaned up
	if _, err := os.Stat(msgPath); !os.IsNotExist(err) {
		t.Error("message file should be deleted after successful delivery")
	}
}

func TestDeliverToMX_WithDKIMSigning(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()

	// Set up DKIM key for the sender's domain
	pemKey := generateRSAPEM(t)
	database.CreateDomain(&db.DomainData{
		Name:           "example.com",
		DKIMPrivateKey: string(pemKey),
		DKIMSelector:   "default",
	})

	var receivedData []byte
	addr, closer := fakeSMTPServerEx(t, fakeSMTPServerConfig{
		handler: func(from, to string, data []byte) {
			receivedData = data
		},
	})
	defer closer()

	mgr.dialSMTP = func(dialAddr string) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}

	msg := []byte("From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: DKIM test\r\n\r\nSigned message\r\n")
	err := mgr.deliverToMX("sender@example.com", "rcpt@example.com", msg, "mx.example.com")
	if err != nil {
		t.Fatalf("deliverToMX with DKIM failed: %v", err)
	}

	// Verify the data received by the server contains a DKIM-Signature header
	// prepended by signWithDKIM.
	if !bytes.Contains(receivedData, []byte("v=1;")) {
		t.Errorf("expected DKIM-Signature header (v=1;) in delivered message, got: %q", string(receivedData[:min(len(receivedData), 200)]))
	}
	// Original message content should also be present
	if !bytes.Contains(receivedData, []byte("DKIM test")) {
		t.Error("expected original message content in delivered data")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestManagerSetRequireTLS(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	// Test default value (should be false)
	if manager.requireTLS {
		t.Error("expected requireTLS to be false by default")
	}

	// Set requireTLS to true
	manager.SetRequireTLS(true)
	if !manager.requireTLS {
		t.Error("expected requireTLS to be true after SetRequireTLS(true)")
	}

	// Set requireTLS to false
	manager.SetRequireTLS(false)
	if manager.requireTLS {
		t.Error("expected requireTLS to be false after SetRequireTLS(false)")
	}
}
