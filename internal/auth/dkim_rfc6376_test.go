package auth

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// Regression tests for Round 26 findings F5075 (DKIM key lookup bypassed the
// injected resolver), F5076 (relaxed empty-body canonicalization) and F5077
// (signature whose h= omits From accepted), RFC 6376 §3.4.4 / §6.1.1.
type dkimCountingResolver struct {
	*mockDNSResolver
	calls       int
	hadDeadline bool
}

func (r *dkimCountingResolver) LookupTXT(ctx context.Context, d string) ([]string, error) {
	r.calls++
	_, r.hadDeadline = ctx.Deadline()
	return r.mockDNSResolver.LookupTXT(ctx, d)
}

func TestDKIMVerify_UsesInjectedResolver_F5075(t *testing.T) {
	blk, _ := pem.Decode([]byte(rfc8463RSAKey))
	key, _ := x509.ParsePKCS1PrivateKey(blk.Bytes)
	s := NewDKIMSigner(newMockDNSResolver(), key, "f5075.invalid", "sel")
	hdrs := map[string][]string{"From": {"a@f5075.invalid"}, "Subject": {"hi"}}
	body := []byte("Hello\r\n")
	val, _ := s.Sign(hdrs, body)
	sig := "DKIM-Signature: " + val

	// 1. reproduction: key served only by the injected resolver → pass, with deadline.
	r := &dkimCountingResolver{mockDNSResolver: newMockDNSResolver()}
	r.txtRecords["sel._domainkey.f5075.invalid"] = []string{"v=DKIM1; k=rsa; p=" + GetPublicKeyForDNS(key)}
	v := NewDKIMVerifier(r)
	if res, _, err := v.Verify(hdrs, body, sig); res != DKIMPass || r.calls != 1 || !r.hadDeadline {
		t.Fatalf("repro: res=%s err=%v calls=%d deadline=%v", res, err, r.calls, r.hadDeadline)
	}
	// 2. repeated call is served from cache, resolver not hit again.
	if res, _, _ := v.Verify(hdrs, body, sig); res != DKIMPass || r.calls != 1 {
		t.Fatalf("cache: res=%s calls=%d", res, r.calls)
	}
	// 3. injected temporary failure surfaces as temperror.
	r2 := &dkimCountingResolver{mockDNSResolver: newMockDNSResolver()}
	r2.tempFail["sel._domainkey.f5075.invalid"] = true
	if res, _, _ := NewDKIMVerifier(r2).Verify(hdrs, body, sig); res != DKIMTempError || r2.calls != 1 {
		t.Fatalf("tempfail: res=%s calls=%d", res, r2.calls)
	}
	// 4. resolver says no record → fail (not pass), no system DNS used.
	r3 := &dkimCountingResolver{mockDNSResolver: newMockDNSResolver()}
	if res, _, _ := NewDKIMVerifier(r3).Verify(hdrs, body, sig); res != DKIMFail || r3.calls != 1 {
		t.Fatalf("nxdomain: res=%s calls=%d", res, r3.calls)
	}
}

func TestDKIMVerify_RelaxedEmptyBody_F5076(t *testing.T) {
	const emptyBH = "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
	blk, _ := pem.Decode([]byte(rfc8463RSAKey))
	key, _ := x509.ParsePKCS1PrivateKey(blk.Bytes)
	hdrs := map[string][]string{"From": {"a@football.example.com"}, "Subject": {"empty"}}
	val := "v=1; a=rsa-sha256; c=relaxed/relaxed; d=football.example.com; s=test; h=from:subject; bh=" + emptyBH + "; b="
	b, _ := signRSA(key, []byte(canonicalizeHeaders(hdrs, []string{"from", "subject"}, "relaxed")+dkimSignatureFieldForHash(val, "relaxed")))
	v := NewDKIMVerifier(newMockDNSResolver())
	v.cache.set("test", "football.example.com", &key.PublicKey, nil)
	// 1. reproduction + equivalent bodies that canonicalize to empty.
	for _, body := range []string{"", "\r\n", "\r\n\r\n\r\n", " \t \r\n\r\n", "\n\n"} {
		if res, _, err := v.Verify(hdrs, []byte(body), "DKIM-Signature: "+val+b); res != DKIMPass {
			t.Fatalf("body %q: %s %v", body, res, err)
		}
	}
	// 2. non-empty body must still differ (no over-trimming).
	if got := string(canonicalizeBodyRelaxed([]byte(" x \r\n\r\n"))); got != " x\r\n" {
		t.Fatalf("non-empty: %q", got)
	}
	// 3. simple canonicalization keeps CRLF for empty body (RFC 6376 §3.4.3).
	if got := string(canonicalizeBody(nil, "simple")); got != "\r\n" {
		t.Fatalf("simple empty: %q", got)
	}
	// 4. bodies with content do not verify against the empty hash.
	if res, _, _ := v.Verify(hdrs, []byte("x\r\n"), "DKIM-Signature: "+val+b); res == DKIMPass {
		t.Fatal("non-empty body verified against empty bh")
	}
}

func TestDKIMVerify_FromMustBeSigned_F5077(t *testing.T) {
	blk, _ := pem.Decode([]byte(rfc8463RSAKey))
	key, _ := x509.ParsePKCS1PrivateKey(blk.Bytes)
	hdrs := map[string][]string{"From": {"ceo@football.example.com"}, "Subject": {"s"}, "To": {"x@example.net"}}
	body := []byte("Hi\r\n")
	run := func(hTag string) DKIMResult {
		h := parseHeaderList(hTag)
		val := "v=1; a=rsa-sha256; c=relaxed/relaxed; d=football.example.com; s=test; h=" + hTag + "; bh=" + sha256Hash(canonicalizeBody(body, "relaxed")) + "; b="
		b, _ := signRSA(key, []byte(canonicalizeHeaders(hdrs, h, "relaxed")+dkimSignatureFieldForHash(val, "relaxed")))
		v := NewDKIMVerifier(newMockDNSResolver())
		v.cache.set("test", "football.example.com", &key.PublicKey, nil)
		res, _, _ := v.Verify(hdrs, body, "DKIM-Signature: "+val+b)
		return res
	}
	// 1. reproduction
	if r := run("subject:to"); r != DKIMPERMError {
		t.Fatalf("no From: %s", r)
	}
	// 2. From present in any case / position / oversigned → pass
	for _, h := range []string{"From:subject", "subject:from", "from:from:subject"} {
		if r := run(h); r != DKIMPass {
			t.Fatalf("%s: %s", h, r)
		}
	}
	// 3. a header merely containing "from" as substring is not From
	if r := run("x-from:subject"); r != DKIMPERMError {
		t.Fatalf("x-from: %s", r)
	}
}
