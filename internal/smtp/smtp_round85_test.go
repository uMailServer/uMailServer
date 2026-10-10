package smtp

// Round 85 (F5670-F5677): SMTP protocol conformance of the non-AUTH command
// paths. Each Defect test fails on the unfixed code with a "DEFECT F56xx"
// message; each Control test exercises a neighbouring behaviour that was
// already correct. Driven through the real command loop over net.Pipe.

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type r85Client struct {
	conn net.Conn
	r    *bufio.Reader
}

type r85Stage struct {
	mu      sync.Mutex
	from    []string
	hdrKeys int
}

func (*r85Stage) Name() string { return "R85" }
func (s *r85Stage) Process(ctx *MessageContext) PipelineResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.from = append([]string(nil), ctx.Headers["From"]...)
	s.hdrKeys = len(ctx.Headers)
	return ResultAccept
}

func r85Start(t *testing.T, maxSize int64, p *Pipeline, delivered *[]string) *r85Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{
		Hostname:       "mx.test",
		MaxMessageSize: maxSize,
		MaxRecipients:  10,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}, nil)
	if p != nil {
		srv.SetPipeline(p)
	}
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error {
		if delivered != nil {
			*delivered = append(*delivered, string(data))
		}
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(15 * time.Second))
	c := &r85Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	return c
}

func (c *r85Client) reply(t *testing.T) string {
	t.Helper()
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("INVALID: read reply: %v", err)
		}
		if len(line) >= 4 && line[3] == ' ' {
			return strings.TrimRight(line, "\r\n")
		}
	}
}

func (c *r85Client) cmd(t *testing.T, s string) string {
	t.Helper()
	// Write concurrently: net.Pipe is unbuffered, the server may answer
	// (and stop reading) before it has consumed everything we send.
	werr := make(chan error, 1)
	go func() { _, err := c.conn.Write([]byte(s)); werr <- err }()
	rep := c.reply(t)
	select {
	case <-werr:
	case <-time.After(5 * time.Second):
	}
	return rep
}

func (c *r85Client) hello(t *testing.T) {
	t.Helper()
	if r := c.cmd(t, "EHLO client.example.org\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID: EHLO %q", r)
	}
}

func (c *r85Client) txn(t *testing.T, rcpts ...string) {
	t.Helper()
	c.hello(t)
	if r := c.cmd(t, "MAIL FROM:<a@example.org>\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID: MAIL %q", r)
	}
	for _, rc := range rcpts {
		if r := c.cmd(t, "RCPT TO:<"+rc+">\r\n"); !strings.HasPrefix(r, "250") {
			t.Fatalf("INVALID: RCPT %q", r)
		}
	}
}

// ---- F5670: bare-LF messages are invisible to header-based pipeline stages

const r85MsgCRLF = "From: ceo@victim.example\r\nTo: b@mx.test\r\nSubject: hi\r\n\r\nbody\r\n"

func r85SendData(t *testing.T, msg string, bdat bool) (*r85Stage, string) {
	t.Helper()
	st := &r85Stage{}
	p := NewPipeline(nil)
	p.AddStage(st)
	var delivered []string
	c := r85Start(t, 1<<20, p, &delivered)
	c.txn(t, "b@mx.test")
	var final string
	if bdat {
		final = c.cmd(t, fmt.Sprintf("BDAT %d LAST\r\n%s", len(msg), msg))
	} else {
		if r := c.cmd(t, "DATA\r\n"); !strings.HasPrefix(r, "354") {
			t.Fatalf("INVALID: DATA %q", r)
		}
		// The end-of-data indicator is only recognised after <CRLF> (F4906),
		// so a bare-LF body is closed with an empty CRLF line first.
		end := ".\r\n"
		if !strings.HasSuffix(msg, "\r\n") {
			end = "\r\n.\r\n"
		}
		final = c.cmd(t, msg+end)
	}
	if !strings.HasPrefix(final, "250") || len(delivered) != 1 {
		t.Fatalf("INVALID: final=%q delivered=%d", final, len(delivered))
	}
	return st, delivered[0]
}

func r85BareLF(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			return true
		}
	}
	return false
}

func TestRound85F5670Control(t *testing.T) {
	st, data := r85SendData(t, r85MsgCRLF, false)
	if len(st.from) != 1 || r85BareLF(data) {
		t.Fatalf("INVALID control: from=%v bareLF=%v", st.from, r85BareLF(data))
	}
}

func TestRound85F5670BareLFHeadersVisibleToPipeline(t *testing.T) {
	lf := strings.ReplaceAll(r85MsgCRLF, "\r\n", "\n")
	for _, bdat := range []bool{false, true} {
		st, data := r85SendData(t, lf, bdat)
		if len(st.from) != 1 {
			t.Errorf("DEFECT F5670 (bdat=%v): pipeline saw From=%v, want 1 value (headers parsed: %d)", bdat, st.from, st.hdrKeys)
		}
		if r85BareLF(data) {
			t.Errorf("DEFECT F5670 (bdat=%v): delivered data still contains a bare LF", bdat)
		}
	}
}

// ---- F5671: unbounded buffering of one unterminated line

