package queue

import (
	"context"
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

	t.Run("Enqueue", func(t *testing.T) {
		from := "sender@example.com"
		to := []string{"recipient@example.com"}
		message := []byte("Subject: Test\r\n\r\nBody")

		id, err := manager.Enqueue(from, to, message)
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}

		if id == "" {
			t.Error("Expected non-empty ID")
		}

		// Check that queue entry was created
		// Note: In real implementation, we would verify the entry exists
	})
}
