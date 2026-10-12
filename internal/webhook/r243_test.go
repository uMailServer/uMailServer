package webhook

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// F7200: Stop must wait for in-flight deliveries.
func TestF7200_StopDrainsInFlight(t *testing.T) {
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		hit.Add(1)
	}))
	defer srv.Close()
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	m.hooks = append(m.hooks, &Webhook{ID: "h", URL: srv.URL, Events: []string{"*"}, Active: true})
	m.Trigger("x", nil)
	time.Sleep(50 * time.Millisecond)
	m.Stop()
	if m.delivered.Load() != 1 {
		t.Fatalf("Stop returned before in-flight delivery finished (delivered=%d)", m.delivered.Load())
	}
}

// F7200: the drain wait is bounded.
func TestF7200_StopDrainBounded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	m.drainTimeout = 100 * time.Millisecond
	m.hooks = append(m.hooks, &Webhook{ID: "h", URL: srv.URL, Events: []string{"*"}, Active: true})
	m.Trigger("x", nil)
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	m.Stop()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Stop not bounded: %v", d)
	}
}

// F7201: DNS resolution during validation has a deadline.
func TestF7201_LookupHasTimeout(t *testing.T) {
	m := NewManager(nil, "")
	m.dnsTimeout = 100 * time.Millisecond
	m.lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	ok, _ := m.isValidWebhookURL("http://slow.example.test/x")
	if ok {
		t.Fatal("unresolvable host accepted")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("lookup not bounded: %v", d)
	}
}
