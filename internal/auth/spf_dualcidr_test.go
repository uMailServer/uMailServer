package auth

// F5312: RFC 7208 §5.3/§5.4 dual-cidr-length on a and mx.

import (
	"context"
	"net"
	"testing"
)

func spfDualCIDRCheck(record, client string) SPFResult {
	r := newMockDNSResolver()
	r.txtRecords["example.com"] = []string{record}
	r.ipRecords["example.com"] = []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")}
	r.ipRecords["mail.example.com"] = []net.IP{net.ParseIP("198.51.100.20")}
	r.mxRecords["example.com"] = []*net.MX{{Host: "mail.example.com", Pref: 10}}
	res, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP(client), "example.com", "u@example.com")
	return res
}

func TestSPF_DualCIDRLength_F5312(t *testing.T) {
	cases := []struct {
		rec, ip string
		want    SPFResult
	}{
		{"v=spf1 a/24 -all", "192.0.2.77", SPFPass},
		{"v=spf1 a/24 -all", "192.0.3.77", SPFFail},             // boundary: outside /24
		{"v=spf1 a:example.com/32 -all", "192.0.2.10", SPFPass}, // /32 exact
		{"v=spf1 a:example.com/32 -all", "192.0.2.11", SPFFail},
		{"v=spf1 mx/24 -all", "198.51.100.99", SPFPass},
		{"v=spf1 mx:example.com/24 -all", "198.51.100.99", SPFPass},
		{"v=spf1 a//64 -all", "2001:db8::ffff", SPFPass},
		{"v=spf1 a/24//64 -all", "2001:db8:0:1::1", SPFFail},
		{"v=spf1 a/0 -all", "203.0.113.1", SPFPass}, // /0 matches any v4
		{"v=spf1 a/33 -all", "192.0.2.10", SPFPermError},
		{"v=spf1 a//129 -all", "192.0.2.10", SPFPermError},
		{"v=spf1 a -all", "192.0.2.10", SPFPass}, // no cidr unchanged
		{"v=spf1 a -all", "192.0.2.11", SPFFail},
		{"v=spf1 -all/24 +all", "192.0.2.10", SPFPass}, // only a/mx take a cidr
	}
	for _, c := range cases {
		if got := spfDualCIDRCheck(c.rec, c.ip); got != c.want {
			t.Fatalf("%q %s: got %v want %v", c.rec, c.ip, got, c.want)
		}
	}
}
