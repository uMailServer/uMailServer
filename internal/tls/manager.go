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
	"strings"
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
	certSrc     map[string]certSource // files each certCache entry was loaded from (F5187)
	certMu      sync.RWMutex
	certDir     string
}

// Config holds TLS manager configuration
type Config struct {
	Enabled           bool
	AutoTLS           bool
	CertFile          string
	KeyFile           string
	Email             string
	Domains           []string
	ACMEEndpoint      string
	UseStaging        bool
	MinVersion        uint16 // TLS version (e.g., tls.VersionTLS12, tls.VersionTLS13). Default: TLS 1.2
	ClientAuth        bool   // Enable client certificate authentication
	RequireClientCert bool   // Require client certificate (mTLS)
	ClientCAFile      string // CA file for client certificate verification
	ClientAuthMode    tls.ClientAuthType
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
		certSrc:   make(map[string]certSource),
		certDir:   "./certs",
	}

	// Ensure cert directory exists
	if err := os.MkdirAll(m.certDir, 0o700); err != nil {
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

// defaultCertCacheKey is the single cache slot holding the fallback
// certificate (the one loaded from Config.CertFile/KeyFile). Serving it from
// one fixed key instead of one key per requested SNI keeps the cache from
// growing with every name an attacker sends in the ClientHello: SNI is
// attacker-controlled and arrives before any authentication.
const defaultCertCacheKey = "\x00default-cert"

// fileStamp identifies one version of a certificate or key file on disk.
type fileStamp struct {
	mod  time.Time
	size int64
}

// certSource records the files a cached certificate was loaded from, so a
// certificate replaced on disk (certbot, manual renewal) is picked up without
// a restart (F5187).
type certSource struct {
	certPath, keyPath   string
	certStamp, keyStamp fileStamp
}

func statFile(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{mod: fi.ModTime(), size: fi.Size()}, nil
}

// normalizeServerName lowercases the SNI value (host names are
// case-insensitive, RFC 6066 section 3 / RFC 4343, F5185) and returns "" for
// anything that is not a plain host name, so an attacker-controlled SNI can
// never be used as a path component outside certDir (F5186).
func normalizeServerName(serverName string) string {
	name := strings.ToLower(strings.TrimSuffix(serverName, "."))
	if name == "" || name[0] == '.' || strings.Contains(name, "..") {
		return ""
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' && c != '_' {
			return ""
		}
	}
	return name
}

// cachedCertificate returns the cached certificate for key. fresh is false
// when the files it was loaded from have changed since, in which case the
// caller should reload and may fall back to the returned (stale) certificate
// if the reload fails. If the files cannot be stat'ed the cached copy is kept.
func (m *Manager) cachedCertificate(key string) (cert *tls.Certificate, fresh bool) {
	m.certMu.RLock()
	cert, ok := m.certCache[key]
	src, hasSrc := m.certSrc[key]
	m.certMu.RUnlock()
	if !ok {
		return nil, false
	}
	if !hasSrc {
		return cert, true
	}
	cs, cerr := statFile(src.certPath)
	ks, kerr := statFile(src.keyPath)
	if cerr != nil || kerr != nil {
		return cert, true
	}
	return cert, cs == src.certStamp && ks == src.keyStamp
}

// getManualCertificate loads a certificate from file
func (m *Manager) getManualCertificate(serverName string) (*tls.Certificate, error) {
	name := normalizeServerName(serverName)

	// Check cache first (read lock)
	var stale *tls.Certificate
	if name != "" {
		if cert, fresh := m.cachedCertificate(name); cert != nil {
			if fresh {
				return cert, nil
			}
			stale = cert
		}
	}

	// Determine cert paths
	certPath := m.config.CertFile
	keyPath := m.config.KeyFile

	// If server-specific certs exist, use those
	specific := false
	if name != "" {
		specificCert := filepath.Join(m.certDir, name+".crt")
		specificKey := filepath.Join(m.certDir, name+".key")

		if _, err := os.Stat(specificCert); err == nil {
			if _, err := os.Stat(specificKey); err == nil {
				certPath = specificCert
				keyPath = specificKey
				specific = true
			}
		}
	}

	// Resolve per-domain certificates before consulting the shared fallback.
	if !specific {
		stale = nil
		if cert, fresh := m.cachedCertificate(defaultCertCacheKey); cert != nil {
			if fresh {
				return cert, nil
			}
			stale = cert
		}
	}

	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("no certificate configured for %s", serverName)
	}

	// Stamp the files before reading them, so a write racing with the load is
	// detected as a change on the next handshake rather than missed.
	certStamp, cerr := statFile(certPath)
	keyStamp, kerr := statFile(keyPath)

	// Load certificate
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		if stale != nil {
			// A renewal may be mid-write (cert updated, key not yet): keep
			// serving the previous pair and retry on the next handshake.
			m.logger.Warn("Reloading changed certificate failed; serving previous one", "cert", certPath, "error", err)
			return stale, nil
		}
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	// Cache certificate (write lock). Only per-domain certificates are cached
	// under their own name; the fallback certificate gets one shared slot, so
	// the cache is bounded by the number of domains that actually have a
	// certificate plus one, and cannot be flooded with attacker-chosen names.
	key := defaultCertCacheKey
	if specific {
		key = name
	}
	m.certMu.Lock()
	m.certCache[key] = &cert
	if cerr == nil && kerr == nil {
		if m.certSrc == nil {
			m.certSrc = make(map[string]certSource)
		}
		m.certSrc[key] = certSource{certPath: certPath, keyPath: keyPath, certStamp: certStamp, keyStamp: keyStamp}
	} else {
		delete(m.certSrc, key)
	}
	m.certMu.Unlock()

	return &cert, nil
}

