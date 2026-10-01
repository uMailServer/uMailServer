package auth

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// maxRetainedPerSourceIP bounds the per-source-IP detail retained for one
// domain. RecordResult appends one element per message, and the only drain
// (GenerateAndSendReport) has no production caller, so an unbounded slice grows
// for the life of the process. Entry.Count already carries the message volume,
// so retaining a small window of recent values is sufficient for reporting.
const maxRetainedPerSourceIP = 10

// appendBounded appends v to s, retaining only the most recent max elements.
// It never reorders and keeps the newest value last, which is what BuildReport
// consumes.
func appendBounded(s []string, v string, max int) []string {
	s = append(s, v)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

// DMARCReporter collects DMARC evaluation results and sends aggregate reports
type DMARCReporter struct {
	resolver  DNSResolver
	logger    *slog.Logger
	reports   map[string]*reportData // key: domain
	reportsMu sync.Mutex
	orgName   string
	fromEmail string
	interval  time.Duration
}

// reportData holds accumulated data for a domain's report
type reportData struct {
	Domain    string
	Policy    DMARCPolicy
	Subdomain DMARCPolicy
	Adkim     DMARCAlignment
	Aspf      DMARCAlignment
	Pct       int
	Entries   []ReportEntry
	Begin     time.Time
	End       time.Time
}

// DMARCReporterConfig holds configuration for the reporter
type DMARCReporterConfig struct {
	OrgName     string        // Organization name for reports
	FromEmail   string        // Sender email for reports
	ReportEmail string        // Email to send reports to
	Interval    time.Duration // How often to send reports
}

// NewDMARCReporter creates a new DMARC reporter
func NewDMARCReporter(resolver DNSResolver, logger *slog.Logger, config DMARCReporterConfig) *DMARCReporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &DMARCReporter{
		resolver:  resolver,
		logger:    logger,
		reports:   make(map[string]*reportData),
		orgName:   config.OrgName,
		fromEmail: config.FromEmail,
		interval:  config.Interval,
	}
}

// RecordResult records a DMARC evaluation result for reporting
func (r *DMARCReporter) RecordResult(domain string, eval *DMARCEvaluation, sourceIP string, spfResult, dkimResult string) {
	if r == nil || domain == "" {
		return
	}

	r.reportsMu.Lock()
	defer r.reportsMu.Unlock()

	data, exists := r.reports[domain]
	if !exists {
		data = &reportData{
			Domain: domain,
			Begin:  time.Now().Add(-r.interval),
			End:    time.Now(),
		}
		r.reports[domain] = data
	}

	// Find or create entry for this source IP
	var entry *ReportEntry
	for i := range data.Entries {
		if data.Entries[i].SourceIP == sourceIP {
			entry = &data.Entries[i]
			break
		}
	}
	if entry == nil {
		data.Entries = append(data.Entries, ReportEntry{
			SourceIP: sourceIP,
		})
		entry = &data.Entries[len(data.Entries)-1]
	}

	entry.Count++
	// These slices are appended once per message, but they only ever feed
	// BuildReport, which takes the most recent value of each. Without a bound
	// the reporter retains one element per message forever: GenerateAndSendReport
	// is the only drain and has no production caller, so memory grows with every
	// message the server accepts. Entry.Count already carries the volume.
	entry.Dispositions = appendBounded(entry.Dispositions, eval.Disposition, maxRetainedPerSourceIP)
	entry.SPFResults = appendBounded(entry.SPFResults, spfResult, maxRetainedPerSourceIP)
	entry.DKIMResults = appendBounded(entry.DKIMResults, dkimResult, maxRetainedPerSourceIP)
	entry.HeaderFrom = domain
}

// SetPolicy updates the policy info for a domain
func (r *DMARCReporter) SetPolicy(domain string, policy DMARCPolicy, sp DMARCPolicy, adkim DMARCAlignment, aspf DMARCAlignment, pct int) {
	if r == nil || domain == "" {
		return
	}

	r.reportsMu.Lock()
	defer r.reportsMu.Unlock()

	data, exists := r.reports[domain]
	if !exists {
		data = &reportData{
			Domain: domain,
			Begin:  time.Now().Add(-r.interval),
		}
		r.reports[domain] = data
	}

	data.Policy = policy
	data.Subdomain = sp
	data.Adkim = adkim
	data.Aspf = aspf
	data.Pct = pct
	data.End = time.Now()
}

// GenerateAndSendReport generates a report for a domain and sends it via email
func (r *DMARCReporter) GenerateAndSendReport(domain, ruaEmail string) error {
	r.reportsMu.Lock()
	data, exists := r.reports[domain]
	if !exists {
		r.reportsMu.Unlock()
		return fmt.Errorf("no data for domain %s", domain)
	}

	// Copy entries for sending
	entries := make([]ReportEntry, len(data.Entries))
	copy(entries, data.Entries)

	// Reset entries after copy
	data.Entries = nil
	data.Begin = time.Now()
	r.reportsMu.Unlock()

	// Build XML report
	xmlData, err := BuildReport(
		r.orgName,
		ruaEmail,
		data.Domain,
		string(data.Adkim),
		string(data.Aspf),
		data.Policy,
		data.Subdomain,
		data.Pct,
		data.Begin,
		data.End,
		entries,
	)
	if err != nil {
		return fmt.Errorf("failed to build report: %w", err)
	}

	// Send email with report
	if err := r.sendReportEmail(ruaEmail, domain, xmlData); err != nil {
		return fmt.Errorf("failed to send report: %w", err)
	}

	r.logger.Info("DMARC report sent",
		"domain", domain,
		"to", ruaEmail,
		"entries", len(entries),
	)

	return nil
}

// sendReportEmail sends the DMARC report via email
func (r *DMARCReporter) sendReportEmail(to, domain string, xmlData []byte) error {
	subject := fmt.Sprintf("DMARC Aggregate Report for %s", domain)

	// Parse the recipient email
	toEmail, err := ParseRUAEmail(to)
	if err != nil {
		return fmt.Errorf("invalid recipient email: %w", err)
	}

	// Build RFC 5321 format message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", r.fromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: message/feedback-report\r\n")
	msg.WriteString("\r\n")

	// Write XML report directly
	msg.Write(xmlData)

	// Send via SMTP (localhost relay assumed)
	addr := "localhost:25"
	err = smtp.SendMail(addr, nil, r.fromEmail, []string{toEmail}, []byte(msg.String()))
	if err != nil {
		r.logger.Warn("Failed to send DMARC report via SMTP, stored for retry",
			"to", toEmail,
			"error", err,
		)
		// In production, would queue for retry
		return err
	}

	return nil
}

// GetReportStats returns statistics for a domain's report
func (r *DMARCReporter) GetReportStats(domain string) (int, int) {
	r.reportsMu.Lock()
	defer r.reportsMu.Unlock()

	data, exists := r.reports[domain]
	if !exists {
		return 0, 0
	}

	totalMessages := 0
	for _, entry := range data.Entries {
		totalMessages += entry.Count
	}

	return len(data.Entries), totalMessages
}
