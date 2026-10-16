package auth

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// r207CountTXT counts TXT lookups and answers none.
type r207CountTXT struct {
	n int64
}

func (r *r207CountTXT) LookupTXT(ctx context.Context, d string) ([]string, error) {
	atomic.AddInt64(&r.n, 1)
	return nil, &net.DNSError{IsNotFound: true}
}
func (r *r207CountTXT) LookupIP(context.Context, string) ([]net.IP, error)  { return nil, nil }
func (r *r207CountTXT) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, nil }

// F6840: a message carrying thousands of ARC sets (or a malformed chain) must
// fail without a DNS lookup + RSA verify per set (RFC 8617 §5.2: max 50
// instances; structural failure ends validation).
func TestARCValidate_OversizedOrMalformedChainNoDNS_F6840(t *testing.T) {
	build := func(instances []int) map[string][]string {
		h := map[string][]string{}
		for _, i := range instances {
			h["ARC-Authentication-Results"] = append(h["ARC-Authentication-Results"], fmt.Sprintf("i=%d; x", i))
			h["ARC-Message-Signature"] = append(h["ARC-Message-Signature"], fmt.Sprintf("i=%d; a=rsa-sha256; d=e.com; s=s; bh=AA; b=AA; h=from", i))
			h["ARC-Seal"] = append(h["ARC-Seal"], fmt.Sprintf("i=%d; a=rsa-sha256; cv=none; d=e.com; s=s; b=AA", i))
		}
		return h
	}
	many := make([]int, 300)
	for i := range many {
		many[i] = i + 1
	}
	for name, inst := range map[string][]int{"oversized": many, "gap": {1, 2, 4}} {
		r := &r207CountTXT{}
		c, err := NewARCValidator(r).Validate(context.Background(), build(inst), []byte("x\r\n"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.CV != "fail" || c.ChainValid {
			t.Errorf("%s: cv=%s valid=%v", name, c.CV, c.ChainValid)
		}
		if r.n != 0 {
			t.Errorf("%s: %d DNS lookups for an already-invalid chain", name, r.n)
		}
	}
}

// F6841: filling the SPF cache must not stall inserts with an O(n^2) sort
// under the write lock.
func TestSPFCacheEvictionIsFast_F6841(t *testing.T) {
	c := &spfCache{records: map[string]*cacheEntry{}, maxSize: 20000, nextCleanup: time.Now().Add(time.Hour)}
	for i := 0; i < 20000; i++ {
		c.set(fmt.Sprintf("d%d.example", i), "v=spf1 -all", time.Hour)
	}
	start := time.Now()
	c.set("trigger.example", "v=spf1 -all", time.Hour)
	// The bound catches an eviction regression holding the lock; it must stay
	// tight for normal builds but tolerate the race detector's 5-20x
	// instrumentation overhead, which otherwise makes it fail under load even
	// with a correct O(1) eviction (observed at 104ms, F7760).
	limit := 100 * time.Millisecond
	if raceEnabled {
		limit = time.Second
	}
	if d := time.Since(start); d > limit {
		t.Fatalf("eviction held the lock for %v", d)
	}
	if len(c.records) > 18001 {
		t.Fatalf("size after eviction = %d", len(c.records))
	}
}

// F6842: RFC 7208 §4.6.1/§5: include, exists, a:/mx: with an empty domain-spec
// and all with an argument are syntax errors (permerror), not silent
// non-matches or matches against the current domain.
func TestSPF_EmptyDomainSpecIsPermError_F6842(t *testing.T) {
	for _, rec := range []string{
		"v=spf1 exists -all", "v=spf1 exists: -all", "v=spf1 include: -all",
		"v=spf1 a: -all", "v=spf1 mx: -all", "v=spf1 all:x", "v=spf1 redirect=",
	} {
		r := newMockDNSResolver()
		r.txtRecords["example.com"] = []string{rec}
		r.ipRecords["example.com"] = []net.IP{net.ParseIP("192.0.2.1")}
		res, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP("192.0.2.1"), "example.com", "u@example.com")
		if res != SPFPermError {
			t.Errorf("%q => %v, want permerror", rec, res)
		}
	}
}

// F6843: unknown c= canonicalization / q= methods make the signature
// unverifiable (RFC 6376 §3.5, §6.1.1) instead of being treated as "simple".
func TestParseDKIMSignature_UnknownCanonAndQuery_F6843(t *testing.T) {
	base := "v=1; a=rsa-sha256; d=e.com; s=s; bh=AAAA; b=BBBB; h=from"
	for _, bad := range []string{"c=foo/bar", "c=relaxed/zzz", "c=Relaxed", "q=http/foo", "c=relaxed/simple/x"} {
		if _, err := parseDKIMSignature(base + "; " + bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"c=relaxed/relaxed", "c=simple", "c=relaxed", "q=dns/txt", "q=dns/txt:foo/bar"} {
		if _, err := parseDKIMSignature(base + "; " + ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
}

// F6844: TLSA records without the AD bit are not DNSSEC-validated and must
// not be trusted (RFC 7672 §2.2).
func TestDANELookupTLSA_RequiresAD_F6844(t *testing.T) {
	for _, ad := range []bool{false, true} {
		cert := daneHexCert(t)
		rec := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
		addr := r207DNS(t, ad, &dns.TLSA{Usage: 3, Selector: 1, MatchingType: 1, Certificate: hex.EncodeToString(rec.Certificate)})
		v := NewDANEValidatorWithDNS(nil, addr)
		res, err := v.Validate("mx.example.org", 25, daneHexState(cert))
		want := DANENone
		if ad {
			want = DANEValidated
		}
		if err != nil || res != want {
			t.Errorf("ad=%v: res=%v err=%v want %v", ad, res, err, want)
		}
	}
}

func r207DNS(t *testing.T, ad bool, rrs ...*dns.TLSA) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	srv := &dns.Server{PacketConn: pc, NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			m := new(dns.Msg)
			m.SetReply(r)
			m.AuthenticatedData = ad
			for _, rr := range rrs {
				c := *rr
				c.Hdr = dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeTLSA, Class: dns.ClassINET, Ttl: 60}
				m.Answer = append(m.Answer, &c)
			}
			_ = w.WriteMsg(m)
		})}
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

type r207MultiTXT struct{ recs []string }

func (r r207MultiTXT) LookupTXT(context.Context, string) ([]string, error) { return r.recs, nil }
func (r r207MultiTXT) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}
func (r r207MultiTXT) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, nil }

