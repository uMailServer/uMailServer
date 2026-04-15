package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rsa"
	"math/big"
	"testing"
	"time"
)

func TestDKIMCache_Set_NewEntry(t *testing.T) {
	cache := newDKIMCache()

	rsaKey := &rsa.PublicKey{N: big.NewInt(12345), E: 65537}
	edKey := ed25519.PublicKey(make([]byte, 32))

	cache.set("selector1", "example.com", rsaKey, edKey)

	retrievedRSA, retrievedED, found := cache.get("selector1", "example.com")
	if !found {
		t.Error("expected to find cached entry")
	}
	if retrievedRSA != rsaKey {
		t.Error("retrieved RSA key doesn't match")
	}
	if !bytes.Equal(retrievedED, edKey) {
		t.Error("retrieved ED25519 key doesn't match")
	}
}

// TestDKIMCache_Set_UpdateEntry tests updating an existing entry
func TestDKIMCache_Set_UpdateEntry(t *testing.T) {
	cache := newDKIMCache()

	rsaKey1 := &rsa.PublicKey{N: big.NewInt(12345), E: 65537}
	edKey1 := ed25519.PublicKey(make([]byte, 32))
	rsaKey2 := &rsa.PublicKey{N: big.NewInt(67890), E: 65537}
	edKey2 := ed25519.PublicKey(make([]byte, 32))

	cache.set("selector1", "example.com", rsaKey1, edKey1)
	cache.set("selector1", "example.com", rsaKey2, edKey2)

	retrievedRSA, _, found := cache.get("selector1", "example.com")
	if !found {
		t.Error("expected to find cached entry")
	}
	if retrievedRSA != rsaKey2 {
		t.Error("expected updated RSA key")
	}
}

// TestDKIMCache_Set_EvictExpired tests that expired entries are evicted when setting new entries
func TestDKIMCache_Set_EvictExpired(t *testing.T) {
	cache := newDKIMCache()
	cache.ttl = time.Millisecond // Very short TTL

	rsaKey := &rsa.PublicKey{N: big.NewInt(12345), E: 65537}
	edKey := ed25519.PublicKey(make([]byte, 32))

	// Add entry with very short TTL
	cache.set("selector1", "example.com", rsaKey, edKey)

	// Wait for expiration
	time.Sleep(2 * time.Millisecond)

	// Add another entry (this should trigger eviction of expired entry)
	cache.set("selector2", "example.com", rsaKey, edKey)

	// First entry should be gone
	_, _, found := cache.get("selector1", "example.com")
	if found {
		t.Error("expected expired entry to be evicted")
	}
}

// TestDKIMCache_EvictOldest_ExpiredEntries tests evicting expired entries
func TestDKIMCache_EvictOldest_ExpiredEntries(t *testing.T) {
	cache := newDKIMCache()
	cache.ttl = time.Millisecond

	rsaKey := &rsa.PublicKey{N: big.NewInt(12345), E: 65537}
	edKey := ed25519.PublicKey(make([]byte, 32))

	// Add expired entry
	cache.set("selector1", "example.com", rsaKey, edKey)
	time.Sleep(2 * time.Millisecond)

	// Add non-expired entry
	cache.set("selector2", "example.com", rsaKey, edKey)

	// Manually trigger eviction
	cache.evictOldest()

	// Expired entry should be gone
	_, _, found := cache.get("selector1", "example.com")
	if found {
		t.Error("expected expired entry to be evicted")
	}

	// Non-expired entry should remain
	_, _, found = cache.get("selector2", "example.com")
	if !found {
		t.Error("expected non-expired entry to remain")
	}
}

// TestDKIMCache_EvictOldest_AtCapacity tests eviction when at capacity
func TestDKIMCache_EvictOldest_AtCapacity(t *testing.T) {
	cache := newDKIMCache()
	cache.maxSize = 2 // Small capacity

	rsaKey := &rsa.PublicKey{N: big.NewInt(12345), E: 65537}
	edKey := ed25519.PublicKey(make([]byte, 32))

	// Fill cache to capacity
	cache.set("selector1", "example.com", rsaKey, edKey)
	cache.set("selector2", "example.com", rsaKey, edKey)

	// Add one more (should trigger eviction)
	cache.set("selector3", "example.com", rsaKey, edKey)

	// Cache should still be at capacity
	if len(cache.entries) > cache.maxSize {
		t.Errorf("cache exceeded max size: %d > %d", len(cache.entries), cache.maxSize)
	}
}

// TestDKIMCache_Get_NotFound tests getting a non-existent entry
func TestDKIMCache_Get_NotFound(t *testing.T) {
	cache := newDKIMCache()

	_, _, found := cache.get("nonexistent", "example.com")
	if found {
		t.Error("expected not to find entry")
	}
}

// TestDKIMCache_Get_Expired tests getting an expired entry
func TestDKIMCache_Get_Expired(t *testing.T) {
	cache := newDKIMCache()
	cache.ttl = time.Millisecond

	rsaKey := &rsa.PublicKey{N: big.NewInt(12345), E: 65537}
	edKey := ed25519.PublicKey(make([]byte, 32))

	cache.set("selector1", "example.com", rsaKey, edKey)

	// Wait for expiration
	time.Sleep(2 * time.Millisecond)

	_, _, found := cache.get("selector1", "example.com")
	if found {
		t.Error("expected expired entry to not be found")
	}
}

// TestNewDKIMCache tests creating a new cache
func TestNewDKIMCache(t *testing.T) {
	cache := newDKIMCache()

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
