package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regression tests for round 37 findings F5185-F5188 in getManualCertificate
// and GetCertificateStatus.

func regServedSerial(t *testing.T, m *Manager, sni string) int64 {
	t.Helper()
	c, err := m.getManualCertificate(sni)
	if err != nil {
		t.Fatalf("getManualCertificate(%q): %v", sni, err)
	}
	p, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatalf("parse served cert: %v", err)
	}
	return p.SerialNumber.Int64()
}

// regManager returns a manager whose fallback pair (serial 1) lives in root
// and whose certDir is root/certs.
func regManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	writeKeyPair(t, root, "default", 1)
	m, err := NewManager(Config{Enabled: true, CertFile: filepath.Join(root, "default.crt"), KeyFile: filepath.Join(root, "default.key")}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.certDir = filepath.Join(root, "certs")
	if err := os.MkdirAll(m.certDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return m, root
}

func regBump(t *testing.T, offset time.Duration, paths ...string) {
	t.Helper()
	ts := time.Now().Add(offset)
	for _, p := range paths {
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
}

// F5185: SNI host names are case-insensitive (RFC 6066 section 3).
func TestRegressionF5185_SNICaseInsensitive(t *testing.T) {
	m, _ := regManager(t)
	writeKeyPair(t, m.certDir, "mail.example.com", 42)
	for _, sni := range []string{"MAIL.Example.COM", "mail.example.com", "Mail.Example.Com."} {
		if got := regServedSerial(t, m, sni); got != 42 {
			t.Errorf("SNI %q served serial %d, want per-domain 42", sni, got)
		}
	}
	if got := regServedSerial(t, m, "Unknown.Example.COM"); got != 1 {
		t.Errorf("unknown SNI served serial %d, want fallback 1", got)
	}
	m.certMu.RLock()
	n := len(m.certCache)
	m.certMu.RUnlock()
	if n != 2 { // one per-domain entry + the shared fallback slot
		t.Errorf("cache holds %d entries, want 2 (case variants must share one)", n)
	}
}

// F5186: an attacker-controlled SNI must never select files outside certDir.
func TestRegressionF5186_SNIPathTraversal(t *testing.T) {
	m, root := regManager(t)
	writeKeyPair(t, root, "outside", 77)
	writeKeyPair(t, m.certDir, "mail.example.com", 42)
	for _, sni := range []string{"../outside", "..\\outside", "certs/../../outside", "/" + strings.TrimPrefix(filepath.Join(root, "outside"), "/"), "a/b"} {
		if got := regServedSerial(t, m, sni); got != 1 {
			t.Errorf("SNI %q served serial %d, want fallback 1", sni, got)
		}
		m.certMu.RLock()
		_, cached := m.certCache[sni]
		m.certMu.RUnlock()
		if cached {
			t.Errorf("SNI %q was cached under its own name", sni)
		}
	}
	if got := regServedSerial(t, m, "mail.example.com"); got != 42 {
		t.Errorf("legitimate per-domain SNI served %d, want 42", got)
	}
}

// F5187: a certificate replaced on disk is served without a restart, and a
// half-written renewal keeps the previous pair in service.
func TestRegressionF5187_ReloadChangedCertificate(t *testing.T) {
	m, root := regManager(t)
	writeKeyPair(t, m.certDir, "mail.example.com", 42)

	if got := regServedSerial(t, m, "other.example.com"); got != 1 {
		t.Fatalf("initial fallback serial %d", got)
	}
	if got := regServedSerial(t, m, "mail.example.com"); got != 42 {
		t.Fatalf("initial per-domain serial %d", got)
	}

	// Unchanged files: the cached pointer is reused (no reload).
	a, _ := m.getManualCertificate("other.example.com")
	b, _ := m.getManualCertificate("other.example.com")
	if a != b {
		t.Error("unchanged certificate was reloaded")
	}

	// Fallback renewed on disk.
	writeKeyPair(t, root, "default", 2)
	regBump(t, time.Hour, filepath.Join(root, "default.crt"), filepath.Join(root, "default.key"))
	if got := regServedSerial(t, m, "other.example.com"); got != 2 {
		t.Errorf("renewed fallback: served %d, want 2", got)
	}

	// Per-domain renewed on disk.
	writeKeyPair(t, m.certDir, "mail.example.com", 43)
	regBump(t, time.Hour, filepath.Join(m.certDir, "mail.example.com.crt"), filepath.Join(m.certDir, "mail.example.com.key"))
	if got := regServedSerial(t, m, "mail.example.com"); got != 43 {
		t.Errorf("renewed per-domain: served %d, want 43", got)
	}

	// Half-written renewal: new cert, old key. The previous pair stays in
	// service instead of failing the handshake.
	keyPath := filepath.Join(root, "default.key")
	oldKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	writeKeyPair(t, root, "default", 3)
	if err := os.WriteFile(keyPath, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	regBump(t, 2*time.Hour, filepath.Join(root, "default.crt"), keyPath)
	if got := regServedSerial(t, m, "other.example.com"); got != 2 {
		t.Errorf("mismatched pair mid-renewal: served %d, want previous 2", got)
	}
	// Renewal completes.
	writeKeyPair(t, root, "default", 4)
	regBump(t, 3*time.Hour, filepath.Join(root, "default.crt"), keyPath)
	if got := regServedSerial(t, m, "other.example.com"); got != 4 {
		t.Errorf("completed renewal: served %d, want 4", got)
	}
}

// F5188: GetCertificateStatus reads the autocert DirCache entry
// (<certDir>/<domain>: key PEM followed by the chain).
func TestRegressionF5188_StatusReadsAutocertCache(t *testing.T) {
	m := testManager(t, Config{Enabled: true, Domains: []string{"acme.example.com", "manual.example.com", "missing.example.com"}})
	m.certDir = t.TempDir()
	notAfter := time.Now().Add(3 * 24 * time.Hour).Truncate(time.Second).UTC()
	blob := auditF5188RegBlob(t, "acme.example.com", notAfter)
	if err := os.WriteFile(filepath.Join(m.certDir, "acme.example.com"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	writeKeyPair(t, m.certDir, "manual.example.com", 9)

	st := m.GetCertificateStatus()
	if len(st) != 3 {
		t.Fatalf("got %d statuses", len(st))
	}
	if !st[0].Valid || !st[0].ExpiresAt.Equal(notAfter) || st[0].Warning == "" {
		t.Errorf("acme cert: valid=%v expires=%v warning=%q err=%q", st[0].Valid, st[0].ExpiresAt, st[0].Warning, st[0].Error)
	}
	if !st[1].Valid {
		t.Errorf("manual cert not valid: %q", st[1].Error)
	}
	if st[2].Valid || !strings.Contains(st[2].Error, "missing.example.com.crt") {
		t.Errorf("missing cert: valid=%v err=%q", st[2].Valid, st[2].Error)
	}
	// A cache entry holding only a key must still be reported as unparsable.
	if _, err := parseCertificate(blob[:strings.Index(string(blob), "-----BEGIN CERTIFICATE")]); err == nil {
		t.Error("key-only PEM parsed as certificate")
	}
}

// auditF5188RegBlob (F5188) mirrors autocert.Manager.cachePut: private key PEM
// first, then the certificate chain, stored under the bare domain key.
func auditF5188RegBlob(t *testing.T, domain string, notAfter time.Time) []byte {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(5), DNSNames: []string{domain}, Subject: pkix.Name{CommonName: domain}, Issuer: pkix.Name{CommonName: domain}, NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(priv)
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
}

// F5187: handshakes running while a reload swaps the cached pair must be
// race-free (run with -race) and always get a usable certificate.
func TestRegressionF5187_ConcurrentReload(t *testing.T) {
	m, root := regManager(t)
	// Serve the pair once first: the invariant is that a cert already being
	// served survives a reload, and a cold first load racing the writer has
	// no previous pair to fall back on.
	if _, err := m.getManualCertificate("other.example.com"); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	start := make(chan struct{})
	errs := make(chan error, 8)
	for g := 0; g < 8; g++ {
		go func() {
			<-start
			for i := 0; i < 200; i++ {
				c, err := m.getManualCertificate("other.example.com")
				if err != nil || c == nil || len(c.Certificate) == 0 {
					errs <- err
					return
				}
			}
			errs <- nil
		}()
	}
	close(start)
	for i := int64(2); i < 6; i++ {
		writeKeyPair(t, root, "default", i)
		regBump(t, time.Duration(i)*time.Hour, filepath.Join(root, "default.crt"), filepath.Join(root, "default.key"))
	}
	for g := 0; g < 8; g++ {
		if err := <-errs; err != nil {
			t.Errorf("handshake during reload failed: %v", err)
		}
	}
	if got := regServedSerial(t, m, "other.example.com"); got != 5 {
		t.Errorf("after reloads served %d, want 5", got)
	}
}
