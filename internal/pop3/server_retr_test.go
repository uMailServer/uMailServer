package pop3

// Regression tests for POP3 RETR dot-stuffing.
//
// RFC 1939 §3.1 (RETR): the server transmits the message, finishing it with a
// line containing only a period; "if the first octet of a line is a period,
// and there are other octets on the line, the server ... transmits the line
// with an additional period prepended."
//
// Bug (internal/pop3/server.go RETR): the message was written verbatim with
// s.writer.Write(msg.Data), with no dot-stuffing. A body line that is exactly
// "." was therefore emitted as the terminator line and a POP3 client stopped
// reading there, silently truncating the rest of the message. (WriteDataLine,
// used by TOP, already dot-stuffed; RETR's raw write did not.)

import (
	"strings"
	"testing"
)

// runRETR performs a POP3 session and returns the data lines a real client would
// receive for RETR 1 (everything up to, but not including, the "." terminator).
func runRETR(t *testing.T, raw string) (string, []string) {
	t.Helper()
	store := newMockMailstore()
	store.dataMap[0] = []byte(raw)
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	status, lines := sendCmdMulti(t, conn, reader, "RETR 1")
	return status, lines
}

func retrHasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// TestRETR_DotStuffsBodyDotLine: a body line that is exactly "." must be
// transmitted dot-stuffed ("..") so the client does not treat it as the
// end-of-message marker; the following content must still be delivered.
func TestRETR_DotStuffsBodyDotLine(t *testing.T) {
	raw := "Subject: Test\r\n\r\nLine1\r\n.\r\nLine3\r\n"
	status, lines := runRETR(t, raw)
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("RETR: expected +OK, got %s", status)
	}
	if !retrHasLine(lines, "Line3") {
		t.Fatalf("RETR did not dot-stuff the body line \".\"; client read it as end-of-message and "+
			"truncated the message. Received: %q (RFC 1939 §3.1)", lines)
	}
}

// TestRETR_DotStuffsDoubledDotLine: a body line ".." must be transmitted as
// "..." so the client de-stuffs it back to ".." rather than reading the first
// dot as a terminator.
func TestRETR_DotStuffsDoubledDotLine(t *testing.T) {
	raw := "Subject: Test\r\n\r\nbefore\r\n..\r\nafter\r\n"
	_, lines := runRETR(t, raw)
	// The client sees the stuffed form and would de-stuff to ".."; the key
	// assertion is that "after" is still delivered (no early termination).
	if !retrHasLine(lines, "after") {
		t.Fatalf("RETR terminated early on a \"..\" body line; received: %q", lines)
	}
}

// TestRETR_DotLineFirstInBody: a message whose very first body line is "."
// (no preceding content) must still deliver the content after it.
func TestRETR_DotLineFirstInBody(t *testing.T) {
	raw := "Subject: Test\r\n\r\n.\r\nContent\r\n"
	_, lines := runRETR(t, raw)
	if !retrHasLine(lines, "Content") {
		t.Fatalf("RETR terminated early when body began with \".\"; received: %q", lines)
	}
}

// TestRETR_PlainMessageControl: a message with no dot-prefixed body lines must
// be delivered verbatim, both before and after the fix.
func TestRETR_PlainMessageControl(t *testing.T) {
	raw := "Subject: Test\r\n\r\nLine1\r\nLine2\r\n"
	_, lines := runRETR(t, raw)
	for _, want := range []string{"Line1", "Line2"} {
		if !retrHasLine(lines, want) {
			t.Fatalf("plain message control: missing %q; received %q", want, lines)
		}
	}
}

// TestDotStuffData_Unit exercises the dot-stuffing helper directly, including
// bare-LF line endings and the empty / no-dot boundary cases.
func TestDotStuffData_Unit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no dot lines", "a\r\nb\r\n", "a\r\nb\r\n"},
		{"dot line mid crlf", "a\r\n.\r\nb\r\n", "a\r\n..\r\nb\r\n"},
		{"dot line first crlf", ".\r\na\r\n", "..\r\na\r\n"},
		{"bare lf dot line", "a\n.\nb\n", "a\n..\nb\n"},
		{"dotdot line", "a\r\n..\r\n", "a\r\n...\r\n"},
		{"dot after cr only", "a\r.b\r\n", "a\r.b\r\n"}, // '.' mid-line is not stuffed
		{"no trailing newline dot", "a\r\n.", "a\r\n.."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(dotStuffData([]byte(tt.in)))
			if got != tt.want {
				t.Errorf("dotStuffData(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
