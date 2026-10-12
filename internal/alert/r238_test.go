package alert

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// F7150: a failed delivery must not consume the cooldown.
func TestF7150_FailedDeliveryReleasesCooldown(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = srv.URL
	cfg.MinInterval = time.Hour
	m := NewManager(cfg, nil)
	m.SetAllowPrivateIP(true)
	if err := m.Send("disk", SeverityCritical, "x", nil); err == nil {
		t.Fatal("expected failure")
	}
	fail.Store(false)
	if err := m.Send("disk", SeverityCritical, "x", nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("second send suppressed by cooldown of undelivered alert: hits=%d", hits.Load())
	}
	// A delivered alert still starts the cooldown.
	_ = m.Send("disk", SeverityCritical, "x", nil)
	if hits.Load() != 2 {
		t.Fatalf("cooldown not applied after success: hits=%d", hits.Load())
	}
}

func TestF7152_AlertBlocksNAT64(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	if err := m.dialControl("tcp", "[64:ff9b::7f00:1]:80", nil); err == nil {
		t.Fatal("NAT64 address must be rejected")
	}
}
