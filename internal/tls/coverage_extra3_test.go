package tls

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"os"
	"testing"
)

func testManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	return m
}

func TestVerifyClientCert_Nil(t *testing.T) {
	m := testManager(t, Config{Enabled: false})
	_, err := m.VerifyClientCert(nil)
	if err == nil {
		t.Error("expected error for nil cert")
	}
}

func TestVerifyClientCert_Email(t *testing.T) {
	m := testManager(t, Config{Enabled: false})
	cert := &x509.Certificate{
		EmailAddresses: []string{"user@example.com"},
	}
	id, err := m.VerifyClientCert(cert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "user@example.com" {
		t.Errorf("expected user@example.com, got %s", id)
	}
}

func TestVerifyClientCert_CommonName(t *testing.T) {
	m := testManager(t, Config{Enabled: false})
	cert := &x509.Certificate{
		Subject: pkix.Name{CommonName: "test-client"},
	}
	id, err := m.VerifyClientCert(cert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "test-client" {
		t.Errorf("expected test-client, got %s", id)
	}
}

func TestVerifyClientCert_NoIdentity(t *testing.T) {
	m := testManager(t, Config{Enabled: false})
	cert := &x509.Certificate{}
	_, err := m.VerifyClientCert(cert)
	if err == nil {
		t.Error("expected error when no identity in certificate")
	}
}

func TestGetTLSConfigWithClientAuth_VerifyIfGiven(t *testing.T) {
	m := testManager(t, Config{
		Enabled:    true,
		ClientAuth: true,
		MinVersion: 0,
	})

	cfg := m.GetTLSConfigWithClientAuth(false)
	if cfg.ClientAuth == 0 {
		t.Error("expected non-zero ClientAuth mode")
	}
}

func TestGetTLSConfigWithClientAuth_ModeOverride(t *testing.T) {
	m := testManager(t, Config{
		Enabled:        true,
		ClientAuthMode: 4, // tls.RequireAnyClientCert
		MinVersion:     0,
	})

	cfg := m.GetTLSConfigWithClientAuth(false)
	if cfg.ClientAuth != 4 {
		t.Errorf("expected ClientAuth mode 4, got %d", cfg.ClientAuth)
	}
}

func TestGetTLSConfigWithClientAuth_MissingCAFile(t *testing.T) {
	m := testManager(t, Config{
		Enabled:      true,
		ClientAuth:   true,
		ClientCAFile: "/nonexistent/ca.pem",
		MinVersion:   0,
	})

	cfg := m.GetTLSConfigWithClientAuth(false)
	if cfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
}
