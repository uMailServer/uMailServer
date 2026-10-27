package auth

// DMARC aggregate reports were collected but never sent: no production code
// called the sender, the published policy was never filled in, rua= was
// never read, and the message went to localhost:25 as bare XML. These tests
// drive the reporter from real evaluations through to the RFC 7489 §7.2
// report message.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

type sentReport struct {
	from string
	to   []string
	msg  []byte
}

func newTestReporter(res DNSResolver, sent *[]sentReport) *DMARCReporter {
	var mu sync.Mutex // Flush sends domains in parallel
	r := NewDMARCReporter(res, nil, DMARCReporterConfig{
		OrgName:   "Test Org",
		FromEmail: "dmarc@mx.receiver.test",
		Receiver:  "mx.receiver.test",
		Send: func(from string, to []string, msg []byte) error {
			mu.Lock()
			defer mu.Unlock()
			*sent = append(*sent, sentReport{from, to, msg})
			return nil
		},
	})
	clock := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	return r
}

func evaluate(t *testing.T, e *DMARCEvaluator, from string, spf SPFResult, spfDomain string, dkim DKIMResult, dkimDomain string) *DMARCEvaluation {
	t.Helper()
	ev, err := e.Evaluate(context.Background(), from, spf, spfDomain, dkim, dkimDomain)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// decodeReport parses a report message and returns its subject, attachment
// name and decoded feedback document.
func decodeReport(t *testing.T, raw []byte) (subject, filename string, fb DMARCReport) {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("report message does not parse: %v", err)
	}
	subject = msg.Header.Get("Subject")
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if part.Header.Get("Content-Type") != "application/gzip" {
			continue
		}
		filename = part.FileName()
		zr, err := gzip.NewReader(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatalf("attachment is not gzip: %v", err)
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		if err := xml.Unmarshal(data, &fb); err != nil {
			t.Fatalf("attachment is not report XML: %v\n%s", err, data)
		}
		return subject, filename, fb
	}
	t.Fatal("report message has no gzip attachment")
	return
}

func TestDMARCAggregateReportSent(t *testing.T) {
	res := newMockDNSResolver()
	res.txtRecords["_dmarc.sender.test"] = []string{"v=DMARC1; p=reject; adkim=s; rua=mailto:agg@sender.test"}
	e := NewDMARCEvaluator(res)
	var sent []sentReport
	r := newTestReporter(res, &sent)

	pass := evaluate(t, e, "sender.test", SPFPass, "sender.test", DKIMPass, "sender.test")
	fail := evaluate(t, e, "sender.test", SPFFail, "spoof.test", DKIMNone, "")
	if fail.Disposition != "reject" || pass.Disposition != "none" {
		t.Fatalf("INVALID: dispositions pass=%q fail=%q", pass.Disposition, fail.Disposition)
	}
	good := ReportSample{SourceIP: "192.0.2.1", HeaderFrom: "sender.test", EnvelopeFrom: "sender.test",
		SPFResult: "pass", DKIMDomain: "sender.test", DKIMResult: "pass"}
	r.Record(pass, good)
	r.Record(pass, good)
	r.Record(fail, ReportSample{SourceIP: "198.51.100.7", HeaderFrom: "sender.test", EnvelopeFrom: "spoof.test", SPFResult: "fail"})

	if n := r.Flush(context.Background()); n != 1 || len(sent) != 1 {
		t.Fatalf("Flush sent %d (%d messages), want 1", n, len(sent))
	}
	if got := sent[0].to; len(got) != 1 || got[0] != "agg@sender.test" || sent[0].from != "dmarc@mx.receiver.test" {
		t.Fatalf("envelope from=%q to=%v, want dmarc@mx.receiver.test -> agg@sender.test", sent[0].from, got)
	}
	subject, filename, fb := decodeReport(t, sent[0].msg)
	if !strings.HasPrefix(subject, "Report Domain: sender.test Submitter: mx.receiver.test Report-ID: <") {
		t.Errorf("subject %q", subject)
	}
	if !strings.HasPrefix(filename, "mx.receiver.test!sender.test!") || !strings.HasSuffix(filename, ".xml.gz") {
		t.Errorf("attachment name %q", filename)
	}
	pp := fb.PolicyPUB
	if pp.Domain != "sender.test" || pp.Policy != "reject" || pp.Adkim != "s" || pp.Aspf != "r" || pp.Pct != 100 {
		t.Errorf("policy_published %+v", pp)
	}
	if fb.ReportMeta.OrgName != "Test Org" || fb.ReportMeta.Email != "dmarc@mx.receiver.test" {
		t.Errorf("report_metadata %+v", fb.ReportMeta)
	}
	if dr := fb.ReportMeta.DateRange; dr.End <= dr.Begin {
		t.Errorf("date_range %+v is empty", dr)
	}
	rows := map[string]Record{}
	for _, rec := range fb.Records {
		rows[rec.Row.SourceIP] = rec
	}
	if g := rows["192.0.2.1"]; g.Row.Count != 2 || g.Row.Disposition != "none" || g.Row.DKIM != "pass" || g.Row.SPF != "pass" ||
		len(g.AuthResult.DKIM) != 1 || g.AuthResult.DKIM[0].Domain != "sender.test" {
		t.Errorf("passing row %+v", g)
	}
	if b := rows["198.51.100.7"]; b.Row.Count != 1 || b.Row.Disposition != "reject" || b.Row.DKIM != "fail" || b.Row.SPF != "fail" ||
		b.Identifiers.EnvelopeFrom != "spoof.test" || len(b.AuthResult.DKIM) != 0 {
		t.Errorf("failing row %+v", b)
	}

	// The data was drained: nothing more to send.
	if n := r.Flush(context.Background()); n != 0 || len(sent) != 1 {
		t.Fatalf("second Flush sent %d, want 0", n)
	}
}

