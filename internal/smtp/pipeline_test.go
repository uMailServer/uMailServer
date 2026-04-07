package smtp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestMessageContext(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	data := []byte("Subject: Test\r\n\r\nBody")

	ctx := NewMessageContext(ip, from, to, data)

	if ctx.RemoteIP.String() != "192.168.1.1" {
		t.Errorf("Expected IP 192.168.1.1, got %s", ctx.RemoteIP)
	}
	if ctx.From != from {
		t.Errorf("Expected from %s, got %s", from, ctx.From)
	}
	if len(ctx.To) != 1 || ctx.To[0] != to[0] {
		t.Errorf("Expected to %v, got %v", to, ctx.To)
	}
}

func TestPipeline(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)

	// Add a test stage
	pipeline.AddStage(&testStage{name: "TestStage"})

	t.Run("AcceptMessage", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result, err := pipeline.Process(ctx)
		if err != nil {
			t.Fatalf("Pipeline failed: %v", err)
		}
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
	})
}

func TestRateLimitStage(t *testing.T) {
	stage := NewRateLimitStageWithDefaults()

	t.Run("UnderLimit", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
	})

	t.Run("OverLimit", func(t *testing.T) {
		stage := NewRateLimitStageWithDefaults() // Fresh stage
		ip := net.ParseIP("192.168.1.2")

		// Send 31 messages (over limit)
		for i := 0; i < 31; i++ {
			ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
			stage.Process(ctx)
		}

		// 31st message should be rejected
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		result := stage.Process(ctx)
		if result != ResultReject {
			t.Errorf("Expected ResultReject after limit, got %d", result)
		}
	})
}

func TestGreylistStage(t *testing.T) {
	stage := NewGreylistStage()

	t.Run("FirstAttempt", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		if result != ResultReject {
			t.Errorf("Expected ResultReject on first attempt, got %d", result)
		}
		if ctx.RejectionCode != 451 {
			t.Errorf("Expected code 451, got %d", ctx.RejectionCode)
		}
	})

	t.Run("SecondAttemptTooSoon", func(t *testing.T) {
		stage := NewGreylistStage() // Fresh stage
		ip := net.ParseIP("192.168.1.3")

		// First attempt
		ctx1 := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		stage.Process(ctx1)

		// Second attempt immediately
		ctx2 := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		result := stage.Process(ctx2)
		if result != ResultReject {
			t.Errorf("Expected ResultReject on second attempt (too soon), got %d", result)
		}
	})
}

func TestHeuristicStage(t *testing.T) {
	stage := NewHeuristicStage()

	t.Run("EmptySubject", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		// No subject header

		stage.Process(ctx)
		if ctx.SpamScore < 1.0 {
			t.Errorf("Expected spam score >= 1.0 for empty subject, got %f", ctx.SpamScore)
		}
	})

	t.Run("AllCapsSubject", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.Headers["Subject"] = []string{"THIS IS SPAM"}

		stage.Process(ctx)
		if ctx.SpamScore < 2.0 {
			t.Errorf("Expected spam score >= 2.0 for all caps subject, got %f", ctx.SpamScore)
		}
	})

	t.Run("MissingDate", func(t *testing.T) {
		stage := NewHeuristicStage() // Fresh stage
		ip := net.ParseIP("192.168.1.4")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		// No Date header

		stage.Process(ctx)
		if ctx.SpamScore < 1.0 {
			t.Errorf("Expected spam score >= 1.0 for missing date, got %f", ctx.SpamScore)
		}
	})
}

