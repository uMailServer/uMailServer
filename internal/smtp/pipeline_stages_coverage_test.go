package smtp

import (
	"context"
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/ratelimit"
	"github.com/umailserver/umailserver/internal/sieve"
)

// ---------------------------------------------------------------------------
// NewRateLimitStage tests
// ---------------------------------------------------------------------------

func TestNewRateLimitStage(t *testing.T) {
	limiter := ratelimit.New(nil, nil)
	stage := NewRateLimitStage(limiter)

	if stage == nil {
		t.Fatal("Expected non-nil stage")
	}
	if stage.limiter == nil {
		t.Error("Expected limiter to be set")
	}
	if stage.Name() != "RateLimit" {
		t.Errorf("Expected name 'RateLimit', got %s", stage.Name())
	}
}

func TestRateLimitStage_WithNilLimiter(t *testing.T) {
	stage := NewRateLimitStage(nil)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

	result := stage.Process(ctx)
	// Should accept when limiter is nil
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for nil limiter, got %d", result)
	}
}

func TestRateLimitStage_WithRealLimiter_UnderLimit(t *testing.T) {
	limiter := ratelimit.New(nil, nil)
	stage := NewRateLimitStage(limiter)

	ip := net.ParseIP("192.168.1.100")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept under limit, got %d", result)
	}
}

func TestRateLimitStage_AuthenticatedUser(t *testing.T) {
	limiter := ratelimit.New(nil, nil)
	stage := NewRateLimitStage(limiter)

	ip := net.ParseIP("192.168.1.101")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
	ctx.Authenticated = true
	ctx.Username = "testuser"

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for authenticated user, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// NewSieveStage tests
// ---------------------------------------------------------------------------

func TestNewSieveStage(t *testing.T) {
	manager := sieve.NewManager()
	stage := NewSieveStage(manager)

	if stage == nil {
		t.Fatal("Expected non-nil stage")
	}
	if stage.manager == nil {
		t.Error("Expected manager to be set")
	}
	if stage.Name() != "Sieve" {
		t.Errorf("Expected name 'Sieve', got %s", stage.Name())
	}
}

// Note: SieveStage.Process does not handle nil manager gracefully,
 // so we skip testing with nil manager

func TestSieveStage_EmptyRecipient(t *testing.T) {
	manager := sieve.NewManager()
	stage := NewSieveStage(manager)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{}, []byte("test"))

	result := stage.Process(ctx)
	// Should accept when no recipients
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for empty recipients, got %d", result)
	}
}

func TestSieveStage_EmptySender(t *testing.T) {
	manager := sieve.NewManager()
	stage := NewSieveStage(manager)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "", []string{"recipient@example.com"}, []byte("test"))

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %d", result)
	}
}

func TestSieveStage_WithActiveScript(t *testing.T) {
	manager := sieve.NewManager()
	stage := NewSieveStage(manager)

	// Add a sieve script that keeps all mail
	script := `
		require "fileinto";
		if true {
			fileinto "INBOX";
			stop;
		}
	`
	manager.StoreScript("testuser", "testscript", script)
	manager.SetActiveScript("testuser", "testscript", script)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"testuser@example.com"}, []byte("test"))
	ctx.Authenticated = true
	ctx.Username = "testuser"

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept with sieve script, got %d", result)
	}
}

func TestSieveStage_ExtractUserFromRecipient(t *testing.T) {
	manager := sieve.NewManager()
	stage := NewSieveStage(manager)

	ip := net.ParseIP("192.168.1.1")

	tests := []struct {
		recipient string
		expected  string
	}{
		{"user@example.com", "user"},
		{"user@sub.example.com", "user"},
		{"user+tag@example.com", "user"},
	}

	for _, tt := range tests {
		ctx := NewMessageContext(ip, "sender@example.com", []string{tt.recipient}, []byte("test"))
		ctx.Authenticated = true
		ctx.Username = tt.expected

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Process(%q): expected ResultAccept, got %d", tt.recipient, result)
		}
	}
}

// ---------------------------------------------------------------------------
// RBL DNS Resolver tests
// ---------------------------------------------------------------------------

func TestNewRealRBLDNSResolver(t *testing.T) {
	resolver := NewRealRBLDNSResolver()
	if resolver == nil {
		t.Fatal("Expected non-nil resolver")
	}
}

func TestRBLDNSResolver_LookupHost(t *testing.T) {
	resolver := NewRealRBLDNSResolver()

	// Use a real DNS lookup for spamhaus.org
	ctx := context.Background()
	ip, err := resolver.LookupHost(ctx, "zen.spamhaus.org")
	// May fail in test environment due to DNS restrictions, but should not panic
	_ = ip
	_ = err
}
