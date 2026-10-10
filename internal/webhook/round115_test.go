package webhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRound115RedactURL(t *testing.T) {
	got := redactURL("https://user:pw@example.com/hook?token=SECRET#frag")
	for _, bad := range []string{"pw", "SECRET", "user", "frag"} {
		if strings.Contains(got, bad) {
			t.Errorf("redactURL leaked %q: %s", bad, got)
		}
	}
	if got != "https://example.com/hook" {
		t.Errorf("got %s", got)
	}
}

func TestRound115CircuitMetricsRedacted(t *testing.T) {
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	m.send(&Webhook{ID: "h", URL: srv.URL + "/x?token=SECRET", Active: true}, Event{Type: "t"})
	for k := range m.GetCircuitBreakerMetrics() {
		if strings.Contains(k, "SECRET") {
			t.Errorf("metrics key leaks token: %s", k)
		}
	}
}

func TestRound115Retries429(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	var attempts int
	var err error
	var ok bool
	m.sendInner(&Webhook{ID: "h", URL: srv.URL}, Event{Type: "t"}, &attempts, &err, &ok)
	if !ok || attempts != 2 {
		t.Errorf("ok=%v attempts=%d err=%v", ok, attempts, err)
	}
}

func TestRound115CreateRejectsBadURL(t *testing.T) {
	m := NewManager(nil, "")
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "", "http://"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"url":"`+u+`","events":["*"]}`))
		w := httptest.NewRecorder()
		m.HTTPHandler(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("url %q: code %d", u, w.Code)
		}
	}
	if len(m.hooks) != 0 {
		t.Error("hook registered")
	}
}
