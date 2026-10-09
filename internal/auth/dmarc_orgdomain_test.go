package auth

import (
	"context"
	"testing"
)

// Regression tests for RFC 7489 organizational-domain policy discovery and
// sp= selection (F4893, F4894).

func TestDMARC_OrgDomainPolicy(t *testing.T) {
	tests := []struct {
		name string
		txt  map[string]string
		from string
		want string
	}{
		{"org record applies to subdomain", map[string]string{"_dmarc.example.test": "v=DMARC1; p=reject"}, "mail.example.test", "reject"},
		{"sp applies via org fallback", map[string]string{"_dmarc.example.test": "v=DMARC1; p=reject; sp=quarantine"}, "a.b.example.test", "quarantine"},
		{"own record wins over org", map[string]string{"_dmarc.example.test": "v=DMARC1; p=reject", "_dmarc.mail.example.test": "v=DMARC1; p=none"}, "mail.example.test", "none"},
		{"sp ignored for publishing domain", map[string]string{"_dmarc.mail.example.test": "v=DMARC1; p=reject; sp=none"}, "mail.example.test", "reject"},
		{"multi-label suffix org domain uses p", map[string]string{"_dmarc.example.co.uk": "v=DMARC1; p=reject; sp=none"}, "example.co.uk", "reject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newMockDNSResolver()
			for k, v := range tt.txt {
				r.txtRecords[k] = []string{v}
			}
			e := NewDMARCEvaluator(r)
			for i := 0; i < 2; i++ { // second call exercises the cache
				ev, err := e.Evaluate(context.Background(), tt.from, SPFFail, "attacker.invalid", DKIMNone, "")
				if err != nil || ev.Result != DMARCFail || ev.Disposition != tt.want {
					t.Fatalf("call %d: %v %s/%s, want fail/%s", i, err, ev.Result, ev.Disposition, tt.want)
				}
			}
		})
	}
}

func TestDMARC_OrgDomainTempError(t *testing.T) {
	r := newMockDNSResolver()
	r.tempFail["_dmarc.example.test"] = true
	ev, _ := NewDMARCEvaluator(r).Evaluate(context.Background(), "mail.example.test", SPFFail, "x.invalid", DKIMNone, "")
	if ev.Result != DMARCTempError {
		t.Errorf("got %s, want temperror", ev.Result)
	}
}
