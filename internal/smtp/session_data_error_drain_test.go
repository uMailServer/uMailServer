package smtp

// F4905: when readData fails mid-message (line too long, NUL byte, size
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

type f4905Client struct {
	conn net.Conn
	r    *bufio.Reader
}

func f4905Start(t *testing.T, maxSize int64, delivered *[][]byte) *f4905Client {
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
	c := &f4905Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t) // greeting
	return c
}

// reply reads one complete (possibly multi-line) reply and returns its code.
func (c *f4905Client) reply(t *testing.T) string {
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

func (c *f4905Client) cmd(t *testing.T, line string) string {
	t.Helper()
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("INVALID: write: %v", err)
	}
	return c.reply(t)
}

// f4905Transaction sends body after DATA (written by a goroutine so the
// server can reply mid-stream on a synchronous pipe), then QUIT, and returns
// every reply code received after the 354.
func f4905Transaction(t *testing.T, c *f4905Client, body string) []string {
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

func TestDATAErrorDrainControl(t *testing.T) {
	var delivered [][]byte
	c := f4905Start(t, 1<<20, &delivered)
	codes := f4905Transaction(t, c, "Subject: ok\r\n\r\nhello\r\nNOOP\r\n.\r\n")
	t.Logf("control replies after 354: %v", codes)
	if strings.Join(codes, ",") != "250,221" || len(delivered) != 1 {
		t.Fatalf("INVALID CONTROL: replies %v delivered %d", codes, len(delivered))
	}
}

func f4905Check(t *testing.T, name string, maxSize int64, body string) {
	var delivered [][]byte
	c := f4905Start(t, maxSize, &delivered)
	codes := f4905Transaction(t, c, body)
	t.Logf("EXPECTED: [5xx/4xx 221] (one reply for the whole message, tail not executed)")
	t.Logf("ACTUAL:   %v delivered=%d", codes, len(delivered))
	if len(codes) != 2 || codes[0] == "250" || codes[1] != "221" || len(delivered) != 0 {
		t.Errorf("DEFECT F4905 (%s): message tail executed as commands: replies %v", name, codes)
	}
}

func TestDATAErrorDrainLongLine(t *testing.T) {
	f4905Check(t, "long line", 1<<20,
		"Subject: x\r\n\r\n"+strings.Repeat("a", 1100)+"\r\nNOOP\r\n.\r\n")
}

func TestDATAErrorDrainNulByte(t *testing.T) {
	f4905Check(t, "nul byte", 1<<20,
		"Subject: x\r\n\r\nbad\x00line\r\nNOOP\r\n.\r\n")
}

func TestDATAErrorDrainTooLarge(t *testing.T) {
	f4905Check(t, "too large", 64,
		"Subject: x\r\n\r\n"+strings.Repeat("b", 100)+"\r\nNOOP\r\n.\r\n")
}

// Edge: after a rejected message the session is usable for a new
// transaction, and RCPT without a new MAIL is refused (transaction reset).
func TestDATAErrorDrainEdgeNextTransaction(t *testing.T) {
	var delivered [][]byte
	c := f4905Start(t, 1<<20, &delivered)
	for _, l := range []string{"EHLO client.test", "MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>"} {
		c.cmd(t, l)
	}
	if code := c.cmd(t, "DATA"); code != "354" {
		t.Fatalf("INVALID: DATA -> %s", code)
	}
	go func() { _, _ = c.conn.Write([]byte(strings.Repeat("z", 1200) + "\r\n.\r\n")) }()
	if code := c.reply(t); code == "250" {
		t.Errorf("DEFECT F4905: overlong line accepted")
	}
	if code := c.cmd(t, "RCPT TO:<b@mx.test>"); code != "503" {
		t.Errorf("DEFECT F4905: transaction not reset after failed DATA: RCPT -> %s", code)
	}
	codes := f4905Transaction(t, c, "Subject: ok\r\n\r\nfine\r\n.\r\n")
	if strings.Join(codes, ",") != "250,221" || len(delivered) != 1 {
		t.Errorf("DEFECT F4905: follow-up transaction failed: %v delivered=%d", codes, len(delivered))
	}
}

// Edge: error on the very first line, and an empty message (immediate ".").
func TestDATAErrorDrainEdgeFirstLineAndEmpty(t *testing.T) {
	f4905Check(t, "first line nul", 1<<20, "\x00\r\nNOOP\r\n.\r\n")
	var delivered [][]byte
	c := f4905Start(t, 1<<20, &delivered)
	codes := f4905Transaction(t, c, ".\r\n")
	if strings.Join(codes, ",") != "250,221" {
		t.Errorf("DEFECT F4905: empty message: %v", codes)
	}
}
