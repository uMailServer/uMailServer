package smtp

// Round 145 regressions (F6270-F6279), driven through the real command loop
// over net.Pipe using the r133 client helpers.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func r145Env(t *testing.T, c *r133Client) {
	t.Helper()
	for _, l := range []string{"EHLO client.test", "MAIL FROM:<a@sender.test>", "RCPT TO:<b@mx.test>"} {
		if got := c.cmd(t, l); !strings.HasPrefix(got, "250") {
			t.Fatalf("%s -> %s", l, got)
		}
	}
}

// F6270: a quota failure is 452 4.2.2 (also when wrapped), any other local
// delivery error 451 4.3.0, for DATA and BDAT alike.
func TestR145DeliveryErrorReplies(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrMailboxFull, "452 4.2.2"},
		{fmt.Errorf("deliver b@mx.test: %w", ErrMailboxFull), "452 4.2.2"},
		{errors.New("disk on fire"), "451 4.3.0"},
	}
	for _, tc := range cases {
		for _, mode := range []string{"DATA", "BDAT"} {
			c := r133Start(t, &Config{ReadTimeout: 5 * time.Second}, func(s *Server) {
				s.SetDeliveryHandler(func(string, []string, []byte) error { return tc.err })
			})
			r145Env(t, c)
			var got string
			if mode == "DATA" {
				if r := c.cmd(t, "DATA"); !strings.HasPrefix(r, "354") {
					t.Fatal(r)
				}
				got = c.body(t, "Subject: x\r\n\r\nhi\r\n.\r\n")
			} else {
				go func() { _, _ = c.conn.Write([]byte("BDAT 14 LAST\r\nSubject: x\r\n\r\nhi")) }()
				got = c.reply(t)
			}
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("%s %v: got %q want prefix %q", mode, tc.err, got, tc.want)
			}
		}
	}
}

// F6271: addresses reaching handlers are lower-cased; Received keeps the
// local part as received with a lower-cased domain.
func TestR145AddressNormalisation(t *testing.T) {
	var gotFrom string
	var gotTo []string
	var gotData []byte
	var allowedFrom string
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second}, func(s *Server) {
		s.SetPipeline(NewPipeline(nil))
		s.SetDeliveryHandler(func(from string, to []string, data []byte) error {
			gotFrom, gotTo, gotData = from, to, data
			return nil
		})
		s.SetSenderAllowedHandler(func(u, from string) bool { allowedFrom = from; return true })
	})
	for _, l := range []string{"EHLO c.test", "MAIL FROM:<Alice@Sender.TEST>", "RCPT TO:<Bob.Smith@MX.Test>", "DATA"} {
		c.cmd(t, l)
	}
	if r := c.body(t, "Subject: x\r\n\r\nhi\r\n.\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatal(r)
	}
	if gotFrom != "alice@sender.test" || len(gotTo) != 1 || gotTo[0] != "bob.smith@mx.test" {
		t.Errorf("handler saw %q %v", gotFrom, gotTo)
	}
	if !bytes.Contains(gotData, []byte("for <Bob.Smith@mx.test>;")) {
		t.Errorf("Received for clause: %q", gotData)
	}
	_ = allowedFrom // only called for authenticated sessions
}

func TestR145SenderAllowedLowercased(t *testing.T) {
	var got string
	srv := NewServer(&Config{Hostname: "mx.test"}, nil)
	srv.SetSenderAllowedHandler(func(u, from string) bool { got = from; return true })
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s := NewSession(a, srv)
	s.state, s.isAuth, s.username = StateGreeted, true, "u"
	go func() { _, _ = io.Copy(io.Discard, b) }()
	_ = s.HandleCommand("MAIL FROM:<Alice@Sender.TEST>")
	if got != "alice@sender.test" {
		t.Errorf("SenderAllowed got %q", got)
	}
}

// F6272: a stuck pipeline stage is cut off with 451 and the session lives on.
type r145Stuck struct{ release chan struct{} }

func (r145Stuck) Name() string { return "Stuck" }
func (s r145Stuck) Process(ctx *MessageContext) PipelineResult {
	<-s.release
	ctx.RejectionMessage = "late write"
	return ResultAccept
}

