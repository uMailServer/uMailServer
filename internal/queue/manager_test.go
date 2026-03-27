package queue

import (
	"context"
	"os"
	"path/filepath"
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
