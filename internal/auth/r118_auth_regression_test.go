package auth

// Round 118 regressions F6000-F6009.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// F6000: unspecified / multicast / CGNAT addresses are blocked for policy fetches.
func TestR118_F6000_BlockedPolicyIPs(t *testing.T) {
	for _, s := range []string{"0.0.0.0", "::", "100.64.0.1", "224.0.0.1", "127.0.0.1", "10.0.0.1", "0.1.2.3"} {
		if !isBlockedPolicyIP(net.ParseIP(s)) {
			t.Errorf("%s must be blocked", s)
		}
	}
	if isBlockedPolicyIP(net.ParseIP("93.184.216.34")) {
		t.Error("public IP must not be blocked")
	}
}

func TestR118_F6000_FetchPolicyRejectsUnspecified(t *testing.T) {
	r := newMockDNSResolver()
	r.ipRecords["mta-sts.example.com"] = []net.IP{net.ParseIP("0.0.0.0")}
	v := NewMTASTSValidator(r)
	_, err := v.fetchPolicyFile(context.Background(), "example.com")
	if err == nil || !strings.Contains(err.Error(), "SSRF blocked") {
		t.Fatalf("want SSRF blocked, got %v", err)
	}
}

// F6001: trailing dot on the MX name or pattern.
func TestR118_F6001_MatchMXTrailingDot(t *testing.T) {
	if !matchMX("mail.example.com", "mail.example.com.") {
		t.Error("exact match must ignore trailing dot")
	}
	if !matchMX("*.example.com", "mx1.example.com.") {
		t.Error("wildcard match must ignore trailing dot")
	}
	if matchMX("*.example.com", "a.b.example.com.") {
		t.Error("wildcard must still match one label only")
	}
}

func r118DNS(t *testing.T, udp, tcp dns.HandlerFunc) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	started := make(chan struct{})
	us := &dns.Server{PacketConn: pc, Handler: udp, NotifyStartedFunc: func() { close(started) }}
	go func() { _ = us.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = us.Shutdown() })
	if tcp != nil {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		st := make(chan struct{})
		ts := &dns.Server{Listener: l, Handler: tcp, NotifyStartedFunc: func() { close(st) }}
		go func() { _ = ts.ActivateAndServe() }()
		<-st
		t.Cleanup(func() { _ = ts.Shutdown() })
	}
	return addr
}

// F6002: SERVFAIL must be an error, not "no TLSA records".
func TestR118_F6002_ServfailIsError(t *testing.T) {
	h := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(m)
	}
	v := NewDANEValidatorWithDNS(nil, r118DNS(t, h, nil))
	recs, err := v.LookupTLSA("mx.example.org", 25)
	if err == nil {
		t.Fatalf("SERVFAIL returned no error (records=%d)", len(recs))
	}
	// NXDOMAIN stays a definitive "no records".
	h2 := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeNameError)
		_ = w.WriteMsg(m)
	}
	v = NewDANEValidatorWithDNS(nil, r118DNS(t, h2, nil))
	if recs, err := v.LookupTLSA("mx.example.org", 25); err != nil || len(recs) != 0 {
		t.Fatalf("NXDOMAIN: recs=%d err=%v", len(recs), err)
	}
}

// F6004: truncated UDP reply is retried over TCP.
func TestR118_F6004_TruncatedRetriesTCP(t *testing.T) {
	udp := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Truncated = true
		_ = w.WriteMsg(m)
	}
	tcp := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = append(m.Answer, &dns.TLSA{
			Hdr:   dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeTLSA, Class: dns.ClassINET, Ttl: 60},
			Usage: 3, Selector: 1, MatchingType: 1, Certificate: hex.EncodeToString(make([]byte, 32)),
		})
		_ = w.WriteMsg(m)
	}
	v := NewDANEValidatorWithDNS(nil, r118DNS(t, udp, tcp))
	recs, err := v.LookupTLSA("mx.example.org", 25)
	if err != nil || len(recs) != 1 {
		t.Fatalf("recs=%d err=%v", len(recs), err)
	}
}

// F6003: an empty dnsServer must not be passed to Exchange verbatim.
func TestR118_F6003_SystemResolverAddress(t *testing.T) {
	v := NewDANEValidatorWithDNS(nil, "")
	_, err := v.LookupTLSA("mx.example.org", 25)
	if err != nil && strings.Contains(err.Error(), "missing address") {
		t.Fatalf("empty resolver address used: %v", err)
	}
}

type r118TLSAResolver struct{ recs []*TLSARecord }

func (r *r118TLSAResolver) LookupTXT(context.Context, string) ([]string, error) { return nil, nil }
func (r *r118TLSAResolver) LookupIP(context.Context, string) ([]net.IP, error)  { return nil, nil }
func (r *r118TLSAResolver) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, nil }
func (r *r118TLSAResolver) LookupTLSA(string) ([]*TLSARecord, error)            { return r.recs, nil }

func r118Chain(t *testing.T) (leaf, ca *x509.Certificate) {
	t.Helper()
	cak, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	catmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	cader, err := x509.CreateCertificate(rand.Reader, catmpl, catmpl, &cak.PublicKey, cak)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ = x509.ParseCertificate(cader)
	lk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ltmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "mx.example.org"},
		DNSNames:  []string{"mx.example.org"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	lder, err := x509.CreateCertificate(rand.Reader, ltmpl, ca, &lk.PublicKey, cak)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ = x509.ParseCertificate(lder)
	return
}

