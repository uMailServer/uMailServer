package smtp

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"
)

// deliverData drives the real DATA path and returns the bytes handed to
// onDeliver, i.e. exactly what the MTA would store.
func deliverData(t *testing.T, wire string) []byte {
	t.Helper()
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var delivered []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		delivered = append([]byte(nil), data...)
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- session.handleDATA() }()

	reader := bufio.NewReader(clientConn)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read 354: %v", err)
	}
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("expected 354, got %s", resp)
	}

	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := clientConn.Write([]byte(wire)); err != nil {
		t.Fatalf("write data: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("handleDATA: %v", err)
	}
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader.ReadString('\n')

	if delivered == nil {
		t.Fatalf("onDeliver was never called")
	}
	return delivered
}

// msgHeaderBlock returns the header section (everything before the first
// blank line), lowercased for case-insensitive header-name matching.
func msgHeaderBlock(msg []byte) string {
	if idx := bytes.Index(msg, []byte("\r\n\r\n")); idx >= 0 {
		return strings.ToLower(string(msg[:idx]))
	}
	return strings.ToLower(string(msg))
}

func countMessageIDHeaders(msg []byte) int {
	return strings.Count(msgHeaderBlock(msg), "message-id:")
}

// RFC 5322 §3.6.4: the Message-ID check must only consider the header block.
// A "message-id:" in the body is quoted text (ordinary in forwards and
// replies) and must not suppress generation of the real header, otherwise the
// stored message has no Message-ID and threading (References/In-Reply-To)
// cannot link it.
func TestHandleDATA_BodyMessageIDDoesNotSuppressHeader(t *testing.T) {
	wire := "Subject: Fwd: something\r\n" +
		"From: sender@example.com\r\n" +
		"To: rcpt@example.com\r\n" +
		"\r\n" +
		"see below, the original said message-id: <orig-123@example.com>\r\n" +
		"regards\r\n" +
		".\r\n"

	got := deliverData(t, wire)
	if !strings.Contains(msgHeaderBlock(got), "message-id:") {
		t.Errorf("delivered message has no Message-ID header although the only "+
			"'message-id:' occurrence is in the body; the check must be scoped to "+
			"the header block. Delivered:\n%s", got)
	}
	if n := countMessageIDHeaders(got); n != 1 {
		t.Errorf("expected exactly 1 Message-ID header, got %d", n)
	}
}

// Controls: a message with no message-id anywhere still gains one, and a
// message that already has one keeps that exact header without gaining a
// second.
func TestHandleDATA_MessageIDControls(t *testing.T) {
	t.Run("none anywhere gains one", func(t *testing.T) {
		wire := "Subject: plain\r\nFrom: s@example.com\r\nTo: r@example.com\r\n\r\nbody\r\n.\r\n"
		got := deliverData(t, wire)
		if n := countMessageIDHeaders(got); n != 1 {
			t.Errorf("expected 1 generated Message-ID header, got %d", n)
		}
	})

	t.Run("existing header preserved and not duplicated", func(t *testing.T) {
		wire := "Subject: has id\r\nFrom: s@example.com\r\nTo: r@example.com\r\n" +
			"Message-ID: <mine-999@example.com>\r\n\r\nbody mentioning message-id: <body-1@example.com>\r\n.\r\n"
		got := deliverData(t, wire)
		if !bytes.Contains(got, []byte("Message-ID: <mine-999@example.com>")) {
			t.Errorf("existing Message-ID header must be preserved; got:\n%s", got)
		}
		if n := countMessageIDHeaders(got); n != 1 {
			t.Errorf("expected exactly 1 Message-ID header, got %d", n)
		}
	})
}
