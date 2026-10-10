package smtp

// Round 133 regressions (F6150-F6159), driven through the real command loop
// over net.Pipe.

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type r133Client struct {
	conn net.Conn
	r    *bufio.Reader
	done chan struct{}
}

func r133Start(t *testing.T, cfg *Config, setup func(*Server)) *r133Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	if cfg.Hostname == "" {
		cfg.Hostname = "mx.test"
	}
	srv := NewServer(cfg, nil)
	if setup != nil {
		setup(srv)
	}
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(15 * time.Second))
	c := &r133Client{conn: clientConn, r: bufio.NewReader(clientConn), done: done}
	c.reply(t)
	return c
}

func (c *r133Client) reply(t *testing.T) string {
	t.Helper()
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if len(line) >= 4 && line[3] == ' ' {
			return strings.TrimRight(line, "\r\n")
		}
	}
}

func (c *r133Client) cmd(t *testing.T, s string) string {
	t.Helper()
	go func() { _, _ = c.conn.Write([]byte(s + "\r\n")) }()
	return c.reply(t)
}

func (c *r133Client) envelope(t *testing.T) {
	t.Helper()
	for _, l := range []string{"EHLO client.test", "MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>", "DATA"} {
		want := "250"
		if l == "DATA" {
			want = "354"
		}
		if got := c.cmd(t, l); !strings.HasPrefix(got, want) {
			t.Fatalf("%s -> %s", l, got)
		}
	}
}

func (c *r133Client) body(t *testing.T, body string) string {
	t.Helper()
	go func() { _, _ = c.conn.Write([]byte(body)) }()
	return c.reply(t)
}

// F6152: zero MaxMessageSize / MaxRecipients must mean a documented default,
// not "refuse everything".
func TestR133ZeroLimitsUseDefaults(t *testing.T) {
	var got [][]byte
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second}, func(s *Server) {
		s.SetDeliveryHandler(func(from string, to []string, data []byte) error {
			got = append(got, data)
			return nil
		})
	})
	c.envelope(t)
	if code := c.body(t, "Subject: x\r\n\r\nhi\r\n.\r\n"); !strings.HasPrefix(code, "250") || len(got) != 1 {
		t.Fatalf("DEFECT F6152: reply %q deliveries %d", code, len(got))
	}
}

// F6151: a bare CR is a line break to some parsers and not to others.
func TestR133BareCRRejected(t *testing.T) {
	var got [][]byte
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxMessageSize: 1 << 20, MaxRecipients: 5}, func(s *Server) {
		s.SetDeliveryHandler(func(from string, to []string, data []byte) error {
			got = append(got, data)
			return nil
		})
	})
	c.envelope(t)
	code := c.body(t, "Subject: x\r\n\r\nline\r.\r\nMAIL FROM:<evil@x.test>\r\n.\r\n")
	if !strings.HasPrefix(code, "554") || len(got) != 0 {
		t.Fatalf("DEFECT F6151: reply %q deliveries %d", code, len(got))
	}
	if r := c.cmd(t, "NOOP"); !strings.HasPrefix(r, "250") {
		t.Fatalf("session desynchronised: %q", r)
	}
}

// F6150: a slow-drip client must hit an absolute DATA deadline.
func TestR133DataAbsoluteDeadline(t *testing.T) {
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, DataTimeout: 400 * time.Millisecond,
		MaxMessageSize: 1 << 20, MaxRecipients: 5}, nil)
	c.envelope(t)
	go func() { _, _ = io.Copy(io.Discard, c.r) }()
	start := time.Now()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				if _, err := c.conn.Write([]byte("X: y\r\n")); err != nil {
					return
				}
			}
		}
	}()
	defer close(stop)
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("DEFECT F6150: slow-drip DATA still open after %v", time.Since(start))
	}
}

