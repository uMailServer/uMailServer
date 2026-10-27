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