// A subdomain without its own record is reported under the organizational
// domain whose record governed it.
func TestDMARCAggregateReportOrgDomain(t *testing.T) {
	res := newMockDNSResolver()
	res.txtRecords["_dmarc.sender.test"] = []string{"v=DMARC1; p=quarantine; rua=mailto:agg@sender.test"}
	e := NewDMARCEvaluator(res)
	var sent []sentReport
	r := newTestReporter(res, &sent)
	ev := evaluate(t, e, "news.sender.test", SPFFail, "x.test", DKIMNone, "")
	if ev.PolicyDomain != "sender.test" {
		t.Fatalf("PolicyDomain %q, want sender.test", ev.PolicyDomain)
	}
	r.Record(ev, ReportSample{SourceIP: "192.0.2.9", HeaderFrom: "news.sender.test", SPFResult: "fail"})
	r.Flush(context.Background())
	if len(sent) != 1 {
		t.Fatalf("sent %d reports, want 1", len(sent))
	}
	_, _, fb := decodeReport(t, sent[0].msg)
	if fb.PolicyPUB.Domain != "sender.test" || len(fb.Records) != 1 || fb.Records[0].Identifiers.HeaderFrom != "news.sender.test" {
		t.Fatalf("policy domain %q, records %+v", fb.PolicyPUB.Domain, fb.Records)
	}
}

