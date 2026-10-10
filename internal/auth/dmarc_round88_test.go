package auth

// Round 88 regression (F5707): "v=DMARC10" is not a DMARC record.

import (
	"context"
	"testing"
)

func TestRound88_F5707_VersionPrefix(t *testing.T) {
	for name, tc := range map[string]struct {
		txt  []string
		want string
	}{
		"plain":                {[]string{"v=DMARC1; p=reject"}, "reject"},
		"no space before semi": {[]string{"v=DMARC1;p=reject"}, "reject"},
		"space before semi":    {[]string{"v=DMARC1 ; p=reject"}, "reject"},
		"DMARC10 ignored":      {[]string{"v=DMARC10; p=none", "v=DMARC1; p=reject"}, "reject"},
		"DMARC1x ignored":      {[]string{"v=DMARC1x; p=none", "v=DMARC1; p=reject"}, "reject"},
		"DMARC10 alone no rec": {[]string{"v=DMARC10; p=reject"}, ""},
		"two real records":     {[]string{"v=DMARC1; p=none", "v=DMARC1; p=reject"}, ""},
	} {
		r := newMockDNSResolver()
		r.txtRecords["_dmarc.example.com"] = tc.txt
		ev, _ := NewDMARCEvaluator(r).Evaluate(context.Background(), "example.com", SPFFail, "example.com", DKIMNone, "")
		if ev.Disposition != tc.want {
			t.Errorf("%s: disposition %q want %q (%+v)", name, ev.Disposition, tc.want, ev)
		}
	}
}
