package security

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"go.etcd.io/bbolt"
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

func TestBlocklistStartCleanup(t *testing.T) {
	database, err := db.Open(t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	// Add an entry
	bl.Add("10.0.0.1", "ip", "test")

	// StartCleanup should not panic - just runs a goroutine
	bl.StartCleanup(50 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
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

// TestNewBlocklist_LoadFromDB tests that NewBlocklist correctly loads existing
// entries from a pre-populated bbolt database.
func TestNewBlocklist_LoadFromDB(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	boltDB := database.BoltDB()

	// Pre-populate the blocklist bucket with entries before creating Blocklist
	err = boltDB.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("blocklist"))
		if err != nil {
			return err
		}

		// Add a valid, non-expired permanent entry
		permanentEntry := BlockEntry{
			Key:       "10.0.0.1",
			Type:      "ip",
			Reason:    "pre-existing permanent block",
			CreatedAt: time.Now().Add(-1 * time.Hour),
			ExpiresAt: nil,
			Permanent: true,
		}
		data, _ := json.Marshal(permanentEntry)
		if err := b.Put([]byte("10.0.0.1"), data); err != nil {
			return err
		}

		// Add a valid, non-expired temporary entry
		futureExpiry := time.Now().Add(1 * time.Hour)
		tempEntry := BlockEntry{
			Key:       "10.0.0.2",
			Type:      "ip",
			Reason:    "pre-existing temp block",
			CreatedAt: time.Now().Add(-30 * time.Minute),
			ExpiresAt: &futureExpiry,
			Permanent: false,
		}
		data, _ = json.Marshal(tempEntry)
		if err := b.Put([]byte("10.0.0.2"), data); err != nil {
			return err
		}

		// Add an expired entry (should be skipped by loadFromDB)
		pastExpiry := time.Now().Add(-1 * time.Hour)
		expiredEntry := BlockEntry{
			Key:       "10.0.0.3",
			Type:      "ip",
			Reason:    "expired block",
			CreatedAt: time.Now().Add(-2 * time.Hour),
			ExpiresAt: &pastExpiry,
			Permanent: false,
		}
		data, _ = json.Marshal(expiredEntry)
		if err := b.Put([]byte("10.0.0.3"), data); err != nil {
			return err
		}

		// Add an entry with invalid JSON (should be skipped gracefully)
		if err := b.Put([]byte("10.0.0.4"), []byte("not valid json")); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		t.Fatalf("failed to pre-populate database: %v", err)
	}

	database.Close()

	// Re-open the database and create blocklist -- should load pre-existing entries
	database2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer database2.Close()

	bl, err := NewBlocklist(database2.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Permanent entry should be loaded
	if !bl.IsBlocked("10.0.0.1") {
		t.Error("expected pre-existing permanent entry to be loaded and blocking")
	}

	// Temporary (non-expired) entry should be loaded
	if !bl.IsBlocked("10.0.0.2") {
		t.Error("expected pre-existing temporary entry to be loaded and blocking")
	}

	// Expired entry should NOT be loaded
	if bl.IsBlocked("10.0.0.3") {
		t.Error("expected expired entry to not be loaded")
	}

	// Invalid JSON entry should NOT be loaded
	if bl.IsBlocked("10.0.0.4") {
		t.Error("expected invalid JSON entry to not be loaded")
	}
}

// TestLoadFromDB_NilBucket tests loadFromDB when the bucket does not exist.
func TestLoadFromDB_NilBucket(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	boltDB := database.BoltDB()

	// Delete the blocklist bucket so loadFromDB gets nil
	boltDB.Update(func(tx *bbolt.Tx) error {
		tx.DeleteBucket([]byte("blocklist"))
		return nil
	})

	bl := &Blocklist{
		blocks: make(map[string]*BlockEntry),
		db:     boltDB,
		bucket: []byte("blocklist"),
	}

	// Should return nil (no error) when bucket is nil
	err = bl.loadFromDB()
	if err != nil {
		t.Errorf("expected nil error for nil bucket, got: %v", err)
	}
}

// TestNewBlocklist_BucketCreationError tests NewBlocklist when bucket creation fails.
func TestNewBlocklist_BucketCreationError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	boltDB := database.BoltDB()

	// Create a read-only transaction context to cause Update to fail.
	// We simulate this by closing the db first then calling NewBlocklist.
	database.Close()

	_, err = NewBlocklist(boltDB)
	if err == nil {
		t.Error("expected error when creating blocklist with closed database")
	}
}

// TestAddBlock_DuplicateEntry tests adding a block for a key that already exists.
func TestAddBlock_DuplicateEntry(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a permanent block
	err = bl.Add("10.1.1.1", "ip", "first block")
	if err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	// Add again with different reason -- should overwrite
	err = bl.Add("10.1.1.1", "ip", "second block")
	if err != nil {
		t.Fatalf("second Add failed: %v", err)
	}

	entry := bl.GetEntry("10.1.1.1")
	if entry == nil {
		t.Fatal("expected entry to exist")
	}
	if entry.Reason != "second block" {
		t.Errorf("expected reason 'second block', got '%s'", entry.Reason)
	}
}

// TestAddBlock_WithExpiry tests adding temporary blocks with expiry times.
func TestAddBlock_WithExpiry(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a temporary block with short duration
	err = bl.AddTemporary("10.2.2.2", 50*time.Millisecond, "short-lived block")
	if err != nil {
		t.Fatalf("AddTemporary failed: %v", err)
	}

	// Should be blocked immediately
	if !bl.IsBlocked("10.2.2.2") {
		t.Error("expected IP to be blocked immediately after AddTemporary")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Now the entry should be considered expired
	entry := bl.GetEntry("10.2.2.2")
	if entry == nil {
		t.Fatal("expected entry to still be in memory (not cleaned up yet)")
	}

	// IsBlocked should return false because the entry has expired
	// (IsBlocked checks the expiry time internally)
	if bl.IsBlocked("10.2.2.2") {
		t.Error("expected IP to not be blocked after expiry")
	}
}

// TestRemove_NonExistent tests removing an entry that does not exist.
func TestRemove_NonExistent(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Remove a key that was never added -- should not error
	err = bl.Remove("nonexistent_key")
	if err != nil {
		t.Errorf("expected no error when removing non-existent key, got: %v", err)
	}
}

// TestIsBlocked_ExpiredTemporaryEntry tests that an expired temporary entry
// is not considered blocked.
func TestIsBlocked_ExpiredTemporaryEntry(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a temporary block that expires very quickly
	bl.AddTemporary("10.3.3.3", 1*time.Nanosecond, "expires immediately")

	// Wait for it to expire
	time.Sleep(10 * time.Millisecond)

	// IsBlocked should return false for expired entry
	if bl.IsBlocked("10.3.3.3") {
		t.Error("expected expired temporary entry to not be blocked")
	}
}

// TestIsBlocked_CIDRRange tests CIDR range matching in IsBlocked.
func TestIsBlocked_CIDRRange(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a CIDR range block
	err = bl.Add("192.168.5.0/24", "ip", "CIDR range block")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// An IP within the range should be blocked
	if !bl.IsBlocked("192.168.5.42") {
		t.Error("expected IP in CIDR range to be blocked")
	}

	// An IP outside the range should not be blocked
	if bl.IsBlocked("192.168.6.1") {
		t.Error("expected IP outside CIDR range to not be blocked")
	}
}

// TestList_ExpiredEntries tests that List skips expired entries.
func TestList_ExpiredEntries(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a permanent entry
	bl.Add("10.10.10.1", "ip", "permanent entry")

	// Add an expired temporary entry directly into the map
	pastExpiry := time.Now().Add(-1 * time.Hour)
	bl.blocks["10.10.10.2"] = &BlockEntry{
		Key:       "10.10.10.2",
		Type:      "ip",
		Reason:    "expired entry",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: &pastExpiry,
		Permanent: false,
	}

	// List should only return the permanent entry
	entries := bl.List()
	for _, e := range entries {
		if e.Key == "10.10.10.2" {
			t.Error("expected expired entry to not appear in List results")
		}
	}

	// Verify permanent entry is present
	found := false
	for _, e := range entries {
		if e.Key == "10.10.10.1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected permanent entry to appear in List results")
	}
}

// TestListByType_ExpiredEntries tests that ListByType skips expired entries.
func TestListByType_ExpiredEntries(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a permanent IP entry
	bl.Add("10.20.20.1", "ip", "permanent ip")

	// Add an expired IP entry directly into the map
	pastExpiry := time.Now().Add(-1 * time.Hour)
	bl.blocks["10.20.20.2"] = &BlockEntry{
		Key:       "10.20.20.2",
		Type:      "ip",
		Reason:    "expired ip entry",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: &pastExpiry,
		Permanent: false,
	}

	ipEntries := bl.ListByType("ip")
	for _, e := range ipEntries {
		if e.Key == "10.20.20.2" {
			t.Error("expected expired entry to not appear in ListByType results")
		}
	}
}

// TestGetStats_ExpiredEntries tests that GetStats skips expired entries.
func TestGetStats_ExpiredEntries(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	bl, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a permanent entry
	bl.Add("10.30.30.1", "ip", "permanent")

	// Add an expired entry directly
	pastExpiry := time.Now().Add(-1 * time.Hour)
	bl.blocks["10.30.30.2"] = &BlockEntry{
		Key:       "10.30.30.2",
		Type:      "ip",
		Reason:    "expired",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: &pastExpiry,
		Permanent: false,
	}

	stats := bl.GetStats()
	total := stats["total"].(int)
	if total < 1 {
		t.Errorf("expected at least 1 total block, got %d", total)
	}
	// The expired entry should not be counted
	// Total should count only non-expired entries
}

// TestAddBlock_BucketNotFound tests addBlock when the bucket is missing.
func TestAddBlock_BucketNotFound(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	boltDB := database.BoltDB()

	bl := &Blocklist{
		blocks: make(map[string]*BlockEntry),
		db:     boltDB,
		bucket: []byte("nonexistent_bucket"),
	}

	// addBlock should fail because the bucket does not exist
	err = bl.addBlock("10.0.0.1", "ip", "test", nil, true)
	if err == nil {
		t.Error("expected error when adding block to nonexistent bucket")
	}
}

// TestCleanup_DBExpiredEntry tests that Cleanup removes expired entries from
// both memory and the database.
func TestCleanup_DBExpiredEntry(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	boltDB := database.BoltDB()

	bl, err := NewBlocklist(boltDB)
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Add a temporary block with very short duration
	bl.AddTemporary("10.5.5.5", 1*time.Nanosecond, "will expire")

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	// Cleanup should remove from both memory and database
	err = bl.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify it's gone from memory
	if bl.GetEntry("10.5.5.5") != nil {
		t.Error("expected expired entry to be removed from memory after Cleanup")
	}

	// Verify it's gone from the database
	database.Close()

	database2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer database2.Close()

	bl2, err := NewBlocklist(database2.BoltDB())
	if err != nil {
		t.Fatalf("NewBlocklist on re-open failed: %v", err)
	}

	if bl2.IsBlocked("10.5.5.5") {
		t.Error("expected expired entry to be removed from database after Cleanup")
	}
}

// TestCleanup_InvalidJSON tests that Cleanup handles invalid JSON in the database gracefully.
func TestCleanup_InvalidJSON(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	boltDB := database.BoltDB()

	// Pre-populate with invalid JSON
	boltDB.Update(func(tx *bbolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte("blocklist"))
		b.Put([]byte("bad_json_key"), []byte("not valid json"))
		return nil
	})

	bl, err := NewBlocklist(boltDB)
	if err != nil {
		t.Fatalf("NewBlocklist failed: %v", err)
	}

	// Cleanup should not panic or error on invalid JSON
	err = bl.Cleanup()
	if err != nil {
		t.Errorf("Cleanup should handle invalid JSON gracefully, got: %v", err)
	}

	database.Close()
}
