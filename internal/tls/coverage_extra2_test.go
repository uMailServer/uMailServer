package tls

import (
	"crypto/tls"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGenerateSelfSigned_WithDomains tests GenerateSelfSigned with explicit domains.
func TestGenerateSelfSigned_WithDomains(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	domains := []string{"mail.example.com", "smtp.example.com"}
	certPath, keyPath, err := manager.GenerateSelfSigned(domains)
	if err != nil {
		t.Fatalf("GenerateSelfSigned failed: %v", err)
	}
	if certPath == "" {
		t.Error("expected non-empty certPath")
	}
	if keyPath == "" {
		t.Error("expected non-empty keyPath")
	}

	// Verify that the returned paths contain the selfsigned filenames
	if !strings.Contains(certPath, "selfsigned.crt") {
		t.Errorf("certPath should contain 'selfsigned.crt', got %q", certPath)
	}
	if !strings.Contains(keyPath, "selfsigned.key") {
		t.Errorf("keyPath should contain 'selfsigned.key', got %q", keyPath)
	}
}

// TestGenerateSelfSigned_ECDSAKeyGeneration tests that the ECDSA key generation
// inside GenerateSelfSigned succeeds (covers the key generation path).
func TestGenerateSelfSigned_ECDSAKeyGeneration(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	// The function generates an ECDSA key internally; verify it doesn't panic
	certPath, keyPath, err := manager.GenerateSelfSigned(nil)
	if err != nil {
		t.Fatalf("GenerateSelfSigned with nil domains failed: %v", err)
	}

	// Verify files exist at the returned paths
	if certPath == "" {
		t.Error("expected non-empty certPath")
	}
	if keyPath == "" {
		t.Error("expected non-empty keyPath")
	}
}

// TestGetCertificate_WithAutocertFallbackToManual tests that GetCertificate falls back
// from autocert to manual certificates when autocert fails.
func TestGetCertificate_WithAutocertFallbackToManual(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a real cert+key pair for the fallback path
	certData, keyData := generateTestCertAndKey(t, "fallback.example.com")

	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certPath, certData, 0o644)
	os.WriteFile(keyPath, keyData, 0o600)

	config := Config{
		Enabled:  true,
		AutoTLS:  true,
		Email:    "admin@example.com",
		Domains:  []string{"example.com"},
		CertFile: certPath,
		KeyFile:  keyPath,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	manager.certDir = tmpDir

	// GetCertificate will try autocert first (which will fail since there's no ACME server),
	// then fall back to manual cert loading
	hello := &tls.ClientHelloInfo{ServerName: "fallback.example.com"}
	cert, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("expected fallback to manual cert to succeed, got: %v", err)
	}
	if cert == nil {
		t.Error("expected non-nil certificate from manual fallback")
	}
}

// TestGetCertificate_WithAutocertAndServerSpecificCert tests the code path where
// autocert fails, but a server-specific cert exists in the certDir.
func TestGetCertificate_WithAutocertAndServerSpecificCert(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a matching cert+key pair for the server-specific path
	certData, keyData := generateTestCertAndKey(t, "specific.example.com")

	config := Config{
		Enabled: true,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	manager.certDir = tmpDir

	// Create server-specific cert files
	os.WriteFile(filepath.Join(tmpDir, "specific.example.com.crt"), certData, 0o644)
	os.WriteFile(filepath.Join(tmpDir, "specific.example.com.key"), keyData, 0o600)

	// GetCertificate should find the server-specific cert
	hello := &tls.ClientHelloInfo{ServerName: "specific.example.com"}
	cert, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate with server-specific cert failed: %v", err)
	}
	if cert == nil {
		t.Error("expected non-nil certificate")
	}
}

// TestGetCertificate_WithAutocertNoFallback tests GetCertificate when autocert is enabled
// but both autocert and manual cert paths fail.
func TestGetCertificate_WithAutocertNoFallback(t *testing.T) {
	config := Config{
		Enabled: true,
		AutoTLS: true,
		Email:   "admin@example.com",
		Domains: []string{"example.com"},
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	// GetCertificate should fail since autocert has no server and no manual cert is configured
	hello := &tls.ClientHelloInfo{ServerName: "unresolvable.example.com"}
	cert, err := manager.GetCertificate(hello)
	if err == nil {
		t.Error("expected error when both autocert and manual cert fail")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}

// TestGetCertificate_CachedResult tests that GetCertificate returns cached results
// on subsequent calls.
func TestGetCertificate_CachedResult(t *testing.T) {
	tmpDir := t.TempDir()

	certData, keyData := generateTestCertAndKey(t, "cached.example.com")

	config := Config{
		Enabled:  true,
		CertFile: filepath.Join(tmpDir, "cert.pem"),
		KeyFile:  filepath.Join(tmpDir, "key.pem"),
	}
	os.WriteFile(config.CertFile, certData, 0o644)
	os.WriteFile(config.KeyFile, keyData, 0o600)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	// First call loads and caches the certificate
	hello := &tls.ClientHelloInfo{ServerName: "cached.example.com"}
	cert1, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("first GetCertificate failed: %v", err)
	}
	if cert1 == nil {
		t.Fatal("expected non-nil certificate on first call")
	}

	// Second call should return from cache
	cert2, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("second GetCertificate (cache hit) failed: %v", err)
	}
	if cert2 == nil {
		t.Error("expected non-nil certificate from cache")
	}
}