func TestDMARCAggregateReportDestinations(t *testing.T) {
	res := newMockDNSResolver()
	res.txtRecords["_dmarc.sender.test"] = []string{
		"v=DMARC1; p=none; rua=mailto:agg@sender.test,mailto:a@vendor.test,mailto:b@other.test,https://x.test/r"}
	res.txtRecords["sender.test._report._dmarc.vendor.test"] = []string{"v=DMARC1"}
	res.txtRecords["_dmarc.norua.test"] = []string{"v=DMARC1; p=reject"}
	e := NewDMARCEvaluator(res)
	var sent []sentReport
	r := newTestReporter(res, &sent)

	sample := ReportSample{SourceIP: "192.0.2.1", SPFResult: "pass"}
	r.Record(evaluate(t, e, "sender.test", SPFPass, "sender.test", DKIMNone, ""), sample)
	r.Record(evaluate(t, e, "norua.test", SPFPass, "norua.test", DKIMNone, ""), sample)
	r.Record(evaluate(t, e, "none.test", SPFPass, "none.test", DKIMNone, ""), sample)
	if rows, _ := r.GetReportStats("norua.test"); rows != 0 {
		t.Error("kept data for a domain without rua=")
	}
	r.Flush(context.Background())

	var to []string
	for _, s := range sent {
		to = append(to, s.to...)
	}
	// other.test has not authorized reports for sender.test; https: is not
	// a supported scheme.
	if got := fmt.Sprint(to); got != "[agg@sender.test a@vendor.test]" {
		t.Fatalf("reports sent to %s, want [agg@sender.test a@vendor.test]", got)
	}
}

func TestDMARCAggregateReportSendFailureDropped(t *testing.T) {
	res := newMockDNSResolver()
	res.txtRecords["_dmarc.sender.test"] = []string{"v=DMARC1; p=none; rua=mailto:agg@sender.test"}
	e := NewDMARCEvaluator(res)
	calls := 0
	r := NewDMARCReporter(res, nil, DMARCReporterConfig{FromEmail: "d@r.test", Send: func(string, []string, []byte) error {
		calls++
		return fmt.Errorf("queue full")
	}})
	r.Record(evaluate(t, e, "sender.test", SPFPass, "sender.test", DKIMNone, ""), ReportSample{SourceIP: "192.0.2.1"})
	if n := r.Flush(context.Background()); n != 0 || calls != 1 {
		t.Fatalf("Flush = %d after %d send attempts, want 0 after 1", n, calls)
	}
	if n := r.Flush(context.Background()); n != 0 || calls != 1 {
		t.Fatalf("failed report retried: %d attempts", calls)
	}
}

// F7244: reporter state caps (rows per domain, tracked domains).
func TestDMARCAggregateReporterCaps(t *testing.T) {
	r := NewDMARCReporter(nil, nil, DMARCReporterConfig{})
	rec := &DMARCRecord{Policy: DMARCPolicyNone, ReportAggregateURI: []string{"mailto:a@example.com"}}
	ev := &DMARCEvaluation{Record: rec, PolicyDomain: "example.com", Disposition: "none"}
	for i := 0; i < maxReportEntriesPerDomain+500; i++ {
		r.Record(ev, ReportSample{SourceIP: fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255)})
	}
	if n, _ := r.GetReportStats("example.com"); n != maxReportEntriesPerDomain {
		t.Fatalf("rows %d, want the cap %d", n, maxReportEntriesPerDomain)
	}
	// A known row still counts once the cap is reached.
	r.Record(ev, ReportSample{SourceIP: "10.0.0.0"})
	if _, msgs := r.GetReportStats("example.com"); msgs != maxReportEntriesPerDomain+1 {
		t.Fatalf("messages %d, want %d", msgs, maxReportEntriesPerDomain+1)
	}
	for i := 0; i < maxReportDomains+500; i++ {
		r.Record(&DMARCEvaluation{Record: rec, PolicyDomain: fmt.Sprintf("d%d.example", i)}, ReportSample{SourceIP: "192.0.2.1"})
	}
	r.reportsMu.Lock()
	n := len(r.reports)
	r.reportsMu.Unlock()
	if n != maxReportDomains {
		t.Fatalf("domains %d, want the cap %d", n, maxReportDomains)
	}
}

