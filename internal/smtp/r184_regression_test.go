package smtp

// Round 184 regression tests (F6610-F6613).

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type r184Capture struct {
	mu   sync.Mutex
	ctx  *MessageContext
	from string
}

func (c *r184Capture) Name() string { return "r184cap" }
func (c *r184Capture) Process(ctx *MessageContext) PipelineResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = ctx
	c.from = authorDomain(ctx, ctx.From)
	return ResultAccept
}

// r184Send delivers msg over a DATA transaction and returns the stored
// message and the capture stage.
func r184Send(t *testing.T, msg string) (string, *r184Capture) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	p := NewPipeline(nil)
	capt := &r184Capture{}
	p.AddStage(capt)
	srv.SetPipeline(p)
	var delivered []string
	srv.SetDeliveryHandler(func(_ string, _ []string, d []byte) error {
		delivered = append(delivered, string(d))
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	defer func() { _ = clientConn.Close(); <-done }()
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(clientConn)
	reply := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(l) >= 4 && l[3] == ' ' {
				return l[:3]
			}
		}
	}
	cmd := func(s string) string { _, _ = clientConn.Write([]byte(s)); return reply() }
	reply()
	cmd("EHLO c.example\r\n")
	cmd("MAIL FROM:<a@own.test>\r\n")
	cmd("RCPT TO:<b@mx.test>\r\n")
	cmd("DATA\r\n")
	code := cmd(msg + ".\r\n")
	if code != "250" || len(delivered) != 1 {
		t.Fatalf("code=%s delivered=%d", code, len(delivered))
	}
	cmd("QUIT\r\n")
	return delivered[0], capt
}

// F6610: a folded continuation line that looks like "From: ..." must not be
// parsed as a header, or DMARC aligns against an attacker-chosen domain.
func TestR184_FoldedLineNotHeader(t *testing.T) {
	_, c := r184Send(t, "X-Foo: a\r\n From: attacker@own.test\r\nFrom: victim@bank.test\r\nSubject: x\r\n\r\nbody\r\n")
	if c.from != "bank.test" {
		t.Fatalf("author domain = %q, want bank.test", c.from)
	}
	if got := c.ctx.Headers["X-Foo"]; len(got) != 1 || !strings.Contains(got[0], "a") {
		t.Fatalf("X-Foo = %v", got)
	}
}

// F6611: forged X-Spam-*/X-Virus* headers from the sender are removed.
func TestR184_ForgedSpamHeadersStripped(t *testing.T) {
	out, _ := r184Send(t, "X-Spam-Status: No, score=-99\r\nX-Spam-Score: -99.0\r\n continued\r\nX-Virus-Scan: clean\r\nX-Virus: none\r\nSubject: x\r\n\r\nX-Spam-Status: body\r\n")
	hdr := regF5059Header(out)
	l := strings.ToLower(hdr)
	for _, h := range []string{"x-spam-status", "x-virus-scan", "x-virus:", "continued"} {
		if strings.Contains(l, h) {
			t.Fatalf("forged %q survived:\n%s", h, hdr)
		}
	}
	if !strings.Contains(out, "X-Spam-Status: body") {
		t.Fatal("body altered")
	}
}

// F6612: multiple From fields (any case) must not let the attacker choose the
// DMARC domain by map order; the stage rejects.
func TestR184_MultipleFromRejected(t *testing.T) {
	ctx := NewMessageContext(net.ParseIP("192.0.2.1"), "a@own.test", []string{"b@mx.test"}, nil)
	ctx.Headers["From"] = []string{"a@own.test"}
	ctx.Headers["from"] = []string{"v@bank.test"}
	for i := 0; i < 20; i++ {
		if d := authorDomain(ctx, "a@own.test"); d != "" {
			t.Fatalf("ambiguous From gave %q", d)
		}
	}
}

// F6613: greylist triplets aggregate the client IP to /24 (IPv4) or /64 (IPv6).
func TestR184_GreylistAggregation(t *testing.T) {
	s := NewGreylistStage()
	mk := func(ip string) *MessageContext {
		return NewMessageContext(net.ParseIP(ip), "a@x.test", []string{"b@y.test"}, nil)
	}
	if s.Process(mk("192.0.2.10")) != ResultReject {
		t.Fatal("first must defer")
	}
	for _, e := range s.greylist {
		e.firstSeen = e.firstSeen.Add(-10 * time.Minute)
	}
	if s.Process(mk("192.0.2.77")) != ResultAccept {
		t.Fatal("retry from same /24 must pass")
	}
	if s.Process(mk("192.0.3.77")) != ResultReject {
		t.Fatal("different /24 must defer")
	}
	s6 := NewGreylistStage()
	if s6.Process(mk("2001:db8:1:2::1")) != ResultReject {
		t.Fatal("first must defer")
	}
	for _, e := range s6.greylist {
		e.firstSeen = e.firstSeen.Add(-10 * time.Minute)
	}
	if s6.Process(mk("2001:db8:1:2:ffff::9")) != ResultAccept {
		t.Fatal("retry from same /64 must pass")
	}
	if s6.Process(mk("2001:db8:1:3::1")) != ResultReject {
		t.Fatal("different /64 must defer")
	}
}
