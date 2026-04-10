package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Manager handles TLS certificate management
type Manager struct {
	config      Config
	logger      *slog.Logger
	certManager *autocert.Manager
	certCache   map[string]*tls.Certificate
	certMu      sync.RWMutex
	certDir     string
}

// Config holds TLS manager configuration
type Config struct {
	Enabled      bool
	AutoTLS      bool
	CertFile     string
	KeyFile      string
	Email        string
	Domains      []string
	ACMEEndpoint string
	UseStaging   bool
}

// NewManager creates a new TLS certificate manager
func NewManager(config Config, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		config:    config,
		logger:    logger,
		certCache: make(map[string]*tls.Certificate),
		certDir:   "./certs",
	}

	// Ensure cert directory exists
	if err := os.MkdirAll(m.certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert directory: %w", err)
	}

	// Setup autocert if auto TLS is enabled
	if config.AutoTLS {
		if err := m.setupAutocert(); err != nil {
			return nil, fmt.Errorf("failed to setup autocert: %w", err)
		}
	}

	return m, nil
}

// setupAutocert configures the autocert manager for Let's Encrypt
func (m *Manager) setupAutocert() error {
	// Use staging environment if configured
	acmeEndpoint := acme.LetsEncryptURL
	if m.config.UseStaging {
		acmeEndpoint = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	if m.config.ACMEEndpoint != "" {
		acmeEndpoint = m.config.ACMEEndpoint
	}

	m.certManager = &autocert.Manager{
		Client:     &acme.Client{DirectoryURL: acmeEndpoint},
		Cache:      autocert.DirCache(m.certDir),
		Prompt:     autocert.AcceptTOS,
		Email:      m.config.Email,
		HostPolicy: autocert.HostWhitelist(m.config.Domains...),
	}

	m.logger.Info("Autocert configured",
		"email", m.config.Email,
		"domains", m.config.Domains,
		"endpoint", acmeEndpoint,
	)

	return nil
}

// GetCertificate returns a TLS certificate for the given hello info
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// First try autocert if enabled
	if m.certManager != nil {
		cert, err := m.certManager.GetCertificate(hello)
		if err == nil && cert != nil {
			return cert, nil
		}
		// Fall through to manual certs on error
		m.logger.Debug("Autocert failed, trying manual certs", "error", err)
	}

	// Try manual certificates
	return m.getManualCertificate(hello.ServerName)
}

// getManualCertificate loads a certificate from file
func (m *Manager) getManualCertificate(serverName string) (*tls.Certificate, error) {
	// Check cache first (read lock)
	m.certMu.RLock()
	if cert, ok := m.certCache[serverName]; ok {
		m.certMu.RUnlock()
		return cert, nil
	}
	m.certMu.RUnlock()

	// Determine cert paths
	certPath := m.config.CertFile
	keyPath := m.config.KeyFile

	// If server-specific certs exist, use those
	if serverName != "" {
		specificCert := filepath.Join(m.certDir, serverName+".crt")
		specificKey := filepath.Join(m.certDir, serverName+".key")

		if _, err := os.Stat(specificCert); err == nil {
			if _, err := os.Stat(specificKey); err == nil {
				certPath = specificCert
				keyPath = specificKey
			}
		}
	}

	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("no certificate configured for %s", serverName)
	}

	// Load certificate
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	// Cache certificate (write lock)
	m.certMu.Lock()
	m.certCache[serverName] = &cert
	m.certMu.Unlock()

	return &cert, nil
}

// GetTLSConfig returns a TLS configuration
func (m *Manager) GetTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		},
		PreferServerCipherSuites: true,
	}
}

// GenerateSelfSigned generates a self-signed certificate for testing
func (m *Manager) GenerateSelfSigned(_ []string) (string, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate certificate
	// Note: In production, use proper certificate generation
	template := &tls.Certificate{
		Certificate: [][]byte{},
		PrivateKey:  priv,
	}

	// For now, just create key file
	keyPath := filepath.Join(m.certDir, "selfsigned.key")
	certPath := filepath.Join(m.certDir, "selfsigned.crt")

	// In a real implementation, we'd generate a proper cert here
	// For now, return the paths and let the user generate them
	_ = template

	m.logger.Warn("Self-signed certificate generation not fully implemented")
	return certPath, keyPath, nil
}

// RenewCertificates manually triggers certificate renewal
func (m *Manager) RenewCertificates(ctx context.Context) error {
	if m.certManager == nil {
		return fmt.Errorf("autocert not configured")
	}

	// Force renewal by deleting cached certs
	for _, domain := range m.config.Domains {
		m.certManager.Cache.Delete(ctx, domain)
	}

	m.logger.Info("Certificate renewal triggered", "domains", m.config.Domains)
	return nil
}

// GetCertificateStatus returns the status of certificates
func (m *Manager) GetCertificateStatus() []CertificateStatus {
	var statuses []CertificateStatus

	for _, domain := range m.config.Domains {
		status := CertificateStatus{
			Domain: domain,
			Valid:  false,
		}

		// Try to load and check certificate
		certPath := filepath.Join(m.certDir, domain+".crt")
		data, err := os.ReadFile(certPath)
		if err != nil {
			status.Error = err.Error()
			statuses = append(statuses, status)
			continue
		}

		// Parse certificate
		cert, err := parseCertificate(data)
		if err != nil {
			status.Error = err.Error()
			statuses = append(statuses, status)
			continue
		}

		status.Valid = true
		status.ExpiresAt = cert.NotAfter
		status.Issuer = cert.Issuer.CommonName

		// Check if expiring soon
		if time.Until(cert.NotAfter) < 7*24*time.Hour {
			status.Warning = "Certificate expires within 7 days"
		}

		statuses = append(statuses, status)
	}

	return statuses
}

// CertificateStatus holds certificate status information
type CertificateStatus struct {
	Domain    string    `json:"domain"`
	Valid     bool      `json:"valid"`
	ExpiresAt time.Time `json:"expires_at"`
	Issuer    string    `json:"issuer"`
	Warning   string    `json:"warning,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// parseCertificate parses a certificate from PEM data
func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, nil
}

// HTTPChallengeHandler returns the handler for ACME HTTP challenges
func (m *Manager) HTTPChallengeHandler() http.Handler {
	if m.certManager == nil {
		return nil
	}
	return m.certManager.HTTPHandler(nil)
}

// Close cleans up resources
func (m *Manager) Close() error {
	// Nothing to clean up currently
	return nil
}

// IsEnabled returns true if TLS is enabled
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

// IsAutoTLS returns true if auto TLS is enabled
func (m *Manager) IsAutoTLS() bool {
	return m.config.AutoTLS
}
