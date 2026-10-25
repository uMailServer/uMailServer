package alert

// Regression tests for the alert-flood defect: Manager.Send is the single
// choke point for every alert path (the Check* methods construct alerts and
// route them through Send), and the operator rate-limit config (MaxAlerts /
// MinInterval / per-name cooldown) must gate deliveries there. Before the
// wiring, Send delivered every alert regardless of the budget — a failing
// component could flood the webhook and email sinks unbounded.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// With MaxAlerts=1 the hourly budget admits exactly one webhook delivery; the
// remaining Sends must be suppressed silently (no error — suppression is the
// alert system doing its job, not a caller failure).
func TestSend_RateLimitsWithinHourlyWindow(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = srv.URL
	cfg.MaxAlerts = 1 // the hourly delivery budget
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)
	mgr.SetAllowPrivateIP(true) // the localhost test server

	for i := 0; i < 3; i++ {
		if err := mgr.Send(fmt.Sprintf("flood_%d", i), SeverityInfo, "flood probe", nil); err != nil {
			t.Fatalf("FAIL: send %d errored (the rate limiter suppresses silently, it must not fail): %v", i, err)
		}
	}

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("FAIL: %d webhook deliveries for 3 Sends with MaxAlerts=1 — the rate limiter never fired (want exactly 1 delivery, the rest suppressed)", got)
	}
}

// Control: with MaxAlerts unset (0 = the unlimited semantics), every send
// delivers — proving the suppression comes from the budget, not from the
// wiring breaking delivery outright.
func TestSend_UnlimitedWhenMaxAlertsZero(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = srv.URL
	cfg.MaxAlerts = 0 // unlimited
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)
	mgr.SetAllowPrivateIP(true)

	for i := 0; i < 3; i++ {
		if err := mgr.Send(fmt.Sprintf("unlim_%d", i), SeverityInfo, "msg", nil); err != nil {
			t.Fatalf("FAIL: send %d errored: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("FAIL: %d deliveries for 3 sends with MaxAlerts=0 (want 3)", got)
	}
}