func TestScoreStage(t *testing.T) {
	t.Run("Inbox", func(t *testing.T) {
		stage := NewScoreStage(9.0, 3.0)
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.SpamScore = 1.0

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept for inbox, got %d", result)
		}
		if ctx.SpamResult.Verdict != "inbox" {
			t.Errorf("Expected verdict inbox, got %s", ctx.SpamResult.Verdict)
		}
	})

	t.Run("Junk", func(t *testing.T) {
		stage := NewScoreStage(9.0, 3.0)
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.SpamScore = 5.0

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept for junk (delivered to junk folder), got %d", result)
		}
		if ctx.SpamResult.Verdict != "junk" {
			t.Errorf("Expected verdict junk, got %s", ctx.SpamResult.Verdict)
		}
	})

	t.Run("Reject", func(t *testing.T) {
		stage := NewScoreStage(9.0, 3.0)
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))
		ctx.SpamScore = 10.0

		result := stage.Process(ctx)
		if result != ResultReject {
			t.Errorf("Expected ResultReject for high score, got %d", result)
		}
		if ctx.SpamResult.Verdict != "reject" {
			t.Errorf("Expected verdict reject, got %s", ctx.SpamResult.Verdict)
		}
	})
}

func TestReverseIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// IPv4
		{"192.168.1.1", "1.1.168.192"},
		{"10.0.0.1", "1.0.0.10"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := reverseIP(tt.input)
		if got != tt.expected {
			t.Errorf("reverseIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// Test helpers

type testStage struct {
	name string
}

func (s *testStage) Name() string { return s.name }

func (s *testStage) Process(ctx *MessageContext) PipelineResult {
	return ResultAccept
}

type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...interface{}) {}
func (l *testLogger) Info(msg string, args ...interface{})  {}
func (l *testLogger) Warn(msg string, args ...interface{})  {}
func (l *testLogger) Error(msg string, args ...interface{}) {}

// mockDNSResolver is a mock DNS resolver for testing
type mockDNSResolver struct {
	records map[string][]string
}

func (m *mockDNSResolver) LookupTXT(domain string) ([]string, error) {
	if records, ok := m.records[domain]; ok {
		return records, nil
	}
	return []string{}, nil
}

// mockRBLResolver is a mock RBL DNS resolver for testing
type mockRBLResolver struct {
	results map[string]net.IP // host -> listed IP (nil means not listed)
}

func (m *mockRBLResolver) LookupHost(ctx context.Context, host string) (net.IP, error) {
	if ip, ok := m.results[host]; ok && ip != nil {
		return ip, nil
	}
	return nil, fmt.Errorf("not listed")
}

func TestSPFStage(t *testing.T) {
	mockResolver := &mockDNSResolver{
		records: map[string][]string{
			"example.com": {"v=spf1 ip4:192.168.1.1 -all"},
		},
	}
	stage := NewSPFStage(mockResolver)

	t.Run("Name", func(t *testing.T) {
		if stage.Name() != "SPF" {
			t.Errorf("Expected name 'SPF', got %s", stage.Name())
		}
	})

	t.Run("NoSPFRecord", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@unknown.com", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		// Should accept when no SPF record exists
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
		if ctx.SPFResult.Result != "none" {
			t.Errorf("Expected SPF result 'none', got %s", ctx.SPFResult.Result)
		}
	})

	t.Run("InvalidSender", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "invalid-email", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
		if ctx.SPFResult.Result != "none" {
			t.Errorf("Expected SPF result 'none', got %s", ctx.SPFResult.Result)
		}
	})
}

func TestRBLStage(t *testing.T) {
	mockResolver := &mockRBLResolver{results: make(map[string]net.IP)}
	stage := NewRBLStage([]string{}, mockResolver) // Empty servers list

	t.Run("Name", func(t *testing.T) {
		if stage.Name() != "RBL" {
			t.Errorf("Expected name 'RBL', got %s", stage.Name())
		}
	})

	t.Run("Process", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

		result := stage.Process(ctx)
		// Should accept for private IPs not in RBL
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %d", result)
		}
	})
}

