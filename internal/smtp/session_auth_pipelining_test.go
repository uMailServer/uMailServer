package smtp

// Regression test for the AUTH pipelining defect: the AUTH continuation
// lines (PLAIN credentials, LOGIN username/password, SCRAM messages) were
// read through a FRESH bufio.NewReader(s.conn) instead of the session's
// buffered reader (s.reader). EHLO advertises PIPELINING, so a client may
// send "AUTH PLAIN\r\n<base64>\r\n" in a single TCP segment; the main loop
// consumes the first line via s.reader — whose read-ahead has already
// buffered the credential line — and dispatches it. The fresh reader then
// blocks on the raw connection (the buffered bytes are invisible to it)
// until ReadTimeout expires, killing the session instead of
// authenticating. The fix reads continuation lines through s.reader, which
// sees the pipelined bytes and preserves their order.

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// startPipelinedAuthSession wires a loopback session with a permissive
// onAuth hook, TLS forced on, s.reader installed (as the production main
// loop does), and a bounded ReadTimeout so a broken continuation read
// fails fast instead of hanging the test.
func startPipelinedAuthSession(t *testing.T) (*Session, net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverConnCh := make(chan net.Conn, 1)
	go func() {
		c, _ := listener.Accept()
		serverConnCh <- c
	}()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	serverConn := <-serverConnCh
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	server := &Server{
		config: &Config{
			Hostname:       "testhost",
			MaxMessageSize: 1024 * 1024,
			MaxRecipients:  100,
			ReadTimeout:    2 * time.Second,
		},
		onAuth: func(username, password string) (bool, error) {
			return true, nil
		},
	}
	session := NewSession(serverConn, server)
	session.mutex.Lock()
	session.isTLS = true
	session.state = StateGreeted
	session.reader = bufio.NewReader(session.conn) // installed like server.go's main loop
	session.mutex.Unlock()

	return session, clientConn
}

func TestAUTHPipelinedCredentialsAccepted(t *testing.T) {
	session, conn := startPipelinedAuthSession(t)
	_ = session.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// PIPELINING: both lines in ONE segment — exactly what a client may do
	// against the advertised PIPELINING capability.
	creds := base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))
	if _, err := conn.Write([]byte("AUTH PLAIN\r\n" + creds + "\r\n")); err != nil {
		t.Fatalf("pipelined write: %v", err)
	}

	// The main loop reads the AUTH line through s.reader (whose read-ahead
	// buffers the credential line), then dispatches.
	line, err := session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: reading the AUTH line: %v", err)
	}
	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: pipelined AUTH PLAIN failed: %v (the handler read the credential line from the raw conn, bypassing s.reader's buffer)", err)
	}
}

func TestAUTHNonPipelinedCredentialsStillAccepted(t *testing.T) {
	session, conn := startPipelinedAuthSession(t)
	_ = session.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Control: the classic two-step flow — AUTH line first, the handler
	// blocks on the 334 credential read, THEN the creds arrive.
	if _, err := conn.Write([]byte("AUTH PLAIN\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: read AUTH line: %v", err)
	}

	creds := base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))
	var wg sync.WaitGroup
	wg.Add(1)
	var hErr error
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond) // creds arrive after the handler blocks
		_, wErr := conn.Write([]byte(creds + "\r\n"))
		if wErr != nil {
			hErr = wErr
		}
	}()

	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: non-pipelined AUTH PLAIN failed: %v", err)
	}
	wg.Wait()
	if hErr != nil {
		t.Fatalf("FAIL: cred write: %v", hErr)
	}
}
