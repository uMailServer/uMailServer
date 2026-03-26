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
