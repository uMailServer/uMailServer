package imap

import (
	"strings"
	"testing"
)

// F7230: an APPEND literal is a sequence of octets (RFC 3501 §4.3); messages in
// legacy 8-bit charsets (ISO-8859-x, Shift-JIS) or with 8bit/binary parts are
// not UTF-8 and must be stored byte-for-byte, not refused.
func TestF7230_AppendAcceptsNonUTF8Octets(t *testing.T) {
	c, _ := round82Server(t)
	msg := "From: a@example.com\r\nSubject: latin1\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: 8bit\r\n\r\ncaf\xe9 \xff\xfe\r\n"
	got := round75Tagged(round82Append(t, c, "p1", "INBOX", msg))
	if !strings.HasPrefix(got, "p1 OK") {
		t.Fatalf("DEFECT F7230: APPEND of 8-bit non-UTF-8 message replied %q", got)
	}
	c.cmd("p2 SELECT INBOX")
	out := strings.Join(c.cmd("p3 FETCH 1 BODY.PEEK[TEXT]"), "\n")
	if !strings.Contains(out, "caf\xe9 \xff\xfe") {
		t.Fatalf("DEFECT F7230: stored bytes altered: %q", out)
	}
}

// F7231: the <origin> echoed in a partial FETCH response is the client's
// 32-bit number, not an internal clamp.
func TestF7231_PartialOriginEchoedVerbatim(t *testing.T) {
	c, _ := round82Server(t, round91Msg("one", 200))
	c.cmd("q1 SELECT INBOX")
	out := strings.Join(c.cmd("q2 FETCH 1 BODY.PEEK[]<4294967295.5>"), "\n")
	if !strings.Contains(out, "BODY[]<4294967295> {0}") {
		t.Fatalf("DEFECT F7231: origin not echoed verbatim: %q", out)
	}
}