// Rows hold attacker-chosen strings (DKIM d=, MAIL FROM) for a whole
// interval, so the total row count and field lengths are bounded too.
func TestDMARCAggregateReporterTotalAndFieldCaps(t *testing.T) {
	r := NewDMARCReporter(nil, nil, DMARCReporterConfig{})
	rec := &DMARCRecord{Policy: DMARCPolicyNone, ReportAggregateURI: []string{"mailto:a@example.com"}}
	r.Record(&DMARCEvaluation{Record: rec, PolicyDomain: "long.example"},
		ReportSample{SourceIP: "192.0.2.1", DKIMDomain: strings.Repeat("a", maxReportFieldLen+1)})
	if n, _ := r.GetReportStats("long.example"); n != 0 {
		t.Fatal("kept a row with an over-long field")
	}
	// A domain that already exists keeps growing after the others fill the cap.
	early := &DMARCEvaluation{Record: rec, PolicyDomain: "early.example"}
	r.Record(early, ReportSample{SourceIP: "192.0.2.1"})
	for d := 0; d*maxReportEntriesPerDomain < maxReportRowsTotal+2*maxReportEntriesPerDomain; d++ {
		ev := &DMARCEvaluation{Record: rec, PolicyDomain: fmt.Sprintf("d%d.example", d)}
		for i := 0; i < maxReportEntriesPerDomain; i++ {
			r.Record(ev, ReportSample{SourceIP: fmt.Sprintf("10.0.%d.%d", i>>8, i&255)})
		}
	}
	for i := 0; i < 10; i++ {
		r.Record(early, ReportSample{SourceIP: fmt.Sprintf("198.51.100.%d", i)})
	}
	r.reportsMu.Lock()
	total, empty := 0, 0
	for _, data := range r.reports {
		total += len(data.Rows)
		if len(data.Rows) == 0 {
			empty++
		}
	}
	r.reportsMu.Unlock()
	if total != maxReportRowsTotal || empty != 0 {
		t.Fatalf("rows %d (want the cap %d), %d domains without rows", total, maxReportRowsTotal, empty)
	}
	r.drain()
	r.Record(&DMARCEvaluation{Record: rec, PolicyDomain: "after.example"}, ReportSample{SourceIP: "192.0.2.1"})
	if n, _ := r.GetReportStats("after.example"); n != 1 {
		t.Fatal("the total cap was not reset by a flush")
	}
}

// The report schema requires sp and fo; they default to p and "0".
func TestDMARCReportPublishesSpAndFo(t *testing.T) {
	xmlData, err := BuildReport(ReportInput{Domain: "example.com", Record: DMARCRecord{Policy: DMARCPolicyReject, Percentage: 100},
		Begin: time.Unix(1, 0), End: time.Unix(2, 0), Rows: map[ReportRow]int{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<sp>reject</sp>", "<fo>0</fo>"} {
		if !strings.Contains(string(xmlData), want) {
			t.Errorf("report lacks %s:\n%s", want, xmlData)
		}
	}
}

// Flush sent domains one at a time; with DNS verification of up to 10 s per
// destination a full reporter could take longer than its interval.
func TestDMARCAggregateFlushSendsDomainsInParallel(t *testing.T) {
	res := newMockDNSResolver()
	e := NewDMARCEvaluator(res)
	var (
		mu     sync.Mutex
		active int
		peak   int
	)
	both := make(chan struct{})
	r := NewDMARCReporter(res, nil, DMARCReporterConfig{FromEmail: "d@r.test", Send: func(string, []string, []byte) error {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		if active == 2 {
			close(both)
		}
		mu.Unlock()
		// Hold the send until a second one is running (or give up).
		select {
		case <-both:
		case <-time.After(2 * time.Second):
		}
		mu.Lock()
		active--
		mu.Unlock()
		return nil
	}})
	for _, d := range []string{"a.test", "b.test"} {
		res.txtRecords["_dmarc."+d] = []string{"v=DMARC1; p=none; rua=mailto:agg@" + d}
		r.Record(evaluate(t, e, d, SPFPass, d, DKIMNone, ""), ReportSample{SourceIP: "192.0.2.1"})
	}
	if n := r.Flush(context.Background()); n != 2 {
		t.Fatalf("Flush sent %d, want 2", n)
	}
	if peak < 2 {
		t.Fatalf("at most %d report sent at a time, want parallel sends", peak)
	}
}
