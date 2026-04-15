package auth

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestNewDMARCReporter tests creating a new DMARC reporter
func TestNewDMARCReporter(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	config := DMARCReporterConfig{
		OrgName:     "Test Org",
		FromEmail:   "reports@example.com",
		ReportEmail: "rua@example.com",
		Interval:    24 * time.Hour,
	}

	reporter := NewDMARCReporter(resolver, logger, config)
	if reporter == nil {
		t.Fatal("expected non-nil reporter")
	}

	if reporter.orgName != "Test Org" {
		t.Errorf("expected org name 'Test Org', got %s", reporter.orgName)
	}
	if reporter.fromEmail != "reports@example.com" {
		t.Errorf("expected from email 'reports@example.com', got %s", reporter.fromEmail)
	}
	if reporter.interval != 24*time.Hour {
		t.Errorf("expected interval 24h, got %v", reporter.interval)
	}
}

// TestNewDMARCReporter_NilLogger tests creating a reporter with nil logger
func TestNewDMARCReporter_NilLogger(t *testing.T) {
	resolver := newMockDNSResolver()

	config := DMARCReporterConfig{
		OrgName:   "Test Org",
		FromEmail: "reports@example.com",
	}

	reporter := NewDMARCReporter(resolver, nil, config)
	if reporter == nil {
		t.Fatal("expected non-nil reporter")
	}
	if reporter.logger == nil {
		t.Error("expected default logger to be set")
	}
}

// TestDMARCReporter_RecordResult tests recording DMARC results
func TestDMARCReporter_RecordResult(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	config := DMARCReporterConfig{
		OrgName:   "Test Org",
		FromEmail: "reports@example.com",
		Interval:  24 * time.Hour,
	}

	reporter := NewDMARCReporter(resolver, logger, config)

	eval := &DMARCEvaluation{
		Domain:      "example.com",
		Policy:      DMARCPolicyQuarantine,
		Disposition: "none",
	}

	// Record a result
	reporter.RecordResult("example.com", eval, "192.0.2.1", "pass", "pass")

	// Verify stats
	entries, messages := reporter.GetReportStats("example.com")
	if entries != 1 {
		t.Errorf("expected 1 entry, got %d", entries)
	}
	if messages != 1 {
		t.Errorf("expected 1 message, got %d", messages)
	}
}

// TestDMARCReporter_RecordResult_MultipleIPs tests recording results from multiple IPs
func TestDMARCReporter_RecordResult_MultipleIPs(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{
		Interval: 24 * time.Hour,
	})

	eval := &DMARCEvaluation{
		Domain:      "example.com",
		Disposition: "none",
	}

	// Record results from different IPs
	reporter.RecordResult("example.com", eval, "192.0.2.1", "pass", "pass")
	reporter.RecordResult("example.com", eval, "192.0.2.2", "pass", "fail")
	reporter.RecordResult("example.com", eval, "192.0.2.1", "pass", "pass") // Same IP again

	entries, messages := reporter.GetReportStats("example.com")
	if entries != 2 {
		t.Errorf("expected 2 entries, got %d", entries)
	}
	if messages != 3 {
		t.Errorf("expected 3 messages, got %d", messages)
	}
}

// TestDMARCReporter_RecordResult_NilReceiver tests recording to nil reporter
func TestDMARCReporter_RecordResult_NilReceiver(t *testing.T) {
	var reporter *DMARCReporter
	eval := &DMARCEvaluation{}

	// Should not panic
	reporter.RecordResult("example.com", eval, "192.0.2.1", "pass", "pass")
}

// TestDMARCReporter_RecordResult_EmptyDomain tests recording with empty domain
func TestDMARCReporter_RecordResult_EmptyDomain(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{
		Interval: 24 * time.Hour,
	})

	eval := &DMARCEvaluation{}

	// Should not panic
	reporter.RecordResult("", eval, "192.0.2.1", "pass", "pass")

	// Verify no data was recorded
	entries, messages := reporter.GetReportStats("")
	if entries != 0 || messages != 0 {
		t.Error("expected no data for empty domain")
	}
}

