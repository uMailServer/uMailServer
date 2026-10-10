package server

// Regression tests for F5520 (imap.port, 993, was a plaintext listener, so
// RFC 8314 implicit-TLS clients failed the handshake) and F5521
// (imap.starttls_port, 143, was never bound).

import (
	"bufio"
	"crypto/tls"
	"io"
	"log/slog"
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

// imapTLSConfig is a DefaultConfig with IMAP on two free loopback ports.
func imapTLSConfig(t *testing.T, withCert bool) *config.Config {
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
	cfg.IMAP.Enabled = true
	cfg.IMAP.Bind = "127.0.0.1"
	cfg.IMAP.Port = pop3TLSFreePort(t)
	cfg.IMAP.STARTTLSPort = pop3TLSFreePort(t)
	return cfg
}

// imapTLSNew mirrors Start(): New, an account, the IMAP mailstore.
func imapTLSNew(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
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
	return srv
}

// imapTLSStart starts IMAP and returns the server and both addresses.
func imapTLSStart(t *testing.T, withCert bool) (*Server, string, string) {
	t.Helper()
	cfg := imapTLSConfig(t, withCert)
	srv := imapTLSNew(t, cfg)
	if err := srv.startIMAP(srv.mailstore); err != nil {
		t.Fatalf("INVALID: startIMAP: %v", err)
	}
	if srv.imapServer == nil {
		t.Fatalf("INVALID: imapServer not set")
	}
	return srv, imapTLSAddr(cfg.IMAP.Port), imapTLSAddr(cfg.IMAP.STARTTLSPort)
}

func imapTLSAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// imapTLSCmd sends cmd (none for the greeting) and returns the first
// non-untagged reply line.
func imapTLSCmd(conn net.Conn, r *bufio.Reader, cmd string) string {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if cmd != "" {
		_, _ = conn.Write([]byte(cmd + "\r\n"))
	}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return "ERR " + err.Error()
		}
		if cmd == "" || !strings.HasPrefix(l, "* ") {
			return strings.TrimSpace(l)
		}
	}
}

