package alert

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// F6500: the SSRF guard only inspected the literal host, so a name that
// resolves to loopback ("localhost." bypasses the exact-match check) reached
// internal services. The dialer must refuse the resolved address.
func TestF6500_AlertWebhookBlocksResolvedLoopback(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = "http://localhost.:" + port + "/hook"
	m := NewManager(cfg, nil)
	if err := m.Send("x", SeverityInfo, "m", nil); err == nil {
		t.Fatal("expected SSRF rejection, got nil error")
	}
	if hits.Load() != 0 {
		t.Fatalf("internal server was reached %d times", hits.Load())
	}
}

// F6501: SMTP delivery had no timeout; an unresponsive server hung the caller.
func TestF6501_AlertEmailTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { <-release; c.Close() }() // never speaks
		}
	}()
	old := smtpTimeout
	smtpTimeout = 200 * time.Millisecond
	defer func() { smtpTimeout = old }()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.SMTPServer = "127.0.0.1"
	cfg.SMTPPort = ln.Addr().(*net.TCPAddr).Port
	cfg.FromAddress = "a@example.com"
	cfg.ToAddresses = []string{"b@example.com"}
	m := NewManager(cfg, nil)

	done := make(chan error, 1)
	go func() { done <- m.Send("x", SeverityInfo, "m", nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sendEmail hung on an unresponsive SMTP server")
	}
}

// F6502: expired cooldown entries must be pruned so the map stays bounded.
func TestF6502_CooldownMapPruned(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAlerts = 1 << 30
	m := NewManager(cfg, nil)
	old := time.Now().Add(-2 * cfg.MinInterval)
	for i := 0; i < 5000; i++ {
		m.lastAlert[string(rune('a'+i%26))+time.Duration(i).String()] = old
	}
	m.shouldSend("fresh")
	m.mu.RLock()
	n := len(m.lastAlert)
	m.mu.RUnlock()
	if n > 10 {
		t.Fatalf("expired cooldown entries not pruned: %d remain", n)
	}
}
