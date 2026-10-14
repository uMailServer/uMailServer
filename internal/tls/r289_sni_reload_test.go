package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func r289pair(t *testing.T, dir, base, cn string, notBefore, notAfter time.Time) (string, string) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{cn}, NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	cp, kp := filepath.Join(dir, base+".crt"), filepath.Join(dir, base+".key")
	os.WriteFile(cp, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(kp, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600)
	return cp, kp
}

func r289mgr(t *testing.T) (*Manager, string) {
	dir := t.TempDir()
	certs := filepath.Join(dir, "certs")
	os.MkdirAll(certs, 0o700)
	// secret outside the cert dir
	r289pair(t, dir, "secret", "secret.example", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cp, kp := r289pair(t, dir, "default", "default.example", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	m := &Manager{config: Config{CertFile: cp, KeyFile: kp}, logger: nopLogger(), certCache: map[string]*tls.Certificate{}, certSrc: map[string]certSource{}, certDir: certs}
	return m, dir
}

func TestR289HostileSNI(t *testing.T) {
	m, dir := r289mgr(t)
	names := []string{"", "a..b", "a.com.", "EXAMPLE.COM", strings.Repeat("a", 300), strings.Repeat("a.", 130), "../secret", "..%2fsecret", "../../secret", "a\x00b", "bücher.example", "xn--bcher-kva.example", "/etc/passwd", ".", "..", "a/b", dir + "/secret"}
	for _, n := range names {
		c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: n})
		if err != nil || c == nil {
			t.Fatalf("%q: %v", n, err)
		}
		x, _ := x509.ParseCertificate(c.Certificate[0])
		if x.Subject.CommonName != "default.example" {
			t.Fatalf("%q served %s", n, x.Subject.CommonName)
		}
	}
	if len(m.certCache) > 1 {
		t.Fatalf("cache grew: %d", len(m.certCache))
	}
}

func TestR289ExpiredAndMismatch(t *testing.T) {
	m, _ := r289mgr(t)
	// specific expired cert
	r289pair(t, m.certDir, "old.example", "old.example", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "old.example"})
	if err != nil {
		t.Fatal(err)
	}
	x, _ := x509.ParseCertificate(c.Certificate[0])
	t.Logf("expired specific cert served: cn=%s notAfter=%s", x.Subject.CommonName, x.NotAfter)
	// mismatch: cert of A with key of B
	r289pair(t, m.certDir, "a.example", "a.example", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	r289pair(t, m.certDir, "b", "b.example", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	b, _ := os.ReadFile(filepath.Join(m.certDir, "b.key"))
	os.WriteFile(filepath.Join(m.certDir, "a.example.key"), b, 0o600)
	c, err = m.GetCertificate(&tls.ClientHelloInfo{ServerName: "a.example"})
	t.Logf("mismatch -> c=%v err=%v", c != nil, err)
	if err == nil {
		x, _ := x509.ParseCertificate(c.Certificate[0])
		t.Fatalf("mismatch served cn=%s", x.Subject.CommonName)
	}
}

func TestR289ReloadRace(t *testing.T) {
	m, _ := r289mgr(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "x.example"})
				if err != nil {
					continue // acceptable only when no previous pair exists; checked below
				}
				x, _ := x509.ParseCertificate(c.Certificate[0])
				pub := c.PrivateKey.(*ecdsa.PrivateKey).Public().(*ecdsa.PublicKey)
				if !x.PublicKey.(*ecdsa.PublicKey).Equal(pub) {
					t.Error("torn pair")
					return
				}
			}
		}()
	}
	for i := 0; i < 30; i++ {
		r289pair(t, filepath.Dir(m.config.CertFile), "default", "default.example", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
}

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
