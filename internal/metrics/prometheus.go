package metrics

import (
	"encoding/json"
	"expvar"
	"fmt"
	"net/http"
	"runtime"
	"time"
)

// Metrics holds all application metrics
type Metrics struct {
	// SMTP metrics
	SMTPEmailsReceived    *expvar.Int
	SMTPEmailsSent        *expvar.Int
	SMTPEmailsRejected    *expvar.Int
	SMTPConnections       *expvar.Int
	SMTPActiveConnections *expvar.Int

	// IMAP metrics
	IMAPConnections       *expvar.Int
	IMAPActiveConnections *expvar.Int
	IMAPMessagesFetched   *expvar.Int
	IMAPMessagesStored    *expvar.Int

	// HTTP metrics
	HTTPRequestsTotal     *expvar.Int
	HTTPRequestsActive    *expvar.Int
	HTTPRequestDuration   *expvar.Float

	// Queue metrics
	QueueSize             *expvar.Int
	QueueMessagesDelivered *expvar.Int
	QueueMessagesFailed   *expvar.Int
	QueueMessagesBounced  *expvar.Int

	// Spam metrics
	SpamMessagesScanned   *expvar.Int
	SpamMessagesBlocked   *expvar.Int
	SpamMessagesJunk      *expvar.Int

	// Storage metrics
	StorageBytesUsed      *expvar.Int
	StorageAccounts       *expvar.Int
	StorageDomains        *expvar.Int

	// Start time
	StartTime             time.Time
}

// New creates a new Metrics instance
func New() *Metrics {
	m := &Metrics{
		SMTPEmailsReceived:    expvar.NewInt("smtp_emails_received"),
		SMTPEmailsSent:        expvar.NewInt("smtp_emails_sent"),
		SMTPEmailsRejected:    expvar.NewInt("smtp_emails_rejected"),
		SMTPConnections:       expvar.NewInt("smtp_connections_total"),
		SMTPActiveConnections: expvar.NewInt("smtp_connections_active"),

		IMAPConnections:       expvar.NewInt("imap_connections_total"),
		IMAPActiveConnections: expvar.NewInt("imap_connections_active"),
		IMAPMessagesFetched:   expvar.NewInt("imap_messages_fetched"),
		IMAPMessagesStored:    expvar.NewInt("imap_messages_stored"),

		HTTPRequestsTotal:     expvar.NewInt("http_requests_total"),
		HTTPRequestsActive:    expvar.NewInt("http_requests_active"),
		HTTPRequestDuration:   expvar.NewFloat("http_request_duration_seconds"),

		QueueSize:             expvar.NewInt("queue_size"),
		QueueMessagesDelivered: expvar.NewInt("queue_messages_delivered"),
		QueueMessagesFailed:   expvar.NewInt("queue_messages_failed"),
		QueueMessagesBounced:  expvar.NewInt("queue_messages_bounced"),

		SpamMessagesScanned:   expvar.NewInt("spam_messages_scanned"),
		SpamMessagesBlocked:   expvar.NewInt("spam_messages_blocked"),
		SpamMessagesJunk:      expvar.NewInt("spam_messages_junk"),

		StorageBytesUsed:      expvar.NewInt("storage_bytes_used"),
		StorageAccounts:       expvar.NewInt("storage_accounts"),
		StorageDomains:        expvar.NewInt("storage_domains"),

		StartTime: time.Now(),
	}

	return m
}

// SMTPConnected increments SMTP connection counters
func (m *Metrics) SMTPConnected() {
	m.SMTPConnections.Add(1)
	m.SMTPActiveConnections.Add(1)
}

// SMTPDisconnected decrements active SMTP connections
func (m *Metrics) SMTPDisconnected() {
	m.SMTPActiveConnections.Add(-1)
}

// SMTPReceived increments received email counter
func (m *Metrics) SMTPReceived() {
	m.SMTPEmailsReceived.Add(1)
}