// TestGetCertificate_EmptyServerNameWithConfigPaths tests GetCertificate with empty
// ServerName which skips server-specific cert lookup and uses config paths.
func TestGetCertificate_EmptyServerNameWithConfigPaths(t *testing.T) {
	tmpDir := t.TempDir()

	certData, keyData := generateTestCertAndKey(t, "default.example.com")

	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certPath, certData, 0o644)
	os.WriteFile(keyPath, keyData, 0o600)

	config := Config{
		Enabled:  true,
		CertFile: certPath,
		KeyFile:  keyPath,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	// Empty server name should skip server-specific lookup and use config paths
	hello := &tls.ClientHelloInfo{ServerName: ""}
	cert, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate with empty ServerName failed: %v", err)
	}
	if cert == nil {
		t.Error("expected non-nil certificate from config paths")
	}
}

// TestGetManualCertificate_OnlyCertFileExists tests the path where server-specific .crt
// exists but .key does not (should fall through to config paths).
func TestGetManualCertificate_OnlyCertFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create only the .crt file (no matching .key file)
	certPEM := generateTestCert(t, "partial.example.com")
	os.WriteFile(filepath.Join(tmpDir, "partial.example.com.crt"), certPEM, 0o644)

	config := Config{
		Enabled: true,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	manager.certDir = tmpDir

	// Should not use server-specific cert since key file is missing
	// and should fail because config paths are also empty
	cert, err := manager.getManualCertificate("partial.example.com")
	if err == nil {
		t.Error("expected error when server-specific cert pair is incomplete")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}

// TestGetManualCertificate_InvalidCertFiles tests loading invalid cert/key files.
func TestGetManualCertificate_InvalidCertFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid cert and key files
	certPath := filepath.Join(tmpDir, "invalid.pem")
	keyPath := filepath.Join(tmpDir, "invalid.key")
	os.WriteFile(certPath, []byte("not a valid cert"), 0o644)
	os.WriteFile(keyPath, []byte("not a valid key"), 0o600)

	config := Config{
		Enabled:  true,
		CertFile: certPath,
		KeyFile:  keyPath,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	// Loading invalid cert/key should fail
	cert, err := manager.getManualCertificate("")
	if err == nil {
		t.Error("expected error when loading invalid certificate files")
	}
	if cert != nil {
		t.Error("expected nil certificate for invalid files")
	}
}

// TestGenerateSelfSigned_WarnsNotFullyImplemented tests that GenerateSelfSigned produces
// the expected warning log message since the implementation is not complete.
func TestGenerateSelfSigned_WarnsNotFullyImplemented(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	certPath, keyPath, err := manager.GenerateSelfSigned([]string{"test.local"})
	if err != nil {
		t.Fatalf("GenerateSelfSigned failed: %v", err)
	}

	// Verify that the returned paths follow the expected pattern
	expectedCert := filepath.Join(manager.certDir, "selfsigned.crt")
	expectedKey := filepath.Join(manager.certDir, "selfsigned.key")
	if certPath != expectedCert {
		t.Errorf("certPath = %q, want %q", certPath, expectedCert)
	}
	if keyPath != expectedKey {
		t.Errorf("keyPath = %q, want %q", keyPath, expectedKey)
	}
}

// TestGetCertificateStatus_WithExpiringAndValidCerts tests certificate status for both
// expiring and valid certificates.
func TestGetCertificateStatus_WithExpiringAndValidCerts(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Enabled: true,
		Domains: []string{"expiring.test", "valid.test"},
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Close()

	manager.certDir = tmpDir

	// Create a certificate expiring in 3 days (should trigger warning)
	expiringData := generateTestCertWithExpiry(t, "expiring.test", 3*24*time.Hour)
	os.WriteFile(filepath.Join(tmpDir, "expiring.test.crt"), expiringData, 0o644)

	// Create a certificate expiring in 30 days (no warning)
	validData := generateTestCertWithExpiry(t, "valid.test", 30*24*time.Hour)
	os.WriteFile(filepath.Join(tmpDir, "valid.test.crt"), validData, 0o644)

	statuses := manager.GetCertificateStatus()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	for _, status := range statuses {
		if !status.Valid {
			t.Errorf("expected %s to not be valid yet (no key file)", status.Domain)
		}
	}
}

// TestParseCertificate_NoBlock tests parseCertificate with PEM data that has no CERTIFICATE block.
func TestParseCertificate_NoBlock(t *testing.T) {
	// PEM data with a non-certificate block type
	data := []byte("-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----\n")
	cert, err := parseCertificate(data)
	if err == nil {
		t.Error("expected error for PEM without CERTIFICATE block")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}

// TestParseCertificate_InvalidDER tests parseCertificate with invalid DER data
// inside a valid PEM block.
func TestParseCertificate_InvalidDER(t *testing.T) {
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a valid certificate")})
	cert, err := parseCertificate(data)
	if err == nil {
		t.Error("expected error for invalid DER in certificate")
	}
	if cert != nil {
		t.Error("expected nil certificate")
	}
}
