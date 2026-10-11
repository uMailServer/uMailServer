package smtp

// Round 197 regression tests (F6740-F6743).

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type r197AV struct{ virus string }

func (a r197AV) IsEnabled() bool { return true }
func (a r197AV) Scan([]byte) (*AVScanResult, error) {
	return &AVScanResult{Infected: true, Virus: a.virus}, nil
}

// r197Deliver runs one DATA transaction through a server (optionally with a
// pipeline) and returns the delivered bytes.
func r197Deliver(t *testing.T, p *Pipeline, msg string) string {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	if p != nil {
		srv.SetPipeline(p)
	}
	var out string
	srv.SetDeliveryHandler(func(_ string, _ []string, d []byte) error { out = string(d); return nil })
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
	if code := cmd(msg + ".\r\n"); code != "250" {
		t.Fatalf("code=%s", code)
	}
	cmd("QUIT\r\n")
	return out
}

// F6614: the AV "tag" action must put X-Virus on the delivered message.
func TestR197_AVTagHeaderDelivered(t *testing.T) {
	p := NewPipeline(nil)
	p.AddStage(NewAVStage(r197AV{virus: "Eicar-Test\r\nBcc: x@evil"}, "tag"))
	out := r197Deliver(t, p, "Subject: x\r\n\r\nbody\r\n")
	if !strings.Contains(out, "X-Virus: Eicar-Test") {
		t.Fatalf("X-Virus missing from delivered message:\n%s", out)
	}
	if strings.Contains(out, "\r\nBcc:") || strings.Contains(out, "\nBcc:") {
		t.Fatalf("virus name injected a header:\n%s", out)
	}
}

// F6617: Received is added on accept even when no pipeline is configured.
func TestR197_ReceivedWithoutPipeline(t *testing.T) {
	out := r197Deliver(t, nil, "Subject: x\r\n\r\nbody\r\n")
	if !strings.HasPrefix(out, "Received: from c.example") {
		t.Fatalf("no Received header first:\n%s", out)
	}
}

// F6615: per-IP concurrent connection cap.
func TestR197_PerIPConnectionLimit(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxConnectionsPerIP: 2,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	var clients []net.Conn
	var dones []chan struct{}
	open := func() string {
		s, c := net.Pipe()
		clients = append(clients, c)
		d := make(chan struct{})
		dones = append(dones, d)
		go func() { srv.handleConnection(s); close(d) }()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		l, _ := bufio.NewReader(c).ReadString('\n')
		return l[:3]
	}
	if a, b := open(), open(); a != "220" || b != "220" {
		t.Fatalf("control: first two = %s %s", a, b)
	}
	if c := open(); c != "421" {
		t.Fatalf("third connection from same IP = %s, want 421", c)
	}
	// Closing one frees a slot.
	_ = clients[0].Close()
	<-dones[0]
	if d := open(); d != "220" {
		t.Fatalf("after release = %s, want 220", d)
	}
	for i, c := range clients {
		_ = c.Close()
		<-dones[i]
	}
}

type r197RBLResolver struct {
	mu       sync.Mutex
	answers  map[string]net.IP
	inflight int32
	maxSeen  int32
	calls    int32
	gate     int32 // when >0, each lookup waits (bounded) for this many in flight
}

func (r *r197RBLResolver) LookupHost(ctx context.Context, host string) (net.IP, error) {
	atomic.AddInt32(&r.calls, 1)
	n := atomic.AddInt32(&r.inflight, 1)
	for {
		m := atomic.LoadInt32(&r.maxSeen)
		if n <= m || atomic.CompareAndSwapInt32(&r.maxSeen, m, n) {
			break
		}
	}
	defer atomic.AddInt32(&r.inflight, -1)
	if g := atomic.LoadInt32(&r.gate); g > 0 {
		deadline := time.After(300 * time.Millisecond)
		for atomic.LoadInt32(&r.inflight) < g {
			select {
			case <-deadline:
				goto done
			case <-time.After(time.Millisecond):
			}
		}
	}
done:
	r.mu.Lock()
	defer r.mu.Unlock()
	if ip, ok := r.answers[host]; ok {
		return ip, nil
	}
	return nil, context.DeadlineExceeded
}