// F6845: RFC 8461 §3.1 — more than one _mta-sts TXT record means no policy.
func TestMTASTSRecord_MultipleRecordsIsNoPolicy_F6845(t *testing.T) {
	v := NewMTASTSValidator(r207MultiTXT{[]string{"v=STSv1; id=a", "v=STSv1; id=b"}})
	rec, err := v.lookupMTASTSRecord(context.Background(), "example.com")
	if err != nil || rec != nil {
		t.Fatalf("rec=%v err=%v, want nil", rec, err)
	}
	v = NewMTASTSValidator(r207MultiTXT{[]string{"v=STSv1; id=a", "unrelated txt"}})
	if rec, _ := v.lookupMTASTSRecord(context.Background(), "example.com"); rec == nil || rec.ID != "a" {
		t.Fatalf("single record lost: %v", rec)
	}
}

// F6846: the default policy-fetch client must refuse to dial private
// addresses itself (the pre-check resolves separately: DNS rebinding).
func TestMTASTSDefaultClientBlocksPrivateDial_F6846(t *testing.T) {
	v := NewMTASTSValidator(r207MultiTXT{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := newReq(ctx, "https://127.0.0.1:1/x")
	resp, err := v.httpClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("err=%v, want blocked-address error", err)
	}
}

func newReq(ctx context.Context, u string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "GET", u, nil)
}
