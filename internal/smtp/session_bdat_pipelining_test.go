package smtp

// Regression test for the BDAT-phase pipelining defect (the CHUNKING
// sibling of the round-18/19 fixes): handleBDAT read the chunk payload
// through io.ReadFull(s.conn, ...) instead of the session's buffered
// reader (s.reader). EHLO advertises CHUNKING (and RFC 3030 explicitly
// allows BDAT pipelined with MAIL/RCPT), so a client may send
// "BDAT <n>\r\n<n octets>BDAT 0 LAST\r\n" in one TCP segment; the main
// loop's s.reader consumes the BDAT line and its read-ahead buffers the
// chunk octets — invisible to io.ReadFull(s.conn), which blocks until
// ReadTimeout expires. handleBDAT then returns a bare error (no reply is
// written for a non-LAST chunk), so the connection simply dies and the
// message is lost. The fix reads chunk payloads through s.reader.
//
// Hermetic: loopback connection pair, in-package Server with the onDeliver
// hook capturing the delivery; no external network, no database.

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func startBDATPipelinedSession(t *testing.T, delivered *[]byte) (*Session, net.Conn) {
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
		onDeliver: func(mailFrom string, rcptTo []string, data []byte) error {
			*delivered = data
			return nil
		},
	}
	session := NewSession(serverConn, server)
	session.mutex.Lock()
	session.state = StateRcptTo                    // BDAT is valid once recipients are set
	session.reader = bufio.NewReader(session.conn) // installed like server.go's main loop
	session.mutex.Unlock()

	return session, clientConn
}

func TestBDATPipelinedChunkAccepted(t *testing.T) {
	var delivered []byte
	session, conn := startBDATPipelinedSession(t, &delivered)
	reply := bufio.NewReader(conn)
	_ = session.conn.SetReadDeadline(time.Now().Add(6 * time.Second))

	// PIPELINING (RFC 3030): the chunk size line, its exact payload octets,
	// and the terminating BDAT 0 LAST in ONE segment.
	pipeline := "BDAT 10\r\n" + "0123456789" + "BDAT 0 LAST\r\n"
	if _, err := conn.Write([]byte(pipeline)); err != nil {
		t.Fatalf("pipelined write: %v", err)
	}

	// Drive the first BDAT line through s.reader (the production main
	// loop): its read-ahead buffers the payload octets.
	line, err := session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: reading BDAT line: %v", err)
	}
	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: pipelined BDAT failed: %v (the chunk payload was read from the raw conn, bypassing s.reader's buffer)", err)
	}

	// Terminating chunk through s.reader; the 250 comes only after LAST.
	line, err = session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: reading BDAT 0 LAST line: %v", err)
	}
	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: BDAT 0 LAST handler: %v", err)
	}
	if code := readReplyCode(t, conn, reply); code != 250 {
		t.Fatalf("FAIL: BDAT 0 LAST replied %d, want 250", code)
	}
	if delivered == nil {
		t.Fatalf("FAIL: onDeliver was not invoked for the pipelined chunk message")
	}
	if string(delivered) != "0123456789" {
		t.Fatalf("FAIL: delivered payload = %q, want %q", string(delivered), "0123456789")
	}
}

func TestBDATNonPipelinedChunkStillAccepted(t *testing.T) {
	var delivered []byte
	session, conn := startBDATPipelinedSession(t, &delivered)
	reply := bufio.NewReader(conn)
	_ = session.conn.SetReadDeadline(time.Now().Add(6 * time.Second))

	// Two-step control: the payload arrives while the handler is blocked in
	// ReadFull — must still be accepted (guards the fix against breaking
	// the non-pipelined path).
	if _, err := conn.Write([]byte("BDAT 10\r\n")); err != nil {
		t.Fatalf("BDAT write: %v", err)
	}
	line, err := session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: reading BDAT line: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = conn.Write([]byte("0123456789"))
	}()
	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: two-step BDAT failed: %v", err)
	}

	if _, err := conn.Write([]byte("BDAT 0 LAST\r\n")); err != nil {
		t.Fatalf("BDAT 0 LAST write: %v", err)
	}
	line, err = session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: reading BDAT 0 LAST line: %v", err)
	}
	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: BDAT 0 LAST handler: %v", err)
	}
	if code := readReplyCode(t, conn, reply); code != 250 {
		t.Fatalf("FAIL: BDAT 0 LAST replied %d, want 250 (control)", code)
	}
	if string(delivered) != "0123456789" {
		t.Fatalf("FAIL: delivered payload = %q, want %q (control)", string(delivered), "0123456789")
	}
}
