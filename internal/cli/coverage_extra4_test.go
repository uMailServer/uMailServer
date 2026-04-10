package cli

import (
	"testing"
)

// --- defaultRBLServers coverage ---

func TestDefaultRBLServers_ReturnsList(t *testing.T) {
	servers := defaultRBLServers()

	if len(servers) != 3 {
		t.Errorf("Expected 3 RBL servers, got %d", len(servers))
	}

	expected := []string{
		"bl.spamcop.net",
		"b.barracudacentral.org",
		"dnsbl.sorbs.net",
	}

	for i, exp := range expected {
		if servers[i] != exp {
			t.Errorf("Server[%d] = %q, want %q", i, servers[i], exp)
		}
	}
}

// --- Diagnostics with nil config ---

func TestDiagnostics_CheckDNS_NilConfig(t *testing.T) {
	d := &Diagnostics{}

	_, err := d.CheckDNS("example.com")
	// Should not panic with nil config
	if err != nil {
		t.Errorf("CheckDNS with nil config error: %v", err)
	}
}

func TestDiagnostics_CheckDeliverability_NilConfig(t *testing.T) {
	d := &Diagnostics{}

	result, err := d.CheckDeliverability("example.com")
	if err != nil {
		t.Fatalf("CheckDeliverability error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Domain != "example.com" {
		t.Errorf("Expected domain 'example.com', got %q", result.Domain)
	}
}
