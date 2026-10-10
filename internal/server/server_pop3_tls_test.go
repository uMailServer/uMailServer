package server

// Regression tests for F5420: startPOP3 gated the POP3 TLS config on
// tlsManager.IsEnabled() (never set by New) and always listened in plaintext,
// so with requireTLS on no client could authenticate. The POP3 port is
// implicit TLS (RFC 8314, default 995) when tls.cert_file/key_file are set.

import (
	"bufio"
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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/imap"
	"golang.org/x/crypto/bcrypt"
)

func pop3TLSCert(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("INVALID: key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mx.test.com"},
		DNSNames:     []string{"mx.test.com"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("INVALID: cert: %v", err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("INVALID: key marshal: %v", err)
	}
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("INVALID: write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatalf("INVALID: write key: %v", err)
	}
	return certFile, keyFile
}

func pop3TLSFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("INVALID: listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// pop3TLSStart mirrors Start(): New, an account, the IMAP mailstore, startPOP3.
func pop3TLSStart(t *testing.T, withCert bool) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	if withCert {
		cfg.TLS.CertFile, cfg.TLS.KeyFile = pop3TLSCert(t, dir)
	}
	cfg.POP3.Enabled = true
	cfg.POP3.Bind = "127.0.0.1"
	cfg.POP3.Port = pop3TLSFreePort(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("INVALID: domain: %v", err)
	}
	if err := srv.database.CreateAccount(&db.AccountData{Email: "alice@test.com", LocalPart: "alice", Domain: "test.com", PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatalf("INVALID: account: %v", err)
	}
	srv.mailstore = imap.NewBboltMailstoreWithInterfaces(srv.storageDB, srv.msgStore)
	if err := srv.startPOP3(srv.mailstore); err != nil {
		t.Fatalf("INVALID: startPOP3: %v", err)
	}
	if srv.pop3Server == nil {
		t.Fatalf("INVALID: pop3Server not set")
	}
	return srv, net.JoinHostPort(cfg.POP3.Bind, strconv.Itoa(cfg.POP3.Port))
}

// pop3TLSLogin runs USER/PASS over conn and returns the three replies.
func pop3TLSLogin(conn net.Conn) (greet, user, pass string) {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	greet, _ = r.ReadString('\n')
	_, _ = conn.Write([]byte("USER alice@test.com\r\n"))
	user, _ = r.ReadString('\n')
	_, _ = conn.Write([]byte("PASS secret\r\n"))
	pass, _ = r.ReadString('\n')
	return strings.TrimSpace(greet), strings.TrimSpace(user), strings.TrimSpace(pass)
}

// Defect: with a certificate configured, an RFC 8314 client (implicit TLS
// on the POP3 port) can log in.
func TestStartPOP3ImplicitTLSLogin(t *testing.T) {
	_, addr := pop3TLSStart(t, true)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- self-signed test cert
	if err != nil {
		// Diagnose what a plaintext client gets on the same port.
		pc, perr := net.DialTimeout("tcp", addr, 5*time.Second)
		diag := ""
		if perr == nil {
			g, u, p := pop3TLSLogin(pc)
			diag = "plaintext greeting=" + g + " USER=" + u + " PASS=" + p
			_ = pc.Close()
		}
		t.Logf("ACTUAL: TLS handshake failed: %v; %s", err, diag)
		t.Fatalf("DEFECT F5420: POP3 port does not speak implicit TLS; no client can authenticate")
	}
	defer func() { _ = conn.Close() }()
	g, u, p := pop3TLSLogin(conn)
	t.Logf("ACTUAL: greeting=%q USER=%q PASS=%q", g, u, p)
	if !strings.HasPrefix(g, "+OK") || !strings.HasPrefix(u, "+OK") || !strings.HasPrefix(p, "+OK") {
		t.Fatalf("DEFECT F5420: login over implicit TLS failed")
	}
}

// Implicit TLS session: login, STAT, and STLS refused as already-TLS.
func TestStartPOP3SessionAfterLogin(t *testing.T) {
	_, addr := pop3TLSStart(t, true)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- self-signed test cert
	if err != nil {
		t.Fatalf("DEFECT F5420: TLS dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	g, u, p := pop3TLSLogin(conn)
	if !strings.HasPrefix(g, "+OK") || !strings.HasPrefix(u, "+OK") || !strings.HasPrefix(p, "+OK") {
		t.Fatalf("DEFECT F5420: login %q %q %q", g, u, p)
	}
	r := bufio.NewReader(conn)
	_, _ = conn.Write([]byte("STAT\r\nSTLS\r\nQUIT\r\n"))
	stat, _ := r.ReadString('\n')
	stls, _ := r.ReadString('\n')
	if !strings.HasPrefix(stat, "+OK 0 ") || !strings.HasPrefix(stls, "-ERR") {
		t.Fatalf("DEFECT F5420: STAT=%q STLS=%q", stat, stls)
	}
}

// A cleartext client on the implicit-TLS port is never sent the greeting.
func TestStartPOP3NoCleartextGreeting(t *testing.T) {
	_, addr := pop3TLSStart(t, true)
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("INVALID: dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	if strings.Contains(string(buf[:n]), "+OK") {
		t.Fatalf("DEFECT F5420: cleartext greeting on implicit-TLS port: %q", buf[:n])
	}
}

// No cert configured: POP3 still starts (plaintext) and still refuses auth.
func TestStartPOP3NoCertUnchanged(t *testing.T) {
	_, addr := pop3TLSStart(t, false)
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("DEFECT F5420: no-cert listener down: %v", err)
	}
	defer func() { _ = c.Close() }()
	g, u, _ := pop3TLSLogin(c)
	if !strings.HasPrefix(g, "+OK") || !strings.HasPrefix(u, "-ERR TLS required") {
		t.Fatalf("DEFECT F5420: no-cert behavior changed: %q %q", g, u)
	}
}

// Unloadable cert: POP3 still starts (plaintext, auth refused) instead of
// aborting server startup; a failed StartTLS leaves no stale listener.
func TestStartPOP3BadCertFallsBack(t *testing.T) {
	srv, _ := pop3TLSStart(t, false)
	_ = srv.pop3Server.Stop()
	srv.pop3Server = nil
	bad := t.TempDir() + "/bad.pem"
	if err := os.WriteFile(bad, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	srv.config.TLS.CertFile, srv.config.TLS.KeyFile = bad, bad
	srv.config.POP3.Port = pop3TLSFreePort(t)
	if err := srv.startPOP3(srv.mailstore); err != nil || srv.pop3Server == nil {
		t.Fatalf("DEFECT F5420: startPOP3 with an unloadable certificate: %v", err)
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.config.POP3.Port)), 5*time.Second)
	if err != nil {
		t.Fatalf("DEFECT F5420: fallback listener down: %v", err)
	}
	defer func() { _ = c.Close() }()
	g, u, _ := pop3TLSLogin(c)
	if !strings.HasPrefix(g, "+OK") || !strings.HasPrefix(u, "-ERR TLS required") {
		t.Fatalf("DEFECT F5420: fallback session: %q %q", g, u)
	}
}
