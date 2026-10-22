package auth

import (
	"testing"
)

// RFC 6376 §3.5 makes v= and a= REQUIRED tags of the DKIM-Signature header,
// and §6.1.1 requires the verifier to reject a signature missing required
// tags. parseDKIMSignature silently defaulted an absent a= to rsa-sha256 and
// accepted an absent v=, so a non-conformant signature was verified instead
// of rejected. Each case below is cryptographically valid over its own tag
// list: only the missing-required-tag contract can reject it.

func TestDKIMVerifyRejectsMissingRequiredTags(t *testing.T) {
	privateKey, _, err := GenerateDKIMKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateDKIMKeyPair: %v", err)
	}
	dnsKey := GetPublicKeyForDNS(privateKey)

	resolver := newMockDNSResolver()
	resolver.txtRecords["sel._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=" + dnsKey}
	verifier := NewDKIMVerifier(resolver)

	headers := map[string][]string{"From": {"Alice <alice@example.com>"}}
	body := []byte("Hello DKIM\r\n")
	bh := computeBodyHash(body, "simple")

	// sign builds a signature whose tag list is exactly tags (plus bh=, h=
	// and the empty b= placeholder), signs the exact header text the
	// verifier will hash, and appends b=. A signature built this way passes
	// the cryptographic check for any tag list; only required-tag
	// enforcement can distinguish the cases.
	sign := func(tags string) string {
		header := tags + " bh=" + bh + "; h=from; b="
		sigData := canonicalizeHeaders(headers, []string{"from"}, "simple") + dkimSignatureFieldForHash(header, "simple")
		sig, err := signRSA(privateKey, []byte(sigData))
		if err != nil {
			t.Fatalf("signRSA: %v", err)
		}
		return header + sig
	}

	complete := sign("v=1; a=rsa-sha256; d=example.com; s=sel; c=simple/simple;")
	missingA := sign("v=1; d=example.com; s=sel; c=simple/simple;")
	missingV := sign("a=rsa-sha256; d=example.com; s=sel; c=simple/simple;")

	// Control: a complete signature verifies.
	if result, _, err := verifier.Verify(headers, body, complete); result != DKIMPass {
		t.Fatalf("control complete signature failed: %v (%v)", result, err)
	}

	if result, _, err := verifier.Verify(headers, body, missingA); result == DKIMPass {
		t.Errorf("FAIL: signature without a= accepted (RFC 6376 §3.5: a= REQUIRED; §6.1.1: missing required tags MUST be rejected)")
	} else {
		t.Logf("missing a= rejected as expected: %v (%v)", result, err)
	}

	if result, _, err := verifier.Verify(headers, body, missingV); result == DKIMPass {
		t.Errorf("FAIL: signature without v= accepted (RFC 6376 §3.5: v= REQUIRED)")
	} else {
		t.Logf("missing v= rejected as expected: %v (%v)", result, err)
	}
}
