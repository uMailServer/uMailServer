package smtp

// Regression test for the DATA-phase pipelining defect (the readData
// sibling of the round-18 AUTH fix): readData read the message through a
// FRESH bufio.NewReader(s.conn) instead of the session's buffered reader
// (s.reader). EHLO advertises PIPELINING, so a client may send
// "MAIL FROM…\r\nRCPT TO…\r\nDATA\r\n<message>\r\n.\r\n" in one TCP
// segment; the main loop's s.reader consumes the command lines and its
// read-ahead buffers the message bytes — invisible to the fresh reader,
// whose per-line ReadBytes then blocks until ReadTimeout expires. The
// message is lost and the session killed. The fix reads message data
// through s.reader, which sees the pipelined bytes and preserves order.
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

func startDataPipelinedSession(t *testing.T, delivered *[]byte) (*Session, net.Conn) {
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
	session.state = StateGreeted
	session.reader = bufio.NewReader(session.conn) // installed like server.go's main loop
	session.mutex.Unlock()

	return session, clientConn
}

func TestDATAPipelinedMessageAccepted(t *testing.T) {
	var delivered []byte
	session, conn := startDataPipelinedSession(t, &delivered)
	reply := bufio.NewReader(conn)
	_ = session.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// PIPELINING: the whole transaction in ONE segment — exactly what a
	// client may do against the advertised PIPELINING capability.
	pipeline := "MAIL FROM:<sender@example.com>\r\n" +
		"RCPT TO:<rcpt@example.com>\r\n" +
		"DATA\r\n" +
		"Message-ID: <r49-pipelined@testhost>\r\n" +
		"Subject: t\r\n" +
		"\r\n" +
		"body\r\n" +
		".\r\n"
	if _, err := conn.Write([]byte(pipeline)); err != nil {
		t.Fatalf("pipelined write: %v", err)
	}

	// Drive the first two commands through s.reader (the production main
	// loop): each dispatch consumes its line from the buffer and replies.
	for _, cmd := range []string{"MAIL FROM:<sender@example.com>", "RCPT TO:<rcpt@example.com>"} {
		line, err := session.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("FAIL: reading %q line: %v", cmd, err)
		}
		if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
			t.Fatalf("FAIL: %q handler: %v", cmd, err)
		}
		if code := readReplyCode(t, conn, reply); code != 250 {
			t.Fatalf("FAIL: %q replied %d, want 250 (harness broken)", cmd, code)
		}
	}

	// DEFECT: the DATA dispatch must complete the message read from the
	// s.reader buffer (354 written, message collected, delivery hook run,
	// 250 written). Pre-fix, readData blocked on the raw conn until the
	// per-line ReadTimeout expired and the handler errored out.
	line, err := session.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: reading DATA line: %v", err)
	}
	if err := session.HandleCommand(strings.TrimSpace(line)); err != nil {
		t.Fatalf("FAIL: pipelined DATA failed: %v (readData read the message from the raw conn, bypassing s.reader's buffer)", err)
	}
	if code := readReplyCode(t, conn, reply); code != 354 {
		t.Fatalf("FAIL: DATA first reply %d, want 354 (harness broken)", code)
	}
	if code := readReplyCode(t, conn, reply); code != 250 {
		t.Fatalf("FAIL: DATA final reply %d, want 250 — the message read timed out (readData read from the raw conn, bypassing s.reader's buffer)", code)
	}
	if delivered == nil {
		t.Fatalf("FAIL: onDeliver was not invoked for the pipelined message")
	}
	want := "Message-ID: <r49-pipelined@testhost>\r\nSubject: t\r\n\r\nbody\r\n"
	if string(delivered) != want {
		t.Fatalf("FAIL: delivered body = %q, want %q", string(delivered), want)
	}
}
