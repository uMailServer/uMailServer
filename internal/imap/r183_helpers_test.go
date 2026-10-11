package imap

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

type r183Client struct {
	t  *testing.T
	c  net.Conn
	r  *bufio.Reader
	n  int
	ss *Session
}

func newR183(t *testing.T, ms *BboltMailstore, user string) *r183Client {
	t.Helper()
	client, srv := net.Pipe()
	server := NewServer(&Config{Addr: ":0"}, ms)
	ss := NewSession(srv, server)
	ss.state = StateAuthenticated
	ss.user = user
	ss.tlsActive = true
	go ss.Handle()
	t.Cleanup(func() { ss.Close(); client.Close() })
	return &r183Client{t: t, c: client, r: bufio.NewReader(client), ss: ss}
}

func (c *r183Client) send(line string) {
	c.c.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := c.c.Write([]byte(line + "\r\n")); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// cmd sends a tagged command and returns all response lines up to and including the tagged one.
func (c *r183Client) cmd(line string) []string {
	c.n++
	tag := "t" + string(rune('a'+c.n%26)) + string(rune('0'+c.n/26))
	c.send(tag + " " + line)
	return c.until(tag)
}

func (c *r183Client) until(tag string) []string {
	var out []string
	for {
		c.c.SetReadDeadline(time.Now().Add(4 * time.Second))
		l, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("read after %q: %v", out, err)
		}
		l = strings.TrimRight(l, "\r\n")
		out = append(out, l)
		if strings.HasPrefix(l, tag+" ") {
			return out
		}
	}
}

func joined(ls []string) string { return strings.Join(ls, "\n") }