func TestR145StageTimeout(t *testing.T) {
	stuck := r145Stuck{release: make(chan struct{})}
	defer close(stuck.release)
	delivered := false
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, StageTimeout: 100 * time.Millisecond}, func(s *Server) {
		p := NewPipeline(nil)
		p.AddStage(stuck)
		s.SetPipeline(p)
		s.SetDeliveryHandler(func(string, []string, []byte) error { delivered = true; return nil })
	})
	r145Env(t, c)
	c.cmd(t, "DATA")
	start := time.Now()
	r := c.body(t, "Subject: x\r\n\r\nhi\r\n.\r\n")
	if !strings.HasPrefix(r, "451 4.4.7") || delivered || time.Since(start) > 3*time.Second {
		t.Fatalf("reply %q delivered=%v after %v", r, delivered, time.Since(start))
	}
	if r := c.cmd(t, "NOOP"); !strings.HasPrefix(r, "250") {
		t.Fatalf("session dead after timeout: %q", r)
	}
}

// F6273: total octets per session are capped.
func TestR145SessionByteCap(t *testing.T) {
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxSessionBytes: 4096}, nil)
	line := "NOOP " + strings.Repeat("x", 200) + "\r\n"
	var last string
	for i := 0; i < 100; i++ {
		go func() { _, _ = c.conn.Write([]byte(line)) }()
		last = c.reply(t)
		if strings.HasPrefix(last, "421") {
			break
		}
	}
	if !strings.HasPrefix(last, "421 4.7.0") {
		t.Fatalf("no 421 after exceeding cap, last %q", last)
	}
	// message data counts too
	c = r133Start(t, &Config{ReadTimeout: 5 * time.Second, MaxSessionBytes: 4096, MaxMessageSize: 1 << 20}, nil)
	r145Env(t, c)
	c.cmd(t, "DATA")
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := c.conn.Write([]byte(strings.Repeat("y", 100) + "\r\n")); err != nil {
				return
			}
		}
	}()
	if r := c.reply(t); !strings.HasPrefix(r, "421") {
		t.Fatalf("DATA over cap: %q", r)
	}
}

// F6274: BODY / SMTPUTF8 / unadvertised parameters.
func TestR145MailParameters(t *testing.T) {
	var delivered []byte
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second}, func(s *Server) {
		s.SetDeliveryHandler(func(_ string, _ []string, d []byte) error { delivered = d; return nil })
	})
	// capability list
	go func() { _, _ = c.conn.Write([]byte("EHLO c.test\r\n")) }()
	var caps []string
	for {
		l, _ := c.r.ReadString('\n')
		caps = append(caps, l)
		if len(l) > 3 && l[3] == ' ' {
			break
		}
	}
	all := strings.Join(caps, "")
	for _, want := range []string{"8BITMIME", "BINARYMIME", "CHUNKING", "SMTPUTF8"} {
		if !strings.Contains(all, want) {
			t.Errorf("EHLO missing %s", want)
		}
	}
	for _, no := range []string{"REQUIRETLS", "MT-PRIORITY"} {
		if strings.Contains(all, no) {
			t.Errorf("EHLO advertises %s", no)
		}
	}
	for _, p := range []string{"REQUIRETLS", "MT-PRIORITY=3", "SMTPUTF8=1", "BODY=BOGUS"} {
		if r := c.cmd(t, "MAIL FROM:<a@s.test> "+p); strings.HasPrefix(r, "250") {
			t.Errorf("%s accepted: %s", p, r)
		}
	}
	// UTF-8 address needs SMTPUTF8
	if r := c.cmd(t, "MAIL FROM:<josé@s.test>"); !strings.HasPrefix(r, "553 5.6.7") {
		t.Errorf("utf8 sender without SMTPUTF8: %s", r)
	}
	c.cmd(t, "RSET")
	c.cmd(t, "MAIL FROM:<a@s.test>")
	if r := c.cmd(t, "RCPT TO:<josé@mx.test>"); !strings.HasPrefix(r, "553 5.6.7") {
		t.Errorf("utf8 rcpt without SMTPUTF8: %s", r)
	}
	c.cmd(t, "RSET")
	if r := c.cmd(t, "MAIL FROM:<josé@s.test> SMTPUTF8 BODY=8BITMIME"); !strings.HasPrefix(r, "250") {
		t.Errorf("utf8 sender with SMTPUTF8: %s", r)
	}
	if r := c.cmd(t, "RCPT TO:<josé@mx.test>"); !strings.HasPrefix(r, "250") {
		t.Errorf("utf8 rcpt with SMTPUTF8: %s", r)
	}
	c.cmd(t, "RSET")
	// BINARYMIME: DATA refused, BDAT carries raw octets
	if r := c.cmd(t, "MAIL FROM:<a@s.test> BODY=BINARYMIME"); !strings.HasPrefix(r, "250") {
		t.Fatalf("BINARYMIME: %s", r)
	}
	c.cmd(t, "RCPT TO:<b@mx.test>")
	if r := c.cmd(t, "DATA"); !strings.HasPrefix(r, "503") {
		t.Errorf("DATA after BINARYMIME: %s", r)
	}
	payload := "Subject: x\r\n\r\na\rb\x00c\nd"
	go func() { _, _ = c.conn.Write([]byte(fmt.Sprintf("BDAT %d LAST\r\n%s", len(payload), payload))) }()
	if r := c.reply(t); !strings.HasPrefix(r, "250") {
		t.Fatalf("BDAT binary: %s", r)
	}
	if !bytes.HasSuffix(delivered, []byte("a\rb\x00c\nd")) {
		t.Errorf("binary body altered: %q", delivered)
	}
}

