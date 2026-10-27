package server

// DMARC aggregate reports were never sent: the reporter had no production
// caller. The server now starts it with dmarc.enabled and sends reports
// through the normal delivery path.

import (
	"context"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/auth"
)

func TestDMARCReportDelivered(t *testing.T) {
	srv := junkRTServer(t, true)
	srv.config.DMARC.Enabled = true
	srv.config.DMARC.FromEmail = "dmarc@mx.test.com"
	reporter := srv.startDMARCReporter(nil)
	if reporter == nil {
		t.Fatal("reporter not started with dmarc.enabled and from_email set")
	}
	ev := &auth.DMARCEvaluation{
		Record:       &auth.DMARCRecord{Policy: auth.DMARCPolicyReject, Percentage: 100, ReportAggregateURI: []string{"mailto:alice@test.com"}},
		PolicyDomain: "test.com",
		Disposition:  "reject",
	}
	reporter.Record(ev, auth.ReportSample{SourceIP: "192.0.2.1", HeaderFrom: "test.com", EnvelopeFrom: "spoof.example", SPFResult: "fail"})
	if n := reporter.Flush(context.Background()); n != 1 {
		t.Fatalf("Flush sent %d reports, want 1", n)
	}
	stored := junkRTStored(t, srv, "INBOX")
	if !strings.Contains(stored, "Subject: Report Domain: test.com Submitter: mx.test.com") ||
		!strings.Contains(stored, "application/gzip") {
		t.Fatalf("delivered report:\n%s", stored)
	}
}

func TestDMARCReporterOff(t *testing.T) {
	srv := junkRTServer(t, true)
	srv.config.DMARC.Enabled = true
	srv.config.DMARC.FromEmail = ""
	if srv.startDMARCReporter(nil) != nil {
		t.Fatal("reporter started without dmarc.from_email")
	}
	srv.config.DMARC.Enabled = false
	srv.config.DMARC.FromEmail = "dmarc@mx.test.com"
	if srv.startDMARCReporter(nil) != nil {
		t.Fatal("reporter started with dmarc.enabled: false")
	}
}
