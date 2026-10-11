package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"math/big"
	"net"
	"testing"
)

// F6490: relaxed header canonicalization trims only SP/HTAB, not Unicode
// whitespace such as NBSP (a trailing NBSP must change the hash input).
func TestRelaxedHeaderCanon_OnlyWSPTrimmed_F6490(t *testing.T) {
	a := canonicalizeHeaderRelaxed("Subject", "hello")
	b := canonicalizeHeaderRelaxed("Subject", "hello ")
	if a == b {
		t.Fatalf("NBSP trimmed: %q", b)
	}
	if got := canonicalizeHeaderRelaxed("Subject", " \t a  b \t"); got != "subject:a b\r\n" {
		t.Fatalf("got %q", got)
	}
}

// F6491: RFC 8301 — keys shorter than 1024 bits must not be accepted.
func TestParseDKIMPublicKey_MinRSASize_F6491(t *testing.T) {
	p1, _ := rand.Prime(rand.Reader, 256)
	p2, _ := rand.Prime(rand.Reader, 256)
	small := &rsa.PublicKey{N: new(big.Int).Mul(p1, p2), E: 65537}
	der, _ := x509.MarshalPKIXPublicKey(small)
	if _, _, err := parseDKIMPublicKey("v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)); err == nil {
		t.Fatal("512-bit key accepted")
	}
	ok, _ := rsa.GenerateKey(rand.Reader, 1024)
	der, _ = x509.MarshalPKIXPublicKey(&ok.PublicKey)
	if _, _, err := parseDKIMPublicKey("v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)); err != nil {
		t.Fatal(err)
	}
}

// F6492/F6493: malformed numeric tags and duplicate tags invalidate the signature.
func TestParseDKIMSignature_StrictTags_F6492(t *testing.T) {
	base := "v=1; a=rsa-sha256; d=e.com; s=s; bh=AAAA; b=BBBB; h=from"
	if _, err := parseDKIMSignature(base + "; x=abc"); err == nil {
		t.Error("x=abc accepted")
	}
	if _, err := parseDKIMSignature(base + "; l=-5"); err == nil {
		t.Error("l=-5 accepted")
	}
	if _, err := parseDKIMSignature(base + "; l=99999999999999999999999"); err == nil {
		t.Error("overflowing l accepted")
	}
	if _, err := parseDKIMSignature(base + "; t=12x"); err == nil {
		t.Error("t=12x accepted")
	}
	if _, err := parseDKIMSignature(base + "; d=evil.com"); err == nil {
		t.Error("duplicate d= accepted")
	}
	if sig, err := parseDKIMSignature(base + "; l=10; x=2000000000; t=1"); err != nil || sig.BodyLength != 10 {
		t.Errorf("valid rejected: %v", err)
	}
}

// F6494: simple body canonicalization must not write into the caller's buffer.
func TestCanonicalizeBodySimple_NoAliasing_F6494(t *testing.T) {
	buf := []byte("abcXY")
	body := buf[:3]
	_ = canonicalizeBodySimple(body)
	if string(buf) != "abcXY" {
		t.Fatalf("caller buffer modified: %q", buf)
	}
}

// F6495: more than one redirect modifier is a permerror (RFC 7208 §6).
func TestSPF_DuplicateRedirect_F6495(t *testing.T) {
	r := &mockDNSResolver{txtRecords: map[string][]string{
		"a.example": {"v=spf1 redirect=b.example redirect=c.example"},
		"b.example": {"v=spf1 +all"},
		"c.example": {"v=spf1 -all"},
	}}
	res, _ := NewSPFChecker(r).CheckSPF(context.Background(), net.ParseIP("1.2.3.4"), "a.example", "u@a.example")
	if res != SPFPermError {
		t.Fatalf("got %v", res)
	}
}
