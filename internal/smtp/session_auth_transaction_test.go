package smtp

// Regression test for the AUTH-after-MAIL sequencing defect: handleAUTH
// checked only for StateNew and already-authenticated, so AUTH issued
// INSIDE a mail transaction (after MAIL FROM, while s.state is
// StateMailFrom or later) proceeded into credential handling instead of
// being rejected. RFC 4954 §4: after a MAIL command has been transmitted
// the client MUST NOT issue AUTH, and the server MUST reject it with a
// 503 reply. AUTH stays valid after EHLO and again after a completed
// transaction (resetTransaction returns the session to StateGreeted).
//
// Hermetic: loopback connection pair, in-package Server with the onAuth
// hook allowing all credentials, session.isTLS forced so the AUTH TLS gate
// passes; no external network, no database.

import (
	"bufio"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startAuthTestSession wires a loopback session with a permissive onAuth
// hook and TLS forced on (so handleAUTH's encryption gate passes).
func startAuthTestSession(t *testing.T) (*Session, net.Conn) {
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
		},
		onAuth: func(username, password string) (bool, error) {
			return true, nil
		},
	}
	session := NewSession(serverConn, server)
	session.mutex.Lock()
	session.isTLS = true
	session.mutex.Unlock()

	return session, clientConn
}

// driveCommand sends an SMTP command and returns the terminating reply code.
func driveCommand(t *testing.T, session *Session, conn net.Conn, reply *bufio.Reader, line string) int {
	t.Helper()
	if err := session.HandleCommand(line); err != nil {
		t.Fatalf("HandleCommand(%q): %v", line, err)
	}
	return readReplyCode(t, conn, reply)
}

// readReplyCode reads SMTP reply lines until the final one (4th char is a
// space or the line is bare) and returns its numeric code. Each read is
// deadline-bounded so a missing reply surfaces as an explicit failure
// instead of a hang.
func readReplyCode(t *testing.T, conn net.Conn, reader *bufio.Reader) int {
	t.Helper()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply: %v", err)
		}
		_ = conn.SetReadDeadline(time.Time{})
		line = strings.TrimRight(line, "\r\n")
		if len(line) >= 3 {
			if code, convErr := strconv.Atoi(line[:3]); convErr == nil {
				if len(line) == 3 || line[3] == ' ' {
					return code
				}
				continue // multiline continuation ("250-...")
			}
		}
	}
}

func plainAuthArg(user, pass string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
	return "AUTH PLAIN " + encoded
}

func TestAUTHAfterMailIsRejectedWith503(t *testing.T) {
	// CONTROL: AUTH before any transaction is accepted (235).
	session, conn := startAuthTestSession(t)
	reply := bufio.NewReader(conn)
	driveCommand(t, session, conn, reply, "EHLO client.test")
	if code := driveCommand(t, session, conn, reply, plainAuthArg("user", "pass")); code != 235 {
		t.Fatalf("FAIL: control AUTH before MAIL got %d, want 235 (harness broken)", code)
	}

	// DEFECT: AUTH issued inside a mail transaction (after MAIL FROM) must
	// be rejected with 503 per RFC 4954 §4 — not silently authenticated.
	session2, conn2 := startAuthTestSession(t)
	reply2 := bufio.NewReader(conn2)
	driveCommand(t, session2, conn2, reply2, "EHLO client.test")
	if code := driveCommand(t, session2, conn2, reply2, "MAIL FROM:<sender@example.com>"); code != 250 {
		t.Fatalf("FAIL: MAIL FROM got %d, want 250 (harness broken)", code)
	}
	if code := driveCommand(t, session2, conn2, reply2, plainAuthArg("user", "pass")); code != 503 {
		t.Fatalf("FAIL: AUTH after MAIL got %d, want 503 (RFC 4954 §4) — the server authenticated inside an open transaction", code)
	}
}
