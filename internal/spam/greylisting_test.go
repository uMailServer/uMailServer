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
