package smtp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func r300Probe(t *testing.T, attempts int, exists func(string) bool) (refused, closedAt int) {
	t.Helper()
	// A real TCP connection, not net.Pipe: a pipe is synchronous, so the server
	// writing its 421 while the client writes the next RCPT would deadlock.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := NewServer(&Config{Hostname: "mx.test", MaxRecipients: 1000, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	srv.SetRecipientExistsHandler(exists)
	done := make(chan struct{})
	go func() {
		c, err := ln.Accept()
		if err == nil {
			srv.handleConnection(c)
		}
		close(done)
	}()
	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(20 * time.Second))
	r := bufio.NewReader(clientConn)
	read := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return ""
			}
			if len(l) >= 4 && l[3] == ' ' {
				return l
			}
		}
	}
	read() // greeting
	for _, c := range []string{"EHLO c.test", "MAIL FROM:<a@sender.test>"} {
		fmt.Fprintf(clientConn, "%s\r\n", c)
		read()
	}
	for i := 1; i <= attempts; i++ {
		if _, err := fmt.Fprintf(clientConn, "RCPT TO:<user%d@mx.test>\r\n", i); err != nil {
			return refused, i
		}
		resp := read()
		switch {
		case strings.HasPrefix(resp, "550"):
			refused++
		case resp == "" || strings.HasPrefix(resp, "421"):
			return refused, i
		}
	}
	fmt.Fprintf(clientConn, "QUIT\r\n") // end the session cleanly instead of waiting for a timeout
	read()
	return refused, 0
}

// Directory harvesting through RCPT-time validation (F7670) is bounded: the
// session ends after a few refused recipients, and a client whose recipients all
// exist is never cut off.
func TestRCPTProbingEndsTheSession(t *testing.T) {
	if refused, closed := r300Probe(t, 60, func(string) bool { return true }); refused != 0 || closed != 0 {
		t.Fatalf("a client with valid recipients was cut off: refused=%d closedAt=%d", refused, closed)
	}
	refused, closedAt := r300Probe(t, 60, func(string) bool { return false })
	if closedAt == 0 || refused > 25 {
		t.Fatalf("probing was not bounded: refused=%d, closed at attempt %d (0 = never)", refused, closedAt)
	}
}
