package tls

import (
	"slices"
	"testing"
)

// R224 F7010: with auto TLS the config must negotiate acme-tls/1 so autocert
// can answer TLS-ALPN-01 (there is no port-80 listener).
func TestGetTLSConfigAdvertisesACMEALPN(t *testing.T) {
	t.Chdir(t.TempDir())
	m, err := NewManager(Config{Enabled: true, AutoTLS: true, Email: "a@example.com", Domains: []string{"mail.example.com"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(m.GetTLSConfig().NextProtos, "acme-tls/1") {
		t.Fatal("acme-tls/1 missing from NextProtos")
	}
	m2, err := NewManager(Config{Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.GetTLSConfig().NextProtos) != 0 {
		t.Fatal("manual-cert config must not advertise ALPN")
	}
}
