package auth

import (
	"testing"
	"time"
)

// TestDMARCCache_Set_NewEntry tests setting a new entry in the DMARC cache
func TestDMARCCache_Set_NewEntry(t *testing.T) {
	cache := newDMARCCache()

	record := &DMARCRecord{
		Version: "DMARC1",
		Policy:  DMARCPolicyReject,
	}

	cache.set("example.com", record)

	retrieved, found := cache.get("example.com")
	if !found {
		t.Error("expected to find cached entry")
	}
	if retrieved.Policy != DMARCPolicyReject {
		t.Errorf("expected policy reject, got %v", retrieved.Policy)
	}
}

// TestDMARCCache_Set_UpdateEntry tests updating an existing entry
func TestDMARCCache_Set_UpdateEntry(t *testing.T) {
	cache := newDMARCCache()

	record1 := &DMARCRecord{
		Version: "DMARC1",
		Policy:  DMARCPolicyReject,
	}
	record2 := &DMARCRecord{
		Version: "DMARC1",
		Policy:  DMARCPolicyQuarantine,
	}

	cache.set("example.com", record1)
	cache.set("example.com", record2)

	retrieved, found := cache.get("example.com")
	if !found {
		t.Error("expected to find cached entry")
	}
	if retrieved.Policy != DMARCPolicyQuarantine {
		t.Error("expected updated policy")
	}
}

// TestDMARCCache_Get_NotFound tests getting a non-existent entry
func TestDMARCCache_Get_NotFound(t *testing.T) {
	cache := newDMARCCache()

	_, found := cache.get("nonexistent.com")
	if found {
		t.Error("expected not to find entry")
	}
}

// TestDMARCCache_Get_Expired tests getting an expired entry
func TestDMARCCache_Get_Expired(t *testing.T) {
	cache := newDMARCCache()
	cache.ttl = time.Millisecond

	record := &DMARCRecord{
		Version: "DMARC1",
		Policy:  DMARCPolicyReject,
	}

	cache.set("example.com", record)

	// Wait for expiration
	time.Sleep(2 * time.Millisecond)

	_, found := cache.get("example.com")
	if found {
		t.Error("expected expired entry to not be found")
	}
}

// TestDMARCCache_EvictOldest_ExpiredEntries tests evicting expired entries
func TestDMARCCache_EvictOldest_ExpiredEntries(t *testing.T) {
	cache := newDMARCCache()
	cache.ttl = time.Millisecond

	record := &DMARCRecord{
		Version: "DMARC1",
		Policy:  DMARCPolicyReject,
	}

	// Add expired entry
	cache.set("example1.com", record)
	time.Sleep(2 * time.Millisecond)

	// Add non-expired entry
	cache.set("example2.com", record)

	// Manually trigger eviction
	cache.evictOldest()

	// Expired entry should be gone
	_, found := cache.get("example1.com")
	if found {
		t.Error("expected expired entry to be evicted")
	}

	// Non-expired entry should remain
	_, found = cache.get("example2.com")
	if !found {
		t.Error("expected non-expired entry to remain")
	}
}

// TestDMARCCache_EvictOldest_AtCapacity tests eviction when at capacity
func TestDMARCCache_EvictOldest_AtCapacity(t *testing.T) {
	cache := newDMARCCache()
	cache.maxSize = 2 // Small capacity

	record := &DMARCRecord{
		Version: "DMARC1",
		Policy:  DMARCPolicyReject,
	}

	// Fill cache to capacity
	cache.set("example1.com", record)
	cache.set("example2.com", record)

	// Add one more (should trigger eviction)
	cache.set("example3.com", record)

	// Cache should still be at capacity
	if len(cache.entries) > cache.maxSize {
		t.Errorf("cache exceeded max size: %d > %d", len(cache.entries), cache.maxSize)
	}
}

// TestNewDMARCCache tests creating a new DMARC cache
func TestNewDMARCCache(t *testing.T) {
	cache := newDMARCCache()

	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
	if cache.entries == nil {
		t.Error("expected entries map to be initialized")
	}
	if cache.maxSize != 10000 {
		t.Errorf("expected maxSize 10000, got %d", cache.maxSize)
	}
	if cache.ttl != 5*time.Minute {
		t.Errorf("expected ttl 5m, got %v", cache.ttl)
	}
}