// TestDMARCReporter_SetPolicy tests setting policy
func TestDMARCReporter_SetPolicy(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{
		Interval: 24 * time.Hour,
	})

	reporter.SetPolicy("example.com", DMARCPolicyReject, DMARCPolicyQuarantine, DMARCAlignmentStrict, DMARCAlignmentRelaxed, 100)

	// Verify stats still work
	entries, _ := reporter.GetReportStats("example.com")
	if entries != 0 {
		t.Errorf("expected 0 entries (no messages recorded), got %d", entries)
	}
}

// TestDMARCReporter_SetPolicy_NilReceiver tests setting policy on nil reporter
func TestDMARCReporter_SetPolicy_NilReceiver(t *testing.T) {
	var reporter *DMARCReporter

	// Should not panic
	reporter.SetPolicy("example.com", DMARCPolicyReject, DMARCPolicyQuarantine, DMARCAlignmentStrict, DMARCAlignmentRelaxed, 100)
}

// TestDMARCReporter_SetPolicy_EmptyDomain tests setting policy with empty domain
func TestDMARCReporter_SetPolicy_EmptyDomain(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{})

	// Should not panic
	reporter.SetPolicy("", DMARCPolicyReject, DMARCPolicyQuarantine, DMARCAlignmentStrict, DMARCAlignmentRelaxed, 100)
}

// TestDMARCReporter_GetReportStats_NoData tests getting stats when no data exists
func TestDMARCReporter_GetReportStats_NoData(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{})

	entries, messages := reporter.GetReportStats("nonexistent.com")
	if entries != 0 || messages != 0 {
		t.Errorf("expected 0 entries and 0 messages, got %d and %d", entries, messages)
	}
}

// TestDMARCReporter_GenerateAndSendReport_NoData tests generating report with no data
func TestDMARCReporter_GenerateAndSendReport_NoData(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{})

	err := reporter.GenerateAndSendReport("example.com", "rua@example.com")
	if err == nil {
		t.Error("expected error for domain with no data")
	}
}

// TestDMARCReporter_GenerateAndSendReport_InvalidEmail tests generating report with invalid email
func TestDMARCReporter_GenerateAndSendReport_InvalidEmail(t *testing.T) {
	resolver := newMockDNSResolver()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	reporter := NewDMARCReporter(resolver, logger, DMARCReporterConfig{
		OrgName:   "Test",
		FromEmail: "test@example.com",
	})

	// First record some data
	eval := &DMARCEvaluation{Disposition: "none"}
	reporter.RecordResult("example.com", eval, "192.0.2.1", "pass", "pass")

	// Try to send to invalid email
	err := reporter.GenerateAndSendReport("example.com", "invalid-email")
	if err == nil {
		t.Error("expected error for invalid email")
	}
}

// TestParseRUAEmail_Valid tests parsing valid RUA emails
func TestParseRUAEmail_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"mailto:rua@example.com", "rua@example.com"},
		{"rua@example.com", "rua@example.com"},
		{"mailto:reports@dmarc.example.com", "reports@dmarc.example.com"},
	}

	for _, tc := range tests {
		result, err := ParseRUAEmail(tc.input)
		if err != nil {
			t.Errorf("ParseRUAEmail(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("ParseRUAEmail(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestParseRUAEmail_Invalid tests parsing invalid RUA emails
func TestParseRUAEmail_Invalid(t *testing.T) {
	tests := []string{
		"",
		"mailto:",
		"invalid",
		"@example.com",
	}

	for _, input := range tests {
		result, err := ParseRUAEmail(input)
		if err == nil {
			t.Errorf("ParseRUAEmail(%q): expected error, got %q", input, result)
		}
	}
}