// GetTLSConfig returns a TLS configuration
func (m *Manager) GetTLSConfig() *tls.Config {
	return m.GetTLSConfigWithClientAuth(false)
}

// GetTLSConfigWithClientAuth returns a TLS configuration with optional client certificate auth
func (m *Manager) GetTLSConfigWithClientAuth(requireClientCert bool) *tls.Config {
	// Use configured min version or default to TLS 1.2
	minVersion := m.config.MinVersion
	if minVersion < tls.VersionTLS12 {
		minVersion = tls.VersionTLS12
	}

	// Determine client auth mode
	clientAuth := tls.NoClientCert
	if m.config.ClientAuth || requireClientCert {
		if m.config.RequireClientCert || requireClientCert {
			clientAuth = tls.RequireAndVerifyClientCert
		} else {
			clientAuth = tls.VerifyClientCertIfGiven
		}
	}
	// Allow override from config
	if m.config.ClientAuthMode != 0 {
		clientAuth = m.config.ClientAuthMode
	}

	var clientCAs *x509.CertPool
	if m.config.ClientCAFile != "" {
		clientCAs = x509.NewCertPool()
		caData, err := os.ReadFile(m.config.ClientCAFile)
		if err == nil {
			clientCAs.AppendCertsFromPEM(caData)
		} else {
			m.logger.Warn("Failed to load client CA file", "file", m.config.ClientCAFile, "error", err)
		}
	}

	// #nosec G402 -- MinVersion is runtime-validated to be >= TLS 1.2 above
	return &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     minVersion,
		MaxVersion:     tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		},
		ClientAuth: clientAuth,
		ClientCAs:  clientCAs,
	}
}

// VerifyClientCert verifies a client certificate and returns the identity
func (m *Manager) VerifyClientCert(cert *x509.Certificate) (string, error) {
	if cert == nil {
		return "", fmt.Errorf("no certificate provided")
	}

	// Extract email from certificate subject
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0], nil
	}

	// Use common name if no email
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName, nil
	}

	return "", fmt.Errorf("no identity found in certificate")
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
	var renewalErr error
	for _, domain := range m.config.Domains {
		if err := m.certManager.Cache.Delete(ctx, domain); err != nil {
			m.logger.Warn("Failed to delete cached cert", "domain", domain, "error", err)
			if renewalErr == nil {
				renewalErr = fmt.Errorf("failed to delete cached certificate for %s: %w", domain, err)
			}
		}
	}
	if renewalErr != nil {
		return renewalErr
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
		data, err := os.ReadFile(filepath.Clean(certPath))
		if os.IsNotExist(err) {
			// ACME certificates live in the autocert DirCache under the bare
			// domain name (key PEM followed by the chain), F5188.
			if acmeData, acmeErr := os.ReadFile(filepath.Join(m.certDir, domain)); acmeErr == nil {
				data, err = acmeData, nil
			}
		}
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

// parseCertificate parses the first CERTIFICATE block from PEM data, skipping
// any preceding blocks such as the private key in an autocert cache entry.
func parseCertificate(data []byte) (*x509.Certificate, error) {
	var block *pem.Block
	for {
		block, data = pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("failed to parse certificate PEM")
		}
		if block.Type == "CERTIFICATE" {
			break
		}
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
