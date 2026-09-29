package auth

import (
	"testing"
	"time"
)

// TestDMARCReporter_PerMessageStateIsBounded pins the memory contract of the
// DMARC aggregate reporter.
//
// RecordResult is production-wired: internal/smtp/auth_pipeline.go:223 calls it
// for every inbound message whose DMARC result is not "none", whenever DMARC
// reporting is enabled (server_smtp.go wires the reporter under
// DMARC.Enabled && DMARC.ReportEmail != "").
//
// It appends one element to Dispositions, SPFResults and DKIMResults per
// message. GenerateAndSendReport is the only function that releases that state
// and it has no production caller (test-only), so before the fix the retained
// state grew by three string headers for every message the server ever
// accepted, with no eviction and no cap.
//
// Entry.Count already carries the message volume, so per-message retention
// costs unbounded memory and adds no reporting fidelity.
func TestDMARCReporter_PerMessageStateIsBounded(t *testing.T) {
	r := NewDMARCReporter(nil, nil, DMARCReporterConfig{
		OrgName:   "org",
		FromEmail: "dmarc@example.com",
		Interval:  24 * time.Hour,
	})

	const messages = 5000
	eval := &DMARCEvaluation{
		Result:      DMARCFail,
		Policy:      DMARCPolicyReject,
		Disposition: "reject",
	}
	// One domain, one source IP: the single-source-IP case, which needs no
	// attacker to spread across many IPs to demonstrate the growth.
	for i := 0; i < messages; i++ {
		r.RecordResult("example.com", eval, "198.51.100.7", "fail", "fail")
	}

	r.reportsMu.Lock()
	data := r.reports["example.com"]
	r.reportsMu.Unlock()

	if data == nil {
		t.Fatalf("no report data recorded for example.com")
	}
	if len(data.Entries) != 1 {
		t.Fatalf("precondition: want 1 entry for a single source IP, got %d", len(data.Entries))
	}
	entry := data.Entries[0]
	if entry.Count != messages {
		t.Fatalf("Entry.Count = %d, want %d; the cap must not disturb counting", entry.Count, messages)
	}

	if got := len(entry.Dispositions); got > maxRetainedPerSourceIP {
		t.Errorf("retained %d Dispositions after %d messages (cap %d); DMARCReporter.RecordResult "+
			"appends per message with no bound and no eviction, and GenerateAndSendReport (the only "+
			"drain) has no production caller", got, messages, maxRetainedPerSourceIP)
	}
	if got := len(entry.SPFResults); got > maxRetainedPerSourceIP {
		t.Errorf("retained %d SPFResults after %d messages (cap %d); unbounded per-message retention",
			got, messages, maxRetainedPerSourceIP)
	}
	if got := len(entry.DKIMResults); got > maxRetainedPerSourceIP {
		t.Errorf("retained %d DKIMResults after %d messages (cap %d); unbounded per-message retention",
			got, messages, maxRetainedPerSourceIP)
	}
}

// TestDMARCReporter_Control_CountAndDrainStillWork is the control: the cap must
// not disturb the two behaviours that matter. Entry.Count still tracks the true
// message volume, and the existing drain still clears per-source-IP detail.
// Both pass before AND after the fix.
func TestDMARCReporter_Control_CountAndDrainStillWork(t *testing.T) {
	r := NewDMARCReporter(nil, nil, DMARCReporterConfig{
		OrgName:   "org",
		FromEmail: "dmarc@example.com",
		Interval:  24 * time.Hour,
	})

	eval := &DMARCEvaluation{Result: DMARCFail, Disposition: "reject"}
	const messages = 25
	for i := 0; i < messages; i++ {
		r.RecordResult("example.com", eval, "203.0.113.9", "fail", "fail")
	}

	r.reportsMu.Lock()
	data := r.reports["example.com"]
	var count int
	if data != nil && len(data.Entries) == 1 {
		count = data.Entries[0].Count
	}
	r.reportsMu.Unlock()

	if count != messages {
		t.Errorf("Entry.Count = %d, want %d; the cap must not disturb counting", count, messages)
	}

	// The drain path must still release per-source-IP detail. It fails to send
	// (no SMTP listener in the test), but it clears entries before sending, so
	// this asserts the clearing behaviour only.
	_ = r.GenerateAndSendReport("example.com", "rua@example.com")

	r.reportsMu.Lock()
	remaining := 0
	if data != nil {
		remaining = len(data.Entries)
	}
	r.reportsMu.Unlock()

	if remaining != 0 {
		t.Errorf("drain left %d entries, want 0; the cap must not change draining", remaining)
	}
}

// TestAppendBounded_KeepsNewestValue pins the retention window semantics:
// BuildReport reads the most recent value of each slice, so the newest element
// must remain last after truncation.
func TestAppendBounded_KeepsNewestValue(t *testing.T) {
	var s []string
	for i := 0; i < 25; i++ {
		s = appendBounded(s, string(rune('a'+i)), maxRetainedPerSourceIP)
	}

	if len(s) != maxRetainedPerSourceIP {
		t.Fatalf("len = %d, want %d", len(s), maxRetainedPerSourceIP)
	}
	// Values a..y appended 25 times; the last 10 are p..y, newest ('y') last.
	if want := "y"; s[len(s)-1] != want {
		t.Errorf("newest value = %q, want %q; BuildReport reads the most recent value", s[len(s)-1], want)
	}
	if want := "p"; s[0] != want {
		t.Errorf("oldest retained value = %q, want %q", s[0], want)
	}
}
