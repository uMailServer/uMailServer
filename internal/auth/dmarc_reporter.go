package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Caps on aggregate-report state. The header-From domain and source IP are
// attacker-chosen, so both the tracked domains and the per-domain rows must
// be bounded (F7244). The DKIM d= and MAIL FROM domains need not resolve,
// so row fields are capped at a DNS name's length and the rows of all
// domains together are bounded too. Messages beyond a cap are not reported.
const (
	maxReportEntriesPerDomain = 1000
	maxReportDomains          = 10000
	maxReportRowsTotal        = 100000
	maxReportFieldLen         = 253
)

// ReportSendFunc delivers a report message from from to the to addresses.
type ReportSendFunc func(from string, to []string, msg []byte) error

// DMARCReporter collects the DMARC results of inbound mail and periodically
// sends aggregate reports (RFC 7489 §7.2) to the rua= addresses of the
// domains whose policy governed it. Results are kept in memory, so data not
// yet reported is lost on restart.
type DMARCReporter struct {
	resolver  DNSResolver
	logger    *slog.Logger
	send      ReportSendFunc
	reports   map[string]*reportData // key: policy domain
	rowCount  int                    // rows across all domains
	reportsMu sync.Mutex
	orgName   string
	fromEmail string
	email     string // report_metadata contact address
	receiver  string // reporting host, used in attachment names
	interval  time.Duration
	now       func() time.Time
}

// reportData holds the accumulated rows of one policy domain since its last
// report.
type reportData struct {
	Record DMARCRecord // latest record seen; policy_published and rua
	Begin  time.Time
	End    time.Time
	Rows   map[ReportRow]int
}

// DMARCReporterConfig holds configuration for the reporter
type DMARCReporterConfig struct {
	OrgName     string         // Organization name for reports
	FromEmail   string         // Sender address of report messages
	ReportEmail string         // Contact address in report metadata (default FromEmail)
	Receiver    string         // Reporting host name, e.g. the server hostname
	Interval    time.Duration  // How often reports are sent (default 24h)
	Send        ReportSendFunc // Delivers report messages
}

// NewDMARCReporter creates a new DMARC reporter
func NewDMARCReporter(resolver DNSResolver, logger *slog.Logger, config DMARCReporterConfig) *DMARCReporter {
	if logger == nil {
		logger = slog.Default()
	}
	email := config.ReportEmail
	if email == "" {
		email = config.FromEmail
	}
	receiver := config.Receiver
	if receiver == "" {
		if at := strings.LastIndex(config.FromEmail, "@"); at >= 0 {
			receiver = config.FromEmail[at+1:]
		}
	}
	interval := config.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &DMARCReporter{
		resolver:  resolver,
		logger:    logger,
		send:      config.Send,
		reports:   make(map[string]*reportData),
		orgName:   config.OrgName,
		fromEmail: config.FromEmail,
		email:     email,
		receiver:  receiver,
		interval:  interval,
		now:       time.Now,
	}
}

// ReportSample identifies one evaluated message for aggregate reporting.
type ReportSample struct {
	SourceIP     string
	HeaderFrom   string
	EnvelopeFrom string // domain SPF checked
	SPFResult    string
	DKIMDomain   string
	DKIMResult   string
}

// Record adds an evaluated message to the next report of its policy domain.
// Messages governed by no record, or by one that asks for no aggregate
// reports (no mailto: rua), are not kept.
func (r *DMARCReporter) Record(ev *DMARCEvaluation, s ReportSample) {
	if r == nil || ev == nil || ev.Record == nil || ev.PolicyDomain == "" ||
		!dmarcHasValidRua(ev.Record.ReportAggregateURI) {
		return
	}
	row := ReportRow{
		SourceIP:     s.SourceIP,
		HeaderFrom:   strings.ToLower(s.HeaderFrom),
		EnvelopeFrom: strings.ToLower(s.EnvelopeFrom),
		Disposition:  ev.Disposition,
		DKIMEval:     passFail(ev.DKIMAligned),
		SPFEval:      passFail(ev.SPFAligned),
		DKIMDomain:   strings.ToLower(s.DKIMDomain),
		DKIMResult:   s.DKIMResult,
		SPFResult:    s.SPFResult,
	}
	if row.Disposition == "" {
		row.Disposition = "none"
	}
	if row.DKIMDomain == "" {
		row.DKIMResult = ""
	}
	for _, f := range []string{row.SourceIP, row.HeaderFrom, row.EnvelopeFrom, row.DKIMDomain, row.DKIMResult, row.SPFResult, ev.PolicyDomain} {
		if len(f) > maxReportFieldLen {
			return
		}
	}
	now := r.now()

	r.reportsMu.Lock()
	defer r.reportsMu.Unlock()
	data, ok := r.reports[ev.PolicyDomain]
	if !ok {
		if len(r.reports) >= maxReportDomains || r.rowCount >= maxReportRowsTotal {
			return
		}
		data = &reportData{Begin: now, Rows: make(map[ReportRow]int)}
		r.reports[ev.PolicyDomain] = data
	}
	if _, seen := data.Rows[row]; !seen {
		if len(data.Rows) >= maxReportEntriesPerDomain || r.rowCount >= maxReportRowsTotal {
			return
		}
		r.rowCount++
	}
	data.Record = *ev.Record
	data.Rows[row]++
	data.End = now
}

