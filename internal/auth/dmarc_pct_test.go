package auth

import (
	"context"
	"testing"
)

// Regression test for F5078: RFC 7489 §6.6.4 — a failing message not sampled
// by pct= gets the next-lower policy (reject → quarantine, quarantine → none).
func TestDMARCEvaluate_PctNextLowerPolicy_F5078(t *testing.T) {
	eval := func(rec, from string) *DMARCEvaluation {
		r := newMockDNSResolver()
		r.txtRecords["_dmarc.example.com"] = []string{rec}
		ev, err := NewDMARCEvaluator(r).Evaluate(context.Background(), from, SPFFail, "evil.test", DKIMNone, "")
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	cases := []struct {
		rec, from string
		want      DMARCPolicy
		disp      string
	}{
		{"v=DMARC1; p=reject; pct=0", "example.com", DMARCPolicyQuarantine, "quarantine"},              // reproduction
		{"v=DMARC1; p=quarantine; pct=0", "example.com", DMARCPolicyNone, "none"},                      // quarantine → none
		{"v=DMARC1; p=reject; pct=100", "example.com", DMARCPolicyReject, "reject"},                    // boundary: sampled
		{"v=DMARC1; p=none; sp=reject; pct=0", "sub.example.com", DMARCPolicyQuarantine, "quarantine"}, // sp path
		{"v=DMARC1; p=none; pct=0", "example.com", DMARCPolicyNone, "none"},
	}
	for _, c := range cases {
		ev := eval(c.rec, c.from)
		if ev.Result != DMARCFail || ev.AppliedPolicy != c.want || ev.Disposition != c.disp {
			t.Fatalf("%s (%s): got %s/%s want %s/%s", c.rec, c.from, ev.AppliedPolicy, ev.Disposition, c.want, c.disp)
		}
	}
	// A passing message is never affected by pct.
	r := newMockDNSResolver()
	r.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject; pct=0"}
	ev, _ := NewDMARCEvaluator(r).Evaluate(context.Background(), "example.com", SPFPass, "example.com", DKIMNone, "")
	if ev.Result != DMARCPass || ev.Disposition != "none" {
		t.Fatalf("pass: %v %s", ev.Result, ev.Disposition)
	}
}
