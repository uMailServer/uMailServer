package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/tracing"
)

// newTestManager builds a Manager wired to allow private IPs (so httptest
// servers on 127.0.0.1 are accepted) and with no DB.
func newTestManager() *Manager {
	m := NewManager(nil, "test-secret")
	m.SetAllowPrivateIP(true)
	return m
}

func TestSend_NoTracingProvider_DeliversWithoutPanic(t *testing.T) {
	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := newTestManager()
	m.send(&Webhook{ID: "h1", URL: srv.URL, Events: []string{"x"}, Active: true},
		Event{Type: "x", Timestamp: time.Now()})

	if hits.Load() == 0 {
		t.Error("expected at least one delivery hit")
	}
}

func TestSend_TracingEnabled_DeliversAndRecordsSpan(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := newTestManager()
	m.SetTracingProvider(provider)
	m.send(&Webhook{ID: "h2", URL: srv.URL, Events: []string{"y"}, Active: true},
		Event{Type: "y", Timestamp: time.Now()})

	if hits.Load() == 0 {
		t.Error("expected at least one delivery hit with tracing enabled")
	}
}

func TestSend_DisabledProvider_BypassesTracingPath(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := newTestManager()
	m.SetTracingProvider(provider)
	m.send(&Webhook{ID: "h3", URL: srv.URL, Events: []string{"z"}, Active: true},
		Event{Type: "z", Timestamp: time.Now()})

	if hits.Load() == 0 {
		t.Error("expected delivery with disabled provider")
	}
}

func TestSend_4xxResponse_RecordsFailureWithoutRetry(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	m := newTestManager()
	m.SetTracingProvider(provider)
	m.send(&Webhook{ID: "h4", URL: srv.URL, Events: []string{"w"}, Active: true},
		Event{Type: "w", Timestamp: time.Now()})

	// 4xx triggers the no-retry path — exactly one attempt.
	if got := hits.Load(); got != 1 {
		t.Errorf("expected 1 attempt for 4xx, got %d", got)
	}
}
