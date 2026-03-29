package spam

import (
	"net"
	"testing"
	"time"
)

func TestDefaultGreylistConfig(t *testing.T) {
	cfg := DefaultGreylistConfig()

	if !cfg.Enabled {
		t.Error("Expected enabled to be true")
	}

	if cfg.Delay != 5*time.Minute {
		t.Errorf("Expected delay 5m, got %v", cfg.Delay)
	}

	if cfg.WhitelistPass != 5 {
		t.Errorf("Expected whitelist pass 5, got %d", cfg.WhitelistPass)
	}
}

func TestNewGreylisting(t *testing.T) {
	cfg := DefaultGreylistConfig()
	g := NewGreylisting(cfg)
	defer g.Close()

	if g == nil {
		t.Fatal("Greylisting should not be nil")
	}

	if g.triplets == nil {
		t.Error("triplets should be initialized")
	}
}

func TestGreylistingCheckFirstTime(t *testing.T) {
	cfg := DefaultGreylistConfig()
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// First check should be greylisted
	allowed, retryAfter, err := g.Check(ip, sender, recipient)

	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if allowed {
		t.Error("Expected not allowed on first check")
	}

	if retryAfter == 0 {
		t.Error("Expected retryAfter > 0")
	}
}

func TestGreylistingCheckDisabled(t *testing.T) {
	cfg := GreylistConfig{
		Enabled: false,
		Delay:   5 * time.Minute,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// When disabled, should always allow
	allowed, retryAfter, err := g.Check(ip, sender, recipient)

	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if !allowed {
		t.Error("Expected allowed when disabled")
	}

	if retryAfter != 0 {
		t.Errorf("Expected retryAfter 0, got %v", retryAfter)
	}
}

func TestGreylistingWhitelist(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       true,
		Delay:         0, // No delay for testing
		WhitelistPass: 2,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// First check - not whitelisted
	whitelisted := g.IsWhitelisted(ip, sender, recipient)
	if whitelisted {
		t.Error("Expected not whitelisted on first check")
	}

	// Simulate passing the greylist
	g.Check(ip, sender, recipient)

	// Should still not be whitelisted (need multiple passes)
	whitelisted = g.IsWhitelisted(ip, sender, recipient)
	if whitelisted {
		t.Error("Expected not whitelisted after first pass")
	}
}

func TestGreylistingGetStats(t *testing.T) {
	cfg := DefaultGreylistConfig()
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	g.Check(ip, "sender1@example.com", "recipient@example.com")
	g.Check(ip, "sender2@example.com", "recipient@example.com")

	stats := g.GetStats()

	totalTriplets, ok := stats["total_triplets"].(int)
	if !ok || totalTriplets != 2 {
		t.Errorf("Expected 2 triplets, got %v", stats["total_triplets"])
	}
}

func TestGreylistingReset(t *testing.T) {
	cfg := DefaultGreylistConfig()
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	g.Check(ip, "sender@example.com", "recipient@example.com")

	g.Reset()

	stats := g.GetStats()
	totalTriplets := stats["total_triplets"].(int)

	if totalTriplets != 0 {
		t.Errorf("Expected 0 triplets after reset, got %d", totalTriplets)
	}
}

func TestMakeTriplet(t *testing.T) {
	cfg := DefaultGreylistConfig()
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "Sender@Example.COM"
	recipient := "Recipient@Example.COM"

	triplet := g.makeTriplet(ip, sender, recipient)

	// Should be lowercase and contain IP
	expected := "192.168.1.1|sender@example.com|recipient@example.com"
	if triplet != expected {
		t.Errorf("Expected triplet %s, got %s", expected, triplet)
	}
}

func TestMakeTripletIPv6(t *testing.T) {
	cfg := DefaultGreylistConfig()
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("2001:db8::1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	triplet := g.makeTriplet(ip, sender, recipient)

	// Should contain the IPv6 address
	if triplet == "" {
		t.Error("Expected non-empty triplet for IPv6")
	}
}

// TestDoCleanup tests the cleanup of expired greylist entries
func TestDoCleanup(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       true,
		Delay:         5 * time.Minute,
		Expiry:        100 * time.Millisecond, // Short expiry for testing
		WhitelistPass: 5,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// Create a greylist entry
	g.Check(ip, sender, recipient)

	// Should have 1 entry
	stats := g.GetStats()
	if stats["total_triplets"] != 1 {
		t.Errorf("Expected 1 triplet, got %v", stats["total_triplets"])
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Run cleanup
	g.doCleanup()

	// Should have 0 entries after cleanup
	stats = g.GetStats()
	if stats["total_triplets"] != 0 {
		t.Errorf("Expected 0 triplets after cleanup, got %v", stats["total_triplets"])
	}
}

// TestGreylistingRetryAfterDelay tests greylist retry after delay has passed
func TestGreylistingRetryAfterDelay(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       true,
		Delay:         100 * time.Millisecond,
		Expiry:        1 * time.Hour,
		WhitelistPass: 5,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// First check - greylisted
	allowed, retryAfter, err := g.Check(ip, sender, recipient)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if allowed {
		t.Error("Expected not allowed on first check")
	}
	if retryAfter == 0 {
		t.Error("Expected retryAfter > 0")
	}

	// Wait for the delay to pass
	time.Sleep(150 * time.Millisecond)

	// Second check - should be allowed now
	allowed, retryAfter, err = g.Check(ip, sender, recipient)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !allowed {
		t.Error("Expected allowed after delay")
	}
	if retryAfter != 0 {
		t.Errorf("Expected retryAfter 0, got %v", retryAfter)
	}
}

// TestGreylistingMultipleAttempts tests multiple greylist attempts
func TestGreylistingMultipleAttempts(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       true,
		Delay:         500 * time.Millisecond,
		Expiry:        1 * time.Hour,
		WhitelistPass: 5,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// Multiple checks before delay passes - should remain greylisted
	for i := 0; i < 3; i++ {
		allowed, _, err := g.Check(ip, sender, recipient)
		if err != nil {
			t.Fatalf("Check failed: %v", err)
		}
		if allowed {
			t.Errorf("Expected not allowed on attempt %d", i+1)
		}
	}

	// Verify count was incremented
	g.mu.RLock()
	triplet := g.makeTriplet(ip, sender, recipient)
	entry := g.triplets[triplet]
	g.mu.RUnlock()

	if entry == nil {
		t.Fatal("Expected entry to exist")
	}
	if entry.Count != 3 {
		t.Errorf("Expected count 3, got %d", entry.Count)
	}
}

// TestGreylistingIsWhitelistedDisabled tests IsWhitelisted when greylisting is disabled
func TestGreylistingIsWhitelistedDisabled(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       false,
		Delay:         5 * time.Minute,
		WhitelistPass: 5,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// When disabled, should always return false
	whitelisted := g.IsWhitelisted(ip, sender, recipient)
	if whitelisted {
		t.Error("Expected not whitelisted when disabled")
	}
}

// TestGreylistingIsWhitelistedTrue tests IsWhitelisted when entry is whitelisted
func TestGreylistingIsWhitelistedTrue(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       true,
		Delay:         0, // No delay
		Expiry:        1 * time.Hour,
		WhitelistPass: 1,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// First check - creates entry
	g.Check(ip, sender, recipient)

	// Manually set Passed=true and Count >= WhitelistPass
	triplet := g.makeTriplet(ip, sender, recipient)
	g.mu.Lock()
	if entry, exists := g.triplets[triplet]; exists {
		entry.Passed = true
		entry.Count = 2 // Ensure >= WhitelistPass
	}
	g.mu.Unlock()

	whitelisted := g.IsWhitelisted(ip, sender, recipient)
	if !whitelisted {
		t.Error("Expected whitelisted after setting Passed=true and sufficient count")
	}
}

// TestGreylistingCleanupKeepsWhitelisted tests that cleanup preserves whitelisted entries
func TestGreylistingCleanupKeepsWhitelisted(t *testing.T) {
	cfg := GreylistConfig{
		Enabled:       true,
		Delay:         0,
		Expiry:        100 * time.Millisecond,
		WhitelistPass: 1,
	}
	g := NewGreylisting(cfg)
	defer g.Close()

	ip := net.ParseIP("192.168.1.1")
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// Create entry
	g.Check(ip, sender, recipient)

	// Manually set Passed=true to make it whitelisted
	triplet := g.makeTriplet(ip, sender, recipient)
	g.mu.Lock()
	if entry, exists := g.triplets[triplet]; exists {
		entry.Passed = true
	}
	g.mu.Unlock()

	// Wait past normal expiry time but not past extended expiry (7x)
	time.Sleep(150 * time.Millisecond)

	// Run cleanup
	g.doCleanup()

	// Whitelisted entry should still exist (extended expiry = 700ms)
	stats := g.GetStats()
	if stats["total_triplets"] != 1 {
		t.Errorf("Expected 1 triplet after cleanup (whitelisted), got %v", stats["total_triplets"])
	}
}
