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
	"testing"
	"time"
)

func writeCertR155(t *testing.T, path string, notBefore, notAfter time.Time) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"}, NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// F6370: an expired certificate must not be reported as Valid.
func TestR155_ExpiredCertNotValid(t *testing.T) {
	m := &Manager{config: Config{Domains: []string{"exp.example.com", "future.example.com"}}, certDir: t.TempDir()}
	writeCertR155(t, filepath.Join(m.certDir, "exp.example.com.crt"), time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
	writeCertR155(t, filepath.Join(m.certDir, "future.example.com.crt"), time.Now().Add(time.Hour), time.Now().Add(48*time.Hour))
	for _, s := range m.GetCertificateStatus() {
		if s.Valid || s.Error == "" {
			t.Errorf("%s: want invalid with error, got %+v", s.Domain, s)
		}
	}
}

// F6371: pre-existing cert dir with loose permissions is tightened.
func TestR155_CertDirPermsTightened(t *testing.T) {
	old, _ := os.Getwd()
	d := t.TempDir()
	os.Chdir(d)
	defer os.Chdir(old)
	if err := os.Mkdir("certs", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(Config{}, nil); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat("certs")
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("certs dir perm %v", fi.Mode().Perm())
	}
}
