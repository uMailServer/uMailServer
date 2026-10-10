package alert

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestF6295_RedirectToInternalHostBlocked(t *testing.T) {
	var hit atomic.Bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Store(true) }))
	defer internal.Close()
	m := NewManager(Config{Enabled: true, WebhookURL: "http://203.0.113.9/x", MaxAlerts: 10}, nil)
	// Public-looking first hop is simulated by allowing private for hop 1 only:
	// use the guard directly on the redirect target.
	req, _ := http.NewRequest("GET", internal.URL, nil)
	if err := m.httpClient.CheckRedirect(req, nil); err == nil {
		t.Fatal("redirect to loopback must be rejected")
	}
	m.SetAllowPrivateIP(true)
	if err := m.httpClient.CheckRedirect(req, nil); err != nil {
		t.Fatalf("allowed in test mode: %v", err)
	}
}

func TestF6296_ErrorDoesNotLeakURLCredentials(t *testing.T) {
	m := NewManager(Config{Enabled: true, WebhookURL: "http://user:pw123@127.0.0.1:1/h?token=tok456", MaxAlerts: 10}, nil)
	m.SetAllowPrivateIP(true)
	err := m.Send("a", SeverityInfo, "m", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, s := range []string{"pw123", "tok456"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("leaked %s: %v", s, err)
		}
	}
}

func TestF6297_UnspecifiedAddressBlocked(t *testing.T) {
	m := NewManager(Config{}, nil)
	for _, u := range []string{"http://0.0.0.0/x", "http://[::]/x"} {
		if m.isValidWebhookURL(u) {
			t.Fatalf("%s must be blocked", u)
		}
	}
}
