package smtp

// Regression tests for the DATA handler rejecting legitimate binary mail.
//
// readData previously applied utf8.Valid to every line of the DATA payload
// and aborted the whole message on any invalid UTF-8 byte, so ISO-8859-1 /
// Windows-1252 bodies and raw 8-bit content were rejected with 451
// "Requested action aborted". RFC 6532 permits UTF-8 mail when SMTPUTF8 is
// negotiated; it never licenses rejecting non-UTF-8 data, and RFC 5321 DATA
// semantics (dot-stuffing/termination over line-oriented content) plus
// universal MTA practice accept arbitrary bytes. Null bytes remain rejected
// by a separate, intentional security check that these tests pin in place.

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

// driveDATA pushes a full DATA payload through the real session and reports
// the final SMTP response line plus what onDeliver captured.
func driveDATA(t *testing.T, bodyLines ...string) (string, string, []string, []byte) {
	t.Helper()
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	type delivery struct {
		from string
		to   []string
		data []byte
	}
	delivered := make(chan delivery, 1)
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		delivered <- delivery{from, to, data}
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp354, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read 354 response: %v", err)
	}
	if !strings.HasPrefix(resp354, "354") {
		t.Fatalf("Expected 354 response, got: %s", resp354)
	}

	payload := strings.Join(bodyLines, "") + ".\r\n"
	if _, err := clientConn.Write([]byte(payload)); err != nil {
		t.Fatalf("Failed to write DATA payload: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("handleDATA returned error: %v", err)
	}
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	finalResp, _ := reader.ReadString('\n')

	var d delivery
	select {
	case d = <-delivered:
	default:
	}
	return finalResp, d.from, d.to, d.data
}

func TestHandleDATA_AcceptsRawBinaryBody(t *testing.T) {
	// "café" with the é as the raw single byte 0xE9 (ISO-8859-1): invalid UTF-8,
	// but perfectly legal DATA content per RFC 5321.
	finalResp, _, _, data := driveDATA(t,
		"Subject: latin1\r\nFrom: sender@example.com\r\nTo: rcpt@example.com\r\n\r\n",
		"caf\xe9 au lait\r\n",
	)
	if !strings.HasPrefix(finalResp, "250") {
		t.Fatalf("FAIL: legitimate binary body rejected: got %q, want 250 (the server must not require UTF-8 in DATA)", strings.TrimSpace(finalResp))
	}
	if len(data) == 0 {
		t.Fatal("FAIL: message was not delivered")
	}
	if !strings.Contains(string(data), "caf\xe9 au lait") {
		t.Fatal("FAIL: delivered data lost the raw byte (dot-unstuffing or decoding changed the body)")
	}
}

func TestHandleDATA_ValidUTF8StillAccepted(t *testing.T) {
	// Control: a valid UTF-8 body must keep working (pre- and post-fix).
	finalResp, _, _, data := driveDATA(t,
		"Subject: utf8\r\nFrom: sender@example.com\r\nTo: rcpt@example.com\r\n\r\n",
		"caf\u00e9 au lait\r\n",
	)
	if !strings.HasPrefix(finalResp, "250") {
		t.Fatalf("CONTROL FAILED (harness): valid UTF-8 body rejected: got %q", strings.TrimSpace(finalResp))
	}
	if !strings.Contains(string(data), "caf\u00e9 au lait") {
		t.Fatal("CONTROL FAILED (harness): valid UTF-8 body corrupted in delivery")
	}
}

func TestHandleDATA_RejectsNullBytesStill(t *testing.T) {
	// Boundary: the separate intentional null-byte security check must stay.
	finalResp, _, _, data := driveDATA(t,
		"Subject: null\r\nFrom: sender@example.com\r\nTo: rcpt@example.com\r\n\r\n",
		"bad\x00byte\r\n",
	)
	if !strings.HasPrefix(finalResp, "451") {
		t.Fatalf("CONTROL FAILED (harness): null-byte body unexpectedly accepted: got %q", strings.TrimSpace(finalResp))
	}
	if data != nil {
		t.Fatal("CONTROL FAILED (harness): null-byte body was delivered")
	}
}
