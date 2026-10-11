package audit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// F6801: forwarding headers from an untrusted peer must not set the audit IP.
func TestExtractIP_UntrustedPeerCannotForge(t *testing.T) {
	SetTrustedProxies(nil)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "198.51.100.9:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	if ip := ExtractIP(req); ip != "198.51.100.9" {
		t.Fatalf("forged header honoured: %s", ip)
	}
	SetTrustedProxies([]string{"10.0.0.0/8"})
	t.Cleanup(func() { SetTrustedProxies(nil) })
	if ip := ExtractIP(req); ip != "198.51.100.9" {
		t.Fatalf("non-matching peer honoured header: %s", ip)
	}
	req.RemoteAddr = "10.1.2.3:99"
	if ip := ExtractIP(req); ip != "1.2.3.4" {
		t.Fatalf("trusted CIDR peer: got %s", ip)
	}
}
