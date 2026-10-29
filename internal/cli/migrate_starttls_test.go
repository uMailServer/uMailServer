package cli

// F7790: imap:// sources were dialled in plain text and never upgraded with
// STARTTLS, so every server that refuses LOGIN before TLS (Dovecot's default,
// uMailServer itself) failed with "LOGIN requires TLS".

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func starttlsTestCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "imap.example.test"},
		DNSNames:     []string{"127.0.0.1", "imap.example.test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// starttlsIMAPServer refuses LOGIN until STARTTLS has completed.
func starttlsIMAPServer(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				r := bufio.NewReader(conn)
				w := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
				w("* OK [CAPABILITY IMAP4rev1 STARTTLS LOGINDISABLED] ready")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						_ = conn.Close()
						return
					}
					f := strings.Fields(line)
					if len(f) < 2 {
						continue
					}
					switch strings.ToUpper(f[1]) {
					case "CAPABILITY":
						w("* CAPABILITY IMAP4rev1 STARTTLS LOGINDISABLED")
						w(f[0] + " OK done")
					case "STARTTLS":
						w(f[0] + " OK begin TLS")
						mig69ServeIMAPSession(tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}}), false)
						return
					case "LOGIN":
						w(f[0] + " NO LOGIN requires TLS - use STARTTLS first")
					default:
						w(f[0] + " BAD unsupported")
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestMigrateIMAPUpgradesPlainConnectionWithSTARTTLS(t *testing.T) {
	cert, pool := starttlsTestCert(t)
	addr := starttlsIMAPServer(t, cert)
	ms, base := mig69Store(t)
	mm := NewMigrationManager(nil, ms, nil)
	mm.tlsConfig = &tls.Config{RootCAs: pool}
	err := mm.MigrateFromIMAP(MigrateOptions{
		SourceURL:  "imap://" + addr,
		Username:   "username",
		Password:   "password",
		TargetUser: "dave@example.test",
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if msgs := mig69Stored(t, base, "dave@example.test"); len(msgs) != 1 || msgs[0] != mig69Msg {
		t.Errorf("stored %q, want the one source message", msgs)
	}
}

func TestMigrateIMAPRefusesUntrustedSTARTTLSCertificate(t *testing.T) {
	cert, _ := starttlsTestCert(t)
	addr := starttlsIMAPServer(t, cert)
	ms, base := mig69Store(t)
	mm := NewMigrationManager(nil, ms, nil) // system roots: the test CA is unknown
	err := mm.MigrateFromIMAP(MigrateOptions{
		SourceURL:  "imap://" + addr,
		Username:   "username",
		Password:   "password",
		TargetUser: "dave@example.test",
	})
	if err == nil || !strings.Contains(err.Error(), "start TLS") {
		t.Fatalf("want a TLS failure, got %v", err)
	}
	if msgs := mig69Stored(t, base, "dave@example.test"); len(msgs) != 0 {
		t.Errorf("stored %d messages over an unverified connection", len(msgs))
	}
}