type r85Flood struct {
	left      int
	trailer   string
	peak      uint64
	base      uint64
	sampledAt int
}

func (f *r85Flood) Read(p []byte) (int, error) {
	if f.left > 0 {
		n := len(p)
		if n > f.left {
			n = f.left
		}
		for i := 0; i < n; i++ {
			p[i] = 'A'
		}
		f.left -= n
		if f.left < 16<<20 && f.sampledAt == 0 {
			f.sampledAt = 1
			var ms runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&ms)
			f.peak = ms.HeapAlloc
		}
		return n, nil
	}
	if f.trailer == "" {
		return 0, io.EOF
	}
	n := copy(p, f.trailer)
	f.trailer = f.trailer[n:]
	return n, nil
}

func TestRound85F5671DataLineNotBuffered(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10}, nil)
	s := NewSession(nil, srv)
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	fl := &r85Flood{left: 64 << 20, trailer: "\r\n.\r\n", base: ms.HeapAlloc}
	s.reader = bufio.NewReader(fl)
	_, err := s.readData()
	if err == nil {
		t.Fatalf("INVALID: overlong line accepted")
	}
	grown := int64(fl.peak) - int64(fl.base)
	if grown > 24<<20 {
		t.Errorf("DEFECT F5671: server buffered %d MiB of one unterminated line (64 MiB sent, 1 MiB message limit)", grown>>20)
	}
}

func TestRound85F5671CommandLineLimit(t *testing.T) {
	c := r85Start(t, 1<<20, nil, nil)
	if r := c.cmd(t, "NOOP "+strings.Repeat("B", 60)+"\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID control: %q", r)
	}
	r := c.cmd(t, "NOOP "+strings.Repeat("A", 4000)+"\r\n")
	if !strings.HasPrefix(r, "500") {
		t.Errorf("DEFECT F5671: 4000-byte command line answered %q, want 500 (RFC 5321 §4.5.3.1.4)", r)
	}
	if r := c.cmd(t, "NOOP\r\n"); !strings.HasPrefix(r, "250") {
		t.Errorf("session out of sync after overlong command line: %q", r)
	}
}

// ---- F5672: SIZE parameter

func TestRound85F5672MailSizeEnforced(t *testing.T) {
	c := r85Start(t, 1000, nil, nil)
	c.hello(t)
	if r := c.cmd(t, "MAIL FROM:<a@example.org> SIZE=1000\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID control (SIZE at limit): %q", r)
	}
	c.cmd(t, "RSET\r\n")
	if r := c.cmd(t, "MAIL FROM:<a@example.org> SIZE=1001\r\n"); !strings.HasPrefix(r, "552") {
		t.Errorf("DEFECT F5672: MAIL SIZE=1001 over limit 1000 answered %q, want 552", r)
	}
	if r := c.cmd(t, "MAIL FROM:<a@example.org> SIZE=abc\r\n"); !strings.HasPrefix(r, "501") {
		t.Errorf("DEFECT F5672: MAIL SIZE=abc answered %q, want 501", r)
	}
}

// ---- F5673: parameter values and unknown keywords

func TestRound85F5673Params(t *testing.T) {
	c := r85Start(t, 1<<20, nil, nil)
	c.hello(t)
	if r := c.cmd(t, "MAIL FROM:<a@example.org> BODY=8BITMIME SMTPUTF8 RET=HDRS ENVID=abc123 AUTH=<>\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID control (valid params): %q", r)
	}
	if r := c.cmd(t, "RCPT TO:<b@mx.test> NOTIFY=SUCCESS,FAILURE ORCPT=rfc822;b@mx.test\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID control (valid RCPT params): %q", r)
	}
	for _, tc := range []struct{ line, want string }{
		{"MAIL FROM:<a@example.org> BODY=GARBAGE\r\n", "501"},
		{"MAIL FROM:<a@example.org> RET=GARBAGE\r\n", "501"},
		{"MAIL FROM:<a@example.org> FOO=bar\r\n", "555"},
	} {
		if r := c.cmd(t, tc.line); !strings.HasPrefix(r, tc.want) {
			t.Errorf("DEFECT F5673: %q answered %q, want %s", strings.TrimSpace(tc.line), r, tc.want)
		}
		c.cmd(t, "RSET\r\n")
	}
	c.cmd(t, "MAIL FROM:<a@example.org>\r\n")
	for _, tc := range []struct{ line, want string }{
		{"RCPT TO:<b@mx.test> NOTIFY=GARBAGE\r\n", "501"},
		{"RCPT TO:<b@mx.test> NOTIFY=NEVER,SUCCESS\r\n", "501"},
		{"RCPT TO:<b@mx.test> FOO=bar\r\n", "555"},
	} {
		if r := c.cmd(t, tc.line); !strings.HasPrefix(r, tc.want) {
			t.Errorf("DEFECT F5673: %q answered %q, want %s", strings.TrimSpace(tc.line), r, tc.want)
		}
	}
}

// ---- F5674 / F5678: Received trace header

