package smtp

import (
	"strings"
	"testing"
)

// F7311: a second MAIL FROM while a transaction is open is 503 (RFC 5321
// §4.1.4); the open transaction survives, and RSET allows a new one.
func TestF7311NestedMailIs503(t *testing.T) {
	c := r85Start(t, 1<<20, nil, nil)
	c.hello(t)
	if r := c.cmd(t, "MAIL FROM:<a@example.org>\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("control: first MAIL: %q", r)
	}
	if r := c.cmd(t, "MAIL FROM:<b@example.org>\r\n"); !strings.HasPrefix(r, "503") {
		t.Fatalf("nested MAIL after MAIL answered %q, want 503", r)
	}
	if r := c.cmd(t, "RCPT TO:<x@mx.test>\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("original transaction lost after 503: %q", r)
	}
	if r := c.cmd(t, "MAIL FROM:<b@example.org>\r\n"); !strings.HasPrefix(r, "503") {
		t.Fatalf("nested MAIL after RCPT answered %q, want 503", r)
	}
	c.cmd(t, "RSET\r\n")
	if r := c.cmd(t, "MAIL FROM:<b@example.org>\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("MAIL after RSET answered %q, want 250", r)
	}
}
