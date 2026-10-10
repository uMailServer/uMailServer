package alert

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// F5790: concurrent sends with the same key must be deduplicated atomically.
func TestF5790_ConcurrentSendDedup(t *testing.T) {
	var hits atomic.Int32
	srv := newCountingServer(t, &hits)
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = srv.URL
	cfg.MinInterval = time.Hour
	m := NewManager(cfg, nil)
	m.SetAllowPrivateIP(true)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = m.Send("same", SeverityInfo, "x", nil) }()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("expected 1 delivery, got %d", hits.Load())
	}
}

func TestF5790_AlertIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := generateAlertID()
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
}

func newCountingServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(20 * time.Millisecond)
	}))
}