func r85Received(data string) string {
	for _, l := range strings.Split(data, "\r\n") {
		if strings.HasPrefix(l, "Received:") {
			return l
		}
	}
	return ""
}

func r85SendRcpts(t *testing.T, rcpts ...string) string {
	t.Helper()
	st := &r85Stage{}
	p := NewPipeline(nil)
	p.AddStage(st)
	var delivered []string
	c := r85Start(t, 1<<20, p, &delivered)
	c.txn(t, rcpts...)
	c.cmd(t, "DATA\r\n")
	if r := c.cmd(t, r85MsgCRLF+".\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID: final %q", r)
	}
	return r85Received(delivered[0])
}

func TestRound85F5674ReceivedForClause(t *testing.T) {
	if rc := r85SendRcpts(t, "alice@mx.test"); !strings.Contains(rc, "for <alice@mx.test>") {
		t.Fatalf("INVALID control: single-recipient Received %q", rc)
	}
	rc := r85SendRcpts(t, "alice@mx.test", "hidden-bcc@mx.test")
	if strings.Contains(rc, "alice@mx.test") {
		t.Errorf("DEFECT F5674: multi-recipient Received header discloses one recipient to all: %q", rc)
	}
}

type r85AddrConn struct {
	net.Conn
	addr net.Addr
}

func (c r85AddrConn) RemoteAddr() net.Addr { return c.addr }

func TestRound85F5678ReceivedIPLiteral(t *testing.T) {
	recv := func(ip string) string {
		srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10}, nil)
		s := NewSession(r85AddrConn{addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 2525}}, srv)
		s.helloDomain = "client.example"
		s.rcptTo = []string{"b@mx.test"}
		out := s.addTraceHeaders(&MessageContext{}, []byte(r85MsgCRLF))
		return r85Received(string(out))
	}
	if rc := recv("192.0.2.1"); !strings.Contains(rc, "([192.0.2.1])") {
		t.Fatalf("INVALID control: %q", rc)
	}
	if rc := recv("2001:db8::1"); !strings.Contains(rc, "[IPv6:2001:db8::1]") {
		t.Errorf("DEFECT F5678: IPv6 peer not rendered as an RFC 5321 address literal: %q", rc)
	}
}

// ---- F5675: BDAT chunk buffer allocated from the declared size

type r85BlockReader struct {
	heap uint64
	done bool
}

func (b *r85BlockReader) Read(p []byte) (int, error) {
	if !b.done {
		b.done = true
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		b.heap = ms.HeapAlloc
	}
	return 0, io.ErrUnexpectedEOF
}

func TestRound85F5675BDATDoesNotPreallocate(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 64 << 20, MaxRecipients: 10}, nil)
	s := NewSession(nil, srv)
	s.state = StateRcptTo
	br := &r85BlockReader{}
	s.reader = bufio.NewReader(br)
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	base := ms.HeapAlloc
	_ = s.handleBDAT("50000000 LAST")
	if !br.done {
		t.Fatalf("INVALID: chunk never read")
	}
	if grown := int64(br.heap) - int64(base); grown > 16<<20 {
		t.Errorf("DEFECT F5675: %d MiB allocated before any chunk octet arrived (declared 50000000)", grown>>20)
	}
}

// ---- F5676: DATA after BDAT

func TestRound85F5676DataAfterBDAT(t *testing.T) {
	c := r85Start(t, 1<<20, nil, nil)
	c.txn(t, "b@mx.test")
	if r := c.cmd(t, "BDAT 5\r\nhello"); !strings.HasPrefix(r, "250") {
		t.Fatalf("INVALID: BDAT %q", r)
	}
	if r := c.cmd(t, "DATA\r\n"); !strings.HasPrefix(r, "503") {
		t.Errorf("DEFECT F5676: DATA in the middle of a BDAT transaction answered %q, want 503 (RFC 3030 §3)", r)
	}
	c2 := r85Start(t, 1<<20, nil, nil)
	c2.txn(t, "b@mx.test")
	if r := c2.cmd(t, "DATA\r\n"); !strings.HasPrefix(r, "354") {
		t.Fatalf("INVALID control: DATA %q", r)
	}
}

// ---- F5677: EHLO/HELO argument syntax (value is echoed into Received)

func TestRound85F5677HelloSyntax(t *testing.T) {
	for _, ok := range []string{"client.example.org", "[192.0.2.1]", "[IPv6:2001:db8::1]", "WIN-ABC_1"} {
		c := r85Start(t, 1<<20, nil, nil)
		if r := c.cmd(t, "EHLO "+ok+"\r\n"); !strings.HasPrefix(r, "250") {
			t.Fatalf("INVALID control: EHLO %s -> %q", ok, r)
		}
	}
	for _, verb := range []string{"EHLO", "HELO"} {
		for _, bad := range []string{"x by evil.example with ESMTP", "a;b", "a(b)", "a<b>"} {
			c := r85Start(t, 1<<20, nil, nil)
			if r := c.cmd(t, verb+" "+bad+"\r\n"); !strings.HasPrefix(r, "501") {
				t.Errorf("DEFECT F5677: %s %q answered %q, want 501", verb, bad, r)
			}
		}
	}
}
