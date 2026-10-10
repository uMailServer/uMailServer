package pop3

// Regression tests for POP3 connection admission and implicit TLS.
//
// F5401: the maxConnections check read the session count under RLock and
// registered the session later under a separate Lock, so concurrent
// connections all passed the check before any registered.
// F5404: a connection that reached registration after Stop was added to the
// post-Stop session map and served, never closed by Stop.
// F5402: sessions from the implicit-TLS listener (StartTLS) were not marked
// TLS, so requireTLS refused USER/PASS on them and CAPA offered STLS.
//
// Ordering is forced with a gated crypto/rand reader (NewSession draws its
// IDs from it), never with sleeps.

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
	"strings"
	"sync"
	"testing"
	"time"
)

type admStore struct{}

func (admStore) Authenticate(_, p string) (bool, error)     { return p == "pw", nil }
func (admStore) ListMessages(string) ([]*Message, error)    { return nil, nil }
func (admStore) GetMessage(string, int) (*Message, error)   { return nil, io.EOF }
func (admStore) GetMessageData(string, int) ([]byte, error) { return nil, io.EOF }
func (admStore) DeleteMessage(string, int) error            { return nil }
func (admStore) GetMessageCount(string) (int, error)        { return 0, nil }
func (admStore) GetMessageSize(string, int) (int64, error)  { return 0, nil }

func admServer() *Server {
	return NewServer("127.0.0.1:0", admStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

type admClient struct {
	c net.Conn
	r *bufio.Reader
}

func (c *admClient) line() string {
	l, err := c.r.ReadString('\n')
	if err != nil {
		return "<closed>"
	}
	return strings.TrimRight(l, "\r\n")
}

func (c *admClient) cmd(s string) string {
	if _, err := c.c.Write([]byte(s + "\r\n")); err != nil {
		return "<closed>"
	}
	return c.line()
}

func admConn(t *testing.T, srv *Server, wrap func(net.Conn) net.Conn) (*admClient, chan struct{}) {
	t.Helper()
	s, c := net.Pipe()
	var sc net.Conn = s
	if wrap != nil {
		sc = wrap(s)
	}
	done := make(chan struct{})
	go func() { srv.handleConnection(sc); _ = sc.Close(); close(done) }()
	t.Cleanup(func() { _ = c.Close(); <-done })
	return &admClient{c: c, r: bufio.NewReader(c)}, done
}

// admGate blocks the first n crypto/rand reads until release is closed.
type admGate struct {
	orig    io.Reader
	mu      sync.Mutex
	left    int
	arrived chan struct{}
	release chan struct{}
}

func (g *admGate) Read(p []byte) (int, error) {
	g.mu.Lock()
	gated := g.left > 0
	if gated {
		g.left--
	}
	g.mu.Unlock()
	if gated {
		g.arrived <- struct{}{}
		<-g.release
	}
	return g.orig.Read(p)
}

func admInstallGate(t *testing.T, n int) *admGate {
	g := &admGate{orig: rand.Reader, left: n, arrived: make(chan struct{}, n), release: make(chan struct{})}
	rand.Reader = g
	t.Cleanup(func() { rand.Reader = g.orig })
	return g
}

func TestHandleConnection_ConcurrentAdmissionRespectsMax(t *testing.T) {
	srv := admServer()
	srv.SetMaxConnections(1)
	g := admInstallGate(t, 2)
	a, _ := admConn(t, srv, nil)
	b, _ := admConn(t, srv, nil)
	<-g.arrived
	<-g.arrived
	close(g.release)
	accepted := 0
	for _, greet := range []string{a.line(), b.line()} {
		if strings.HasPrefix(greet, "+OK") {
			accepted++
		} else if !strings.Contains(greet, "Too many connections") {
			t.Fatalf("unexpected rejection %q", greet)
		}
	}
	if accepted != 1 {
		t.Fatalf("maxConnections=1 but %d concurrent connections accepted", accepted)
	}
}

func TestHandleConnection_NotServedAfterStop(t *testing.T) {
	srv := admServer()
	g := admInstallGate(t, 1)
	cl, _ := admConn(t, srv, nil)
	<-g.arrived // connection accepted, session being created
	if err := srv.Stop(); err != nil {
		t.Fatal(err)
	}
	close(g.release)
	if greet := cl.line(); strings.HasPrefix(greet, "+OK") {
		t.Fatalf("connection served after Stop: %q", greet)
	}
	srv.sessionsMu.RLock()
	n := len(srv.sessions)
	srv.sessionsMu.RUnlock()
	if n != 0 {
		t.Fatalf("%d sessions registered after Stop", n)
	}
}

func TestImplicitTLS_SessionMarkedTLS(t *testing.T) {
	srv := admServer()
	srv.SetRequireTLS(true)
	srv.SetTLSConfig(admTestCert(t))
	cfg, err := srv.getTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	cl, _ := admConn(t, srv, func(c net.Conn) net.Conn { return tls.Server(c, cfg) })
	tc := tls.Client(cl.c, &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"}) // #nosec G402 -- self-signed test cert
	defer tc.Close()
	c := &admClient{c: tc, r: bufio.NewReader(tc)}
	if g := c.line(); !strings.HasPrefix(g, "+OK") {
		t.Fatalf("greeting %q", g)
	}
	if r := c.cmd("USER u"); !strings.HasPrefix(r, "+OK") {
		t.Fatalf("USER over implicit TLS with requireTLS: %q", r)
	}
	if r := c.cmd("STLS"); !strings.HasPrefix(r, "-ERR") {
		t.Fatalf("STLS on implicit TLS session: %q", r)
	}
}

// admTestCert writes a self-signed localhost certificate and returns its config.
func admTestCert(t *testing.T) *TLSConfig {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC), DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cf, kf := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if err := os.WriteFile(cf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0600); err != nil {
		t.Fatal(err)
	}
	return &TLSConfig{CertFile: cf, KeyFile: kf}
}
