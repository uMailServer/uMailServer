package db

import (
	"testing"
)

func TestQueuePriority_String(t *testing.T) {
	tests := []struct {
		p        QueuePriority
		expected string
	}{
		{PriorityLow, "low"},
		{PriorityNormal, "normal"},
		{PriorityHigh, "high"},
		{PriorityUrgent, "urgent"},
		{QueuePriority(99), "normal"},
	}

	for _, tt := range tests {
		got := tt.p.String()
		if got != tt.expected {
			t.Errorf("QueuePriority(%d).String() = %q, want %q", tt.p, got, tt.expected)
		}
	}
}

func TestDB_BoltDB(t *testing.T) {
	database := helperDB(t)
	defer database.Close()

	bolt := database.BoltDB()
	if bolt == nil {
		t.Error("expected non-nil bbolt.DB from BoltDB()")
	}
}

func TestDB_RunMigrations(t *testing.T) {
	database := helperDB(t)
	defer database.Close()

	err := database.RunMigrations()
	if err != nil {
		t.Errorf("RunMigrations failed: %v", err)
	}

	// Running again should be idempotent (no-op since already migrated)
	err = database.RunMigrations()
	if err != nil {
		t.Errorf("RunMigrations second run failed: %v", err)
	}
}
