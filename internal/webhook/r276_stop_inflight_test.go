package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// F7501: when Stop's drain times out, the in-flight delivery's request must
// be aborted instead of outliving Stop.

func r276Hang(t *testing.T) (*httptest.Server, chan struct{}, chan string, chan struct{}) {
	arrived := make(chan struct{}, 1)
	hold := make(chan struct{})
	res := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // lets the server notice a client disconnect
		arrived <- struct{}{}
		select {
		case <-r.Context().Done():
			res <- "canceled"
		case <-hold:
			res <- "released"
		}
	}))
	t.Cleanup(srv.Close)
	return srv, arrived, res, hold
}

func r276Hook(m *Manager, url string) {
	m.mu.Lock()
	m.hooks = append(m.hooks, &Webhook{ID: "wh", URL: url, Active: true, Events: []string{"*"}})
	m.mu.Unlock()
}

func TestR276_StopDrainTimeoutCancelsInflightRequest(t *testing.T) {
	srv, arrived, res, hold := r276Hang(t)
	defer close(hold)
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	m.drainTimeout = 50 * time.Millisecond
	r276Hook(m, srv.URL)
	m.Trigger("x", 1)
	<-arrived
	m.Stop()
	select {
	case got := <-res:
		if got != "canceled" {
			t.Fatalf("server saw %q, want canceled", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request still running after Stop drain timeout")
	}
	m.Stop() // idempotent
}

func TestR276_StopWithinDrainDoesNotCancel(t *testing.T) {
	srv, arrived, res, hold := r276Hang(t)
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	r276Hook(m, srv.URL)
	m.Trigger("x", 1)
	<-arrived
	stopped := make(chan struct{})
	go func() { m.Stop(); close(stopped) }()
	// Stop must be waiting (drain), not cancelling, while the request runs.
	close(hold)
	<-stopped
	if got := <-res; got != "released" {
		t.Fatalf("server saw %q, want released (drained, not cancelled)", got)
	}
	if d, _, _ := m.DeliveryStats(); d != 1 {
		t.Fatalf("delivered=%d, want 1", d)
	}
}
