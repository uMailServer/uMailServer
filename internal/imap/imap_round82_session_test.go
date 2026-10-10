package imap

import (
	"bufio"
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"
)

// round82Client is a protocol-level client over net.Pipe to
// Server.handleConnection (greeting, capabilities, literals included), for
// tests that need the bytes a real client sees.
type round82Client struct {
	t     *testing.T
	conn  net.Conn
	lines chan string
}

func newRound82Client(t *testing.T, conn net.Conn) *round82Client {
	t.Helper()
	c := &round82Client{t: t, conn: conn, lines: make(chan string, 4096)}
	go func() {
		defer close(c.lines)
		r := bufio.NewReader(conn)
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			c.lines <- strings.TrimRight(l, "\r\n")
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return c
}

// until returns every line up to and including the first one that has prefix.
func (c *round82Client) until(prefix string) []string {
	c.t.Helper()
	var out []string
	timeout := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				c.t.Fatalf("connection closed waiting for %q (got %q)", prefix, out)
			}
			out = append(out, l)
			if strings.HasPrefix(l, prefix) {
				return out
			}
		case <-timeout:
			c.t.Fatalf("timeout waiting for %q (got %q)", prefix, out)
		}
	}
}

// cmd sends one command line (CRLF added) and returns the reply lines up to
// the tagged completion.
func (c *round82Client) cmd(line string) []string {
	c.t.Helper()
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		c.t.Fatal(err)
	}
	return c.until(strings.Fields(line)[0] + " ")
}

func (c *round82Client) write(s string) {
	c.t.Helper()
	if _, err := c.conn.Write([]byte(s)); err != nil {
		c.t.Fatal(err)
	}
}

// round82Server starts a plain-text IMAP session over a real BboltMailstore
// holding the given INBOX messages plus an empty Archive; the client is
// logged in as user@example.com (plain auth allowed for the test).
func round82Server(t *testing.T, msgs ...string) (*round82Client, *BboltMailstore) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox("user@example.com", mb); err != nil {
			t.Fatal(err)
		}
	}
	for i, m := range msgs {
		if err := ms.AppendMessage("user@example.com", "INBOX", nil, time.Date(2024, 1, 10+i, 12, 0, 0, 0, time.UTC), []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	srv := NewServer(&Config{}, ms)
	srv.SetAllowPlainAuth(true)
	srv.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
	clientConn, serverConn := net.Pipe()
	go srv.handleConnection(serverConn)
	c := newRound82Client(t, clientConn)
	c.until("* OK")
	if out := c.cmd("a0 LOGIN user@example.com pw"); !strings.HasPrefix(out[len(out)-1], "a0 OK") {
		t.Fatalf("login: %q", out)
	}
	return c, ms
}

// round82TLSServer serves an implicit-TLS listener (what StartTLS() does)
// and returns a connected client plus its greeting.
func round82TLSServer(t *testing.T) (*round82Client, string) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	cfg := &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}, MinVersion: tls.VersionTLS12}
	srv := NewServer(&Config{TLSConfig: cfg}, ms)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.running.Store(true)
	srv.listeners = append(srv.listeners, ln)
	go srv.acceptLoop(ln)
	t.Cleanup(func() { _ = srv.Stop() })
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	c := newRound82Client(t, conn)
	return c, c.until("* OK")[0]
}
