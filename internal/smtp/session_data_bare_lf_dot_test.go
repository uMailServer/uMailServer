package smtp

// F4906: when readData fails mid-message (line too long, NUL byte, size
// limit), the session replies immediately and returns to the command loop
// without consuming the rest of the message. The remaining message lines are
// then executed as SMTP commands (DATA-phase command smuggling).
//
// Driven through the real command loop (Server.handleConnection) over net.Pipe.

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

type f4906Client struct {
	conn net.Conn
	r    *bufio.Reader
}

func f4906Start(t *testing.T, maxSize int64, delivered *[][]byte) *f4906Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{
		Hostname:       "mx.test",
		MaxMessageSize: maxSize,
		MaxRecipients:  10,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}, nil)
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error {
		*delivered = append(*delivered, append([]byte(nil), data...))
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	c := &f4906Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t) // greeting
	return c
}

// reply reads one complete (possibly multi-line) reply and returns its code.
func (c *f4906Client) reply(t *testing.T) string {
	t.Helper()
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("INVALID: read reply: %v", err)
		}
		if len(line) >= 4 && line[3] == ' ' {
			return line[:3]
		}
		if len(line) < 4 {
			t.Fatalf("INVALID: short reply %q", line)
		}
	}
}

func (c *f4906Client) cmd(t *testing.T, line string) string {
	t.Helper()
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("INVALID: write: %v", err)
	}
	return c.reply(t)
}

// f4906Transaction sends body after DATA (written by a goroutine so the
// server can reply mid-stream on a synchronous pipe), then QUIT, and returns
// every reply code received after the 354.
func f4906Transaction(t *testing.T, c *f4906Client, body string) []string {
	t.Helper()
	for _, l := range []string{"EHLO client.test", "MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>"} {
		if code := c.cmd(t, l); code != "250" {
			t.Fatalf("INVALID: %s -> %s", l, code)
		}
	}
	if code := c.cmd(t, "DATA"); code != "354" {
		t.Fatalf("INVALID: DATA -> %s", code)
	}
	go func() { _, _ = c.conn.Write([]byte(body + "QUIT\r\n")) }()
	var codes []string
	for {
		code := c.reply(t)
		codes = append(codes, code)
		if code == "221" || len(codes) > 10 {
			return codes
		}
	}
}

func TestDATABareLFDotControl(t *testing.T) {
	var delivered [][]byte
	c := f4906Start(t, 1<<20, &delivered)
	codes := f4906Transaction(t, c, "Subject: ok\r\n\r\nhello\r\n.\r\n")
	t.Logf("control replies after 354: %v", codes)
	if strings.Join(codes, ",") != "250,221" || len(delivered) != 1 {
		t.Fatalf("INVALID CONTROL: replies %v delivered %d", codes, len(delivered))
	}
}

// <LF>.<CRLF> is not the end-of-data indicator (RFC 5321 §4.1.1.4 requires
// <CRLF>.<CRLF>). Accepting it lets a message relayed by a CRLF-strict
// upstream end early here, so the rest of the message is executed as
// commands: SMTP smuggling.
func TestDATABareLFDotBareLFDot(t *testing.T) {
	var delivered [][]byte
	c := f4906Start(t, 1<<20, &delivered)
	codes := f4906Transaction(t, c, "Subject: x\r\n\r\nhello\n.\r\nNOOP\r\n.\r\n")
	t.Logf("EXPECTED: [250 221], one delivery containing the NOOP line")
	t.Logf("ACTUAL:   %v delivered=%d", codes, len(delivered))
	if strings.Join(codes, ",") != "250,221" || len(delivered) != 1 || !strings.Contains(string(delivered[0]), "NOOP") {
		t.Errorf("DEFECT F4906: <LF>.<CRLF> ended DATA early and the tail ran as commands: replies %v", codes)
	}
}

// Edge: dot-stuffing after CRLF is still removed, a dot line after a bare LF
// is preserved verbatim, and "\r\n.\n" (dot + bare LF) does not end DATA
// (its transparency dot is removed, as for any dot-led line).
func TestDATABareLFDotEdgeStuffing(t *testing.T) {
	var delivered [][]byte
	c := f4906Start(t, 1<<20, &delivered)
	codes := f4906Transaction(t, c, "Subject: x\r\n\r\n..stuffed\r\nlf\n..kept\r\nx\r\n.\nNOOP\r\n.\r\n")
	if strings.Join(codes, ",") != "250,221" || len(delivered) != 1 {
		t.Fatalf("DEFECT F4906: replies %v delivered=%d", codes, len(delivered))
	}
	got := string(delivered[0])
	for _, want := range []string{"\r\n.stuffed\r\n", "lf\n..kept\r\n", "x\r\n\nNOOP\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("DEFECT F4906: delivered body missing %q: %q", want, got)
		}
	}
}

// Edge: a bare-LF dot line at the very end must not swallow the session:
// the real <CRLF>.<CRLF> after it still terminates.
func TestDATABareLFDotEdgeBareLFThenTerminator(t *testing.T) {
	var delivered [][]byte
	c := f4906Start(t, 1<<20, &delivered)
	codes := f4906Transaction(t, c, "a\n.\r\n.\r\n")
	if strings.Join(codes, ",") != "250,221" || len(delivered) != 1 || !strings.HasSuffix(string(delivered[0]), "\r\na\n.\r\n") {
		t.Errorf("DEFECT F4906: replies %v delivered=%q", codes, delivered)
	}
}
