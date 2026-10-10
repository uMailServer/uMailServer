package auth

// Regression tests (F5770-F5779) for RFC 8617 ARC seal/verify round trips.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
)

func arcRTSetup(t *testing.T) (*ARCValidator, *ARCSigner, *ARCSigner, *mockDNSResolver) {
	t.Helper()
	res := newMockDNSResolver()
	k1, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := rsa.GenerateKey(rand.Reader, 2048)
	res.txtRecords["a._domainkey.one.example"] = []string{"v=DKIM1; k=rsa; p=" + GetPublicKeyForDNS(k1)}
	res.txtRecords["b._domainkey.two.example"] = []string{"v=DKIM1; k=rsa; p=" + GetPublicKeyForDNS(k2)}
	return NewARCValidator(res),
		NewARCSigner(res, k1, "one.example", "a"),
		NewARCSigner(res, k2, "two.example", "b"), res
}

func arcRTMsg() (map[string][]string, []byte) {
	return map[string][]string{
		"From":    {"alice@example.com"},
		"To":      {"bob@example.org"},
		"Subject": {"hello"},
		"Date":    {"Mon, 1 Jan 2024 00:00:00 +0000"},
	}, []byte("line one\r\nline two\r\n")
}

func cloneHdr(h map[string][]string) map[string][]string {
	n := make(map[string][]string)
	for k, v := range h {
		n[k] = append([]string(nil), v...)
	}
	return n
}

func TestARCRoundTrip_SingleAndMultiHop(t *testing.T) {
	v, s1, s2, _ := arcRTSetup(t)
	h, body := arcRTMsg()

	h1, err := s1.Seal(h, body, "mx.one.example; spf=pass")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h1["ARC-Seal"][0], "cv=none") || !strings.HasPrefix(h1["ARC-Seal"][0], "i=1;") {
		t.Fatalf("bad first seal: %s", h1["ARC-Seal"][0])
	}
	chain, err := v.Validate(context.Background(), h1, body)
	if err != nil || chain.CV != "pass" || !chain.ChainValid {
		t.Fatalf("1-hop: cv=%v err=%v", chain, err)
	}

	h2, err := s2.Seal(h1, body, "mx.two.example; arc=pass")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h2["ARC-Seal"][0], "cv=pass") || !strings.HasPrefix(h2["ARC-Seal"][0], "i=2;") {
		t.Fatalf("bad second seal: %v", h2["ARC-Seal"])
	}
	chain, err = v.Validate(context.Background(), h2, body)
	if err != nil || chain.CV != "pass" || chain.ChainLength != 2 || chain.SealDomain != "two.example" {
		t.Fatalf("2-hop: %+v err=%v", chain, err)
	}
}

func TestARCTamperDetected(t *testing.T) {
	v, s1, s2, _ := arcRTSetup(t)
	h, body := arcRTMsg()
	h1, _ := s1.Seal(h, body, "mx.one.example; spf=pass")
	h2, _ := s2.Seal(h1, body, "mx.two.example; arc=pass")

	check := func(name string, hh map[string][]string, b []byte) {
		t.Helper()
		c, err := v.Validate(context.Background(), hh, b)
		if err != nil || c.CV != "fail" || c.ChainValid {
			t.Errorf("%s: tampering undetected (cv=%v err=%v)", name, c.CV, err)
		}
	}
	check("body", h2, []byte("line one\r\nEVIL\r\n"))

	t1 := cloneHdr(h2)
	t1["Subject"] = []string{"tampered"}
	check("signed header", t1, body)

	t2 := cloneHdr(h2)
	for k, vals := range t2 {
		if strings.EqualFold(k, "ARC-Authentication-Results") {
			for i := range vals {
				if strings.HasPrefix(vals[i], "i=1;") {
					vals[i] = "i=1; mx.one.example; spf=fail"
				}
			}
		}
	}
	check("older AAR", t2, body)

	t3 := cloneHdr(h2)
	for i, vv := range t3["ARC-Seal"] {
		if strings.HasPrefix(vv, "i=2;") {
			t3["ARC-Seal"][i] = strings.Replace(vv, "cv=pass", "cv=none", 1)
		}
	}
	check("cv rewrite", t3, body)

	// Dropping set 1 leaves a chain starting at 2.
	t4 := cloneHdr(h2)
	for _, k := range []string{"ARC-Seal", "ARC-Message-Signature", "ARC-Authentication-Results"} {
		var keep []string
		for _, vv := range t4[k] {
			if !strings.HasPrefix(vv, "i=1;") {
				keep = append(keep, vv)
			}
		}
		t4[k] = keep
	}
	check("missing set 1", t4, body)
}

