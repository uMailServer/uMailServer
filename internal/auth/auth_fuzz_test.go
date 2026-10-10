package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// fuzzResolver answers every TXT query with the same fuzzed records, which
// makes self-referential include/redirect/exists chains reachable.
type fuzzResolver struct {
	txt            []string
	txtN, ipN, mxN int
}

func (r *fuzzResolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	r.txtN++
	if len(r.txt) == 0 {
		return nil, errors.New("no records")
	}
	return r.txt, nil
}
func (r *fuzzResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	r.ipN++
	return []net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")}, nil
}
func (r *fuzzResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	r.mxN++
	return []*net.MX{{Host: "mx." + domain, Pref: 10}}, nil
}

func fuzzDeadline(t *testing.T, start time.Time, what string) {
	t.Helper()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("%s took %v (hang)", what, d)
	}
}

func FuzzSPFCheck(f *testing.F) {
	for _, s := range []string{
		"v=spf1 -all",
		"v=spf1 ip4:192.0.2.0/24 ~all",
		"v=spf1 include:%{d} -all",
		"v=spf1 redirect=%{d}",
		"v=spf1 exists:%{i}.%{d} -all",
		"v=spf1 a:%{l1r+-}.%{o}/24//64 mx -all",
		"v=spf1 exp=%{d} -all",
		"v=spf1 ip6:2001:db8::/32 ptr:%{h} ?all",
		"v=spf1 a/99 mx//200 ip4:1.2.3.4/33 -all",
		"v=spf1 %{%{%{",
		"v=spf1 a:%{d1000000r}",
	} {
		f.Add(s, "user@example.com", "192.0.2.1")
	}
	f.Fuzz(func(t *testing.T, record, sender, ipStr string) {
		if len(record) > 4096 {
			return
		}
		ip := net.ParseIP(ipStr)
		if ip == nil {
			ip = net.ParseIP("192.0.2.1")
		}
		domain := "example.com"
		if i := strings.LastIndexByte(sender, '@'); i >= 0 && i+1 < len(sender) {
			domain = sender[i+1:]
		}
		fr := &fuzzResolver{txt: []string{record}}
		c := NewSPFChecker(fr)
		start := time.Now()
		res, _ := c.CheckSPF(context.Background(), ip, domain, sender)
		fuzzDeadline(t, start, "CheckSPF")
		// RFC 7208 4.6.4: at most 10 DNS-querying terms (+1 policy fetch, +1 exp).
		if fr.txtN+fr.mxN > 25 || fr.ipN > 120 {
			t.Fatalf("lookup limit exceeded: txt=%d mx=%d ip=%d", fr.txtN, fr.mxN, fr.ipN)
		}
		if res.String() == "" {
			t.Fatalf("empty result string")
		}
		start = time.Now()
		spfExpand(record, ip, domain, sender)
		fuzzDeadline(t, start, "spfExpand")
		parseSPF(record)
	})
}

func FuzzDMARCParse(f *testing.F) {
	for _, s := range []string{
		"v=DMARC1; p=reject",
		"v=DMARC1; p=none; sp=quarantine; pct=50; adkim=s; aspf=r; rua=mailto:a@b.c,mailto:d@e.f; ruf=mailto:x@y.z; fo=0:1:d:s; ri=86400",
		"v=DMARC1; p=reject; pct=-5",
		"v=DMARC1; pct=99999999999999999999; p=none",
		"v=DMARC1;;;;p=",
		"p=reject",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		rec, err := parseDMARCRecord(s)
		if err == nil && rec == nil {
			t.Fatal("nil record without error")
		}
		if err == nil && (rec.Percentage < 0 || rec.Percentage > 100) {
			t.Fatalf("pct out of range: %d for %q", rec.Percentage, s)
		}
		isDMARCRecord(s)
		parseURIList(s)
		parseFailureOptions(s)
		checkAlignment(s, "example.com", DMARCAlignmentRelaxed)
		checkAlignment("example.com", s, DMARCAlignmentStrict)
	})
}