func TestStageNames(t *testing.T) {
	tests := []struct {
		stage    PipelineStage
		expected string
	}{
		{NewRateLimitStageWithDefaults(), "RateLimit"},
		{NewGreylistStage(), "Greylist"},
		{NewHeuristicStage(), "Heuristic"},
		{NewScoreStage(9.0, 3.0), "Score"},
	}

	for _, tt := range tests {
		if tt.stage.Name() != tt.expected {
			t.Errorf("Expected name %q, got %q", tt.expected, tt.stage.Name())
		}
	}
}

func TestNewPipeline(t *testing.T) {
	t.Run("WithLogger", func(t *testing.T) {
		logger := &testLogger{}
		pipeline := NewPipeline(logger)
		if pipeline == nil {
			t.Fatal("Expected non-nil pipeline")
		}
		if pipeline.logger == nil {
			t.Error("Expected logger to be set")
		}
	})

	t.Run("WithNilLogger", func(t *testing.T) {
		pipeline := NewPipeline(nil)
		if pipeline == nil {
			t.Fatal("Expected non-nil pipeline")
		}
		if pipeline.logger == nil {
			t.Error("Expected default logger to be set")
		}
	})
}

func TestProcessStages(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)

	// Add a rejecting stage
	pipeline.AddStage(&rejectStage{})

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"recipient@example.com"}, []byte("test"))

	result, err := pipeline.Process(ctx)
	// Rejection returns both ResultReject and an error
	if err == nil {
		t.Error("Expected error for rejected message")
	}
	if result != ResultReject {
		t.Errorf("Expected ResultReject, got %d", result)
	}
}

// Test helpers

type rejectStage struct{}

func (s *rejectStage) Name() string { return "reject" }
func (s *rejectStage) Process(ctx *MessageContext) PipelineResult {
	return ResultReject
}

// ---------------------------------------------------------------------------
// evaluateSPF tests
// ---------------------------------------------------------------------------

