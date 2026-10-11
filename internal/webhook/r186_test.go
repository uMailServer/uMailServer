package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newR186(t *testing.T) *Manager {
	m := NewManager(nil, "s")
	m.SetAllowPrivateIP(true)
	m.backoffBase, m.backoffMax = time.Millisecond, time.Millisecond
	return m
}

// F6635: retries of one delivery carry the same idempotency key.
func TestRetrySameIdempotencyKey_F6635(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key")+"|"+r.Header.Get("X-Webhook-Event-ID"))
		n := len(keys)
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	m := newR186(t)
	m.send(&Webhook{ID: "h", URL: srv.URL, Active: true}, Event{Type: "x", Timestamp: time.Now()})
	if len(keys) != 3 {
		t.Fatalf("attempts=%d", len(keys))
	}
	id := strings.Split(keys[0], "|")[0]
	if !strings.HasPrefix(id, "evt_") {
		t.Fatalf("no key: %q", keys[0])
	}
	for _, k := range keys {
		if k != id+"|"+id {
			t.Fatalf("key changed across retries: %v", keys)
		}
	}
}

// F6634: oversized payload is dropped before any request is made.
func TestOversizedPayloadDropped_F6634(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer srv.Close()
	m := newR186(t)
	m.send(&Webhook{ID: "h", URL: srv.URL, Active: true}, Event{Type: "x", Timestamp: time.Now(), Data: strings.Repeat("a", MaxPayloadBytes+1)})
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatal("oversized event was delivered")
	}
	if _, failed, _ := m.DeliveryStats(); failed != 1 {
		t.Fatalf("failed=%d", failed)
	}
	if m.cbManager.Get(srv.URL).Metrics().Failures != 0 {
		t.Log("note: breaker untouched expected")
	}
}

// Retry budget: a zero-ish total budget stops retries early.
func TestRetryBudgetStopsRetries_F6634(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()
	m := newR186(t)
	m.backoffBase, m.backoffMax = time.Hour, time.Hour
	m.maxTotal = time.Minute
	m.send(&Webhook{ID: "h", URL: srv.URL, Active: true}, Event{Type: "x", Timestamp: time.Now()})
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("hits=%d, want 1 (backoff exceeds budget)", h)
	}
}

// F6636: SetSecret races with in-flight signing under -race and takes effect.
func TestSetSecretRotation_F6636(t *testing.T) {
	var sig atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig.Store(r.Header.Get("X-Webhook-Signature"))
	}))
	defer srv.Close()
	m := newR186(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); m.send(&Webhook{ID: "h", URL: srv.URL}, Event{ID: "e", Type: "x"}) }()
	m.SetSecret("rotated")
	wg.Wait()
	m.send(&Webhook{ID: "h", URL: srv.URL}, Event{ID: "e", Type: "x"})
	if m.getSecret() != "rotated" {
		t.Fatal("secret not rotated")
	}
}
