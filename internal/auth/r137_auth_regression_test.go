package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// F6190: the b= tag must be located at a tag boundary; a "b=" inside another
// tag's value (e.g. z= copied headers) must not be emptied in the hash input.
func TestDKIMSigFieldForHash_BTagAnchored_F6190(t *testing.T) {
	v := " v=1; a=rsa-sha256; d=example.com; s=sel; z=Subject:fab=cd; bh=AAAA; b=SIGDATA"
	got := dkimSignatureFieldForHash(v, "simple")
	want := "DKIM-Signature: v=1; a=rsa-sha256; d=example.com; s=sel; z=Subject:fab=cd; bh=AAAA; b="
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = dkimSignatureFieldForHash("v=1; z=x:b=y;\r\n\tb=SIG", "relaxed")
	if !strings.Contains(got, "z=x:b=y") || strings.Contains(got, "SIG") {
		t.Fatalf("relaxed: %q", got)
	}
}

// F6191: pct sampling must be uniform (byte%100 favoured low residues).
func TestShouldApplyPolicy_Uniform_F6191(t *testing.T) {
	const n = 40000
	hits := 0
	for i := 0; i < n; i++ {
		if shouldApplyPolicy(50) {
			hits++
		}
	}
	// biased implementation yields ~58.6%; 5 sigma of fair coin is ~1.25%
	if hits < n*47/100 || hits > n*53/100 {
		t.Fatalf("pct=50 hit rate %d/%d not uniform", hits, n)
	}
}

// F6192: an ed25519 key cannot verify an rsa-sha256 signature (and vice
// versa); the signature algorithm must match the key type (RFC 8463 §4).
func TestDKIMVerify_AlgKeyMismatch_F6192(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	res := newMockDNSResolver()
	res.txtRecords["s._domainkey.example.com"] = []string{"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)}
	headers := map[string][]string{"From": {"a@example.com"}}
	body := []byte("hi\r\n")
	hdr := "v=1; a=rsa-sha256; c=relaxed/simple; d=example.com; s=s; h=from; bh=" + computeBodyHash(body, "simple") + "; b="
	sd := canonicalizeHeaders(headers, []string{"from"}, "relaxed") + dkimSignatureFieldForHash(hdr, "relaxed")
	sig, _ := signEd25519(priv, []byte(sd))
	r, _, err := NewDKIMVerifier(res).Verify(headers, body, hdr+sig)
	if r == DKIMPass {
		t.Fatalf("a=rsa-sha256 verified with ed25519 key (err=%v)", err)
	}
}

// F6193: RFC 7489 §6.6.3 — a record without a valid p= but with a valid rua
// is treated as p=none rather than discarded.
func TestParseDMARC_InvalidPWithRua_F6193(t *testing.T) {
	rec, err := parseDMARCRecord("v=DMARC1; rua=mailto:a@example.com")
	if err != nil || rec.Policy != DMARCPolicyNone {
		t.Fatalf("rec=%+v err=%v", rec, err)
	}
	rec, err = parseDMARCRecord("v=DMARC1; p=bogus; rua=mailto:a@example.com")
	if err != nil || rec.Policy != DMARCPolicyNone {
		t.Fatalf("rec=%+v err=%v", rec, err)
	}
	if _, err = parseDMARCRecord("v=DMARC1; p=bogus"); err == nil {
		t.Fatal("no rua: record must be discarded")
	}
}