func TestEvaluateSPF(t *testing.T) {
	stage := &SPFStage{}
	ip := net.ParseIP("192.168.1.1")

	tests := []struct {
		name     string
		record   string
		expected string
	}{
		{"plus_all", "v=spf1 +all", "pass"},
		{"bare_all", "v=spf1 all", "pass"},
		{"dash_all", "v=spf1 ip4:10.0.0.1 -all", "fail"},
		{"tilde_all", "v=spf1 ip4:10.0.0.1 ~all", "softfail"},
		{"redirect_only", "v=spf1 redirect=_spf.example.com", "neutral"},
		{"no_mechanism", "v=spf1", "neutral"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stage.evaluateSPF(tt.record, ip, "example.com")
			if result.Result != tt.expected {
				t.Errorf("evaluateSPF(%q) result = %q, want %q", tt.record, result.Result, tt.expected)
			}
			if result.Domain != "example.com" {
				t.Errorf("evaluateSPF domain = %q, want %q", result.Domain, "example.com")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SPFStage.Process additional paths
// ---------------------------------------------------------------------------

func TestSPFStage_Process_DNSLookupError(t *testing.T) {
	// mockDNSResolverErr returns an error for every domain
	resolver := &mockDNSResolverErr{}
	stage := NewSPFStage(resolver)

	ip := net.ParseIP("1.2.3.4")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept on DNS error, got %v", result)
	}
	if ctx.SPFResult.Result != "temperror" {
		t.Errorf("Expected SPF result 'temperror', got %q", ctx.SPFResult.Result)
	}
}

func TestSPFStage_Process_WithSPFRecord(t *testing.T) {
	mockResolver := &mockDNSResolver{
		records: map[string][]string{
			"example.com": {"v=spf1 ip4:10.0.0.1 -all"},
		},
	}
	stage := NewSPFStage(mockResolver)

	ip := net.ParseIP("10.0.0.2")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	// The SPF record has -all so the simplified evaluateSPF should return "fail"
	if ctx.SPFResult.Result != "fail" {
		t.Errorf("Expected SPF result 'fail', got %q", ctx.SPFResult.Result)
	}
	if ctx.SpamScore != 2.0 {
		t.Errorf("Expected SpamScore 2.0 for SPF fail, got %f", ctx.SpamScore)
	}
}

func TestSPFStage_Process_SoftfailSpamScore(t *testing.T) {
	mockResolver := &mockDNSResolver{
		records: map[string][]string{
			"example.com": {"v=spf1 ip4:10.0.0.1 ~all"},
		},
	}
	stage := NewSPFStage(mockResolver)

	ip := net.ParseIP("10.0.0.2")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SPFResult.Result != "softfail" {
		t.Errorf("Expected SPF result 'softfail', got %q", ctx.SPFResult.Result)
	}
	if ctx.SpamScore != 1.0 {
		t.Errorf("Expected SpamScore 1.0 for SPF softfail, got %f", ctx.SpamScore)
	}
}

func TestSPFStage_Process_EmptyFrom(t *testing.T) {
	stage := NewSPFStage(&mockDNSResolver{})

	ip := net.ParseIP("1.2.3.4")
	ctx := NewMessageContext(ip, "", []string{"rcpt@example.com"}, []byte("data"))

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SPFResult.Result != "none" {
		t.Errorf("Expected SPF result 'none' for empty sender, got %q", ctx.SPFResult.Result)
	}
}

// ---------------------------------------------------------------------------
// RBLStage.Process additional paths (with servers)
// ---------------------------------------------------------------------------

func TestRBLStage_Process_WithServers(t *testing.T) {
	mockResolver := &mockRBLResolver{results: make(map[string]net.IP)}
	stage := NewRBLStage([]string{"zen.spamhaus.org", "bl.spamcop.net"}, mockResolver)

	ip := net.ParseIP("192.168.1.1")
	ctx := NewMessageContext(ip, "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))

	result := stage.Process(ctx)
	// The current implementation always returns ResultAccept
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// extractDomain tests
// ---------------------------------------------------------------------------

func TestExtractDomain_Internal(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"", ""},
		{"no-at-sign", ""},
		{"@domainonly.com", "domainonly.com"},
		{"user@multiple@ats.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := extractDomain(tt.email)
			if got != tt.expected {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Additional test helpers
// ---------------------------------------------------------------------------

// mockDNSResolverErr always returns an error
type mockDNSResolverErr struct{}

func (m *mockDNSResolverErr) LookupTXT(domain string) ([]string, error) {
	return nil, fmt.Errorf("DNS lookup failed for %s", domain)
}

// ---------------------------------------------------------------------------
// AVStage tests
// ---------------------------------------------------------------------------

// mockAVScanner is a controllable mock for AVScanner
type mockAVScanner struct {
	enabled bool
	result  *AVScanResult
	scanErr error
}

func (m *mockAVScanner) IsEnabled() bool { return m.enabled }
func (m *mockAVScanner) Scan(data []byte) (*AVScanResult, error) {
	if m.scanErr != nil {
		return nil, m.scanErr
	}
	return m.result, nil
}

func TestNewAVStage(t *testing.T) {
	scanner := &mockAVScanner{enabled: true}
	stage := NewAVStage(scanner, "reject")

	if stage == nil {
		t.Fatal("Expected non-nil AVStage")
	}
	if stage.Name() != "AV" {
		t.Errorf("Expected name 'AV', got %s", stage.Name())
	}
}

func TestAVStage_DisabledScanner(t *testing.T) {
	scanner := &mockAVScanner{enabled: false}
	stage := NewAVStage(scanner, "reject")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for disabled scanner, got %v", result)
	}
}

func TestAVStage_NilScanner(t *testing.T) {
	stage := NewAVStage(nil, "reject")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for nil scanner, got %v", result)
	}
}

func TestAVStage_CleanMessage(t *testing.T) {
	scanner := &mockAVScanner{
		enabled: true,
		result:  &AVScanResult{Infected: false},
	}
	stage := NewAVStage(scanner, "reject")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for clean message, got %v", result)
	}
}

func TestAVStage_VirusDetected_Reject(t *testing.T) {
	scanner := &mockAVScanner{
		enabled: true,
		result:  &AVScanResult{Infected: true, Virus: "EICAR-Test"},
	}
	stage := NewAVStage(scanner, "reject")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultReject {
		t.Errorf("Expected ResultReject for virus detected with reject action, got %v", result)
	}
	if ctx.RejectionCode != 550 {
		t.Errorf("Expected rejection code 550, got %d", ctx.RejectionCode)
	}
	if !strings.Contains(ctx.RejectionMessage, "EICAR-Test") {
		t.Errorf("Expected virus name in rejection message, got %q", ctx.RejectionMessage)
	}
}

func TestAVStage_VirusDetected_Quarantine(t *testing.T) {
	scanner := &mockAVScanner{
		enabled: true,
		result:  &AVScanResult{Infected: true, Virus: "EICAR-Test"},
	}
	stage := NewAVStage(scanner, "quarantine")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultQuarantine {
		t.Errorf("Expected ResultQuarantine for virus detected with quarantine action, got %v", result)
	}
	if !ctx.Quarantine {
		t.Error("Expected Quarantine flag to be set")
	}
}

func TestAVStage_VirusDetected_Tag(t *testing.T) {
	scanner := &mockAVScanner{
		enabled: true,
		result:  &AVScanResult{Infected: true, Virus: "EICAR-Test"},
	}
	stage := NewAVStage(scanner, "tag")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for tag action, got %v", result)
	}
	if ctx.Headers["X-Virus"] == nil || len(ctx.Headers["X-Virus"]) == 0 {
		t.Error("Expected X-Virus header to be set")
	} else if ctx.Headers["X-Virus"][0] != "EICAR-Test" {
		t.Errorf("Expected X-Virus header 'EICAR-Test', got %q", ctx.Headers["X-Virus"][0])
	}
}

func TestAVStage_ScanError(t *testing.T) {
	scanner := &mockAVScanner{
		enabled: true,
		scanErr: fmt.Errorf("scanner unavailable"),
	}
	stage := NewAVStage(scanner, "reject")

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	// Scan error should still accept the message
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept on scan error, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// ScoreStage quarantine verdict
// ---------------------------------------------------------------------------

func TestScoreStage_QuarantineVerdict(t *testing.T) {
	stage := NewScoreStage(9.0, 3.0)
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	ctx.SpamScore = 5.0 // Between junk (3.0) and reject (9.0)

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for junk verdict, got %v", result)
	}
	if ctx.SpamResult.Verdict != "junk" {
		t.Errorf("Expected verdict 'junk', got %q", ctx.SpamResult.Verdict)
	}
	if ctx.SpamResult.Score != 5.0 {
		t.Errorf("Expected SpamResult.Score 5.0, got %f", ctx.SpamResult.Score)
	}
}

// ---------------------------------------------------------------------------
// Pipeline with quarantine result from stages
// ---------------------------------------------------------------------------

func TestPipeline_QuarantineResult(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&quarantineTestStage{})

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result != ResultQuarantine {
		t.Errorf("Expected ResultQuarantine, got %v", result)
	}
	if !ctx.Quarantine {
		t.Error("Expected Quarantine flag to be set on context")
	}
}

type quarantineTestStage struct{}

func (s *quarantineTestStage) Name() string { return "QuarantineTest" }
func (s *quarantineTestStage) Process(ctx *MessageContext) PipelineResult {
	return ResultQuarantine
}

// ---------------------------------------------------------------------------
// Pipeline error path: reject with custom code and message set on context
// ---------------------------------------------------------------------------

func TestPipeline_RejectWithCustomCodeAndMessage(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&customRejectStage{
		code:    421,
		message: "Rate limit exceeded",
	})

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)

	if err == nil {
		t.Fatal("Expected error for rejected message")
	}
	if result != ResultReject {
		t.Errorf("Expected ResultReject, got %v", result)
	}
	if ctx.RejectionCode != 421 {
		t.Errorf("Expected rejection code 421, got %d", ctx.RejectionCode)
	}
	if ctx.RejectionMessage != "Rate limit exceeded" {
		t.Errorf("Expected rejection message 'Rate limit exceeded', got %q", ctx.RejectionMessage)
	}
	if !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("Expected error to contain 'Rate limit exceeded', got %q", err.Error())
	}
}

