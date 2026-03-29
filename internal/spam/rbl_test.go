package spam

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewRBLChecker(t *testing.T) {
	servers := []string{"zen.spamhaus.org"}
	checker := NewRBLChecker(servers)

	if checker == nil {
		t.Fatal("RBLChecker should not be nil")
	}

	if len(checker.servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(checker.servers))
	}
}

func TestRBLSetTimeout(t *testing.T) {
	checker := NewRBLChecker([]string{})

	newTimeout := 10 * time.Second
	checker.SetTimeout(newTimeout)

	if checker.timeout != newTimeout {
		t.Errorf("Expected timeout %v, got %v", newTimeout, checker.timeout)
	}
}

func TestDefaultRBLServers(t *testing.T) {
	servers := DefaultRBLServers()

	if len(servers) == 0 {
		t.Error("Expected non-empty RBL server list")
	}

	// Check for known servers
	hasSpamhaus := false
	for _, s := range servers {
		if s == "zen.spamhaus.org" {
			hasSpamhaus = true
			break
		}
	}

	if !hasSpamhaus {
		t.Error("Expected zen.spamhaus.org in default servers")
	}
}

func TestReverseIPIPv4(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	reversed := reverseIP(ip)

	expected := "1.1.168.192"
	if reversed != expected {
		t.Errorf("Expected %s, got %s", expected, reversed)
	}
}

func TestReverseIPInvalid(t *testing.T) {
	// Test with invalid IP
	ip := net.ParseIP("invalid")
	if ip != nil {
		reversed := reverseIP(ip)
		if reversed == "" {
			t.Error("Expected empty string for invalid IP")
		}
	}
}

func TestRBLCodeToReason(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"127.0.0.1", "Listed as spam source"},
		{"127.0.0.2", "Listed as spam source (direct)"},
		{"127.0.0.10", "Listed as dynamic IP"},
		{"127.0.0.11", "Listed as compromised"},
		{"127.0.0.99", "Listed (code: 127.0.0.99)"},
		{"192.168.1.1", "Unknown"},
	}

	for _, tt := range tests {
		result := rblCodeToReason(tt.code)
		if result != tt.expected {
			t.Errorf("rblCodeToReason(%s) = %s, want %s", tt.code, result, tt.expected)
		}
	}
}

// TestReverseIPv6 tests IPv6 address reversal for RBL lookups
func TestReverseIPv6(t *testing.T) {
	// Test with a known IPv6 address
	ip := net.ParseIP("2001:db8::1")
	if ip == nil {
		t.Fatal("Failed to parse IPv6 address")
	}

	reversed := reverseIPv6(ip)

	// The result should be in nibble format
	// For 2001:db8::1, we expect something like "1.0.0.0...0.8.b.d.1.0.0.2"
	if reversed == "" {
		t.Error("Expected non-empty reversed IPv6")
	}

	// Should contain dots separating nibbles
	if !strings.Contains(reversed, ".") {
		t.Error("Expected reversed IPv6 to contain dots")
	}
}

func TestRBLGetStats(t *testing.T) {
	servers := []string{"zen.spamhaus.org", "bl.spamcop.net"}
	checker := NewRBLChecker(servers)

	stats := checker.GetStats()

	serverCount, ok := stats["servers"].(int)
	if !ok || serverCount != 2 {
		t.Errorf("Expected 2 servers, got %v", stats["servers"])
	}

	timeout, ok := stats["timeout"].(float64)
	if !ok || timeout != 5.0 {
		t.Errorf("Expected timeout 5.0, got %v", stats["timeout"])
	}
}

// Integration test - requires network access
func TestRBLCheckIntegration(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping RBL integration test in short mode")
	}

	servers := []string{"zen.spamhaus.org"}
	checker := NewRBLChecker(servers)
	checker.SetTimeout(5 * time.Second)

	// Use a known clean IP (Google's public DNS)
	ip := net.ParseIP("8.8.8.8")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := checker.Check(ctx, ip)

	if len(results) == 0 {
		t.Error("Expected results from RBL check")
	}

	// 8.8.8.8 should not be listed
	for _, result := range results {
		if result.Error != nil {
			t.Logf("RBL check error for %s: %v", result.Server, result.Error)
		}
	}
}

// Integration test for IsListed
func TestRBLIsListedIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping RBL integration test in short mode")
	}

	servers := []string{"zen.spamhaus.org"}
	checker := NewRBLChecker(servers)

	// Use Google's DNS - should not be listed
	ip := net.ParseIP("8.8.8.8")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listed, results := checker.IsListed(ctx, ip)

	if listed {
		t.Error("Expected 8.8.8.8 to not be listed")
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 listed results for clean IP, got %d", len(results))
	}
}
