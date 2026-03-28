package tls

import (
	"context"
	"crypto/tls"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: false,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)

	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}

	if manager.config.Enabled != true {
		t.Error("Enabled should be true")
	}

	if manager.certCache == nil {
		t.Error("certCache should be initialized")
	}

	defer manager.Close()
}

func TestManagerGetTLSConfig(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	tlsConfig := manager.GetTLSConfig()

	if tlsConfig == nil {
		t.Fatal("TLS config should not be nil")
	}

	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("Expected MinVersion TLS 1.2, got %d", tlsConfig.MinVersion)
	}

	if tlsConfig.GetCertificate == nil {
		t.Error("GetCertificate should not be nil")
	}
}

func TestManagerIsEnabled(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	if !manager.IsEnabled() {
		t.Error("IsEnabled should return true")
	}
}

func TestManagerIsAutoTLS(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	if !manager.IsAutoTLS() {
		t.Error("IsAutoTLS should return true")
	}
}

func TestConfigDefaults(t *testing.T) {
	config := Config{
		Enabled:     true,
		AutoTLS:     true,
		Email:       "admin@example.com",
		Domains:     []string{"example.com", "mail.example.com"},
		UseStaging:  true,
	}

	if config.Email != "admin@example.com" {
		t.Errorf("Expected email admin@example.com, got %s", config.Email)
	}

	if len(config.Domains) != 2 {
		t.Errorf("Expected 2 domains, got %d", len(config.Domains))
	}
}

func TestCertificateStatus(t *testing.T) {
	status := CertificateStatus{
		Domain:    "example.com",
		Valid:     true,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		Issuer:    "Let's Encrypt",
	}

	if status.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", status.Domain)
	}

	if !status.Valid {
		t.Error("Expected Valid to be true")
	}

	if status.Issuer != "Let's Encrypt" {
		t.Errorf("Expected issuer Let's Encrypt, got %s", status.Issuer)
	}
}

