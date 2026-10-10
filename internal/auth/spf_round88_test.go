package auth

// Round 88 regressions (F5700-F5704): RFC 7208 version case, exists, typed
// permerrors, macros and ip4/ip6 syntax.

import (
	"context"
	"errors"
	"net"
	"testing"
)

func r88SPF(t *testing.T, r *mockDNSResolver, clientIP, domain, sender string) SPFResult {
	t.Helper()
	res, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP(clientIP), domain, sender)
	return res
}

func r88One(rec string) *mockDNSResolver {
	r := newMockDNSResolver()
	r.txtRecords["example.com"] = []string{rec}
	return r
}

func TestRound88_F5700_VersionCaseInsensitive(t *testing.T) {
	for rec, want := range map[string]SPFResult{
		"V=SPF1 ip4:192.0.2.1 -all":  SPFPass,
		"v=SPF1 ip4:192.0.2.1 -all":  SPFPass,
		"v=spf1 ip4:192.0.2.1 -all":  SPFPass,
		"V=SPF1":                     SPFNeutral,
		"V=SPF10 ip4:192.0.2.1 -all": SPFNone,
	} {
		if got := r88SPF(t, r88One(rec), "192.0.2.1", "example.com", "u@example.com"); got != want {
			t.Errorf("%q: got %v want %v", rec, got, want)
		}
	}
}

