package pop3

// Regression tests for the POP3 TOP header/body split.
//
// RFC 1939 §3.1: TOP <msg> <n> returns the message's header, a blank line, and
// the first <n> lines of the message body.
//
// Bug (internal/pop3/server.go sendTop): the header terminator was located with
// a CRLF blank line ("\r\n\r\n") and a bare-LF fallback ("\n\n"), but the body
// was always sliced at headerEnd+4. For a bare-LF message the separator is 2
// bytes, so the slice dropped the first 2 bytes of the body, truncating the
// first body line returned to the client.

import (
	"strings"
	"testing"
)

// runTOP performs a full POP3 session (USER/PASS) and returns the +OK status
// line and the body lines of a TOP command through the real server path.
func runTOP(t *testing.T, raw string, cmd string) (string, []string) {
	t.Helper()
	store := newMockMailstore()
	store.dataMap[1] = []byte(raw)
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	status, lines := sendCmdMulti(t, conn, reader, cmd)
	return status, lines
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func assertHasLines(t *testing.T, lines []string, want []string, label string) {
	t.Helper()
	for _, w := range want {
		if !containsLine(lines, w) {
			t.Fatalf("%s: missing intact line %q; got %q", label, w, lines)
		}
	}
}

// TestTOP_LFOnlyMessageBodyNotTruncated: a bare-LF message must have its body
// lines returned intact by TOP.
func TestTOP_LFOnlyMessageBodyNotTruncated(t *testing.T) {
	raw := "From: a@example.com\nSubject: LF\n\nLine1\nLine2\nLine3\n"
	status, lines := runTOP(t, raw, "TOP 1 3")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("TOP: expected +OK, got %s", status)
	}
	assertHasLines(t, lines, []string{"Line1", "Line2", "Line3"}, "LF-only TOP")
}

// TestTOP_CRLFMessageControl: a CRLF message must still be handled correctly
// (control that passed before the fix).
func TestTOP_CRLFMessageControl(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: CRLF\r\n\r\nLine1\r\nLine2\r\nLine3\r\n"
	status, lines := runTOP(t, raw, "TOP 1 3")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("TOP: expected +OK, got %s", status)
	}
	assertHasLines(t, lines, []string{"Line1", "Line2", "Line3"}, "CRLF TOP")
}

// TestTOP_LFOnly_RespectsLineCount boundary: TOP must return exactly the first
// n body lines, so Line3 is absent when n=1.
func TestTOP_LFOnly_RespectsLineCount(t *testing.T) {
	raw := "From: a@example.com\nSubject: LF\n\nLine1\nLine2\nLine3\n"
	status, lines := runTOP(t, raw, "TOP 1 1")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("TOP: expected +OK, got %s", status)
	}
	assertHasLines(t, lines, []string{"Line1"}, "LF-only TOP 1 line")
	if containsLine(lines, "Line3") {
		t.Fatalf("LF-only TOP 1 line: Line3 should not be returned; got %q", lines)
	}
}

// TestTOP_LFOnly_SingleBodyLine boundary: a one-line body must be returned
// intact, not truncated to the empty string.
func TestTOP_LFOnly_SingleBodyLine(t *testing.T) {
	raw := "Subject: LF\n\nOnlyBody\n"
	status, lines := runTOP(t, raw, "TOP 1 1")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("TOP: expected +OK, got %s", status)
	}
	assertHasLines(t, lines, []string{"OnlyBody"}, "LF-only single body line")
}

// TestTOP_NoHeaderSeparator: a message with no blank line after the headers has
// no body to return; sendTop falls back to sending the whole message.
func TestTOP_NoHeaderSeparator(t *testing.T) {
	raw := "From: a@example.com\nSubject: NoSep\nLine1\n"
	status, lines := runTOP(t, raw, "TOP 1 1")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("TOP: expected +OK, got %s", status)
	}
	// The whole message (no header/body split) is emitted; Line1 must be present.
	assertHasLines(t, lines, []string{"Line1"}, "no-separator TOP")
}