// F6154: error limit drops a client that only sends garbage.
func TestR133ErrorLimit(t *testing.T) {
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxErrors: 5, MaxMessageSize: 1 << 20, MaxRecipients: 5}, nil)
	var last string
	for i := 0; i < 8; i++ {
		go func() { _, _ = c.conn.Write([]byte("BOGUS\r\n")) }()
		line, err := c.r.ReadString('\n')
		if err != nil {
			break
		}
		last = line
		if strings.HasPrefix(line, "421") {
			select {
			case <-c.done:
				return
			case <-time.After(3 * time.Second):
				t.Fatal("421 sent but connection kept open")
			}
		}
	}
	t.Fatalf("DEFECT F6154: no 421 after repeated errors, last %q", last)
}

func TestR133CommandLimit(t *testing.T) {
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxCommands: 3, MaxMessageSize: 1 << 20, MaxRecipients: 5}, nil)
	for i := 0; i < 3; i++ {
		if r := c.cmd(t, "NOOP"); !strings.HasPrefix(r, "250") {
			t.Fatalf("noop %d: %s", i, r)
		}
	}
	if r := c.cmd(t, "NOOP"); !strings.HasPrefix(r, "421") {
		t.Fatalf("DEFECT F6154: command over limit answered %q", r)
	}
}

type r133PanicStage struct{}

func (r133PanicStage) Name() string                           { return "panicky" }
func (r133PanicStage) Process(*MessageContext) PipelineResult { panic("boom") }

// F6156: a panicking stage rejects (temp-fails) the message, not the process.
func TestR133PipelineStagePanic(t *testing.T) {
	p := NewPipeline(nil)
	p.AddStage(r133PanicStage{})
	res, err := p.Process(NewMessageContext(net.IPv4(1, 2, 3, 4), "a@b.test", []string{"c@d.test"}, []byte("x")))
	if res != ResultReject || err == nil {
		t.Fatalf("DEFECT F6156: result %v err %v", res, err)
	}
}

type r133AcceptStage struct{}

func (r133AcceptStage) Name() string                           { return "ok" }
func (r133AcceptStage) Process(*MessageContext) PipelineResult { return ResultAccept }

// F6153/F6155: submission strips client Received headers and uses ESMTPSA/ESMTPA.
func TestR133SubmissionReceived(t *testing.T) {
	var got []string
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxMessageSize: 1 << 20, MaxRecipients: 5,
		IsSubmission: true, AllowInsecure: true}, func(s *Server) {
		p := NewPipeline(nil)
		p.AddStage(r133AcceptStage{})
		s.SetPipeline(p)
		s.SetAuthHandler(func(u, p string) (bool, error) { return u == "u@mx.test" && p == "pw", nil })
		s.SetDeliveryHandler(func(from string, to []string, data []byte) error {
			got = append(got, string(data))
			return nil
		})
	})
	c.cmd(t, "EHLO client.test")
	// PLAIN with a foreign authzid must be refused (F6158).
	if r := c.cmd(t, "AUTH PLAIN "+b64("other@mx.test\x00u@mx.test\x00pw")); !strings.HasPrefix(r, "535") {
		t.Fatalf("DEFECT F6158: foreign authzid -> %q", r)
	}
	if r := c.cmd(t, "AUTH PLAIN "+b64("\x00u@mx.test\x00pw")); !strings.HasPrefix(r, "235") {
		t.Fatalf("auth: %q", r)
	}
	for _, l := range []string{"MAIL FROM:<u@mx.test>", "RCPT TO:<b@mx.test>", "DATA"} {
		c.cmd(t, l)
	}
	c.body(t, "Received: from fake.example ([6.6.6.6]) by victim.test\r\n\twith ESMTP; Mon, 1 Jan 2001 00:00:00 +0000\r\nSubject: s\r\n\r\nbody\r\n.\r\n")
	if len(got) != 1 {
		t.Fatalf("deliveries %d", len(got))
	}
	if strings.Contains(got[0], "fake.example") || strings.Contains(got[0], "6.6.6.6") {
		t.Errorf("DEFECT F6153: client Received header kept:\n%s", got[0])
	}
	if !strings.Contains(got[0], "with ESMTPA") {
		t.Errorf("DEFECT F6155: want ESMTPA token:\n%s", got[0])
	}
	if !strings.HasPrefix(got[0], "Received:") {
		t.Errorf("Received must be topmost:\n%s", got[0])
	}
}