func passFail(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

// pendingReport is one domain's drained data, ready to be sent.
type pendingReport struct {
	domain string
	data   *reportData
}

// drain removes and returns every domain's accumulated data.
func (r *DMARCReporter) drain() []pendingReport {
	r.reportsMu.Lock()
	defer r.reportsMu.Unlock()
	out := make([]pendingReport, 0, len(r.reports))
	for domain, data := range r.reports {
		out = append(out, pendingReport{domain: domain, data: data})
	}
	r.reports = make(map[string]*reportData)
	r.rowCount = 0
	return out
}

// Flush sends a report for every domain with data since the last flush and
// returns how many report messages were sent. A report that cannot be sent
// is logged and dropped: its data is not carried into the next report.
func (r *DMARCReporter) Flush(ctx context.Context) int {
	if r == nil {
		return 0
	}
	now := r.now()
	sent := 0
	for _, p := range r.drain() {
		end := p.data.End
		if end.Before(p.data.Begin) || end.Equal(p.data.Begin) {
			end = now
		}
		in := ReportInput{
			OrgName:  r.orgName,
			Email:    r.email,
			ReportID: fmt.Sprintf("%s.%d.%d", p.domain, p.data.Begin.Unix(), now.UnixNano()),
			Domain:   p.domain,
			Record:   p.data.Record,
			Begin:    p.data.Begin,
			End:      end,
			Rows:     p.data.Rows,
		}
		sent += r.sendReport(ctx, in, now)
	}
	return sent
}

// sendReport mails one domain's report to each authorized rua= address.
func (r *DMARCReporter) sendReport(ctx context.Context, in ReportInput, now time.Time) int {
	if r.send == nil {
		return 0
	}
	xmlData, err := BuildReport(in)
	if err != nil {
		r.logger.Warn("DMARC report not built", "domain", in.Domain, "error", err)
		return 0
	}
	sent := 0
	for _, uri := range in.Record.ReportAggregateURI {
		to, err := ParseRUAEmail(uri)
		if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(uri)), "mailto:") {
			continue
		}
		// F7241: RFC 7489 §7.1 — never mail a report to a third-party
		// domain that has not opted in.
		vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ok, verr := VerifyReportDestination(vctx, r.resolver, in.Domain, to)
		cancel()
		if !ok {
			r.logger.Info("DMARC report destination not authorized", "domain", in.Domain, "to", to, "error", verr)
			continue
		}
		msg, err := BuildReportMessage(r.fromEmail, to, r.receiver, in, xmlData, now)
		if err == nil {
			err = r.send(r.fromEmail, []string{to}, msg)
		}
		if err != nil {
			r.logger.Warn("DMARC report not sent", "domain", in.Domain, "to", to, "error", err)
			continue
		}
		r.logger.Info("DMARC report sent", "domain", in.Domain, "to", to, "rows", len(in.Rows))
		sent++
	}
	return sent
}

// Run sends reports every interval until ctx is done.
func (r *DMARCReporter) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Flush(ctx)
		}
	}
}

// GetReportStats returns the number of rows and messages pending for a
// policy domain.
func (r *DMARCReporter) GetReportStats(domain string) (rows, messages int) {
	r.reportsMu.Lock()
	defer r.reportsMu.Unlock()
	data, ok := r.reports[domain]
	if !ok {
		return 0, 0
	}
	for _, n := range data.Rows {
		messages += n
	}
	return len(data.Rows), messages
}