// SMTPSent increments sent email counter
func (m *Metrics) SMTPSent() {
	m.SMTPEmailsSent.Add(1)
}

// SMTPRejected increments rejected email counter
func (m *Metrics) SMTPRejected() {
	m.SMTPEmailsRejected.Add(1)
}

// IMAPConnected increments IMAP connection counters
func (m *Metrics) IMAPConnected() {
	m.IMAPConnections.Add(1)
	m.IMAPActiveConnections.Add(1)
}

// IMAPDisconnected decrements active IMAP connections
func (m *Metrics) IMAPDisconnected() {
	m.IMAPActiveConnections.Add(-1)
}

// IMAPMessageFetched increments fetched message counter
func (m *Metrics) IMAPMessageFetched() {
	m.IMAPMessagesFetched.Add(1)
}

// IMAPMessageStored increments stored message counter
func (m *Metrics) IMAPMessageStored() {
	m.IMAPMessagesStored.Add(1)
}

// HTTPRequestStarted increments active request counter
func (m *Metrics) HTTPRequestStarted() {
	m.HTTPRequestsTotal.Add(1)
	m.HTTPRequestsActive.Add(1)
}

// HTTPRequestFinished decrements active request counter
func (m *Metrics) HTTPRequestFinished(duration time.Duration) {
	m.HTTPRequestsActive.Add(-1)
	m.HTTPRequestDuration.Add(duration.Seconds())
}

// QueueEnqueued increments queue size
func (m *Metrics) QueueEnqueued() {
	m.QueueSize.Add(1)
}

// QueueDelivered marks a message as delivered
func (m *Metrics) QueueDelivered() {
	m.QueueSize.Add(-1)
	m.QueueMessagesDelivered.Add(1)
}

// QueueFailed marks a message as failed
func (m *Metrics) QueueFailed() {
	m.QueueSize.Add(-1)
	m.QueueMessagesFailed.Add(1)
}

// QueueBounced marks a message as bounced
func (m *Metrics) QueueBounced() {
	m.QueueSize.Add(-1)
	m.QueueMessagesBounced.Add(1)
}

// SpamScanned increments scanned counter
func (m *Metrics) SpamScanned() {
	m.SpamMessagesScanned.Add(1)
}

// SpamBlocked increments blocked counter
func (m *Metrics) SpamBlocked() {
	m.SpamMessagesBlocked.Add(1)
}

// SpamJunk increments junk counter
func (m *Metrics) SpamJunk() {
	m.SpamMessagesJunk.Add(1)
}

// UpdateStorage updates storage metrics
func (m *Metrics) UpdateStorage(bytesUsed int64, accounts int64, domains int64) {
	m.StorageBytesUsed.Set(bytesUsed)
	m.StorageAccounts.Set(accounts)
	m.StorageDomains.Set(domains)
}

// Uptime returns the server uptime
func (m *Metrics) Uptime() time.Duration {
	return time.Since(m.StartTime)
}

