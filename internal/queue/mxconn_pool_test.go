package queue

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/smtp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const poolTestMX = "mx.example.test"

// fakeMXPeer serves enough SMTP for a full delivery over net.Pipe. With a
// certificate the session is TLS from the start, modelling a pooled connection
// that already ran STARTTLS, and a repeated STARTTLS is refused with 503.
// Recipients containing "reject" get a 550.
func fakeMXPeer(conn net.Conn, cert *tls.Certificate) {
	go func() {
		if cert != nil {
			conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{*cert}})
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250 fake")
			case cmd == "STARTTLS":
				if cert != nil {
					say("503 5.5.1 TLS already active")
				} else {
					say("502 5.5.1 STARTTLS not supported")
				}
			case strings.HasPrefix(cmd, "RCPT") && strings.Contains(cmd, "REJECT"):
				say("550 5.1.1 no such user")
			case cmd == "DATA":
				say("354 go ahead")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
}

func selfSignedMXCert(t *testing.T) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: poolTestMX},
		DNSNames:     []string{poolTestMX},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// newPoolTestManager returns a manager that dials fake peers, and a counter of
// the connections it dialed.
func newPoolTestManager(cert *tls.Certificate) (*Manager, *int32) {
	var dials int32
	m := &Manager{
		mxPools:       make(map[string]*mxPool),
		mxPoolSize:    10,
		mxIdleTimeout: time.Hour,
	}
	m.dialSMTP = func(string) (net.Conn, error) {
		atomic.AddInt32(&dials, 1)
		c, s := net.Pipe()
		fakeMXPeer(s, cert)
		if cert != nil {
			return tls.Client(c, &tls.Config{InsecureSkipVerify: true}), nil
		}
		return c, nil
	}
	return m, &dials
}

func deliverPoolTest(m *Manager, to string) error {
	return m.doDeliverToMX(context.Background(), "a@sender.test", to, []byte("Subject: x\r\n\r\nbody\r\n"), poolTestMX)
}

func pooledClients(m *Manager) []*smtp.Client {
	pool := m.getMXPool(poolTestMX)
	pool.mu.Lock()
	defer pool.mu.Unlock()
	out := make([]*smtp.Client, 0, len(pool.conns))
	for _, c := range pool.conns {
		out = append(out, c.client)
	}
	return out
}

// A freshly dialed connection must be pooled and reused, not closed after
// delivery.
func TestMXPool_FreshConnectionIsReused(t *testing.T) {
	m, dials := newPoolTestManager(nil)
	for i := 0; i < 3; i++ {
		if err := deliverPoolTest(m, "b@example.test"); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	if d := atomic.LoadInt32(dials); d != 1 {
		t.Fatalf("expected 3 deliveries over 1 connection, dialed %d", d)
	}
	if n := len(pooledClients(m)); n != 1 {
		t.Fatalf("expected 1 pooled connection, got %d", n)
	}
}

// A pooled connection must be returned to the pool exactly once; a duplicate
// entry would let two workers share one SMTP session.
func TestMXPool_PooledConnectionReleasedOnce(t *testing.T) {
	m, _ := newPoolTestManager(nil)
	seed, err := m.createMXConn(poolTestMX)
	if err != nil {
		t.Fatal(err)
	}
	m.releaseMXConn(poolTestMX, seed, true)

	for i := 0; i < 2; i++ {
		if err := deliverPoolTest(m, "b@example.test"); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
		clients := pooledClients(m)
		if len(clients) != 1 || clients[0] != seed {
			t.Fatalf("delivery %d: pool should hold the seeded client once, got %d entries", i, len(clients))
		}
	}
}

// A failed delivery closes the connection instead of pooling it.
func TestMXPool_FailedDeliveryNotPooled(t *testing.T) {
	m, dials := newPoolTestManager(nil)
	if err := deliverPoolTest(m, "reject@example.test"); err == nil {
		t.Fatal("expected RCPT rejection")
	}
	if n := len(pooledClients(m)); n != 0 {
		t.Fatalf("failed connection must not be pooled, pool has %d", n)
	}
	if err := deliverPoolTest(m, "b@example.test"); err != nil {
		t.Fatalf("delivery after failure: %v", err)
	}
	if d := atomic.LoadInt32(dials); d != 2 {
		t.Fatalf("expected a new dial after the failed connection, dialed %d", d)
	}
}

// Delivery over a connection that is already TLS must not
// send STARTTLS again, so reused pooled connections keep working.
func TestMXPool_RequireTLSReusesTLSConnection(t *testing.T) {
	m, dials := newPoolTestManager(selfSignedMXCert(t))
	for i := 0; i < 2; i++ {
		if err := deliverPoolTest(m, "b@example.test"); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	if d := atomic.LoadInt32(dials); d != 1 {
		t.Fatalf("expected the TLS connection to be reused, dialed %d", d)
	}
}

// requireTLS still rejects a plaintext peer that cannot do STARTTLS.
func TestMXPool_RequireTLSRejectsPlaintext(t *testing.T) {
	m, _ := newPoolTestManager(nil)
	m.requireTLS = true
	err := deliverPoolTest(m, "b@example.test")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS required but failed") {
		t.Fatalf("expected STARTTLS required error, got %v", err)
	}
	if n := len(pooledClients(m)); n != 0 {
		t.Fatalf("rejected plaintext connection must not be pooled, pool has %d", n)
	}
}
