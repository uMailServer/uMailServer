package auth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// F7240: the report window Begin was reset to now before BuildReport read it.
func TestR247_ReportWindowNotResetBeforeBuild(t *testing.T) {
	r := NewDMARCReporter(nil, nil, DMARCReporterConfig{OrgName: "o", FromEmail: "d@example.com", Interval: 24 * time.Hour})
	r.SetPolicy("example.com", DMARCPolicyReject, "", DMARCAlignmentRelaxed, DMARCAlignmentRelaxed, 100)
	r.RecordResult("example.com", &DMARCEvaluation{Disposition: "none"}, "192.0.2.1", "pass", "pass")
	xmlData, err := r.buildDomainReport("example.com", "rua@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := string(xmlData)
	begin := fmt.Sprintf("<begin>%d</begin>", time.Now().Add(-24*time.Hour).Unix())
	if !strings.Contains(s, begin[:len("<begin>")+6]) { // same leading digits (coarse)
		t.Fatalf("begin should be ~24h ago, got %s", s)
	}
	if n, _ := r.GetReportStats("example.com"); n != 0 {
		t.Fatalf("entries not drained")
	}
}

// F7241: RFC 7489 §7.1 external destination verification.
func TestR247_ExternalReportDestinationVerification(t *testing.T) {
	res := newMockDNSResolver()
	ctx := context.Background()
	// Same organizational domain: no verification needed.
	if ok, err := VerifyReportDestination(ctx, res, "example.com", "rua@mail.example.com"); !ok || err != nil {
		t.Fatalf("same org should be authorized: %v %v", ok, err)
	}
	// External without authorization record.
	if ok, _ := VerifyReportDestination(ctx, res, "example.com", "rua@evil.net"); ok {
		t.Fatal("external destination without record must be refused")
	}
	res.txtRecords["example.com._report._dmarc.evil.net"] = []string{"v=DMARC1"}
	if ok, err := VerifyReportDestination(ctx, res, "example.com", "rua@evil.net"); !ok || err != nil {
		t.Fatalf("authorized external should pass: %v %v", ok, err)
	}
	res.txtRecords["example.com._report._dmarc.other.net"] = []string{"v=spf1 -all"}
	if ok, _ := VerifyReportDestination(ctx, res, "example.com", "rua@other.net"); ok {
		t.Fatal("non-DMARC1 record must not authorize")
	}
}

// F7242: mailto scheme is case-insensitive; !size limit suffix is allowed.
func TestR247_ParseRUAEmail(t *testing.T) {
	for _, in := range []string{"MAILTO:a@example.com", "mailto:a@example.com!10m", "mailto:a@example.com!10m?subject=x"} {
		got, err := ParseRUAEmail(in)
		if err != nil || got != "a@example.com" {
			t.Errorf("%q => %q %v", in, got, err)
		}
	}
}

// F7243: dmarc-version = "v" *WSP "=" *WSP "DMARC1".
func TestR247_VersionTagWhitespace(t *testing.T) {
	if !isDMARCRecord("v = DMARC1; p=reject") {
		t.Fatal("whitespace around = must be accepted")
	}
	if isDMARCRecord("v=DMARC10; p=reject") || isDMARCRecord("p=none; v=DMARC1") {
		t.Fatal("must still reject")
	}
	rec, err := parseDMARCRecord("v = DMARC1; p=reject")
	if err != nil || rec.Policy != DMARCPolicyReject {
		t.Fatalf("%v %v", rec, err)
	}
}

// F7244: reporter state caps (entries per domain, tracked domains).
func TestR247_ReporterCaps(t *testing.T) {
	r := NewDMARCReporter(nil, nil, DMARCReporterConfig{Interval: time.Hour})
	ev := &DMARCEvaluation{Disposition: "none"}
	for i := 0; i < maxReportEntriesPerDomain+500; i++ {
		r.RecordResult("example.com", ev, fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255), "fail", "fail")
	}
	if n, _ := r.GetReportStats("example.com"); n > maxReportEntriesPerDomain {
		t.Fatalf("entries %d exceed cap", n)
	}
	for i := 0; i < maxReportDomains+500; i++ {
		r.RecordResult(fmt.Sprintf("d%d.example", i), ev, "192.0.2.1", "fail", "fail")
	}
	r.reportsMu.Lock()
	n := len(r.reports)
	r.reportsMu.Unlock()
	if n > maxReportDomains {
		t.Fatalf("domains %d exceed cap", n)
	}
}
