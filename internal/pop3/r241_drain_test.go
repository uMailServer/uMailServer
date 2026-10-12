package pop3

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// gatedLogin returns an authFunc that signals entered and blocks until release
// is closed, holding one session inside a command.
func gatedLogin() (fn func(string, string) (bool, error), entered chan struct{}, release chan struct{}) {
	entered = make(chan struct{}, 4)
	release = make(chan struct{})
	fn = func(string, string) (bool, error) {
		entered <- struct{}{}
		<-release
		return true, nil
	}
	return
}

func eofWithin(t *testing.T, c net.Conn, r *bufio.Reader) bool {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := r.ReadString('\n')
	return err != nil
}

// F7181: Stop closes idle sessions at once, lets the busy session finish its
// command (reply delivered), and only then returns.
func TestR241_StopDrainsInFlightCommand(t *testing.T) {
	auth, entered, release := gatedLogin()
	srv, addr := startTestServer(t, &mockMailstore{}, auth)
	srv.after = func(time.Duration) <-chan time.Time { return nil } // never fires

	busy, br := dialAndRead(t, addr)
	defer busy.Close()
	idle, ir := dialAndRead(t, addr)
	defer idle.Close()

	sendCmd(t, busy, br, "USER alice")
	_ = busy.SetDeadline(time.Now().Add(5 * time.Second))
	busy.Write([]byte("PASS secret\r\n"))
	<-entered

	stopped := make(chan struct{})
	go func() { _ = srv.Stop(); close(stopped) }()

	if !eofWithin(t, idle, ir) {
		t.Fatal("idle session was not closed by Stop")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned while a command was in flight")
	default:
	}

	close(release)
	resp, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(resp, "+OK") {
		t.Fatalf("in-flight PASS reply = %q, %v; want +OK", resp, err)
	}
	if !eofWithin(t, busy, br) {
		t.Fatal("busy session not closed after its command finished")
	}
	<-stopped
}

// F7181: when the drain timer fires, the stuck session is force-closed and Stop
// waits for its goroutine so nothing touches the mailstore after Stop returns.
func TestR241_StopForceClosesAfterTimer(t *testing.T) {
	auth, entered, release := gatedLogin()
	srv, addr := startTestServer(t, &mockMailstore{}, auth)
	fire := make(chan time.Time, 1)
	var gotD time.Duration
	srv.SetShutdownDrainTimeout(7 * time.Second)
	srv.after = func(d time.Duration) <-chan time.Time { gotD = d; return fire }

	c, r := dialAndRead(t, addr)
	defer c.Close()
	sendCmd(t, c, r, "USER alice")
	c.Write([]byte("PASS secret\r\n"))
	<-entered

	stopped := make(chan struct{})
	go func() { _ = srv.Stop(); close(stopped) }()

	fire <- time.Time{}
	if !eofWithin(t, c, r) {
		t.Fatal("stuck session not force-closed when the timer fired")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned while the session goroutine was still running")
	default:
	}
	close(release)
	<-stopped
	if gotD != 7*time.Second {
		t.Fatalf("drain timer duration = %v, want 7s", gotD)
	}
}