// F6157: an AUTH continuation line is bounded and "*" cancels.
func TestR133AuthLineBoundedAndCancel(t *testing.T) {
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxMessageSize: 1 << 20, MaxRecipients: 5,
		IsSubmission: true, AllowInsecure: true}, nil)
	c.cmd(t, "EHLO client.test")
	if r := c.cmd(t, "AUTH PLAIN"); !strings.HasPrefix(r, "334") {
		t.Fatalf("want 334: %q", r)
	}
	if r := c.cmd(t, "*"); !strings.HasPrefix(r, "501") {
		t.Fatalf("DEFECT F6158: cancel -> %q", r)
	}
	if r := c.cmd(t, "AUTH PLAIN"); !strings.HasPrefix(r, "334") {
		t.Fatalf("want 334: %q", r)
	}
	if r := c.cmd(t, strings.Repeat("A", 1<<20)); !strings.HasPrefix(r, "501") {
		t.Fatalf("DEFECT F6157: overlong auth line -> %q", r)
	}
	if r := c.cmd(t, "NOOP"); !strings.HasPrefix(r, "250") {
		t.Fatalf("desync: %q", r)
	}
}

func b64(s string) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []byte
	b := []byte(s)
	for i := 0; i < len(b); i += 3 {
		var v uint32
		n := 0
		for j := 0; j < 3; j++ {
			v <<= 8
			if i+j < len(b) {
				v |= uint32(b[i+j])
				n++
			}
		}
		for j := 0; j < 4; j++ {
			if j <= n {
				out = append(out, enc[(v>>(18-6*uint(j)))&63])
			} else {
				out = append(out, '=')
			}
		}
	}
	return string(out)
}

// chunkConn feeds data in fixed-size chunks.
type chunkConn struct {
	net.Conn
	r io.Reader
}

func (c *chunkConn) Read(p []byte) (int, error)      { return c.r.Read(p) }
func (c *chunkConn) SetReadDeadline(time.Time) error { return nil }

type chunkReader struct {
	data  []byte
	sizes []int
	i     int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := 1
	if len(c.sizes) > 0 {
		n = c.sizes[c.i%len(c.sizes)]
		c.i++
	}
	if n < 1 {
		n = 1
	}
	if n > len(p) {
		n = len(p)
	}
	if n > len(c.data) {
		n = len(c.data)
	}
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

func r133ReadData(in []byte, sizes []int, bufSize int) ([]byte, error) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 5}, nil)
	conn := &chunkConn{r: &chunkReader{data: append([]byte(nil), in...), sizes: sizes}}
	s := &Session{conn: conn, server: srv, reader: bufio.NewReaderSize(conn, bufSize)}
	return s.readData()
}

// F6159: the DATA reader result must not depend on TCP chunk boundaries.
func FuzzReadDataChunks(f *testing.F) {
	f.Add([]byte("Subject: x\r\n\r\n.hi\r\n..dot\r\n.\r\n"), uint8(1), uint8(3))
	f.Add([]byte("a\nb\r\nc\r.\r\n.\r\n"), uint8(2), uint8(7))
	f.Add([]byte("x\r\n.\n.\r\n.\r\n"), uint8(5), uint8(1))
	f.Fuzz(func(t *testing.T, in []byte, s1, s2 uint8) {
		whole, werr := r133ReadData(in, []int{1 << 16}, 4096)
		chunked, cerr := r133ReadData(in, []int{int(s1), int(s2)}, 16)
		if (werr == nil) != (cerr == nil) {
			t.Fatalf("error mismatch whole=%v chunked=%v input=%q", werr, cerr, in)
		}
		if werr == nil && !bytes.Equal(whole, chunked) {
			t.Fatalf("data mismatch input=%q\nwhole=%q\nchunk=%q", in, whole, chunked)
		}
	})
}
