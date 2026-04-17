package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync/atomic"
)

// promContentType is the canonical MIME type for the Prometheus text
// exposition format (version 0.0.4). Set on every PrometheusHandler response
// so scrapers parse the body correctly.
const promContentType = "text/plain; version=0.0.4; charset=utf-8"

// promMetric describes a single counter that the SimpleMetrics struct exposes
// to Prometheus. Each entry maps an internal atomic field to its Prometheus
// name + HELP text. Adding a new metric means adding one line here.
type promMetric struct {
	name  string
	help  string
	value func(*SimpleMetrics) uint64
}

// orderedPromMetrics is the canonical list, kept in alphabetical order so the
// scrape output is deterministic (helps differential debugging and tests).
var orderedPromMetrics = []promMetric{
	{
		name:  "umailserver_api_requests_total",
		help:  "Total number of HTTP API requests served.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.apiRequests) },
	},
	{
		name:  "umailserver_delivery_failed_total",
		help:  "Total number of outbound deliveries that ultimately failed.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.deliveriesFailed) },
	},
	{
		name:  "umailserver_delivery_success_total",
		help:  "Total number of outbound messages delivered successfully.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.deliveriesTotal) },
	},
	{
		name:  "umailserver_dkim_cache_hits_total",
		help:  "Total DKIM key cache hits.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.dkimCacheHits) },
	},
	{
		name:  "umailserver_dkim_cache_misses_total",
		help:  "Total DKIM key cache misses (DNS lookup performed).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.dkimCacheMisses) },
	},
	{
		name:  "umailserver_dmarc_cache_hits_total",
		help:  "Total DMARC record cache hits.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.dmarcCacheHits) },
	},
	{
		name:  "umailserver_dmarc_cache_misses_total",
		help:  "Total DMARC record cache misses (DNS lookup performed).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.dmarcCacheMisses) },
	},
	{
		name:  "umailserver_imap_connections_total",
		help:  "Total IMAP client connections accepted.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.imapConnections) },
	},
	{
		name:  "umailserver_smtp_auth_failures_total",
		help:  "Total SMTP authentication failures (wrong password, lockout, etc.).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.smtpAuthFailures) },
	},
	{
		name:  "umailserver_smtp_connections_total",
		help:  "Total SMTP client connections accepted across all listeners (25/465/587).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.smtpConnections) },
	},
	{
		name:  "umailserver_smtp_messages_received_total",
		help:  "Total inbound SMTP messages successfully received (passed pipeline).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.smtpMessages) },
	},
	{
		name:  "umailserver_spam_ham_total",
		help:  "Total messages classified as ham (not spam).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.hamDetected) },
	},
	{
		name:  "umailserver_spam_spam_total",
		help:  "Total messages classified as spam.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.spamDetected) },
	},
	{
		name:  "umailserver_spf_cache_hits_total",
		help:  "Total SPF record cache hits.",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.spfCacheHits) },
	},
	{
		name:  "umailserver_spf_cache_misses_total",
		help:  "Total SPF record cache misses (DNS lookup performed).",
		value: func(m *SimpleMetrics) uint64 { return atomic.LoadUint64(&m.spfCacheMisses) },
	},
}

// init guards against developers adding metrics out of order.
func init() {
	sorted := make([]string, len(orderedPromMetrics))
	for i, m := range orderedPromMetrics {
		sorted[i] = m.name
	}
	if !sort.StringsAreSorted(sorted) {
		panic("metrics: orderedPromMetrics must be alphabetically sorted")
	}
}

// WritePrometheus encodes the current metric snapshot in the Prometheus text
// exposition format (v0.0.4). It writes one HELP + TYPE + value triplet per
// metric, suffixed by an empty line, so the output parses cleanly with the
// reference Prometheus parser.
func (m *SimpleMetrics) WritePrometheus(w io.Writer) error {
	for _, pm := range orderedPromMetrics {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", pm.name, pm.help); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", pm.name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s %d\n", pm.name, pm.value(m)); err != nil {
			return err
		}
	}
	return nil
}

// PrometheusHandler returns the metrics in Prometheus text exposition format.
// Mounted on cfg.Metrics.Path of the dedicated metrics server (so scrapers
// can hit it without API auth).
func (m *SimpleMetrics) PrometheusHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", promContentType)
	_ = m.WritePrometheus(w)
}
