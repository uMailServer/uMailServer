package security

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestBlocklist(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	t.Run("Add", func(t *testing.T) {
		err := bl.Add("192.168.1.1", "ip", "test block")
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}

		if !bl.IsBlocked("192.168.1.1") {
			t.Error("expected IP to be blocked")
		}
	})

	t.Run("AddTemporary", func(t *testing.T) {
		err := bl.AddTemporary("192.168.1.2", 1*time.Hour, "temporary block")
		if err != nil {
			t.Fatalf("AddTemporary failed: %v", err)
		}

		if !bl.IsBlocked("192.168.1.2") {
			t.Error("expected temporary blocked IP to be blocked")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		// Add and verify
		bl.Add("192.168.1.3", "ip", "to be removed")
		if !bl.IsBlocked("192.168.1.3") {
			t.Error("expected IP to be blocked before removal")
		}

		// Remove
		err := bl.Remove("192.168.1.3")
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		// Verify removal
		if bl.IsBlocked("192.168.1.3") {
			t.Error("expected IP to not be blocked after removal")
		}
	})

	t.Run("GetEntry", func(t *testing.T) {
		bl.Add("192.168.1.4", "ip", "test entry")

		entry := bl.GetEntry("192.168.1.4")
		if entry == nil {
			t.Fatal("expected to get entry")
		}

		if entry.Key != "192.168.1.4" {
			t.Errorf("expected key = 192.168.1.4, got %s", entry.Key)
		}
		if entry.Reason != "test entry" {
			t.Errorf("expected reason = 'test entry', got %s", entry.Reason)
		}
	})

	t.Run("List", func(t *testing.T) {
		// Add some entries
		bl.Add("192.168.1.10", "ip", "list test 1")
		bl.Add("192.168.1.11", "ip", "list test 2")

		entries := bl.List()
		if len(entries) < 2 {
			t.Errorf("expected at least 2 entries, got %d", len(entries))
		}
	})

	t.Run("ListByType", func(t *testing.T) {
		bl.Add("192.168.1.20", "ip", "ip type")
		bl.Add("account:test@example.com", "account", "account type")

		ipEntries := bl.ListByType("ip")
		found := false
		for _, e := range ipEntries {
			if e.Key == "192.168.1.20" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find IP entry")
		}
	})

	t.Run("IsIPBlocked", func(t *testing.T) {
		bl.Add("192.168.1.30", "ip", "ip block test")

		if !bl.IsIPBlocked("192.168.1.30") {
			t.Error("expected IsIPBlocked to return true")
		}
	})

	t.Run("IsAccountBlocked", func(t *testing.T) {
		bl.Add("account:blocked@example.com", "account", "account block test")

		if !bl.IsAccountBlocked("blocked@example.com") {
			t.Error("expected IsAccountBlocked to return true")
		}
	})

	t.Run("GetStats", func(t *testing.T) {
		stats := bl.GetStats()

		if _, ok := stats["total"]; !ok {
			t.Error("expected 'total' in stats")
		}
		if _, ok := stats["ip_blocks"]; !ok {
			t.Error("expected 'ip_blocks' in stats")
		}
		if _, ok := stats["account_blocks"]; !ok {
			t.Error("expected 'account_blocks' in stats")
		}
	})

	t.Run("CleanupExpired", func(t *testing.T) {
		// Add temporary block with very short duration
		bl.AddTemporary("192.168.1.50", 1*time.Nanosecond, "expires immediately")

		// Wait a bit
		time.Sleep(10 * time.Millisecond)

		// Cleanup
		err := bl.Cleanup()
		if err != nil {
			t.Fatalf("Cleanup failed: %v", err)
		}

		// Should be expired and removed
		if bl.IsBlocked("192.168.1.50") {
			t.Error("expected expired block to be removed")
		}
	})
}

func TestIsIPInCIDR(t *testing.T) {
	tests := []struct {
		ip       string
		cidr     string
		expected bool
	}{
		{"192.168.1.1", "192.168.1.0/24", true},
		{"192.168.1.255", "192.168.1.0/24", true},
		{"192.168.2.1", "192.168.1.0/24", false},
		{"10.0.0.1", "10.0.0.0/8", true},
		{"10.255.255.255", "10.0.0.0/8", true},
		{"11.0.0.1", "10.0.0.0/8", false},
		{"invalid", "192.168.1.0/24", false},
		{"192.168.1.1", "invalid", false},
	}

	for _, tc := range tests {
		result := isIPInCIDR(tc.ip, tc.cidr)
		if result != tc.expected {
			t.Errorf("isIPInCIDR(%s, %s) = %v, want %v", tc.ip, tc.cidr, result, tc.expected)
		}
	}
}
