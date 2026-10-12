package api

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// F7182: when the shutdown timer fires with a request still in flight, Stop
// force-closes the connection instead of leaving it open.
func TestR241_StopForceClosesAfterTimer(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done(): // cancelled when the connection is force-closed
		}
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go hs.Serve(ln)

	s := &Server{stopCh: make(chan struct{}), httpServer: hs}
	fire := make(chan time.Time, 1)
	var gotD time.Duration
	s.SetShutdownTimeout(11 * time.Second)
	s.shutdownAfter = func(d time.Duration) <-chan time.Time { gotD = d; return fire }

	client := &http.Client{}
	reqErr := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://" + ln.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
		reqErr <- err
	}()
	<-entered

	stopErr := make(chan error, 1)
	go func() { stopErr <- s.Stop() }()
	fire <- time.Time{}

	if err := <-stopErr; err == nil {
		t.Fatal("Stop = nil, want deadline error after forced close")
	}
	if err := <-reqErr; err == nil {
		t.Fatal("in-flight request survived force close")
	}
	close(release)
	if gotD != 11*time.Second {
		t.Fatalf("timer duration = %v, want 11s", gotD)
	}
}
