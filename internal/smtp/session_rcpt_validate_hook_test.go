package smtp

// F4907: when readData fails mid-message (line too long, NUL byte, size
// limit), the session replies immediately and returns to the command loop
// without consuming the rest of the message. The remaining message lines are
// then executed as SMTP commands (DATA-phase command smuggling).
//
// Driven through the real command loop (Server.handleConnection) over net.Pipe.

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type f4907Client struct {
	conn net.Conn
	r    *bufio.Reader
}

// reply reads one complete (possibly multi-line) reply and returns its code.
func (c *f4907Client) reply(t *testing.T) string {
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

func (c *f4907Client) cmd(t *testing.T, line string) string {
	t.Helper()
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("INVALID: write: %v", err)
	}
	return c.reply(t)
}

// F4907: Server.SetValidateHandler installs a recipient/envelope policy hook,
// but the session never calls it, so a policy installed through the public
// API (for example "no relaying to foreign domains") is silently bypassed.
func f4907Policy(srv *Server, calls *int) {
	srv.SetValidateHandler(func(from string, to []string) error {
		*calls++
		for _, r := range to {
			if !strings.HasSuffix(r, "@mx.test") {
				return errors.New("relay denied")
			}
		}
		return nil
	})
}

func f4907Run(t *testing.T, rcpt string) (string, int, int) {
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	var delivered [][]byte
	calls := 0
	f4907Policy(srv, &calls)
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error {
		delivered = append(delivered, data)
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	defer func() { _ = clientConn.Close(); <-done }()
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	c := &f4907Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	c.cmd(t, "EHLO client.test")
	if code := c.cmd(t, "MAIL FROM:<spammer@sender.test>"); code != "250" {
		t.Fatalf("INVALID: MAIL -> %s", code)
	}
	code := c.cmd(t, "RCPT TO:<"+rcpt+">")
	if code == "250" {
		c.cmd(t, "DATA")
		go func() { _, _ = c.conn.Write([]byte("Subject: x\r\n\r\nbody\r\n.\r\n")) }()
		c.reply(t)
	}
	c.cmd(t, "QUIT")
	return code, calls, len(delivered)
}

func TestRCPTValidateHookControl(t *testing.T) {
	code, _, n := f4907Run(t, "user@mx.test")
	t.Logf("control: RCPT local -> %s delivered=%d", code, n)
	if code != "250" || n != 1 {
		t.Fatalf("INVALID CONTROL: RCPT %s delivered %d", code, n)
	}
}

func TestRCPTValidateHookForeignRecipient(t *testing.T) {
	code, calls, n := f4907Run(t, "victim@external.example")
	t.Logf("EXPECTED: RCPT rejected with 5xx, validate hook called, nothing delivered")
	t.Logf("ACTUAL:   RCPT -> %s hookCalls=%d delivered=%d", code, calls, n)
	if !strings.HasPrefix(code, "5") || calls == 0 || n != 0 {
		t.Errorf("DEFECT F4907: validate handler bypassed: RCPT %s, hook calls %d, delivered %d", code, calls, n)
	}
}

// Edge: mixed recipients — only the rejected one is refused and delivery
// goes to the accepted one; without a hook every recipient is accepted.
func TestRCPTValidateHookEdgeMixedAndNoHook(t *testing.T) {
	for _, withHook := range []bool{true, false} {
		serverConn, clientConn := net.Pipe()
		srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
		calls := 0
		if withHook {
			f4907Policy(srv, &calls)
		}
		var got []string
		srv.SetDeliveryHandler(func(from string, to []string, data []byte) error { got = to; return nil })
		done := make(chan struct{})
		go func() { srv.handleConnection(serverConn); close(done) }()
		_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
		c := &f4907Client{conn: clientConn, r: bufio.NewReader(clientConn)}
		c.reply(t)
		c.cmd(t, "EHLO client.test")
		c.cmd(t, "MAIL FROM:<a@sender.test>")
		ext := c.cmd(t, "RCPT TO:<x@external.example>")
		loc := c.cmd(t, "RCPT TO:<u@mx.test>")
		c.cmd(t, "DATA")
		go func() { _, _ = c.conn.Write([]byte("Subject: x\r\n\r\nb\r\n.\r\n")) }()
		c.reply(t)
		c.cmd(t, "QUIT")
		_ = clientConn.Close()
		<-done
		want := "550,250,u@mx.test"
		if !withHook {
			want = "250,250,x@external.example,u@mx.test"
		}
		if have := strings.Join(append([]string{ext, loc}, got...), ","); have != want {
			t.Errorf("DEFECT F4907 (hook=%v): want %s have %s", withHook, want, have)
		}
	}
}
