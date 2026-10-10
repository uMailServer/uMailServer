package server

// End-to-end harness for audit round 132: starts a real Server (loopback
// ephemeral ports, self-signed certificate generated in-test) and talks to it
// over SMTP submission, IMAP, POP3, JMAP and ManageSieve.

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type e2eEnv struct {
	t        *testing.T
	cfg      *config.Config
	srv      *Server
	dir      string
	smtp     int
	sub      int
	imap     int
	pop3     int
	jmap     int
	sieve    int
	password string
}

func e2eFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func e2eWriteCert(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"test.example.com", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	cf, kf := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cf, kf
}

func newE2E(t *testing.T) *e2eEnv {
	t.Helper()
	dir := t.TempDir()
	cf, kf := e2eWriteCert(t, dir)
	e := &e2eEnv{t: t, dir: dir, password: "S3cret-pass!"}
	e.smtp, e.sub, e.imap, e.pop3, e.jmap, e.sieve = e2eFreePort(t), e2eFreePort(t), e2eFreePort(t), e2eFreePort(t), e2eFreePort(t), e2eFreePort(t)
	e.cfg = &config.Config{
		Server:   config.ServerConfig{Hostname: "test.example.com", DataDir: dir},
		Database: config.DatabaseConfig{Path: filepath.Join(dir, "umailserver.db")},
		Logging:  config.LoggingConfig{Level: "error"},
		TLS:      config.TLSConfig{CertFile: cf, KeyFile: kf},
		SMTP: config.SMTPConfig{
			Inbound:    config.InboundSMTPConfig{Enabled: true, Bind: "127.0.0.1", Port: e.smtp, MaxMessageSize: 1 << 20, MaxRecipients: 10},
			Submission: config.SubmissionSMTPConfig{Enabled: true, Bind: "127.0.0.1", Port: e.sub, RequireAuth: true, RequireTLS: true},
		},
		Spam:        config.SpamConfig{RejectThreshold: 100, JunkThreshold: 50},
		IMAP:        config.IMAPConfig{Enabled: true, Bind: "127.0.0.1", Port: e.imap},
		POP3:        config.POP3Config{Enabled: true, Bind: "127.0.0.1", Port: e.pop3},
		JMAP:        config.JMAPConfig{Enabled: true, Bind: "127.0.0.1", Port: e.jmap},
		ManageSieve: config.ManageSieveConfig{Enabled: true, Bind: "127.0.0.1", Port: 0},
		Admin:       config.AdminConfig{Bind: "127.0.0.1", Port: 0},
		Security: config.SecurityConfig{
			JWTSecret:        "test-jwt-secret-0123456789abcdef0123456789",
			MaxLoginAttempts: 50,
		},
	}
	return e
}

// seed creates the domain and accounts directly in the accounts database.
func (e *e2eEnv) seed(accounts ...*db.AccountData) {
	e.t.Helper()
	d, err := db.Open(e.cfg.Database.Path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer d.Close()
	if err := d.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 20, IsActive: true}); err != nil {
		e.t.Fatal(err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(e.password), bcrypt.MinCost)
	for _, a := range accounts {
		a.Domain, a.Email, a.PasswordHash, a.IsActive = "test.com", a.LocalPart+"@test.com", string(h), true
		if err := d.CreateAccount(a); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *e2eEnv) start() {
	e.t.Helper()
	srv, err := New(e.cfg)
	if err != nil {
		e.t.Fatalf("New: %v", err)
	}
	if err := srv.Start(); err != nil {
		e.t.Fatalf("Start: %v", err)
	}
	e.srv = srv
}

func (e *e2eEnv) stop() {
	if e.srv != nil {
		_ = e.srv.Stop()
		e.srv = nil
	}
}

type e2eConn struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

func e2eDial(t *testing.T, port int, implicitTLS bool) *e2eConn {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var c net.Conn
	var err error
	if implicitTLS {
		c, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		c, err = net.DialTimeout("tcp", addr, 5*time.Second)
	}
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { c.Close() })
	return &e2eConn{t: t, c: c, r: bufio.NewReader(c)}
}

func (c *e2eConn) send(format string, a ...any) {
	c.t.Helper()
	_ = c.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprintf(c.c, format+"\r\n", a...); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *e2eConn) line() string {
	c.t.Helper()
	_ = c.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	l, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v (partial %q)", err, l)
	}
	return strings.TrimRight(l, "\r\n")
}

// smtpReply reads a (possibly multi-line) SMTP reply and returns all lines.
func (c *e2eConn) smtpReply() (int, string) {
	c.t.Helper()
	var all []string
	for {
		l := c.line()
		all = append(all, l)
		if len(l) < 4 || l[3] != '-' {
			var code int
			fmt.Sscanf(l, "%d", &code)
			return code, strings.Join(all, "\n")
		}
	}
}

func (c *e2eConn) expectSMTP(code int) string {
	c.t.Helper()
	got, text := c.smtpReply()
	if got != code {
		c.t.Fatalf("SMTP expected %d got %d: %s", code, got, text)
	}
	return text
}

// untagged reads IMAP/POP lines until pred matches a terminal line.
func (c *e2eConn) imapCmd(tag, cmd string) string {
	c.t.Helper()
	c.send("%s %s", tag, cmd)
	var sb strings.Builder
	for {
		l := c.line()
		sb.WriteString(l + "\n")
		if strings.HasPrefix(l, tag+" ") {
			return sb.String()
		}
	}
}

// submit sends a message through the real submission listener using STARTTLS and AUTH PLAIN.
func (e *e2eEnv) submit(user, pass, from string, rcpts []string, msg string) {
	e.t.Helper()
	c := e2eDial(e.t, e.sub, false)
	c.expectSMTP(220)
	c.send("EHLO client.test")
	c.expectSMTP(250)
	c.send("STARTTLS")
	c.expectSMTP(220)
	tc := tls.Client(c.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		e.t.Fatalf("starttls handshake: %v", err)
	}
	c.c, c.r = tc, bufio.NewReader(tc)
	c.send("EHLO client.test")
	c.expectSMTP(250)
	c.send("AUTH PLAIN %s", b64("\x00"+user+"\x00"+pass))
	c.expectSMTP(235)
	c.send("MAIL FROM:<%s>", from)
	c.expectSMTP(250)
	for _, r := range rcpts {
		c.send("RCPT TO:<%s>", r)
		c.expectSMTP(250)
	}
	c.send("DATA")
	c.expectSMTP(354)
	c.send("%s\r\n.", strings.ReplaceAll(strings.TrimRight(msg, "\r\n"), "\n", "\r\n"))
	c.expectSMTP(250)
	c.send("QUIT")
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