func TestARCOlderAMSMayBreak(t *testing.T) {
	// RFC 8617 §5.2: only the newest AMS must verify; an intermediary that
	// changes the Subject invalidates the old AMS but the chain can stay valid
	// when the new hop re-signs.
	v, s1, s2, _ := arcRTSetup(t)
	h, body := arcRTMsg()
	h1, _ := s1.Seal(h, body, "mx.one.example; spf=pass")
	mod := cloneHdr(h1)
	mod["Subject"] = []string{"[list] hello"}
	// Sealing a modified message by validating the modified headers is
	// cv=fail (newest AMS broken); the forwarder passes the as-received cv.
	hf, _ := s2.Seal(mod, body, "mx.two.example; arc=pass")
	if !strings.Contains(hf["ARC-Seal"][0], "cv=fail") {
		t.Fatalf("expected cv=fail when validating modified headers: %s", hf["ARC-Seal"][0])
	}
	rx, _ := v.Validate(context.Background(), h1, body)
	h2, err := s2.SealWithCV(mod, body, "mx.two.example; arc=pass", rx.CV)
	if err != nil {
		t.Fatal(err)
	}
	c, err := v.Validate(context.Background(), h2, body)
	if err != nil || c.CV != "pass" {
		t.Fatalf("expected pass, got %v err=%v", c.CV, err)
	}
}

func TestARCSealOnBrokenChainGetsCVFail(t *testing.T) {
	_, s1, s2, _ := arcRTSetup(t)
	h, body := arcRTMsg()
	h1, _ := s1.Seal(h, body, "mx.one.example; spf=pass")
	h1["ARC-Seal"][0] = strings.Replace(h1["ARC-Seal"][0], "cv=none", "cv=pass", 1) // forged
	h2, err := s2.Seal(h1, body, "mx.two.example; arc=fail")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h2["ARC-Seal"][0], "cv=fail") {
		t.Fatalf("new seal must carry cv=fail, got %s", h2["ARC-Seal"][0])
	}
}

func TestARCInstanceIgnoresNonARCHeaders(t *testing.T) {
	h := map[string][]string{"Subject": {"i=9; hi"}}
	if got := determineNextInstance(h); got != 1 {
		t.Fatalf("next instance = %d, want 1", got)
	}
}

func TestARCAuthResultsCannotInjectHeaders(t *testing.T) {
	_, s1, _, _ := arcRTSetup(t)
	h, body := arcRTMsg()
	set, err := s1.Sign(h, body, "mx; spf=pass\r\nBcc: evil@example.net", 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(set.AAR, "\r\n") {
		t.Fatalf("AAR contains line break: %q", set.AAR)
	}
}

func TestARCSignInstanceMustFollowChain(t *testing.T) {
	_, s1, _, _ := arcRTSetup(t)
	h, body := arcRTMsg()
	if _, err := s1.Sign(h, body, "mx; spf=pass", 3); err == nil {
		t.Fatal("instance 3 on empty chain must be rejected")
	}
}

func TestARCAMSFormat(t *testing.T) {
	_, s1, _, _ := arcRTSetup(t)
	h, body := arcRTMsg()
	set, _ := s1.Sign(h, body, "mx; spf=pass", 1)
	p := parseTagValueList(set.AMS)
	if p["t"] == "" || p["t"] == "0" {
		t.Errorf("AMS t= must be a real timestamp, got %q", p["t"])
	}
	if p["h"] != "from:to:subject:date" {
		t.Errorf("AMS h= must list only present signed headers, got %q", p["h"])
	}
	if p["c"] != "relaxed/relaxed" || p["bh"] == "" {
		t.Errorf("AMS c/bh wrong: %s", set.AMS)
	}
}