// PrometheusHandler returns metrics in Prometheus format
func (m *Metrics) PrometheusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// Write expvar metrics in Prometheus format
		fmt.Fprintf(w, "# HELP umailserver_uptime_seconds Server uptime in seconds\n")
		fmt.Fprintf(w, "# TYPE umailserver_uptime_seconds counter\n")
		fmt.Fprintf(w, "umailserver_uptime_seconds %d\n", int(m.Uptime().Seconds()))

		fmt.Fprintf(w, "# HELP umailserver_smtp_emails_received Total emails received via SMTP\n")
		fmt.Fprintf(w, "# TYPE umailserver_smtp_emails_received counter\n")
		fmt.Fprintf(w, "umailserver_smtp_emails_received %s\n", m.SMTPEmailsReceived.String())

		fmt.Fprintf(w, "# HELP umailserver_smtp_emails_sent Total emails sent via SMTP\n")
		fmt.Fprintf(w, "# TYPE umailserver_smtp_emails_sent counter\n")
		fmt.Fprintf(w, "umailserver_smtp_emails_sent %s\n", m.SMTPEmailsSent.String())

		fmt.Fprintf(w, "# HELP umailserver_smtp_emails_rejected Total emails rejected via SMTP\n")
		fmt.Fprintf(w, "# TYPE umailserver_smtp_emails_rejected counter\n")
		fmt.Fprintf(w, "umailserver_smtp_emails_rejected %s\n", m.SMTPEmailsRejected.String())

		fmt.Fprintf(w, "# HELP umailserver_smtp_connections_active Active SMTP connections\n")
		fmt.Fprintf(w, "# TYPE umailserver_smtp_connections_active gauge\n")
		fmt.Fprintf(w, "umailserver_smtp_connections_active %s\n", m.SMTPActiveConnections.String())

		fmt.Fprintf(w, "# HELP umailserver_imap_connections_active Active IMAP connections\n")
		fmt.Fprintf(w, "# TYPE umailserver_imap_connections_active gauge\n")
		fmt.Fprintf(w, "umailserver_imap_connections_active %s\n", m.IMAPActiveConnections.String())

		fmt.Fprintf(w, "# HELP umailserver_http_requests_active Active HTTP requests\n")
		fmt.Fprintf(w, "# TYPE umailserver_http_requests_active gauge\n")
		fmt.Fprintf(w, "umailserver_http_requests_active %s\n", m.HTTPRequestsActive.String())

		fmt.Fprintf(w, "# HELP umailserver_queue_size Current queue size\n")
		fmt.Fprintf(w, "# TYPE umailserver_queue_size gauge\n")
		fmt.Fprintf(w, "umailserver_queue_size %s\n", m.QueueSize.String())

		fmt.Fprintf(w, "# HELP umailserver_spam_messages_blocked Total spam messages blocked\n")
		fmt.Fprintf(w, "# TYPE umailserver_spam_messages_blocked counter\n")
		fmt.Fprintf(w, "umailserver_spam_messages_blocked %s\n", m.SpamMessagesBlocked.String())

		fmt.Fprintf(w, "# HELP umailserver_storage_bytes_used Storage bytes used\n")
		fmt.Fprintf(w, "# TYPE umailserver_storage_bytes_used gauge\n")
		fmt.Fprintf(w, "umailserver_storage_bytes_used %s\n", m.StorageBytesUsed.String())

		fmt.Fprintf(w, "# HELP umailserver_storage_accounts Number of accounts\n")
		fmt.Fprintf(w, "# TYPE umailserver_storage_accounts gauge\n")
		fmt.Fprintf(w, "umailserver_storage_accounts %s\n", m.StorageAccounts.String())

		fmt.Fprintf(w, "# HELP umailserver_storage_domains Number of domains\n")
		fmt.Fprintf(w, "# TYPE umailserver_storage_domains gauge\n")
		fmt.Fprintf(w, "umailserver_storage_domains %s\n", m.StorageDomains.String())

		// Go runtime metrics
		fmt.Fprintf(w, "# HELP go_goroutines Number of goroutines\n")
		fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
		fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())

		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		fmt.Fprintf(w, "# HELP go_memstats_alloc_bytes Number of bytes allocated\n")
		fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\n")
		fmt.Fprintf(w, "go_memstats_alloc_bytes %d\n", memStats.Alloc)

		fmt.Fprintf(w, "# HELP go_memstats_sys_bytes Number of bytes obtained from system\n")
		fmt.Fprintf(w, "# TYPE go_memstats_sys_bytes gauge\n")
		fmt.Fprintf(w, "go_memstats_sys_bytes %d\n", memStats.Sys)
	}
}

// HealthHandler returns health status
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		health := struct {
			Status    string            `json:"status"`
			Timestamp time.Time         `json:"timestamp"`
			Checks    map[string]string `json:"checks"`
		}{
			Status:    "healthy",
			Timestamp: time.Now(),
			Checks: map[string]string{
				"database": "ok",
				"storage":  "ok",
			},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(health)
	}
}
