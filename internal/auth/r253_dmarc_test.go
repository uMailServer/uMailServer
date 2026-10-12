package auth

import (
	"context"
	"fmt"
	"testing"
)

type nxResolver struct {
	*mockDNSResolver
	missing map[string]bool
}

func (n *nxResolver) DomainExists(ctx context.Context, d string) (bool, error) {
	return !n.missing[d], nil
}

// F7300: RFC 9091 np= applies to a non-existent subdomain, when the resolver
// can report NXDOMAIN; existing subdomains keep sp=.
func TestDMARC_NonExistentSubdomainPolicy_F7300(t *testing.T) {
	m := newMockDNSResolver()
	m.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=none; sp=quarantine; np=reject"}
	r := &nxResolver{m, map[string]bool{"ghost.example.com": true}}
	ev := NewDMARCEvaluator(r)
	got, _ := ev.Evaluate(context.Background(), "ghost.example.com", SPFFail, "x.test", DKIMNone, "")
	if got.AppliedPolicy != DMARCPolicyReject {
		t.Fatalf("nonexistent: %s", got.AppliedPolicy)
	}
	got, _ = ev.Evaluate(context.Background(), "real.example.com", SPFFail, "x.test", DKIMNone, "")
	if got.AppliedPolicy != DMARCPolicyQuarantine {
		t.Fatalf("existing: %s", got.AppliedPolicy)
	}
	// Resolver without NXDOMAIN knowledge: np is not guessed.
	ev2 := NewDMARCEvaluator(m)
	got, _ = ev2.Evaluate(context.Background(), "ghost.example.com", SPFFail, "x.test", DKIMNone, "")
	if got.AppliedPolicy != DMARCPolicyQuarantine {
		t.Fatalf("plain resolver: %s", got.AppliedPolicy)
	}
	// np without sp: non-existent uses np, existing falls back to p.
	m.txtRecords["_dmarc.example.org"] = []string{"v=DMARC1; p=none; np=reject"}
	r.missing["ghost.example.org"] = true
	got, _ = ev.Evaluate(context.Background(), "ghost.example.org", SPFFail, "x.test", DKIMNone, "")
	if got.AppliedPolicy != DMARCPolicyReject {
		t.Fatalf("np without sp: %s", got.AppliedPolicy)
	}
}

// F7301: any aligned passing DKIM signature passes DMARC, even when an earlier
// passing signature is third-party.
func TestDMARC_AnyAlignedSignature_F7301(t *testing.T) {
	m := newMockDNSResolver()
	m.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject"}
	ev := NewDMARCEvaluator(m)
	sigs := []DKIMSignatureResult{
		{DKIMPass, "esp.net"}, {DKIMFail, "example.com"}, {DKIMPass, "mail.example.com"},
	}
	got, _ := ev.EvaluateSignatures(context.Background(), "example.com", "<a@b>", SPFFail, "x.test", sigs)
	if got.Result != DMARCPass {
		t.Fatalf("got %v", got.Result)
	}
	// Failed aligned + passing unaligned must still fail.
	sigs = []DKIMSignatureResult{{DKIMPass, "esp.net"}, {DKIMFail, "example.com"}}
	got, _ = ev.EvaluateSignatures(context.Background(), "example.com", "<a@b>", SPFFail, "x.test", sigs)
	if got.Result != DMARCFail || got.AppliedPolicy != DMARCPolicyReject {
		t.Fatalf("got %v/%s", got.Result, got.AppliedPolicy)
	}
}

// F7302: pct sampling is deterministic per message and roughly uniform
// across messages.
func TestDMARC_PctDeterministic_F7302(t *testing.T) {
	m := newMockDNSResolver()
	m.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject; pct=50"}
	ev := NewDMARCEvaluator(m)
	hits := 0
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("<msg%d@x>", i)
		first, _ := ev.EvaluateSignatures(context.Background(), "example.com", key, SPFFail, "x.test", nil)
		for j := 0; j < 3; j++ {
			again, _ := ev.EvaluateSignatures(context.Background(), "example.com", key, SPFFail, "x.test", nil)
			if again.AppliedPolicy != first.AppliedPolicy {
				t.Fatalf("%s not deterministic", key)
			}
		}
		if first.AppliedPolicy == DMARCPolicyReject {
			hits++
		}
	}
	if hits < 850 || hits > 1150 {
		t.Fatalf("hit rate %d/2000", hits)
	}
}
