package auth

// Regression tests for F5272 (TLSA association data from DNS).
// An in-process miekg/dns server on a loopback UDP socket is the fake behind
// the NewDANEValidatorWithDNS seam; no external DNS.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func daneHexCert(t *testing.T) *x509.Certificate {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mx.example.org"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(4102444800, 0)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	c, _ := x509.ParseCertificate(der)
	return c
}

// daneHexDNS answers every query with the given TLSA RRs.
func daneHexDNS(t *testing.T, rrs ...*dns.TLSA) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	started := make(chan struct{})
	srv := &dns.Server{PacketConn: pc, NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			m := new(dns.Msg)
			m.SetReply(r)
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

func daneHexState(cert *x509.Certificate) *tls.ConnectionState {
	return &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
}

// TestDANELookupTLSA_DecodesHexAssociationData: before F5272 the DNS path
// stored the ASCII hex text as the association data, so no record fetched
// from DNS could ever match.
func TestDANELookupTLSA_DecodesHexAssociationData(t *testing.T) {
	cert := daneHexCert(t)
	want := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	v := NewDANEValidatorWithDNS(nil, daneHexDNS(t, &dns.TLSA{Usage: 3, Selector: 1, MatchingType: 1, Certificate: hex.EncodeToString(want.Certificate)}))
	recs, err := v.LookupTLSA("mx.example.org", 25)
	if err != nil || len(recs) != 1 {
		t.Fatalf("lookup err=%v n=%d", err, len(recs))
	}
	if !bytes.Equal(recs[0].Certificate, want.Certificate) {
		t.Fatalf("association data = %x, want %x", recs[0].Certificate, want.Certificate)
	}
	if res, err := v.Validate("mx.example.org", 25, daneHexState(cert)); res != DANEValidated {
		t.Fatalf("Validate = %v (err %v), want validated", res, err)
	}
}

// TestDANELookupTLSA_FullCertAndMismatch: 3 0 0 (full DER) matches; a
// digest of another certificate does not.
func TestDANELookupTLSA_FullCertAndMismatch(t *testing.T) {
	cert, other := daneHexCert(t), daneHexCert(t)
	v := NewDANEValidatorWithDNS(nil, daneHexDNS(t, &dns.TLSA{Usage: 3, Selector: 0, MatchingType: 0, Certificate: hex.EncodeToString(cert.Raw)}))
	if res, err := v.Validate("mx.example.org", 25, daneHexState(cert)); res != DANEValidated {
		t.Fatalf("full-cert Validate = %v (err %v), want validated", res, err)
	}
	wrong := GenerateTLSARecord(other, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA256)
	v = NewDANEValidatorWithDNS(nil, daneHexDNS(t, &dns.TLSA{Usage: 3, Selector: 1, MatchingType: 1, Certificate: hex.EncodeToString(wrong.Certificate)}))
	if res, _ := v.Validate("mx.example.org", 25, daneHexState(cert)); res != DANEFailed {
		t.Fatalf("mismatched digest Validate = %v, want failed", res)
	}
}

// TestDANELookupTLSA_MultipleRecords: every RR in the answer is decoded
// independently (SHA-512 SPKI and SHA-256 full cert).
func TestDANELookupTLSA_MultipleRecords(t *testing.T) {
	cert := daneHexCert(t)
	a := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingTypeSHA512)
	b := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorFullCert, TLSAMatchingTypeSHA256)
	v := NewDANEValidatorWithDNS(nil, daneHexDNS(t,
		&dns.TLSA{Usage: 3, Selector: 1, MatchingType: 2, Certificate: hex.EncodeToString(a.Certificate)},
		&dns.TLSA{Usage: 3, Selector: 0, MatchingType: 1, Certificate: hex.EncodeToString(b.Certificate)}))
	recs, err := v.LookupTLSA("mx.example.org", 25)
	if err != nil || len(recs) != 2 {
		t.Fatalf("lookup err=%v n=%d", err, len(recs))
	}
	if !bytes.Equal(recs[0].Certificate, a.Certificate) || !bytes.Equal(recs[1].Certificate, b.Certificate) {
		t.Fatalf("decoded data mismatch: %x / %x", recs[0].Certificate, recs[1].Certificate)
	}
}