func TestRound88_F5701_ExistsRequiresA(t *testing.T) {
	for name, tc := range map[string]struct {
		ips  []net.IP
		want SPFResult
	}{
		"A answer":     {[]net.IP{net.ParseIP("127.0.0.2")}, SPFPass},
		"empty answer": {[]net.IP{}, SPFFail},
		"AAAA only":    {[]net.IP{net.ParseIP("2001:db8::1")}, SPFFail},
		"mixed":        {[]net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("127.0.0.2")}, SPFPass},
	} {
		r := r88One("v=spf1 exists:probe.example.com -all")
		r.ipRecords["probe.example.com"] = tc.ips
		if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != tc.want {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
}

func TestRound88_F5702_TypedErrors(t *testing.T) {
	// Domain names containing "timeout"/"temporary" are not temporary errors.
	for _, tgt := range []string{"timeout-corp.example.net", "temporary.example.net"} {
		r := r88One("v=spf1 include:" + tgt + " -all")
		if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != SPFPermError {
			t.Errorf("include:%s -> %v want permerror", tgt, got)
		}
	}
	// A real timeout stays temporary.
	r := r88One("v=spf1 include:slow.example.net -all")
	r.tempFail["slow.example.net"] = true
	if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != SPFTempError {
		t.Errorf("tempfail include -> %v want temperror", got)
	}
	// Resolver errors are classified by type, not by the queried name.
	nf := &net.DNSError{Err: "no such host", Name: "timeout-corp.example", IsNotFound: true}
	if isTemporaryError(nf) {
		t.Error("not-found DNSError for a name containing 'timeout' classified temporary")
	}
	if !isTemporaryError(&net.DNSError{Err: "server misbehaving", Name: "x.example", IsTemporary: true}) {
		t.Error("temporary DNSError not temporary")
	}
	if isTemporaryError(&spfPermError{"timeout"}) {
		t.Error("spfPermError classified temporary")
	}
	if !isTemporaryError(errors.New("i/o timeout")) {
		t.Error("string fallback for plain timeout lost")
	}
	// a/timeout style cidr error is a permerror.
	r = r88One("v=spf1 a/timeout -all")
	if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != SPFPermError {
		t.Errorf("a/timeout -> %v want permerror", got)
	}
}

func TestRound88_F5703_Macros(t *testing.T) {
	exp := func(spec string, ip, domain, sender string) string {
		t.Helper()
		got, err := spfExpand(spec, net.ParseIP(ip), domain, sender)
		if err != nil {
			t.Fatalf("%q: %v", spec, err)
		}
		return got
	}
	// RFC 7208 §7.4 examples (strong-bad@email.example.com, 192.0.2.3).
	s, d := "strong-bad@email.example.com", "email.example.com"
	for spec, want := range map[string]string{
		"%{s}":                  "strong-bad@email.example.com",
		"%{o}":                  "email.example.com",
		"%{d}":                  "email.example.com",
		"%{d4}":                 "email.example.com",
		"%{d3}":                 "email.example.com",
		"%{d2}":                 "example.com",
		"%{d1}":                 "com",
		"%{dr}":                 "com.example.email",
		"%{d2r}":                "example.email",
		"%{l}":                  "strong-bad",
		"%{l-}":                 "strong.bad",
		"%{lr}":                 "strong-bad",
		"%{lr-}":                "bad.strong",
		"%{l1r-}":               "strong",
		"%{ir}.%{v}._spf.%{d2}": "3.2.0.192.in-addr._spf.example.com",
		"%{lr-}.lp._spf.%{d2}":  "bad.strong.lp._spf.example.com",
		"a%%b%_c%-d":            "a%b c%20d",
		"%{L}":                  "strong-bad",
		"%{S}":                  "strong-bad%40email.example.com",
		"no-macros":             "no-macros",
	} {
		if got := exp(spec, "192.0.2.3", d, s); got != want {
			t.Errorf("%q: got %q want %q", spec, got, want)
		}
	}
	if got := exp("%{ir}.%{v}._spf.%{d2}", "2001:db8::cb01", d, s); got != "1.0.b.c.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6._spf.example.com" {
		t.Errorf("ipv6 nibbles: %q", got)
	}
	// Empty sender: local-part postmaster, domain = current domain.
	if got := exp("%{s}", "192.0.2.3", "example.com", ""); got != "postmaster@example.com" {
		t.Errorf("empty sender: %q", got)
	}
	if got := exp("%{l}", "192.0.2.3", "example.com", "@example.com"); got != "postmaster" {
		t.Errorf("no local part: %q", got)
	}
	for _, bad := range []string{"%", "%x", "%{", "%{}", "%{z}", "%{d0}", "%{c}", "%{d1x}", "%{i"} {
		if _, err := spfExpand(bad, net.ParseIP("192.0.2.3"), d, s); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	// End to end: exists with %{i}/%{d}, a with macro + cidr, include and
	// redirect with %{d}.
	r := r88One("v=spf1 exists:%{i}._spf.%{d} -all")
	r.ipRecords["192.0.2.1._spf.example.com"] = []net.IP{net.ParseIP("127.0.0.2")}
	if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != SPFPass {
		t.Errorf("exists macro: %v", got)
	}
	if got := r88SPF(t, r, "192.0.2.9", "example.com", "u@example.com"); got != SPFFail {
		t.Errorf("exists macro non-listed: %v", got)
	}
	r = r88One("v=spf1 a:%{l}.hosts.%{d}/24 -all")
	r.ipRecords["bob.hosts.example.com"] = []net.IP{net.ParseIP("198.51.100.7")}
	if got := r88SPF(t, r, "198.51.100.99", "example.com", "bob@example.com"); got != SPFPass {
		t.Errorf("a macro+cidr: %v", got)
	}
	r = r88One("v=spf1 include:_spf.%{d} -all")
	r.txtRecords["_spf.example.com"] = []string{"v=spf1 ip4:192.0.2.1 -all"}
	if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != SPFPass {
		t.Errorf("include macro: %v", got)
	}
	r = r88One("v=spf1 redirect=_spf.%{d}")
	r.txtRecords["_spf.example.com"] = []string{"v=spf1 ip4:192.0.2.1 -all"}
	if got := r88SPF(t, r, "192.0.2.1", "example.com", "u@example.com"); got != SPFPass {
		t.Errorf("redirect macro: %v", got)
	}
	// Malformed macro -> permerror.
	if got := r88SPF(t, r88One("v=spf1 exists:%{q}.example.com -all"), "192.0.2.1", "example.com", "u@example.com"); got != SPFPermError {
		t.Errorf("bad macro: %v", got)
	}
	// Control: terms without macros unchanged.
	if got := r88SPF(t, r88One("v=spf1 ip4:192.0.2.1 -all"), "192.0.2.1", "example.com", "u@example.com"); got != SPFPass {
		t.Errorf("control: %v", got)
	}
}

func TestRound88_F5704_InvalidIPSpec(t *testing.T) {
	for rec, want := range map[string]SPFResult{
		"v=spf1 ip4:192.0.2.999 +all":                 SPFPermError,
		"v=spf1 ip4:192.0.2.0/33 +all":                SPFPermError,
		"v=spf1 ip4:2001:db8::1 +all":                 SPFPermError,
		"v=spf1 ip4: +all":                            SPFPermError,
		"v=spf1 ip6:192.0.2.1 +all":                   SPFPermError,
		"v=spf1 ip6:2001:db8::/129 +all":              SPFPermError,
		"v=spf1 ip4:192.0.2.0/24 -all":                SPFPass,
		"v=spf1 ip4:192.0.2.1 -all":                   SPFPass,
		"v=spf1 ip6:2001:db8::/32 ip4:192.0.2.1 -all": SPFPass,
	} {
		if got := r88SPF(t, r88One(rec), "192.0.2.1", "example.com", "u@example.com"); got != want {
			t.Errorf("%q: got %v want %v", rec, got, want)
		}
	}
}
