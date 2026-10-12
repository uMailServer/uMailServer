package imap

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func r241Start(t *testing.T, auth func(string, string) (bool, error)) (*Server, string) {
	t.Helper()
	srv := NewServer(&Config{Addr: "127.0.0.1:0"}, &mockMailstore{})
	srv.allowPlainAuth = true
	srv.SetAuthFunc(auth)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	return srv, srv.listeners[0].Addr().String()
}

func r241Dial(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	return c, r
}

func r241EOF(c net.Conn, r *bufio.Reader) bool {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, err := r.ReadString('\n'); err != nil {
			ne, ok := err.(net.Error)
			return !(ok && ne.Timeout())
		}
	}
}

func r241Gate() (func(string, string) (bool, error), chan struct{}, chan struct{}) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	return func(string, string) (bool, error) {
		entered <- struct{}{}
		<-release
		return true, nil
	}, entered, release
}

// F7180: idle sessions close at once, a session mid-command finishes it (tagged
// reply delivered), and Stop returns only after that.
func TestR241_StopDrainsInFlightCommand(t *testing.T) {
	auth, entered, release := r241Gate()
	srv, addr := r241Start(t, auth)
	srv.after = func(time.Duration) <-chan time.Time { return nil }

	busy, br := r241Dial(t, addr)
	defer busy.Close()
	idle, ir := r241Dial(t, addr)
	defer idle.Close()

	busy.Write([]byte("a1 LOGIN alice secret\r\n"))
	<-entered

	stopped := make(chan struct{})
	go func() { _ = srv.Stop(); close(stopped) }()

	if !r241EOF(idle, ir) {
		t.Fatal("idle session not closed by Stop")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned while a command was in flight")
	default:
	}
	close(release)

	var sawTagged bool
	for {
		_ = busy.SetReadDeadline(time.Now().Add(5 * time.Second))
		l, err := br.ReadString('\n')
		if strings.HasPrefix(l, "a1 ") {
			sawTagged = true
		}
		if err != nil {
			break
		}
	}
	if !sawTagged {
		t.Fatal("in-flight LOGIN reply was not delivered before close")
	}
	<-stopped
}

// F7180: the drain timer force-closes stuck sessions; Stop still waits for the
// session goroutine to exit.
func TestR241_StopForceClosesAfterTimer(t *testing.T) {
	auth, entered, release := r241Gate()
	srv, addr := r241Start(t, auth)
	fire := make(chan time.Time, 1)
	var gotD time.Duration
	srv.SetShutdownDrainTimeout(9 * time.Second)
	srv.after = func(d time.Duration) <-chan time.Time { gotD = d; return fire }

	c, r := r241Dial(t, addr)
	defer c.Close()
	c.Write([]byte("a1 LOGIN alice secret\r\n"))
	<-entered

	stopped := make(chan struct{})
	go func() { _ = srv.Stop(); close(stopped) }()
	fire <- time.Time{}
	if !r241EOF(c, r) {
		t.Fatal("stuck session not force-closed")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned before the session goroutine exited")
	default:
	}
	close(release)
	<-stopped
	if gotD != 9*time.Second {
		t.Fatalf("timer duration = %v, want 9s", gotD)
	}
}

// A session that finished its command is idle and must not hold Stop back.
func TestR241_StopDoesNotWaitForIdleSession(t *testing.T) {
	srv, addr := r241Start(t, func(string, string) (bool, error) { return true, nil })
	srv.after = func(time.Duration) <-chan time.Time { return nil }
	c, r := r241Dial(t, addr)
	defer c.Close()
	c.Write([]byte("a1 LOGIN alice secret\r\n"))
	for {
		l, _ := r.ReadString('\n')
		if strings.HasPrefix(l, "a1 ") {
			break
		}
	}
	done := make(chan struct{})
	go func() { _ = srv.Stop(); close(done) }()
	<-done // would hang (timer never fires) if idle sessions were treated as busy
}
