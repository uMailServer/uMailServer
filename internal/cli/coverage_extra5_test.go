package cli

import (
	"testing"
)

// --- Diagnostics utility functions ---

func TestReverseIP_ValidIPv4(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "1.1.168.192"},
		{"10.0.0.1", "1.0.0.10"},
		{"172.16.0.1", "1.0.16.172"},
	}

	for _, tt := range tests {
		result := reverseIP(tt.input)
		if result != tt.expected {
			t.Errorf("reverseIP(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestReverseIP_InvalidIP(t *testing.T) {
	tests := []string{
		"not-an-ip",
		"256.1.1.1",
		"192.168.1",
		"",
	}

	for _, ip := range tests {
		result := reverseIP(ip)
		if result != "" {
			t.Errorf("reverseIP(%q) = %q, want empty for invalid IP", ip, result)
		}
	}
}

func TestReverseIP_IPv6(t *testing.T) {
	// IPv6 addresses should return empty
	result := reverseIP("::1")
	if result != "" {
		t.Errorf("reverseIP(::1) = %q, want empty for IPv6", result)
	}
}

func TestCheckRBLServer_ValidIP(t *testing.T) {
	d := &Diagnostics{}

	// Test with a known clean IP (Google DNS)
	listed, code := d.checkRBLServer("8.8.8.8", "bl.spamcop.net")
	if listed {
		t.Errorf("Expected 8.8.8.8 to NOT be listed on spamcop, got code %q", code)
	}
}

func TestCheckRBLServer_InvalidIP(t *testing.T) {
	d := &Diagnostics{}

	// Invalid IP should return false
	listed, _ := d.checkRBLServer("invalid", "bl.spamcop.net")
	if listed {
		t.Error("Expected invalid IP to return false")
	}
}

func TestCheckDNS_InvalidDomain(t *testing.T) {
	d := &Diagnostics{}

	_, err := d.CheckDNS("this-domain-does-not-exist.invalid")
	// DNS lookup for invalid domain may fail - just verify no panic
	_ = err
}

func TestCheckTLS_NoHostname(t *testing.T) {
	d := &Diagnostics{config: nil}

	// Should not panic with nil config
	_, err := d.CheckTLS("")
	if err != nil {
		t.Errorf("CheckTLS with empty hostname error: %v", err)
	}
}

// --- PrintDeliverabilityResults ---

func TestPrintDeliverabilityResults_NoIssues(t *testing.T) {
	result := &DeliverabilityResult{
		Domain:       "example.com",
		OverallScore: "pass",
		Message:      "All checks passed",
		Issues:       []string{},
	}

	// Should not panic
	PrintDeliverabilityResults(result)
}

func TestPrintDeliverabilityResults_WithIssues(t *testing.T) {
	result := &DeliverabilityResult{
		Domain:       "example.com",
		OverallScore: "fail",
		Message:      "Critical issues found",
		Issues:       []string{"DNS: Missing SPF record", "TLS: Certificate expired"},
	}

	// Should not panic
	PrintDeliverabilityResults(result)
}

func TestPrintDeliverabilityResults_Warning(t *testing.T) {
	result := &DeliverabilityResult{
		Domain:       "example.com",
		OverallScore: "warning",
		Message:      "Some issues found",
		Issues:       []string{"DNS: SPF softfail"},
		DNSResults: []DNSCheckResult{
			{RecordType: "A", Status: "warning", Message: "No A record"},
		},
		RBLResults: []RBLCheckResult{
			{Server: "spam.dnsbl.example.com", Listed: false, Message: "Not listed"},
		},
		TLSResult: &TLSCheckResult{
			Valid:   true,
			Message: "TLS configured correctly",
		},
		SMTPResult: &SMTPCheckResult{
			Reachable: true,
			Message:   "SMTP is reachable",
		},
	}

	// Should not panic
	PrintDeliverabilityResults(result)
}

func TestPrintDeliverabilityResults_WithRBLListings(t *testing.T) {
	result := &DeliverabilityResult{
		Domain:       "example.com",
		OverallScore: "fail",
		Message:      "IP listed on RBL",
		RBLResults: []RBLCheckResult{
			{Server: "spam.dnsbl.example.com", Listed: true, Code: "127.0.0.2", Message: "Listed", Score: "spam"},
		},
	}

	// Should not panic
	PrintDeliverabilityResults(result)
}

func TestPrintDeliverabilityResults_NilTLSAndSMTP(t *testing.T) {
	result := &DeliverabilityResult{
		Domain:       "example.com",
		OverallScore: "pass",
		Message:      "Basic checks passed",
		DNSResults: []DNSCheckResult{
			{RecordType: "A", Status: "pass", Message: "A record found"},
		},
		// TLSResult and SMTPResult are nil
	}

	// Should not panic
	PrintDeliverabilityResults(result)
}
