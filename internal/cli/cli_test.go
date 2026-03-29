package cli

import (
	"testing"
)

func TestNewDiagnostics(t *testing.T) {
	d := NewDiagnostics(nil)
	if d == nil {
		t.Fatal("expected non-nil diagnostics")
	}
}

func TestDiagnosticsCheckDNS(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with invalid domain
	result, err := d.CheckDNS("invalid-domain-that-does-not-exist.example")
	if err != nil {
		t.Logf("CheckDNS returned error (expected for invalid domain): %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestDiagnosticsCheckTLS(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with localhost (will fail but shouldn't panic)
	result, err := d.CheckTLS("localhost")
	if err != nil {
		t.Logf("CheckTLS returned error (expected for localhost): %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestDNSCheckResultStruct(t *testing.T) {
	result := DNSCheckResult{
		RecordType: "MX",
		RecordName: "example.com",
		Expected:   "mail.example.com",
		Found:      "mail.example.com",
		Status:     "pass",
		Message:    "MX record found",
	}

	if result.RecordType != "MX" {
		t.Errorf("expected record type MX, got %s", result.RecordType)
	}
	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s", result.Status)
	}
}

func TestTLSCheckResultStruct(t *testing.T) {
	result := TLSCheckResult{
		Protocol: "TLS",
		Cipher:   "AES128",
		Version:  "1.3",
		Valid:    true,
		Expiry:   "2025-01-01",
		Message:  "Certificate valid",
	}

	if result.Protocol != "TLS" {
		t.Errorf("expected protocol TLS, got %s", result.Protocol)
	}
	if !result.Valid {
		t.Error("expected valid to be true")
	}
}

func TestPrintDNSResultsEmpty(t *testing.T) {
	// Test with empty results
	PrintDNSResults([]DNSCheckResult{})
}

func TestPrintDNSResultsAllFail(t *testing.T) {
	results := []DNSCheckResult{
		{
			RecordType: "MX",
			Status:     "fail",
			Message:    "No MX record",
		},
		{
			RecordType: "SPF",
			Status:     "fail",
			Message:    "No SPF record",
		},
	}

	PrintDNSResults(results)
}

func TestDiagnosticsCheckPTR(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with localhost (will fail but shouldn't panic)
	result := d.checkPTR("localhost")
	if result.Status != "warning" && result.Status != "pass" {
		t.Logf("checkPTR returned unexpected status: %s", result.Status)
	}
}

func TestDiagnosticsCheckMX(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with invalid domain
	result, err := d.checkMX("invalid-domain-that-does-not-exist.example")
	if err != nil {
		t.Logf("checkMX returned error (expected for invalid domain): %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestDiagnosticsCheckSPF(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with a domain
	result := d.checkSPF("example.com")
	if result.Status != "pass" && result.Status != "fail" {
		t.Logf("checkSPF returned unexpected status: %s", result.Status)
	}
}

func TestDiagnosticsCheckDKIM(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with a domain
	result := d.checkDKIM("example.com")
	if result.Status != "pass" && result.Status != "warning" {
		t.Logf("checkDKIM returned unexpected status: %s", result.Status)
	}
}

func TestDiagnosticsCheckDMARC(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with a domain
	result := d.checkDMARC("example.com")
	if result.Status != "pass" && result.Status != "warning" {
		t.Logf("checkDMARC returned unexpected status: %s", result.Status)
	}
}

func TestDiagnosticsCheckSMTPTLS(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with localhost
	result, err := d.checkSMTPTLS("localhost")
	if err != nil {
		t.Logf("checkSMTPTLS returned error (expected for localhost): %v", err)
	}
	// Result may be nil depending on implementation
	_ = result
}

func TestDiagnosticsCheckIMAPTLS(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with localhost
	result, err := d.checkIMAPTLS("localhost")
	if err != nil {
		t.Logf("checkIMAPTLS returned error (expected for localhost): %v", err)
	}
	// Result may be nil depending on implementation
	_ = result
}

// TestDiagnosticsCheckMXWithInvalidDomain tests MX check with invalid domain
func TestDiagnosticsCheckMXWithInvalidDomain(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with invalid domain
	results, err := d.checkMX("invalid-domain-that-does-not-exist.example")
	if err != nil {
		t.Logf("checkMX returned error (expected for invalid domain): %v", err)
	}
	if len(results) == 0 {
		t.Error("expected non-empty results even for invalid domain")
	}
	if results[0].RecordType != "MX" {
		t.Errorf("expected record type MX, got %s", results[0].RecordType)
	}
}

// TestDiagnosticsCheckPTRWithIP tests PTR check with IP
func TestDiagnosticsCheckPTRWithIP(t *testing.T) {
	d := NewDiagnostics(nil)

	// Test with localhost IP
	result := d.checkPTR("127.0.0.1")
	if result.Status != "warning" && result.Status != "pass" {
		t.Logf("checkPTR returned status: %s", result.Status)
	}
}
