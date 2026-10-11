package alert

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// F6871: cooldowns persist across manager restarts.
func TestF6871_CooldownSurvivesRestart(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = srv.URL
	cfg.MinInterval = time.Hour
	path := filepath.Join(t.TempDir(), "alert_state.json")

	mk := func() *Manager {
		m := NewManager(cfg, nil)
		m.SetAllowPrivateIP(true)
		if err := m.SetStateFile(path); err != nil {
			t.Fatal(err)
		}
		return m
	}
	_ = mk().Send("queue", SeverityWarning, "x", nil)
	_ = mk().Send("queue", SeverityWarning, "x", nil) // simulated restart
	if hits.Load() != 1 {
		t.Fatalf("restart re-alerted: %d deliveries", hits.Load())
	}
	_ = mk().Send("other", SeverityWarning, "x", nil)
	if hits.Load() != 2 {
		t.Fatalf("distinct alert suppressed: %d", hits.Load())
	}
}

func TestF6871_CorruptStateIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	m := NewManager(DefaultConfig(), nil)
	if err := writeFile(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetStateFile(path); err == nil {
		t.Fatal("expected error for corrupt state")
	}
	if !func() bool { m.config.Enabled = true; return m.shouldSend("a") }() {
		t.Fatal("corrupt state must not block alerts")
	}
}

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }

func TestF6872_HeaderSafe(t *testing.T) {
	if got := headerSafe("a\r\nBcc: evil@x"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("CRLF survived: %q", got)
	}
}