func FuzzMTASTSParse(f *testing.F) {
	for _, s := range []string{
		"version: STSv1\nmode: enforce\nmx: mail.example.com\nmx: *.example.net\nmax_age: 86400\n",
		"version: STSv1\r\nmode: testing\r\nmax_age: -1\r\n",
		"version: STSv1\nmode: enforce\nmax_age: 99999999999999999999\nmx: *\n",
		"::::\n\n\nmx:",
	} {
		f.Add(s, "mail.example.com")
	}
	f.Add("v=STSv1; id=abc123", "a.b")
	f.Fuzz(func(t *testing.T, s, mx string) {
		p, err := parseMTASTSPolicy(s)
		if err == nil && p == nil {
			t.Fatal("nil policy without error")
		}
		if err == nil {
			for _, pat := range p.MX {
				matchMX(pat, mx)
			}
			if p.MaxAge < 0 {
				t.Fatalf("negative max_age %d", p.MaxAge)
			}
		}
		parseMTASTSRecord(s)
		matchMX(s, mx)
		computePolicyID(s)
		GenerateTLSRPT(s, []MTASTSFailureDetails{{ReceivingMXHelo: mx, ResultType: s}})
	})
}

func FuzzDANETLSA(f *testing.F) {
	for _, s := range []string{
		"3 1 1 abcdef0123456789",
		"3 0 0 ",
		"2 1 2 zz",
		"3 1 1 0A0b",
		"255 255 255 00",
		"3 1",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		rec, err := parseTLSARecord(s)
		if err == nil && rec == nil {
			t.Fatal("nil record without error")
		}
		if rec != nil {
			_ = rec.String()
			tlsaUsable(rec)
			v := NewDANEValidator(&fuzzResolver{})
			v.validateRecord(rec, &x509.Certificate{Raw: []byte("raw"), RawSubjectPublicKeyInfo: []byte("spki")}, &tls.ConnectionState{})
		}
		parseTLSAHex(s)
	})
}

func FuzzDKIMVerify(f *testing.F) {
	f.Add("v=1; a=rsa-sha256; d=example.com; s=sel; c=relaxed/relaxed; h=from:to; bh=AAAA; b=BBBB", "From: a@example.com\r\nTo: b@c.d\r\n", "hello\r\n")
	f.Add("v=1; a=ed25519-sha256; d=x; s=y; l=99999999999; t=-1; x=abc; h=; bh=; b=", "From: a\r\n", "")
	f.Add("v=1;;;==; d=; s=; h=from:from:from; l=-5", "", "\r\n\r\n\r\n")
	f.Fuzz(func(t *testing.T, sigHdr, hdrs, body string) {
		headers := map[string][]string{}
		for _, line := range strings.Split(hdrs, "\n") {
			if i := strings.IndexByte(line, ':'); i > 0 {
				k := strings.TrimSpace(line[:i])
				headers[k] = append(headers[k], strings.TrimSpace(line[i+1:]))
			}
		}
		start := time.Now()
		parseDKIMSignature(sigHdr)
		parseTagValueList(sigHdr)
		parseHeaderList(sigHdr)
		parseCopiedHeaders(sigHdr)
		canonicalizeBody([]byte(body), "relaxed")
		canonicalizeBody([]byte(body), "simple")
		canonicalizeHeaders(headers, parseHeaderList(sigHdr), "relaxed")
		canonicalizeHeaders(headers, parseHeaderList(sigHdr), "simple")
		parseDKIMPublicKey(sigHdr)
		v := NewDKIMVerifier(&fuzzResolver{txt: []string{"v=DKIM1; k=rsa; p=" + sigHdr}})
		v.Verify(headers, []byte(body), sigHdr)
		fuzzDeadline(t, start, "DKIM")
	})
}

func FuzzARCValidate(f *testing.F) {
	f.Add("i=1; a=rsa-sha256; d=x.com; s=s; h=from; bh=AA; b=BB", "i=1; cv=none; a=rsa-sha256; d=x.com; s=s; b=CC", "i=1; x.com; spf=pass", "body\r\n")
	f.Add("i=99999999999999999999; d=", "i=0; cv=fail", "i=-1", "")
	f.Add("i=50", "i=51; cv=pass", "i=2", "x")
	f.Fuzz(func(t *testing.T, ams, as, aar, body string) {
		headers := map[string][]string{
			"ARC-Message-Signature":      {ams, ams},
			"ARC-Seal":                   {as, as},
			"ARC-Authentication-Results": {aar, aar},
			"From":                       {"a@example.com"},
		}
		start := time.Now()
		v := NewARCValidator(&fuzzResolver{txt: []string{"v=DKIM1; k=rsa; p=" + ams}})
		v.Validate(context.Background(), headers, []byte(body))
		extractInstance(ams)
		extractSealInfo(as)
		arcStripB(ams)
		determineNextInstance(headers)
		determineCV(headers)
		extractARCHeaders(headers)
		fuzzDeadline(t, start, "ARC")
	})
}
