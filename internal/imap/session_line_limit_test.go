package imap

import (
	"bufio"
	"net"
	"strings"
	"sync/atomic"
	"testing"
)

type closeCountConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *closeCountConn) Close() error { c.closes.Add(1); return c.Conn.Close() }

func startPipeSession(t *testing.T) (net.Conn, *closeCountConn, <-chan string, <-chan struct{}) {
	t.Helper()
	srv := NewServer(&Config{}, &mockMailstore{})
	clientConn, serverConn := net.Pipe()
	wc := &closeCountConn{Conn: serverConn}
	done := make(chan struct{})
	go func() { defer close(done); srv.handleConnection(wc) }()
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		r := bufio.NewReader(clientConn)
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			lines <- strings.TrimRight(l, "\r\n")
		}
	}()
	if g := <-lines; !strings.HasPrefix(g, "* OK") {
		t.Fatalf("greeting %q", g)
	}
	return clientConn, wc, lines, done
}

// F4956: an unterminated pre-auth line is cut off at maxCommandLineLength
// instead of being buffered without bound.
func TestReadLine_PreAuthLineIsBounded(t *testing.T) {
	c, _, lines, done := startPipeSession(t)
	chunk := []byte(strings.Repeat("A", 32<<10))
	accepted := 0
	for accepted < 8<<20 {
		n, err := c.Write(chunk)
		accepted += n
		if err != nil {
			break
		}
	}
	_ = c.Close()
	<-done
	var got []string
	for l := range lines {
		got = append(got, l)
	}
	if accepted > maxCommandLineLength+64<<10 {
		t.Errorf("server consumed %d bytes of one line (limit %d)", accepted, maxCommandLineLength)
	}
	if len(got) == 0 || got[0] != "* BYE Command line too long" {
		t.Errorf("responses = %q", got)
	}
}

// F4960: the accepted connection is closed when the session ends on a read
// error (peer gone / read deadline), not only on LOGOUT.
func TestHandleConnection_ClosesConnOnReadError(t *testing.T) {
	c, wc, lines, done := startPipeSession(t)
	_ = c.Close()
	<-done
	for range lines {
	}
	if wc.closes.Load() < 1 {
		t.Error("server-side conn was not closed after the session ended on a read error")
	}
}
