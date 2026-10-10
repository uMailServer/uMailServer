package auth

import (
	"context"
	"testing"
)

func dmarcRFC7489Eval(t *testing.T, r *mockDNSResolver, e *DMARCEvaluator, from string) *DMARCEvaluation {
	t.Helper()
	if e == nil {
		e = NewDMARCEvaluator(r)
	}
	ev, err := e.Evaluate(context.Background(), from, SPFFail, "evil.test", DKIMFail, "evil.test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return ev
}

// F5410: RFC 7489 §6.3 — an invalid sp= value is discarded in favour of its
// default (p=). It used to become the applied policy with disposition none.
func TestDMARC_InvalidSubdomainPolicyFallsBackToP_F5410(t *testing.T) {
	cases := []struct {
		org, from, want string
	}{
		{"v=DMARC1; p=reject; sp=bogus", "sub.example.com", "reject"},
		{"v=DMARC1; p=quarantine; sp=bogus", "sub.example.com", "quarantine"},
		{"v=DMARC1; p=reject; sp=", "sub.example.com", "reject"},
		{"v=DMARC1; p=none; sp=REJECT", "sub.example.com", "reject"}, // valid, case-insensitive
		{"v=DMARC1; p=reject; sp=none", "sub.example.com", "none"},   // valid sp honoured
		{"v=DMARC1; p=reject; sp=bogus", "example.com", "reject"},    // own record: sp unused
	}
	for _, c := range cases {
		r := newMockDNSResolver()
		r.txtRecords["_dmarc.example.com"] = []string{c.org}
		ev := dmarcRFC7489Eval(t, r, nil, c.from)
		if ev.Result != DMARCFail || ev.Disposition != c.want || string(ev.AppliedPolicy) != c.want {
			t.Errorf("%q from %s: result=%s applied=%s disposition=%s, want %s", c.org, c.from, ev.Result, ev.AppliedPolicy, ev.Disposition, c.want)
		}
	}
	rec, err := parseDMARCRecord("v=DMARC1; p=reject; sp=bogus")
	if err != nil || rec.SubdomainPolicy != "" {
		t.Errorf("parse sp=bogus: sp=%q err=%v, want empty", rec.SubdomainPolicy, err)
	}
}

// F5411: RFC 7489 §6.6.3 step 4 — more than one v=DMARC1 record terminates
// policy discovery (DMARC not applied, no organizational fallback).
func TestDMARC_MultipleRecordsNoPolicy_F5411(t *testing.T) {
	two := []string{"v=DMARC1; p=reject", "v=DMARC1; p=none"}

	r := newMockDNSResolver()
	r.txtRecords["_dmarc.example.com"] = two
	e := NewDMARCEvaluator(r)
	for i := 0; i < 2; i++ { // second call is served from the cache
		if ev := dmarcRFC7489Eval(t, r, e, "example.com"); ev.Result != DMARCNone || ev.Disposition != "" {
			t.Errorf("call %d: result=%s disposition=%q, want none", i, ev.Result, ev.Disposition)
		}
	}
	if ev := dmarcRFC7489Eval(t, r, e, "sub.example.com"); ev.Result != DMARCNone {
		t.Errorf("subdomain with multiple org records: result=%s, want none", ev.Result)
	}

	r = newMockDNSResolver()
	r.txtRecords["_dmarc.sub.example.com"] = two
	r.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject"}
	if ev := dmarcRFC7489Eval(t, r, nil, "sub.example.com"); ev.Result != DMARCNone {
		t.Errorf("multiple at From domain fell back to org: result=%s disposition=%s, want none", ev.Result, ev.Disposition)
	}

	r = newMockDNSResolver()
	r.txtRecords["_dmarc.example.com"] = []string{"spf-ish=x", "v=DMARC1; p=reject"}
	if ev := dmarcRFC7489Eval(t, r, nil, "example.com"); ev.Result != DMARCFail || ev.Disposition != "reject" {
		t.Errorf("single DMARC among other TXT: result=%s disposition=%s, want fail/reject", ev.Result, ev.Disposition)
	}
}