// F6006: DANE-TA names a CA in the presented chain.
func TestR118_F6006_DANETAChainAnchor(t *testing.T) {
	leaf, ca := r118Chain(t)
	rec := GenerateTLSARecord(ca, TLSAUsageDANETA, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	v := NewDANEValidator(&r118TLSAResolver{recs: []*TLSARecord{rec}})
	st := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf, ca}}
	if res, err := v.Validate("mx.example.org", 25, st); res != DANEValidated {
		t.Fatalf("DANE-TA with CA in chain: %v %v", res, err)
	}
	// Wrong name for the TLSA base domain must fail.
	if res, _ := v.Validate("other.example.net", 25, st); res != DANEFailed {
		t.Fatalf("name mismatch must fail, got %v", res)
	}
	// An unrelated anchor must fail.
	_, other := r118Chain(t)
	rec2 := GenerateTLSARecord(other, TLSAUsageDANETA, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	v = NewDANEValidator(&r118TLSAResolver{recs: []*TLSARecord{rec2}})
	if res, _ := v.Validate("mx.example.org", 25, st); res != DANEFailed {
		t.Fatalf("unrelated anchor must fail, got %v", res)
	}
}

// F6005: only unusable records -> DANEUnusable; bad selector/mtype too.
func TestR118_F6005_Unusable(t *testing.T) {
	leaf, _ := r118Chain(t)
	st := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	for _, rec := range []*TLSARecord{
		{Usage: 1, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)},
		{Usage: 3, Selector: 9, MatchingType: 1, Certificate: make([]byte, 32)},
		{Usage: 3, Selector: 1, MatchingType: 9, Certificate: make([]byte, 32)},
	} {
		v := NewDANEValidator(&r118TLSAResolver{recs: []*TLSARecord{rec}})
		if res, _ := v.Validate("mx.example.org", 25, st); res != DANEUnusable {
			t.Errorf("%v: got %v want unusable", rec, res)
		}
	}
	// One usable mismatching record next to an unusable one is still a failure.
	v := NewDANEValidator(&r118TLSAResolver{recs: []*TLSARecord{
		{Usage: 1, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)},
		{Usage: 3, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)},
	}})
	if res, _ := v.Validate("mx.example.org", 25, st); res != DANEFailed {
		t.Errorf("got %v want failed", res)
	}
}

// F6007/F6008: signer sets t= to now and covers MIME headers + oversigns From.
func TestR118_F6007_SignerTimestampAndHeaders(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := NewDKIMSigner(nil, key, "example.com", "sel")
	hdrs := map[string][]string{
		"From": {"a@example.com"}, "To": {"b@example.org"}, "Subject": {"hi"},
		"Date": {"Mon, 01 Jan 2024 00:00:00 +0000"}, "Message-Id": {"<1@example.com>"},
		"Content-Type": {"text/plain"}, "Mime-Version": {"1.0"},
	}
	before := time.Now().Unix()
	out, err := s.Sign(hdrs, []byte("body\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := parseDKIMSignature(out)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Timestamp < before || sig.Timestamp > time.Now().Unix() {
		t.Errorf("t=%d, want now", sig.Timestamp)
	}
	h := strings.Join(sig.SignedHeaders, ":")
	if !strings.Contains(h, "content-type") || !strings.Contains(h, "mime-version") {
		t.Errorf("h=%s lacks MIME headers", h)
	}
	if strings.Count(h, "from") != 2 {
		t.Errorf("h=%s: From not oversigned", h)
	}
	// Round trip with our verifier.
	r := newMockDNSResolver()
	r.txtRecords["sel._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=" + GetPublicKeyForDNS(key)}
	res, _, verr := NewDKIMVerifier(r).Verify(hdrs, []byte("body\r\n"), "DKIM-Signature: "+out)
	if res != DKIMPass {
		t.Fatalf("round trip %v %v", res, verr)
	}
	// Prepending a second From after signing must break the signature.
	hdrs["From"] = []string{"a@example.com", "evil@example.net"}
	res, _, _ = NewDKIMVerifier(r).Verify(hdrs, []byte("body\r\n"), "DKIM-Signature: "+out)
	if res == DKIMPass {
		t.Fatal("added From header not detected")
	}
}

// F6009: LDAP lockout is case-insensitive and the tracking map is bounded.
func TestR118_F6009_LDAPRateLimitCase(t *testing.T) {
	c := &LDAPClient{}
	for i := 0; i < 5; i++ {
		c.recordLoginFailure("Alice")
	}
	if c.checkLoginRateLimit("alice") {
		t.Fatal("lockout bypassed by changing username case")
	}
	if c.checkLoginRateLimit("ALICE ") {
		t.Fatal("lockout bypassed by case/space")
	}
	c.recordLoginSuccess("ALICE")
	// Bounded growth: stale entries are swept once the cap is reached.
	c2 := &LDAPClient{loginAttempts: map[string]*ldapLoginAttempt{}}
	old := time.Now().Add(-time.Hour)
	for i := 0; i < ldapMaxTrackedLogins; i++ {
		c2.loginAttempts[strings.Repeat("x", 1)+string(rune('a'+i%26))+hex.EncodeToString([]byte{byte(i), byte(i >> 8)})] = &ldapLoginAttempt{count: 1, lastSeen: old}
	}
	c2.recordLoginFailure("newuser")
	if len(c2.loginAttempts) > 2 {
		t.Fatalf("stale entries not swept: %d", len(c2.loginAttempts))
	}
}
