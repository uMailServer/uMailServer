package metrics

import (
	"bytes"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func TestWritePrometheus_EmitsAllMetricsInOrder(t *testing.T) {
	m := &SimpleMetrics{}
	m.SMTPConnection()
	m.SMTPMessageReceived()
	m.SPFCacheHit()
	m.DKIMCacheMiss()

	var buf bytes.Buffer
	if err := m.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	out := buf.String()

	// Every registered metric must appear with HELP, TYPE, and a value line.
	for _, pm := range orderedPromMetrics {
		if !strings.Contains(out, "# HELP "+pm.name+" ") {
			t.Errorf("missing HELP for %s", pm.name)
		}
		if !strings.Contains(out, "# TYPE "+pm.name+" counter\n") {
			t.Errorf("missing TYPE for %s", pm.name)
		}
		valuePrefix := pm.name + " "
		if !strings.Contains(out, valuePrefix) {
			t.Errorf("missing value line for %s", pm.name)
		}
	}

	// Output must be sorted (helps differential debugging across scrapes).
	var seen []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "# HELP ") {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) >= 3 {
				seen = append(seen, parts[2])
			}
		}
	}
	if !sort.StringsAreSorted(seen) {
		t.Errorf("Prometheus metric names not sorted: %v", seen)
	}
}

func TestWritePrometheus_RecordedValuesEmitted(t *testing.T) {
	m := &SimpleMetrics{}
	m.SMTPConnection()
	m.SMTPConnection()
	m.SMTPConnection()
	m.SPFCacheHit()
	m.SPFCacheHit()

	var buf bytes.Buffer
	if err := m.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "umailserver_smtp_connections_total 3\n") {
		t.Errorf("expected smtp_connections=3 in output\n%s", out)
	}
	if !strings.Contains(out, "umailserver_spf_cache_hits_total 2\n") {
		t.Errorf("expected spf_cache_hits=2 in output\n%s", out)
	}
	if !strings.Contains(out, "umailserver_smtp_messages_received_total 0\n") {
		t.Errorf("expected zero metrics to still appear\n%s", out)
	}
}

func TestPrometheusHandler_ContentTypeAndBody(t *testing.T) {
	m := &SimpleMetrics{}
	m.IMAPConnection()

	rr := httptest.NewRecorder()
	m.PrometheusHandler(rr, httptest.NewRequest("GET", "/metrics", nil))

	if got := rr.Header().Get("Content-Type"); got != promContentType {
		t.Errorf("Content-Type = %q, want %q", got, promContentType)
	}
	if !strings.Contains(rr.Body.String(), "umailserver_imap_connections_total 1\n") {
		t.Errorf("body missing imap counter:\n%s", rr.Body.String())
	}
}

func TestPrometheusHandler_AlwaysReturnsBaseline(t *testing.T) {
	// Even with no recorded events, every metric must appear with value 0 so
	// scrapers see a stable surface area on first connect.
	m := &SimpleMetrics{}

	rr := httptest.NewRecorder()
	m.PrometheusHandler(rr, httptest.NewRequest("GET", "/metrics", nil))

	for _, pm := range orderedPromMetrics {
		if !strings.Contains(rr.Body.String(), pm.name+" 0\n") {
			t.Errorf("baseline 0 missing for %s", pm.name)
		}
	}
}
