package auth

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

// Regression tests for RFC 7208 §4.6.4 / §5.2 / §6.1 (F4885-F4888).

func spfRFCCheck(t *testing.T, txt, ips map[string]string) SPFResult {
	t.Helper()
	r := newMockDNSResolver()
	for k, v := range txt {
		r.txtRecords[k] = []string{v}
	}
	for k, v := range ips {
		r.ipRecords[k] = []net.IP{net.ParseIP(v)}
	}
	res, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP("192.0.2.1"), "example.test", "u@example.test")
	return res
}

func spfRFCTerms(kind, prefix string, n int, txt, ips map[string]string) string {
	var terms []string
	for i := 1; i <= n; i++ {
		d := fmt.Sprintf("%s%d.example.test", prefix, i)
		switch {
		case kind == "include":
			txt[d] = "v=spf1 -all"
		case ips != nil:
			ips[d] = "198.51.100.1"
		}
		terms = append(terms, kind+":"+d)
	}
	return strings.Join(terms, " ")
}

func TestSPF_RFC7208Limits(t *testing.T) {
	tests := []struct {
		name  string
		build func(txt, ips map[string]string)
		want  SPFResult
	}{
		{"11 includes exceed limit", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 " + spfRFCTerms("include", "i", 11, txt, ips) + " ip4:192.0.2.1 -all"
		}, SPFPermError},
		{"nested lookups count", func(txt, ips map[string]string) {
			txt["inc.example.test"] = "v=spf1 " + spfRFCTerms("a", "n", 8, txt, ips) + " -all"
			txt["example.test"] = "v=spf1 include:inc.example.test " + spfRFCTerms("a", "t", 2, txt, ips) + " ip4:192.0.2.1 -all"
		}, SPFPermError},
		{"exactly 10 DNS terms allowed", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 " + spfRFCTerms("a", "h", 10, txt, ips) + " ip4:192.0.2.1 -all"
		}, SPFPass},
		{"redirect as 10th term allowed", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 " + spfRFCTerms("a", "h", 9, txt, ips) + " redirect=r.example.test"
			txt["r.example.test"] = "v=spf1 ip4:192.0.2.1 -all"
		}, SPFPass},
		{"two voids allowed", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 " + spfRFCTerms("a", "v", 2, txt, nil) + " ip4:192.0.2.1 -all"
		}, SPFPass},
		{"third void is permerror", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 " + spfRFCTerms("a", "v", 3, txt, nil) + " ip4:192.0.2.1 -all"
		}, SPFPermError},
		{"voids counted across include", func(txt, ips map[string]string) {
			txt["inc.example.test"] = "v=spf1 a:v1.example.test a:v2.example.test -all"
			txt["example.test"] = "v=spf1 include:inc.example.test a:v3.example.test ip4:192.0.2.1 -all"
		}, SPFPermError},
		{"include without SPF record is permerror", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 include:nospf.example.test ip4:192.0.2.1 -all"
		}, SPFPermError},
		{"redirect before exp modifier", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 redirect=_spf.example.test exp=explain.example.test"
			txt["_spf.example.test"] = "v=spf1 ip4:192.0.2.1 -all"
		}, SPFPass},
		{"redirect before unmatched mechanism", func(txt, ips map[string]string) {
			txt["example.test"] = "v=spf1 redirect=_spf.example.test ip4:198.51.100.7"
			txt["_spf.example.test"] = "v=spf1 -all"
		}, SPFFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txt, ips := map[string]string{}, map[string]string{}
			tt.build(txt, ips)
			if got := spfRFCCheck(t, txt, ips); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}
