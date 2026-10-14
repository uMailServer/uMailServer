package smtp

import (
	"strings"
	"testing"
)

// r297Codes runs one DATA transaction (see f4905Transaction) and returns the
// replies after the 354 together with the messages that were delivered.
func r297Codes(t *testing.T, body string) ([]string, int) {
	t.Helper()
	var delivered [][]byte
	c := f4905Start(t, 1<<20, &delivered)
	return f4905Transaction(t, c, body), len(delivered)
}

// F7710: content that can never be accepted gets a permanent 554, not the
// temporary 451 that makes the sender retry for days.
func TestDATAPermanentContentErrors(t *testing.T) {
	cases := map[string]string{
		"overlong line": "Subject: x\r\n\r\n" + strings.Repeat("a", 1100) + "\r\n.\r\n",
		"NUL byte":      "Subject: x\r\n\r\nbad\x00line\r\n.\r\n",
		"bare CR":       "Subject: x\r\n\r\nbad\rline\r\n.\r\n", // control: already 554
	}
	for name, body := range cases {
		codes, delivered := r297Codes(t, body)
		if len(codes) < 1 || codes[0] != "554" || delivered != 0 {
			t.Errorf("%s: replies %v delivered %d, want 554 and nothing delivered", name, codes, delivered)
		}
	}
}

// Boundary: a line of exactly 1000 octets including CRLF is accepted, one octet
// more is refused (RFC 5322 section 2.1.1 / RFC 5321 section 4.5.3.1.6).
func TestDATALineLengthBoundary(t *testing.T) {
	ok, delivered := r297Codes(t, "Subject: x\r\n\r\n"+strings.Repeat("a", 998)+"\r\n.\r\n")
	if len(ok) < 1 || ok[0] != "250" || delivered != 1 {
		t.Fatalf("1000-octet line: replies %v delivered %d, want 250", ok, delivered)
	}
	over, delivered := r297Codes(t, "Subject: x\r\n\r\n"+strings.Repeat("a", 999)+"\r\n.\r\n")
	if len(over) < 1 || over[0] != "554" || delivered != 0 {
		t.Fatalf("1001-octet line: replies %v delivered %d, want 554", over, delivered)
	}
}

// A refused message leaves the session usable for the next transaction.
func TestDATAPermanentErrorKeepsSessionUsable(t *testing.T) {
	var delivered [][]byte
	c := f4905Start(t, 1<<20, &delivered)
	for _, l := range []string{"EHLO client.test", "MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>", "DATA"} {
		c.cmd(t, l)
	}
	if code := c.cmd(t, "Subject: x\r\n\r\n"+strings.Repeat("a", 1100)+"\r\n."); code != "554" {
		t.Fatalf("overlong line: %s, want 554", code)
	}
	for _, l := range []string{"MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>"} {
		if code := c.cmd(t, l); code != "250" {
			t.Fatalf("%s after the refusal: %s, want 250", l, code)
		}
	}
}
