package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeKeyPair generates a self-signed cert/key pair and writes it as
// <dir>/<base>.crt and <dir>/<base>.key. Serial is used to distinguish
// generated pairs in assertions.
func writeKeyPair(t *testing.T, dir, base string, serial int64) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		Subject:      pkix.Name{CommonName: "localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, base+".crt"), certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, base+".key"), keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

// TestProof_TLS_SNICacheIsBounded pins the memory contract of the certificate
// cache: it must not grow with every name an attacker sends in the TLS
// ClientHello.
//
// getManualCertificate (internal/tls/manager.go:123) is reached from
// tls.Config.GetCertificate on every TLS listener (SMTP submission/relay,
// IMAP, ManageSieve), and hello.ServerName is attacker-controlled. When the
// fallback certificate is used (no per-domain file exists), the loaded
// certificate is cached under the attacker-chosen name with no cap and no
// eviction, so every distinct SNI grows the cache by one parsed certificate.
func TestProof_TLS_SNICacheIsBounded(t *testing.T) {
	tmp := t.TempDir()
	writeKeyPair(t, tmp, "default", 1)

	m, err := NewManager(Config{
		Enabled:  true,
		CertFile: filepath.Join(tmp, "default.crt"),
		KeyFile:  filepath.Join(tmp, "default.key"),
	}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const attackerSNIs = 500
	for i := 0; i < attackerSNIs; i++ {
		sni := fmt.Sprintf("attacker-%d.evil.example", i)
		if _, err := m.getManualCertificate(sni); err != nil {
			t.Fatalf("getManualCertificate(%q): %v", sni, err)
		}
	}

	m.certMu.RLock()
	got := len(m.certCache)
	m.certMu.RUnlock()

	if got > 2 {
		t.Errorf("FAIL: certificate cache holds %d entries after %d attacker-supplied "+
			"SNI names; the cache is keyed on attacker-controlled SNI with no cap and "+
			"no eviction, so every distinct name a client sends in the ClientHello grows "+
			"it by one parsed certificate -- a pre-authentication memory DoS on every "+
			"TLS listener", got, attackerSNIs)
	}
}

// TestProof_TLS_SNICache_Control_SameNameDeduplicated is the control: repeated
// identical SNI names must not grow the cache, before and after the fix.
func TestProof_TLS_SNICache_Control_SameNameDeduplicated(t *testing.T) {
	tmp := t.TempDir()
	writeKeyPair(t, tmp, "default", 1)

	m, err := NewManager(Config{
		Enabled:  true,
		CertFile: filepath.Join(tmp, "default.crt"),
		KeyFile:  filepath.Join(tmp, "default.key"),
	}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	for i := 0; i < 50; i++ {
		if _, err := m.getManualCertificate("mail.example.com"); err != nil {
			t.Fatalf("getManualCertificate: %v", err)
		}
	}

	m.certMu.RLock()
	got := len(m.certCache)
	m.certMu.RUnlock()

	if got != 1 {
		t.Errorf("FAIL: repeated identical SNI should deduplicate to 1 cache entry, got %d", got)
	}
}

// TestProof_TLS_SNICache_Control_PerDomainCertStillServed is the second
// control: the per-domain certificate feature must keep working. A name with
// its own certificate file in certDir must be served that certificate, not the
// fallback.
func TestProof_TLS_SNICache_Control_PerDomainCertStillServed(t *testing.T) {
	tmp := t.TempDir()
	writeKeyPair(t, tmp, "default", 1)

	m, err := NewManager(Config{
		Enabled:  true,
		CertFile: filepath.Join(tmp, "default.crt"),
		KeyFile:  filepath.Join(tmp, "default.key"),
	}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// NewManager puts certDir at ./certs relative to the package directory.
	writeKeyPair(t, m.certDir, "mail.example.com", 42)

	cert, err := m.getManualCertificate("mail.example.com")
	if err != nil {
		t.Fatalf("getManualCertificate(per-domain): %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatalf("no certificate returned")
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse served cert: %v", err)
	}
	if parsed.SerialNumber.Int64() != 42 {
		t.Errorf("FAIL: per-domain cert not served: serial %d, want 42", parsed.SerialNumber.Int64())
	}
}
