package smtp

import (
	"bufio"
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"
)

func startDrainServer(t *testing.T, cfg *Config, hook func(*Server)) (*Server, string, chan struct{}) {
	t.Helper()
	srv := NewServer(cfg, nil)
	if hook != nil {
		hook(srv)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }()
	return srv, ln.Addr().String(), done
}

func dialSMTP(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { _ = c.Close() })
	return c, bufio.NewReader(c)
}

func expect(t *testing.T, r *bufio.Reader, prefix string) {
	t.Helper()
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read (want %s): %v", prefix, err)
		}
		if strings.HasPrefix(l, prefix) {
			return
		}
		if len(l) > 3 && l[3] == '-' {
			continue
		}
		t.Fatalf("got %q want %s", l, prefix)
	}
}

// R201 F6780: Stop must let a session inside DATA finish and reply 250, while
// closing an idle session promptly.
func TestR201_StopDrainsDataClosesIdle(t *testing.T) {
	delivering := make(chan struct{})
	release := make(chan struct{})
	srv, addr, served := startDrainServer(t, &Config{Hostname: "t", ReadTimeout: time.Minute, WriteTimeout: time.Minute}, func(s *Server) {
		s.SetDeliveryHandler(func(from string, to []string, data []byte) error {
			close(delivering)
			<-release
			return nil
		})
	})
	idleC, idleR := dialSMTP(t, addr)
	expect(t, idleR, "220")
	_ = idleC

	c, r := dialSMTP(t, addr)
	expect(t, r, "220")
	send := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	send("EHLO x")
	expect(t, r, "250 ")
	send("MAIL FROM:<a@b.example>")
	expect(t, r, "250")
	send("RCPT TO:<c@d.example>")
	expect(t, r, "250")
	send("DATA")
	expect(t, r, "354")
	send("Subject: x\r\n\r\nbody\r\n.")
	<-delivering // session is provably inside DATA processing

	stopped := make(chan struct{})
	go func() { _ = srv.Stop(); close(stopped) }()

	// Idle connection is closed promptly (EOF) while Stop is still draining.
	if _, err := idleR.ReadString('\n'); err == nil {
		t.Fatal("idle connection still open")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned before in-flight DATA finished")
	default:
	}
	close(release)
	expect(t, r, "250")
	<-stopped
	<-served
	if _, err := r.ReadString('\n'); err == nil {
		t.Fatal("session should close after finishing during shutdown")
	}
}

// R201 F6780: the drain is bounded; a stuck session is force-closed.
func TestR201_StopDrainTimeoutForceCloses(t *testing.T) {
	srv, addr, _ := startDrainServer(t, &Config{Hostname: "t", ReadTimeout: time.Minute}, func(s *Server) {
		s.SetShutdownDrainTimeout(50 * time.Millisecond)
	})
	c, r := dialSMTP(t, addr)
	expect(t, r, "220")
	for _, l := range []string{"EHLO x", "MAIL FROM:<a@b.example>", "RCPT TO:<c@d.example>", "DATA"} {
		_, _ = c.Write([]byte(l + "\r\n"))
		if l == "EHLO x" {
			expect(t, r, "250 ")
		} else if l == "DATA" {
			expect(t, r, "354")
		} else {
			expect(t, r, "250")
		}
	}
	_, _ = c.Write([]byte("partial")) // mid-DATA, never finishes
	// wait until the session is non-idle (it is: it is inside DATA)
	if err := srv.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadString('\n'); err == nil {
		t.Fatal("stuck DATA session not force-closed")
	}
}

// R201 F6781: global connection cap must hold under concurrent accepts.
type gateLimiter struct {
	arrived chan struct{}
	release chan struct{}
}

func (g *gateLimiter) Allow(string, string) bool {
	g.arrived <- struct{}{}
	<-g.release
	return true
}

func TestR201_GlobalCapAtomicWithRegistration(t *testing.T) {
	g := &gateLimiter{arrived: make(chan struct{}, 2), release: make(chan struct{})}
	_, addr, _ := startDrainServer(t, &Config{Hostname: "t", MaxConnections: 1, MaxConnectionsPerIP: -1}, func(s *Server) { s.SetRateLimiter(g) })
	c1, r1 := dialSMTP(t, addr)
	c2, r2 := dialSMTP(t, addr)
	_, _ = c1, c2
	<-g.arrived
	<-g.arrived // both passed the early cap check and are parked pre-registration
	close(g.release)
	got := []string{}
	for _, r := range []*bufio.Reader{r1, r2} {
		l, _ := r.ReadString('\n')
		got = append(got, l[:3])
	}
	n220, n421 := 0, 0
	for _, g := range got {
		switch g {
		case "220":
			n220++
		case "421":
			n421++
		}
	}
	if n220 != 1 || n421 != 1 {
		t.Fatalf("cap=1 but replies %v", got)
	}
}

// R201 F6782: a silent implicit-TLS peer must not hold a connection slot.
func TestR201_TLSHandshakeTimeoutReleasesSlot(t *testing.T) {
	old := tlsHandshakeTimeout
	tlsHandshakeTimeout = 100 * time.Millisecond
	defer func() { tlsHandshakeTimeout = old }()
	srv := NewServer(&Config{Hostname: "t"}, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// tls.NewListener with no usable cert; handshake never starts anyway.
	tln := tls.NewListener(ln, &tls.Config{})
	go func() { _ = srv.Serve(tln) }()
	defer srv.Stop()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("unexpected data")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("server never closed a silent TLS connection")
	}
}