// F6275: VRFY / EXPN do not depend on the argument.
func TestR145VrfyExpn(t *testing.T) {
	c := r133Start(t, &Config{ReadTimeout: 5 * time.Second}, func(s *Server) {
		s.SetLocalDomainHandler(func(d string) bool { return d == "mx.test" })
	})
	a := c.cmd(t, "VRFY exists@mx.test")
	b := c.cmd(t, "VRFY nobody@mx.test")
	if !strings.HasPrefix(a, "252") || a != b {
		t.Errorf("VRFY leaks or wrong code: %q vs %q", a, b)
	}
	if r := c.cmd(t, "VRFY"); !strings.HasPrefix(r, "501") {
		t.Errorf("VRFY empty: %q", r)
	}
	x, y := c.cmd(t, "EXPN staff"), c.cmd(t, "EXPN postmaster")
	if !strings.HasPrefix(x, "502") || x != y {
		t.Errorf("EXPN: %q %q", x, y)
	}
}

// F6276: fuzz the whole command stream with arbitrary chunking; no panic and
// no hang.
type r145LogBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *r145LogBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *r145LogBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func FuzzSessionStream(f *testing.F) {
	f.Add([]byte("EHLO a.test\r\nMAIL FROM:<a@b.test>\r\nRCPT TO:<c@mx.test>\r\nDATA\r\nSubject: x\r\n\r\nhi\r\n.\r\nQUIT\r\n"), uint8(3))
	f.Add([]byte("EHLO a\r\nMAIL FROM:<a@b.test> BODY=BINARYMIME\r\nRCPT TO:<c@mx.test>\r\nBDAT 5\r\nabcdeBDAT 3 LAST\r\nxyz\r\nVRFY x\r\nEXPN y\r\n"), uint8(1))
	f.Add([]byte("EHLO a\r\nMAIL FROM:<jé@b.test> SMTPUTF8\r\nRCPT TO:<c@mx.test>\r\nBDAT 99999999999 LAST\r\nAUTH PLAIN\r\n\r\nSTARTTLS\r\nRSET\r\n"), uint8(0))
	f.Fuzz(func(t *testing.T, in []byte, chunk uint8) {
		if len(in) > 1<<16 {
			t.Skip()
		}
		logs := &r145LogBuf{}
		cfg := &Config{Hostname: "mx.test", ReadTimeout: 300 * time.Millisecond, MaxMessageSize: 1 << 16, MaxSessionBytes: 1 << 20}
		srv := NewServer(cfg, slog.New(slog.NewTextHandler(logs, nil)))
		srv.SetPipeline(NewPipeline(nil))
		srv.SetDeliveryHandler(func(string, []string, []byte) error { return nil })
		sc, cc := net.Pipe()
		done := make(chan struct{})
		go func() { srv.handleConnection(sc); close(done) }()
		go func() { _, _ = io.Copy(io.Discard, cc) }()
		go func() {
			step := int(chunk%32) + 1
			for i := 0; i < len(in); i += step {
				end := i + step
				if end > len(in) {
					end = len(in)
				}
				_ = cc.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if _, err := cc.Write(in[i:end]); err != nil {
					break
				}
			}
			_ = cc.Close()
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("session hung")
		}
		if strings.Contains(logs.String(), "Panic in SMTP") {
			t.Fatalf("panic: %s", logs.String())
		}
	})
}
