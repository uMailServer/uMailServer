package auth

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
)

func spfSyntaxCheck(t *testing.T, r DNSResolver, client string) SPFResult {
	t.Helper()
	res, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP(client), "example.com", "u@example.com")
	return res
}

// F5412: RFC 7208 §4.6.1 — mechanism and modifier names are case-insensitive.
func TestSPF_TermNamesCaseInsensitive_F5412(t *testing.T) {
	cases := []struct {
		rec, ip string
		want    SPFResult
	}{
		{"v=spf1 ip4:192.0.2.1 -ALL", "203.0.113.9", SPFFail},
		{"v=spf1 ip4:192.0.2.1 ?All", "203.0.113.9", SPFNeutral},
		{"v=spf1 Include:inc.example.com -all", "198.51.100.7", SPFPass},
		{"v=spf1 IP4:192.0.2.1 -all", "192.0.2.1", SPFPass},
		{"v=spf1 A:host.example.com -all", "192.0.2.50", SPFPass},
		{"v=spf1 MX/24 -all", "198.51.100.99", SPFPass},
		{"v=spf1 Exp=x.example.com REDIRECT=inc.example.com", "198.51.100.7", SPFPass},
		{"v=spf1 ip4:192.0.2.1 -all", "203.0.113.9", SPFFail}, // lowercase unchanged
	}
	for _, c := range cases {
		r := newMockDNSResolver()
		r.txtRecords["example.com"] = []string{c.rec}
		r.txtRecords["inc.example.com"] = []string{"v=spf1 ip4:198.51.100.7 -all"}
		r.ipRecords["host.example.com"] = []net.IP{net.ParseIP("192.0.2.50")}
		r.mxRecords["example.com"] = []*net.MX{{Host: "mail.example.com", Pref: 10}}
		r.ipRecords["mail.example.com"] = []net.IP{net.ParseIP("198.51.100.20")}
		if got := spfSyntaxCheck(t, r, c.ip); got != c.want {
			t.Errorf("%q client %s: got %v want %v", c.rec, c.ip, got, c.want)
		}
	}
}

// F5413: RFC 7208 §4.5 — the version is exactly "v=spf1" (SP or end), and
// more than one SPF record is a permerror (also for include/redirect targets).
func TestSPF_RecordSelection_F5413(t *testing.T) {
	cases := []struct {
		name string
		txt  map[string][]string
		want SPFResult
	}{
		{"two records", map[string][]string{"example.com": {"v=spf1 ip4:192.0.2.1 -all", "v=spf1 -all"}}, SPFPermError},
		{"v=spf10", map[string][]string{"example.com": {"v=spf10 ip4:192.0.2.1 -all"}}, SPFNone},
		{"v=spf1x", map[string][]string{"example.com": {"v=spf1x +all"}}, SPFNone},
		{"bare v=spf1", map[string][]string{"example.com": {"v=spf1"}}, SPFNeutral},
		{"one SPF among other TXT", map[string][]string{"example.com": {"site-verification=1", "v=spf1 ip4:192.0.2.1 -all", "v=spf10 -all"}}, SPFPass},
		{"include target has two", map[string][]string{
			"example.com":     {"v=spf1 include:inc.example.com -all"},
			"inc.example.com": {"v=spf1 +all", "v=spf1 -all"}}, SPFPermError},
		{"redirect target has two", map[string][]string{
			"example.com":   {"v=spf1 redirect=r.example.com"},
			"r.example.com": {"v=spf1 +all", "v=spf1 -all"}}, SPFPermError},
	}
	for _, c := range cases {
		r := newMockDNSResolver()
		for k, v := range c.txt {
			r.txtRecords[k] = v
		}
		if got := spfSyntaxCheck(t, r, "192.0.2.1"); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// F5414: RFC 7208 §4.6/§5/§6 — an unknown mechanism anywhere in the record
// is a permerror before evaluation; unknown modifiers are ignored.
func TestSPF_UnknownMechanismPermError_F5414(t *testing.T) {
	cases := []struct {
		rec  string
		want SPFResult
	}{
		{"v=spf1 ip6only:2001:db8::/32 ip4:192.0.2.1 -all", SPFPermError},
		{"v=spf1 ip4:192.0.2.1 bogus -all", SPFPermError}, // after the matching term
		{"v=spf1 +all/24", SPFPermError},
		{"v=spf1 foo=bar ip4:192.0.2.1 -all", SPFPass}, // unknown modifier ignored
		{"v=spf1 exp=x.example.com ip4:192.0.2.1 -all", SPFPass},
		{"v=spf1 ptr ip4:192.0.2.1 -all", SPFPass}, // known mechanism
	}
	for _, c := range cases {
		r := newMockDNSResolver()
		r.txtRecords["example.com"] = []string{c.rec}
		if got := spfSyntaxCheck(t, r, "192.0.2.1"); got != c.want {
			t.Errorf("%q: got %v want %v", c.rec, got, c.want)
		}
	}
}

type spfCountingResolver struct {
	*mockDNSResolver
	ipLookups int64
}

func (r *spfCountingResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	atomic.AddInt64(&r.ipLookups, 1)
	return r.mockDNSResolver.LookupIP(ctx, host)
}

// F5415: RFC 7208 §4.6.4 — one mx term MUST NOT resolve more than 10 hosts;
// exceeding the limit is a permerror.
func TestSPF_MXAddressLookupLimit_F5415(t *testing.T) {
	cases := []struct {
		nMX, matchAt int // matchAt < 0: client matches no MX host
		want         SPFResult
		maxLookups   int64
	}{
		{25, -1, SPFPermError, 10},
		{11, 10, SPFPermError, 10}, // match only at the 11th host
		{11, 0, SPFPass, 1},        // match before the limit
		{10, 9, SPFPass, 10},       // boundary
		{10, -1, SPFFail, 10},
	}
	for _, c := range cases {
		r := &spfCountingResolver{mockDNSResolver: newMockDNSResolver()}
		r.txtRecords["example.com"] = []string{"v=spf1 mx -all"}
		for i := 0; i < c.nMX; i++ {
			h := fmt.Sprintf("mx%d.example.com", i)
			r.mxRecords["example.com"] = append(r.mxRecords["example.com"], &net.MX{Host: h, Pref: uint16(i)})
			ip := fmt.Sprintf("198.51.100.%d", i+1)
			if i == c.matchAt {
				ip = "203.0.113.9"
			}
			r.ipRecords[h] = []net.IP{net.ParseIP(ip)}
		}
		got := spfSyntaxCheck(t, r, "203.0.113.9")
		n := atomic.LoadInt64(&r.ipLookups)
		if got != c.want || n > c.maxLookups {
			t.Errorf("nMX=%d matchAt=%d: got %v after %d lookups, want %v with <=%d", c.nMX, c.matchAt, got, n, c.want, c.maxLookups)
		}
	}
}
