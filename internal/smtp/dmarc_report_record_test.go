package smtp

// A message rejected by DMARC policy returned before its result was
// recorded, so aggregate reports never showed the failures a domain owner
// most needs to see.

import (
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/auth"
)

func TestAuthDMARCStage_RecordsRejectedMessage(t *testing.T) {
	resolver := &mockAuthDNSResolver{txtRecords: map[string][]string{
		"_dmarc.example.com": {"v=DMARC1; p=reject; rua=mailto:agg@example.com"},
	}}
	stage := NewAuthDMARCStage(auth.NewDMARCEvaluator(resolver), nil)
	reporter := auth.NewDMARCReporter(resolver, nil, auth.DMARCReporterConfig{FromEmail: "d@r.test"})
	stage.SetReporter(reporter)

	ctx := NewMessageContext(net.ParseIP("192.0.2.5"), "sender@example.com", []string{"rcpt@r.test"}, []byte("data"))
	ctx.SPFResult = SPFResult{Result: "fail", Domain: "other.com"}
	if stage.Process(ctx) != ResultReject {
		t.Fatal("INVALID: message not rejected by p=reject")
	}
	if rows, msgs := reporter.GetReportStats("example.com"); rows != 1 || msgs != 1 {
		t.Fatalf("rejected message not recorded: rows=%d messages=%d", rows, msgs)
	}
}