func imapTLSDial(t *testing.T, addr string) *tls.Conn {
	t.Helper()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- self-signed test cert
	if err != nil {
		t.Fatalf("DEFECT F5520: implicit TLS dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// F5520: an RFC 8314 client (implicit TLS on imap.port) can log in and use
// the session.
func TestStartIMAPImplicitTLSLogin(t *testing.T) {
	_, addr, _ := imapTLSStart(t, true)
	conn := imapTLSDial(t, addr)
	r := bufio.NewReader(conn)
	if g := imapTLSCmd(conn, r, ""); !strings.HasPrefix(g, "* OK") {
		t.Fatalf("DEFECT F5520: greeting %q", g)
	}
	if l := imapTLSCmd(conn, r, "a1 LOGIN alice@test.com secret"); !strings.HasPrefix(l, "a1 OK") {
		t.Fatalf("DEFECT F5520: LOGIN over implicit TLS: %q", l)
	}
	if l := imapTLSCmd(conn, r, "a2 SELECT INBOX"); !strings.HasPrefix(l, "a2 OK") {
		t.Fatalf("DEFECT F5520: SELECT after login: %q", l)
	}
}

// F5520: AUTHENTICATE PLAIN also works over implicit TLS.
func TestStartIMAPImplicitTLSAuthenticate(t *testing.T) {
	_, addr, _ := imapTLSStart(t, true)
	conn := imapTLSDial(t, addr)
	r := bufio.NewReader(conn)
	imapTLSCmd(conn, r, "")
	// base64("\x00alice@test.com\x00secret")
	if l := imapTLSCmd(conn, r, "a1 AUTHENTICATE PLAIN AGFsaWNlQHRlc3QuY29tAHNlY3JldA=="); !strings.HasPrefix(l, "a1 OK") {
		t.Fatalf("DEFECT F5520: AUTHENTICATE PLAIN over implicit TLS: %q", l)
	}
}

// F5520: a cleartext client on the implicit-TLS port gets no greeting.
func TestStartIMAPNoCleartextGreeting(t *testing.T) {
	_, addr, _ := imapTLSStart(t, true)
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("INVALID: dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	if strings.Contains(string(buf[:n]), "OK") {
		t.Fatalf("DEFECT F5520: cleartext greeting on implicit-TLS port: %q", buf[:n])
	}
}

// No certificate: imap.port stays plaintext and still refuses LOGIN.
func TestStartIMAPNoCertUnchanged(t *testing.T) {
	_, addr, _ := imapTLSStart(t, false)
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("DEFECT F5520: no-cert listener down: %v", err)
	}
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	g := imapTLSCmd(c, r, "")
	l := imapTLSCmd(c, r, "a1 LOGIN alice@test.com secret")
	if !strings.HasPrefix(g, "* OK") || !strings.HasPrefix(l, "a1 NO") {
		t.Fatalf("DEFECT F5520: no-cert behavior changed: %q %q", g, l)
	}
}

// Unloadable certificate: IMAP still starts (plaintext, auth refused)
// instead of aborting server startup.
func TestStartIMAPBadCertFallsBack(t *testing.T) {
	cfg := imapTLSConfig(t, false)
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	cfg.TLS.CertFile, cfg.TLS.KeyFile = bad, bad
	srv := imapTLSNew(t, cfg)
	if err := srv.startIMAP(srv.mailstore); err != nil || srv.imapServer == nil {
		t.Fatalf("DEFECT F5520: startIMAP with an unloadable certificate: %v", err)
	}
	c, err := net.DialTimeout("tcp", imapTLSAddr(cfg.IMAP.Port), 5*time.Second)
	if err != nil {
		t.Fatalf("DEFECT F5520: fallback listener down: %v", err)
	}
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	g := imapTLSCmd(c, r, "")
	l := imapTLSCmd(c, r, "a1 LOGIN alice@test.com secret")
	if !strings.HasPrefix(g, "* OK") || !strings.HasPrefix(l, "a1 NO") {
		t.Fatalf("DEFECT F5520: fallback session: %q %q", g, l)
	}
}

// F5521: imap.starttls_port is bound; LOGIN is refused before STARTTLS and
// accepted after it.
func TestStartIMAPSTARTTLSPort(t *testing.T) {
	_, _, addr := imapTLSStart(t, true)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("DEFECT F5521: imap.starttls_port not bound: %v", err)
	}
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	if g := imapTLSCmd(conn, r, ""); !strings.HasPrefix(g, "* OK") {
		t.Fatalf("DEFECT F5521: greeting %q", g)
	}
	if l := imapTLSCmd(conn, r, "a0 LOGIN alice@test.com secret"); !strings.HasPrefix(l, "a0 NO") {
		t.Fatalf("DEFECT F5521: LOGIN before STARTTLS: %q", l)
	}
	if l := imapTLSCmd(conn, r, "a1 STARTTLS"); !strings.HasPrefix(l, "a1 OK") {
		t.Fatalf("DEFECT F5521: STARTTLS: %q", l)
	}
	tc := tls.Client(conn, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- self-signed test cert
	if err := tc.Handshake(); err != nil {
		t.Fatalf("DEFECT F5521: STARTTLS handshake: %v", err)
	}
	tr := bufio.NewReader(tc)
	if l := imapTLSCmd(tc, tr, "a2 LOGIN alice@test.com secret"); !strings.HasPrefix(l, "a2 OK") {
		t.Fatalf("DEFECT F5521: LOGIN after STARTTLS: %q", l)
	}
}

// F5521: Stop releases the STARTTLS listener (it must be drained before
// Stop closes the databases), so both ports can be bound again.
func TestStartIMAPSTARTTLSPortReleasedByStop(t *testing.T) {
	srv, addr, starttlsAddr := imapTLSStart(t, true)
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	for _, a := range []string{addr, starttlsAddr} {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			t.Fatalf("DEFECT F5521: %s still bound after Stop: %v", a, err)
		}
		_ = ln.Close()
	}
}

// F5521: a busy starttls_port fails startIMAP and releases imap.port.
func TestStartIMAPSTARTTLSPortBusy(t *testing.T) {
	cfg := imapTLSConfig(t, true)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	defer func() { _ = busy.Close() }()
	cfg.IMAP.STARTTLSPort = busy.Addr().(*net.TCPAddr).Port
	srv := imapTLSNew(t, cfg)
	if err := srv.startIMAP(srv.mailstore); err == nil || srv.imapServer != nil {
		t.Fatalf("DEFECT F5521: startIMAP with busy starttls_port: err=%v", err)
	}
	ln, err := net.Listen("tcp", imapTLSAddr(cfg.IMAP.Port))
	if err != nil {
		t.Fatalf("DEFECT F5521: imap.port still bound after failed startIMAP: %v", err)
	}
	_ = ln.Close()
}
