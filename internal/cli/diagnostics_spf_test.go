package cli

// Regression tests for the SPF vacuous-inclusion defect: checkSPF used
// strings.Contains(txt, hostname) — always true for an empty configured
// hostname — and strings.Contains(txt, "mx"), which matches unrelated tokens
// like include:spf.mxhacker.example. An SPF record that does not authorize
// this server was reported as "pass". The fix skips with an explicit warning
// when no hostname is configured, and matches whole SPF mechanisms (mx, or
// a:<hostname>) otherwise.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

const spfTypeTXT = 16

func spfTXTAnswer(txt string) [][]byte {
	return [][]byte{append([]byte{byte(len(txt))}, []byte(txt)...)}
}

func TestCheckSPFEmptyHostnameDoesNotPass(t *testing.T) {
	fakeAddr := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, spfTXTAnswer("v=spf1 exists:spoofer.example -all")
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, fakeAddr)

	d := NewDiagnostics(&config.Config{})
	result := d.checkSPF("example.com")
	if result.Status == "pass" {
		t.Fatalf("FAIL: empty configured hostname made an unrelated SPF record report %q (%s) — the inclusion test is vacuous", result.Status, result.Message)
	}
}

func TestCheckSPFSubstringMatchDoesNotPass(t *testing.T) {
	fakeAddr := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, spfTXTAnswer("v=spf1 include:spf.mxhacker.example -all")
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, fakeAddr)

	d := NewDiagnostics(&config.Config{Server: config.ServerConfig{Hostname: "mail.example.com"}})
	result := d.checkSPF("example.com")
	if result.Status == "pass" {
		t.Fatalf("FAIL: an unrelated record containing \"mx\" as a substring reported %q (%s) — the test must match SPF mechanisms, not substrings", result.Status, result.Message)
	}
}

func TestCheckSPFAuthorizedHostPasses(t *testing.T) {
	fakeAddr := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, spfTXTAnswer("v=spf1 mx a:mail.example.com -all")
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, fakeAddr)

	d := NewDiagnostics(&config.Config{Server: config.ServerConfig{Hostname: "mail.example.com"}})
	result := d.checkSPF("example.com")
	if result.Status != "pass" {
		t.Fatalf("CONTROL FAILED (harness): authorized host got %q (%s), want pass", result.Status, result.Message)
	}
}