// F6616: lookups run in parallel and are cached.
func TestR197_RBLParallelAndCached(t *testing.T) {
	res := &r197RBLResolver{gate: 3, answers: map[string]net.IP{
		"4.3.2.1.a.test": net.ParseIP("127.0.0.2"),
		"4.3.2.1.b.test": net.ParseIP("127.0.0.2"),
		"4.3.2.1.c.test": net.ParseIP("127.0.0.2"),
	}}
	st := NewRBLStage([]string{"a.test", "b.test", "c.test"}, res)
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "a@b", nil, nil)
	st.Process(ctx)
	if res.maxSeen < 3 {
		t.Fatalf("max concurrent lookups = %d, want 3 (sequential)", res.maxSeen)
	}
	if ctx.SpamScore != 9.0 {
		t.Fatalf("score = %v, want 9", ctx.SpamScore)
	}
	ctx2 := NewMessageContext(net.ParseIP("1.2.3.4"), "a@b", nil, nil)
	st.Process(ctx2)
	if res.calls != 3 {
		t.Fatalf("lookups after repeat = %d, want 3 (uncached)", res.calls)
	}
	if ctx2.SpamScore != 9.0 {
		t.Fatalf("cached score = %v", ctx2.SpamScore)
	}
}

// F6616: only documented 127.0.0.x codes count as listings.
func TestR197_RBLOnlyDocumentedCodes(t *testing.T) {
	for _, tc := range []struct {
		ans  string
		want float64
	}{
		{"127.0.0.2", 3.0}, // control
		{"127.1.2.3", 0},
		{"127.0.0.99", 0},
		{"127.0.0.0", 0},
	} {
		res := &r197RBLResolver{answers: map[string]net.IP{"4.3.2.1.a.test": net.ParseIP(tc.ans)}}
		st := NewRBLStage([]string{"a.test"}, res)
		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "a@b", nil, nil)
		st.Process(ctx)
		if ctx.SpamScore != tc.want {
			t.Errorf("answer %s: score %v, want %v", tc.ans, ctx.SpamScore, tc.want)
		}
	}
}

// r197Session opens a pipe-backed session with a custom config and returns a
// command helper (reads one full reply) and the raw client conn.
func r197Session(t *testing.T, cfg *Config) (func(string) string, net.Conn, <-chan struct{}) {
	t.Helper()
	sc, cc := net.Pipe()
	srv := NewServer(cfg, nil)
	done := make(chan struct{})
	go func() { srv.handleConnection(sc); close(done) }()
	_ = cc.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(cc)
	rd := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return "EOF"
			}
			if len(l) >= 4 && l[3] == ' ' {
				return l[:3]
			}
		}
	}
	rd()
	t.Cleanup(func() { _ = cc.Close(); <-done })
	return func(c string) string { _, _ = cc.Write([]byte(c)); return rd() }, cc, done
}

// F6744: a refused BDAT chunk larger than the session octet budget ends the
// session instead of being read.
func TestR197_BDATDiscardRespectsSessionBudget(t *testing.T) {
	cmd, cc, done := r197Session(t, &Config{Hostname: "mx.test", MaxMessageSize: 1 << 20,
		MaxSessionBytes: 4096, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	cmd("EHLO c.test\r\n")
	// Wrong state (no MAIL/RCPT): control for a small chunk first.
	if got := cmd("BDAT 3 LAST\r\nabc"); got != "503" {
		t.Fatalf("control: small refused chunk reply %s", got)
	}
	// Drain replies: net.Pipe writes block until read.
	go func() { _, _ = io.Copy(io.Discard, cc) }()
	_, _ = cc.Write([]byte("BDAT 900000000 LAST\r\n"))
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("session still waiting for a chunk larger than the session budget")
	}
}

// F6745: BDAT with an unknown keyword is a syntax error and its chunk is not
// taken as a non-final chunk.
func TestR197_BDATUnknownKeyword(t *testing.T) {
	cmd, _, _ := r197Session(t, &Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	cmd("EHLO c.test\r\n")
	cmd("MAIL FROM:<a@b.test>\r\n")
	cmd("RCPT TO:<c@mx.test>\r\n")
	if got := cmd("BDAT 3 FOO\r\nabc"); got != "501" {
		t.Fatalf("BDAT 3 FOO = %s, want 501", got)
	}
	if got := cmd("BDAT 3 EXTRA LAST\r\nabc"); got != "501" {
		t.Fatalf("BDAT with extra token = %s, want 501", got)
	}
	// Control: a well-formed non-final chunk is still accepted.
	if got := cmd("BDAT 3\r\nabc"); got != "250" {
		t.Fatalf("control BDAT 3 = %s", got)
	}
}

// F6746: STARTTLS takes no parameters.
func TestR197_STARTTLSParameter(t *testing.T) {
	cmd, _, _ := r197Session(t, &Config{Hostname: "mx.test", ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	cmd("EHLO c.test\r\n")
	if got := cmd("STARTTLS now\r\n"); got != "501" {
		t.Fatalf("STARTTLS now = %s, want 501", got)
	}
}
