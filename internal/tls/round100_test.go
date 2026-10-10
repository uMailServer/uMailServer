package tls

import (
	"context"
	"crypto/tls"
	"testing"
)

// F5820: client-auth configs must never fall back to system roots.
func TestF5820_ClientAuthPoolNeverNil(t *testing.T) {
	m, err := NewManager(Config{ClientAuth: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.GetTLSConfig()
	if cfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Fatalf("unexpected ClientAuth %v", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs nil: crypto/tls would verify client certs against system roots")
	}
	if m.GetTLSConfigWithClientAuth(true).ClientCAs == nil {
		t.Fatal("ClientCAs nil with required client cert")
	}
}

// F5821: renewal must replace the in-memory autocert manager.
func TestF5821_RenewReplacesAutocertManager(t *testing.T) {
	t.Chdir(t.TempDir())
	m, err := NewManager(Config{AutoTLS: true, Domains: []string{"example.com"}, ACMEEndpoint: "http://127.0.0.1:1/dir"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := m.autocertManager()
	if err := m.RenewCertificates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.autocertManager() == old {
		t.Fatal("autocert manager not replaced; in-memory cert would keep being served")
	}
	if m.HTTPChallengeHandler() == nil {
		t.Fatal("nil challenge handler")
	}
}
