package smtp

import (
	"context"
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/ratelimit"
	"github.com/umailserver/umailserver/internal/sieve"
	"github.com/umailserver/umailserver/internal/spam"
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

// ---------------------------------------------------------------------------
// BayesianStage tests
// ---------------------------------------------------------------------------

func TestNewBayesianStage(t *testing.T) {
	classifier := spam.NewClassifier(nil)
	stage := NewBayesianStage(classifier)

	if stage == nil {
		t.Fatal("Expected non-nil stage")
	}
	if stage.classifier == nil {
		t.Error("Expected classifier to be set")
	}
	if !stage.enabled {
		t.Error("Expected enabled to be true")
	}
	if stage.Name() != "Bayesian" {
		t.Errorf("Expected name 'Bayesian', got %s", stage.Name())
	}
}

func TestNewBayesianStage_NilClassifier(t *testing.T) {
	stage := NewBayesianStage(nil)

	if stage == nil {
		t.Fatal("Expected non-nil stage")
	}
	if stage.enabled {
		t.Error("Expected enabled to be false for nil classifier")
	}
	if stage.Name() != "Bayesian" {
		t.Errorf("Expected name 'Bayesian', got %s", stage.Name())
	}
}

func TestBayesianStage_Disabled(t *testing.T) {
	stage := NewBayesianStage(nil)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

	result := stage.Process(ctx)
	// Should accept when classifier is nil
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for nil classifier, got %d", result)
	}
}

func TestBayesianStage_WithClassifier(t *testing.T) {
	classifier := spam.NewClassifier(nil)
	classifier.Initialize()
	stage := NewBayesianStage(classifier)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
	ctx.Headers = map[string][]string{
		"Subject": {"Test email"},
	}

	result := stage.Process(ctx)
	// Should accept (spam score below reject threshold)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// RBLStage tests
// ---------------------------------------------------------------------------

func TestRBLStage_WithServers_NotListed(t *testing.T) {
	// Use a mock resolver that returns "not listed"
	resolver := &mockRBLResolver{results: map[string]net.IP{}}
	stage := NewRBLStage([]string{"zen.spamhaus.org"}, resolver)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

	result := stage.Process(ctx)
	// Should accept for unlisted IPs
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %d", result)
	}
}

func TestRBLStage_WithServers_Listed(t *testing.T) {
	// Use a mock resolver that returns "listed"
	resolver := &mockRBLResolver{
		results: map[string]net.IP{
			"1.1.168.192.zen.spamhaus.org": net.ParseIP("127.0.0.2"),
		},
	}
	stage := NewRBLStage([]string{"zen.spamhaus.org"}, resolver)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

	result := stage.Process(ctx)
	// Should accept (RBL check adds spam score but doesn't reject)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %d", result)
	}
	if ctx.SpamScore < 2.0 {
		t.Errorf("Expected SpamScore >= 2.0 for listed IP, got %f", ctx.SpamScore)
	}
}

func TestRBLStage_DifferentResultCodes(t *testing.T) {
	testCases := []struct {
		resultIP   string
		minScore   float64
	}{
		{"127.0.0.2", 3.0}, // confirmed spam source
		{"127.0.0.3", 3.0}, // confirmed spam source (alt)
		{"127.0.0.4", 2.0}, // spam domain
		{"127.0.0.5", 2.5}, // phishing domain
		{"127.0.0.6", 3.0}, // malware domain
		{"127.0.0.7", 3.0}, // botnet server
		{"127.0.0.99", 1.5}, // generic positive or unknown
	}

	for _, tc := range testCases {
		resolver := &mockRBLResolver{
			results: map[string]net.IP{
				"1.1.168.192.zen.spamhaus.org": net.ParseIP(tc.resultIP),
			},
		}
		stage := NewRBLStage([]string{"zen.spamhaus.org"}, resolver)

		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		stage.Process(ctx)
		if ctx.SpamScore < tc.minScore {
			t.Errorf("For result %s: expected SpamScore >= %f, got %f", tc.resultIP, tc.minScore, ctx.SpamScore)
		}
	}
}

// ---------------------------------------------------------------------------
// AuthARCStage tests
// ---------------------------------------------------------------------------

func TestNewAuthARCStage(t *testing.T) {
	validator := auth.NewARCValidator(nil)
	stage := NewAuthARCStage(validator, nil)

	if stage == nil {
		t.Fatal("Expected non-nil stage")
	}
	if stage.validator == nil {
		t.Error("Expected validator to be set")
	}
	if stage.Name() != "ARC" {
		t.Errorf("Expected name 'ARC', got %s", stage.Name())
	}
}

func TestAuthARCStage_Name(t *testing.T) {
	validator := auth.NewARCValidator(nil)
	stage := NewAuthARCStage(validator, nil)

	if stage.Name() != "ARC" {
		t.Errorf("Expected name 'ARC', got %s", stage.Name())
	}
}

func TestAuthARCStage_Process_NoARCHeaders(t *testing.T) {
	validator := auth.NewARCValidator(nil)
	stage := NewAuthARCStage(validator, nil)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
	ctx.Headers = map[string][]string{}

	result := stage.Process(ctx)
	// Should accept even with no ARC headers
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %d", result)
	}
}

func TestAuthARCStage_Process_WithNilLogger(t *testing.T) {
	validator := auth.NewARCValidator(nil)
	stage := NewAuthARCStage(validator, nil)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
	ctx.Headers = map[string][]string{}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// extractUserFromRecipient direct unit tests
// ---------------------------------------------------------------------------

func TestExtractUserFromRecipient_BangPath(t *testing.T) {
	tests := []struct {
		recipient string
		expected  string
	}{
		{"user!domain", "user"},
		{"postmaster!mailhub", "postmaster"},
		{"", ""},
		{"no-at-nada", "no-at-nada"},
	}

	for _, tt := range tests {
		result := extractUserFromRecipient(tt.recipient)
		if result != tt.expected {
			t.Errorf("extractUserFromRecipient(%q) = %q, want %q", tt.recipient, result, tt.expected)
		}
	}
}

func TestSieveStage_Process_BangPathRecipient(t *testing.T) {
	manager := sieve.NewManager()
	stage := NewSieveStage(manager)

	ip := net.ParseIP("192.168.1.1")

	// Add a sieve script for the bang path user
	script := `
		require "fileinto";
		if true {
			fileinto "INBOX";
			stop;
		}
	`
	manager.StoreScript("testuser", "testscript", script)
	manager.SetActiveScript("testuser", "testscript", script)

	// Test with bang path recipient
	ctx := NewMessageContext(ip, "sender@example.com", []string{"testuser!bangpath@example.com"}, []byte("test"))
	ctx.Authenticated = true
	ctx.Username = "testuser"

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for bang path recipient, got %d", result)
	}
}
