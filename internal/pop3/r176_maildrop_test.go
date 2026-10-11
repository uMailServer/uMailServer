package pop3

// Round 176 regression tests (F6540, F6541, F6543).

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

type pipeClient struct {
	c  net.Conn
	rd *bufio.Reader
}

func startPipeSession(t *testing.T, srv *Server) (*pipeClient, chan struct{}) {
	t.Helper()
	c1, c2 := net.Pipe()
	sess := NewSession(c1, srv)
	done := make(chan struct{})
	go func() {
		sess.Handle()
		c1.Close()
		close(done)
	}()
	t.Cleanup(func() { c2.Close() })
	return &pipeClient{c: c2, rd: bufio.NewReader(c2)}, done
}

func (p *pipeClient) cmd(t *testing.T, line string) string {
	t.Helper()
	p.c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := p.c.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
	resp, err := p.rd.ReadString('\n')
	if err != nil {
		t.Fatalf("read after %q: %v", line, err)
	}
	return strings.TrimRight(resp, "\r\n")
}

// F6540: RFC 1939 §8 — only one session may hold a user's maildrop; a drop
// releases it without deleting anything.
func TestMaildropExclusiveLock_F6540(t *testing.T) {
	srv, store := newUpdateServer(t)

	a, aDone := startPipeSession(t, srv)
	if r := a.cmd(t, "USER alice"); !strings.HasPrefix(r, "+OK") {
		t.Fatal(r)
	}
	if r := a.cmd(t, "PASS pw"); !strings.HasPrefix(r, "+OK") {
		t.Fatal(r)
	}
	if r := a.cmd(t, "DELE 1"); !strings.HasPrefix(r, "+OK") {
		t.Fatal(r)
	}

	b, _ := startPipeSession(t, srv)
	b.cmd(t, "USER alice")
	if r := b.cmd(t, "PASS pw"); !strings.HasPrefix(r, "-ERR") {
		t.Fatalf("second concurrent session got the maildrop: %q", r)
	}

	// Drop connection without QUIT: nothing deleted, lock released.
	a.c.Close()
	<-aDone
	if len(store.deleted) != 0 {
		t.Fatalf("connection drop deleted messages: %v", store.deleted)
	}
	c, _ := startPipeSession(t, srv)
	c.cmd(t, "USER alice")
	if r := c.cmd(t, "PASS pw"); !strings.HasPrefix(r, "+OK") {
		t.Fatalf("lock not released after drop: %q", r)
	}
}

// F6541: TOP must stop at the first blank line, not a later CRLFCRLF in the body.
func TestTopHeaderSeparatorEarliest_F6541(t *testing.T) {
	var buf bytes.Buffer
	s := &Session{writer: bufio.NewWriter(&buf), server: &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s.sendTop([]byte("Subject: x\n\nsecret1\r\n\r\nsecret2\n"), 0)
	s.writer.Flush()
	if strings.Contains(buf.String(), "secret") {
		t.Fatalf("TOP 0 leaked body: %q", buf.String())
	}
}

// F6543: repeated failed PASS on one connection ends the connection.
func TestPassFailureCapPerConnection_F6543(t *testing.T) {
	srv, _ := newUpdateServer(t)
	p, done := startPipeSession(t, srv)
	for i := 0; i < maxFailedPassPerConn+1; i++ {
		p.c.SetDeadline(time.Now().Add(2 * time.Second))
		p.c.Write([]byte("USER alice\r\n"))
		if _, err := p.rd.ReadString('\n'); err != nil {
			break
		}
		p.c.Write([]byte("PASS bad\r\n"))
		if _, err := p.rd.ReadString('\n'); err != nil {
			break
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("connection not closed after repeated auth failures")
	}
}
