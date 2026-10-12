package server

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// F7090: server.force_close_after drives the shutdown force-close deadline.
func TestR232ForceCloseAfterDuration(t *testing.T) {
	s := &Server{config: &config.Config{}}
	if got := s.forceCloseAfter(); got != 30*time.Second {
		t.Fatalf("default = %v", got)
	}
	s.config.Server.ForceCloseAfter = 7
	if got := s.forceCloseAfter(); got != 7*time.Second {
		t.Fatalf("configured = %v", got)
	}
}

// F7091: a connection that never finishes is force-closed once the (injected)
// timer fires; before that Shutdown keeps waiting.
func TestR232ShutdownHTTPForceCloses(t *testing.T) {
	inHandler := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(inHandler)
		<-release
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer close(release)
	go http.Get("http://" + ln.Addr().String())
	<-inHandler

	fire := make(chan time.Time)
	var gotD time.Duration
	done := make(chan error, 1)
	go func() {
		done <- shutdownHTTP(srv, 9*time.Second, func(d time.Duration) <-chan time.Time { gotD = d; return fire })
	}()
	select {
	case <-done:
		t.Fatal("shutdown returned before the force-close timer fired")
	case <-time.After(50 * time.Millisecond): // negative check only
	}
	fire <- time.Now()
	if err := <-done; err == nil {
		t.Fatal("expected context error after forced close")
	}
	if gotD != 9*time.Second {
		t.Fatalf("timer duration = %v", gotD)
	}
	// listener closed and connections force-closed
	if _, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		t.Fatal("listener still open")
	}
}

// Graceful path: no connections, returns nil without the timer.
func TestR232ShutdownHTTPGraceful(t *testing.T) {
	srv := &http.Server{}
	if err := shutdownHTTP(srv, time.Hour, func(time.Duration) <-chan time.Time { return nil }); err != nil {
		t.Fatal(err)
	}
}
