package alert

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// F7203: a zero MaxAlerts means "default", not "drop everything".
func TestF7203_ZeroMaxAlertsUsesDefault(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer srv.Close()
	cfg := Config{Enabled: true, WebhookURL: srv.URL}
	m := NewManager(cfg, nil)
	m.SetAllowPrivateIP(true)
	if err := m.Send("a", SeverityWarning, "msg", nil); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("alert dropped with MaxAlerts=0 (delivered %d)", n.Load())
	}
}