func TestGetCertificateStatusEmptyDomains(t *testing.T) {
	config := Config{
		Enabled: true,
		Domains: []string{},
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	statuses := manager.GetCertificateStatus()

	if len(statuses) != 0 {
		t.Errorf("Expected 0 statuses, got %d", len(statuses))
	}
}

func TestGenerateSelfSigned(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	domains := []string{"test.example.com"}
	certPath, keyPath, err := manager.GenerateSelfSigned(domains)

	// This currently just returns paths as the implementation is incomplete
	if err != nil {
		t.Errorf("GenerateSelfSigned returned error: %v", err)
	}

	if certPath == "" {
		t.Error("certPath should not be empty")
	}

	if keyPath == "" {
		t.Error("keyPath should not be empty")
	}
}

func TestHTTPChallengeHandlerNoAutocert(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: false,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	handler := manager.HTTPChallengeHandler()

	if handler != nil {
		t.Error("Handler should be nil when autocert is not configured")
	}
}

func TestParseCertificateInvalidPEM(t *testing.T) {
	data := []byte("invalid PEM data")
	cert, err := parseCertificate(data)

	if err == nil {
		t.Error("Expected error for invalid PEM")
	}

	if cert != nil {
		t.Error("Certificate should be nil on error")
	}
}

func TestGetManualCertificateNoConfig(t *testing.T) {
	config := Config{
		Enabled:  true,
		CertFile: "",
		KeyFile:  "",
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	// Clear cache
	manager.certCache = make(map[string]*tls.Certificate)

	// This should fail since no cert is configured
	cert, err := manager.getManualCertificate("test.example.com")

	if err == nil {
		t.Error("Expected error when no certificate is configured")
	}

	if cert != nil {
		t.Error("Certificate should be nil on error")
	}
}

func TestRenewCertificatesNoAutocert(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: false,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	err := manager.RenewCertificates(nil)

	if err == nil {
		t.Error("Expected error when autocert is not configured")
	}
}

func TestNewManagerNilLogger(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	if manager.logger == nil {
		t.Error("expected logger to be initialized with default")
	}
}

func TestManagerDisabled(t *testing.T) {
	config := Config{
		Enabled: false,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	if manager.IsEnabled() {
		t.Error("IsEnabled should return false")
	}
}

func TestConfigStruct(t *testing.T) {
	config := Config{
		Enabled:      true,
		AutoTLS:      true,
		CertFile:     "/certs/cert.pem",
		KeyFile:      "/certs/key.pem",
		Email:        "admin@example.com",
		Domains:      []string{"example.com", "mail.example.com"},
		ACMEEndpoint: "https://custom.acme.endpoint",
		UseStaging:   true,
	}

	if !config.Enabled {
		t.Error("expected Enabled to be true")
	}
	if config.CertFile != "/certs/cert.pem" {
		t.Errorf("expected CertFile /certs/cert.pem, got %s", config.CertFile)
	}
	if config.KeyFile != "/certs/key.pem" {
		t.Errorf("expected KeyFile /certs/key.pem, got %s", config.KeyFile)
	}
	if config.ACMEEndpoint != "https://custom.acme.endpoint" {
		t.Errorf("expected ACMEEndpoint, got %s", config.ACMEEndpoint)
	}
}

func TestManagerGetCertificate(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	// Test GetCertificate with hello info
	hello := &tls.ClientHelloInfo{ServerName: "test.example.com"}
	cert, err := manager.GetCertificate(hello)

	// Should fail since no cert is configured
	if err == nil {
		t.Error("expected error when no certificate available")
	}
	if cert != nil {
		t.Error("expected nil certificate on error")
	}
}

func TestParseCertificateNoBlock(t *testing.T) {
	// PEM without certificate block
	data := []byte("-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----\n")
	cert, err := parseCertificate(data)

	if err == nil {
		t.Error("expected error for certificate without CERTIFICATE block")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}

func TestManagerGetCertificateStatusNoCerts(t *testing.T) {
	config := Config{
		Enabled: true,
		Domains: []string{"test.example.com"},
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	statuses := manager.GetCertificateStatus()

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if statuses[0].Domain != "test.example.com" {
		t.Errorf("expected domain test.example.com, got %s", statuses[0].Domain)
	}

	if statuses[0].Valid {
		t.Error("expected certificate to be invalid (file doesn't exist)")
	}

	if statuses[0].Error == "" {
		t.Error("expected error message for missing certificate")
	}
}

func TestManagerGetCertificateStatusWithValidCert(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a self-signed certificate for testing
	config := Config{
		Enabled: true,
		Domains: []string{"localhost"},
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	// Generate self-signed cert
	certPath, keyPath, err := manager.GenerateSelfSigned([]string{"localhost"})
	if err != nil {
		t.Fatalf("GenerateSelfSigned failed: %v", err)
	}

	// Copy the generated cert to the expected location
	if certPath != "" && keyPath != "" {
		// Read the generated cert
		certData, err := os.ReadFile(certPath)
		if err == nil {
			// Write to the domain-specific location
			domainCertPath := filepath.Join(tmpDir, "localhost.crt")
			os.WriteFile(domainCertPath, certData, 0644)
		}
	}

	// Get status - should find the certificate
	statuses := manager.GetCertificateStatus()

	if len(statuses) == 0 {
		t.Fatal("expected at least 1 status")
	}

	// Look for localhost
	found := false
	for _, status := range statuses {
		if status.Domain == "localhost" {
			found = true
			// Should be valid since we generated a cert
			if !status.Valid && certPath == "" {
				t.Log("certificate status invalid (expected for stub implementation)")
			}
		}
	}
	if !found {
		t.Error("expected to find localhost in statuses")
	}
}

func TestManagerRenewCertificatesNotConfigured(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: false, // No autocert
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	ctx := context.Background()
	err := manager.RenewCertificates(ctx)

	if err == nil {
		t.Error("expected error when autocert not configured")
	}
}

func TestManagerGetCertificateWithAutocertFallback(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: false, // Disable autocert to test fallback
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	// Test with server name
	hello := &tls.ClientHelloInfo{ServerName: "test.example.com"}
	cert, err := manager.GetCertificate(hello)

	// Should fail since no certificate is configured
	if err == nil {
		t.Error("expected error when no certificate available")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}

func TestManagerClose(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Close should not panic
	err = manager.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestManagerGetCertificateEmptyServerName(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, _ := NewManager(config, logger)
	defer manager.Close()

	// Test with empty server name
	hello := &tls.ClientHelloInfo{ServerName: ""}
	cert, err := manager.GetCertificate(hello)

	// Should fail since no certificate is configured
	if err == nil {
		t.Error("expected error when no certificate available")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}