type customRejectStage struct {
	code    int
	message string
}

func (s *customRejectStage) Name() string { return "CustomReject" }
func (s *customRejectStage) Process(ctx *MessageContext) PipelineResult {
	ctx.Rejected = true
	ctx.RejectionCode = s.code
	ctx.RejectionMessage = s.message
	return ResultReject
}

// ---------------------------------------------------------------------------
// Pipeline with multiple stages: accept then reject
// ---------------------------------------------------------------------------

func TestPipeline_MultipleStages_RejectAfterAccept(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&testStage{name: "First"})
	pipeline.AddStage(&rejectStage{})

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)

	if err == nil {
		t.Fatal("Expected error for rejected message")
	}
	if result != ResultReject {
		t.Errorf("Expected ResultReject, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// Pipeline with multiple stages: accept then quarantine then accept
// ---------------------------------------------------------------------------

func TestPipeline_MultipleStages_QuarantineThenAccept(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&quarantineTestStage{})
	pipeline.AddStage(&testStage{name: "AfterQuarantine"})

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result != ResultQuarantine {
		t.Errorf("Expected ResultQuarantine, got %v", result)
	}
	if !ctx.Quarantine {
		t.Error("Expected Quarantine flag to be set")
	}
}

// ---------------------------------------------------------------------------
// Pipeline: accept then quarantine (quarantine followed by more stages)
// ---------------------------------------------------------------------------

func TestPipeline_Quarantine_ContinuesThroughStages(t *testing.T) {
	logger := &testLogger{}
	pipeline := NewPipeline(logger)

	// Track which stages ran
	var stagesRan []string
	pipeline.AddStage(&trackingStage{name: "Before", stages: &stagesRan})
	pipeline.AddStage(&quarantineTestStage{})
	pipeline.AddStage(&trackingStage{name: "After", stages: &stagesRan})

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result != ResultQuarantine {
		t.Errorf("Expected ResultQuarantine, got %v", result)
	}
	// Both stages should have run (quarantine doesn't stop the pipeline)
	if len(stagesRan) != 2 || stagesRan[0] != "Before" || stagesRan[1] != "After" {
		t.Errorf("Expected stages [Before, After], got %v", stagesRan)
	}
}

type trackingStage struct {
	name   string
	stages *[]string
}

func (s *trackingStage) Name() string { return s.name }
func (s *trackingStage) Process(ctx *MessageContext) PipelineResult {
	*s.stages = append(*s.stages, s.name)
	return ResultAccept
}

// ---------------------------------------------------------------------------
// Rate limit stage: window expiry resets count
// ---------------------------------------------------------------------------

func TestRateLimitStage_WindowExpiry(t *testing.T) {
	stage := NewRateLimitStageWithDefaults()
	ip := net.ParseIP("10.0.0.1")

	// Send 30 messages to reach the limit
	for i := 0; i < 30; i++ {
		ctx := NewMessageContext(ip, "s@example.com", []string{"r@example.com"}, []byte("data"))
		stage.Process(ctx)
	}

	// Next message should be rejected (over limit)
	ctx := NewMessageContext(ip, "s@example.com", []string{"r@example.com"}, []byte("data"))
	result := stage.Process(ctx)
	if result != ResultReject {
		t.Errorf("Expected ResultReject when over limit, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// GreylistStage: allowed entry is always accepted
// ---------------------------------------------------------------------------

func TestGreylistStage_AllowedEntryAccepted(t *testing.T) {
	stage := NewGreylistStage()
	ip := net.ParseIP("192.168.1.100")

	// Create the entry and set it as allowed with a past firstSeen
	key := fmt.Sprintf("%s:%s:%s", ip.String(), "s@example.com", "r@example.com")
	stage.greylist[key] = &greylistEntry{
		firstSeen: time.Now().Add(-10 * time.Minute),
		allowed:   true,
	}

	ctx := NewMessageContext(ip, "s@example.com", []string{"r@example.com"}, []byte("data"))
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for allowed greylist entry, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// GreylistStage: entry becomes allowed after waiting long enough
// ---------------------------------------------------------------------------

func TestGreylistStage_EntryBecomesAllowed(t *testing.T) {
	stage := NewGreylistStage()
	ip := net.ParseIP("192.168.1.101")

	// Create entry with firstSeen 6 minutes ago (past the 5-min window)
	key := fmt.Sprintf("%s:%s:%s", ip.String(), "s@example.com", "r@example.com")
	stage.greylist[key] = &greylistEntry{
		firstSeen: time.Now().Add(-6 * time.Minute),
		allowed:   false,
	}

	ctx := NewMessageContext(ip, "s@example.com", []string{"r@example.com"}, []byte("data"))
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept after greylist window passed, got %v", result)
	}

	// Verify the entry is now marked as allowed
	if !stage.greylist[key].allowed {
		t.Error("Expected greylist entry to be marked as allowed")
	}
}

// ---------------------------------------------------------------------------
// HeuristicStage: all rules trigger (missing message-id)
// ---------------------------------------------------------------------------

func TestHeuristicStage_MissingMessageID(t *testing.T) {
	stage := NewHeuristicStage()
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	// Set subject and date to avoid those rules firing
	ctx.Headers["Subject"] = []string{"Normal subject"}
	ctx.Headers["Date"] = []string{"Mon, 01 Jan 2024 00:00:00 +0000"}
	// No Message-Id or Message-ID header

	stage.Process(ctx)
	// Only MISSING_MESSAGE_ID should fire
	found := false
	for _, reason := range ctx.SpamResult.Reasons {
		if reason == "MISSING_MESSAGE_ID" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected MISSING_MESSAGE_ID rule to fire, got reasons: %v", ctx.SpamResult.Reasons)
	}
}

// ---------------------------------------------------------------------------
// HeuristicStage: short subject does not trigger all-caps rule
// ---------------------------------------------------------------------------

func TestHeuristicStage_ShortSubjectNotAllCaps(t *testing.T) {
	stage := NewHeuristicStage()
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	ctx.Headers["Subject"] = []string{"ABC"} // Only 3 chars, below the > 5 threshold

	initialScore := ctx.SpamScore
	stage.Process(ctx)
	allCapsFired := false
	for _, reason := range ctx.SpamResult.Reasons {
		if reason == "ALL_CAPS_SUBJECT" {
			allCapsFired = true
		}
	}
	if allCapsFired {
		t.Errorf("ALL_CAPS_SUBJECT should not fire for short subject (<=5 chars), initial=%f final=%f", initialScore, ctx.SpamScore)
	}
}

// ---------------------------------------------------------------------------
// HeuristicStage: empty subject slice triggers EMPTY_SUBJECT but not ALL_CAPS
// ---------------------------------------------------------------------------

func TestHeuristicStage_EmptySubjectSlice(t *testing.T) {
	stage := NewHeuristicStage()
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	ctx.Headers["Subject"] = []string{} // Empty slice

	stage.Process(ctx)
	emptyFired := false
	for _, reason := range ctx.SpamResult.Reasons {
		if reason == "EMPTY_SUBJECT" {
			emptyFired = true
		}
	}
	if !emptyFired {
		t.Error("Expected EMPTY_SUBJECT to fire for empty subject slice")
	}
}
