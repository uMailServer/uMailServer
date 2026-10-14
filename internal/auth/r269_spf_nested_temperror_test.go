package auth

import (
	"context"
	"net"
	"testing"
)

// F7430: a temporary DNS failure inside an included record is a temperror
// (RFC 7208 §5.2), not a permerror.
func TestR269_SPFNestedTempErrorStaysTemp(t *testing.T) {
	cases := map[string]struct {
		inc  string
		temp string
	}{
		"include->a":  {"v=spf1 a:slow.example.net -all", "slow.example.net"},
		"include->mx": {"v=spf1 mx:slow.example.net -all", "slow.example.net"},
	}
	for name, tc := range cases {
		r := newMockDNSResolver()
		r.txtRecords["example.com"] = []string{"v=spf1 include:inc.example.net -all"}
		r.txtRecords["inc.example.net"] = []string{tc.inc}
		r.tempFail[tc.temp] = true
		got, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP("192.0.2.1"), "example.com", "u@example.com")
		if got != SPFTempError {
			t.Errorf("%s: got %v want temperror", name, got)
		}
	}

	// A permanent error in the included record stays a permerror.
	r := newMockDNSResolver()
	r.txtRecords["example.com"] = []string{"v=spf1 include:inc.example.net -all"}
	r.txtRecords["inc.example.net"] = []string{"v=spf1 bogus -all"}
	if got, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP("192.0.2.1"), "example.com", "u@example.com"); got != SPFPermError {
		t.Errorf("nested syntax error: got %v want permerror", got)
	}
}
