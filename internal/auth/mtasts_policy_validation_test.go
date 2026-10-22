package auth

import (
	"testing"
)

// RFC 8461 §4.2 (ABNF): sts-policy-mx is "required at least once, except when
// mode is none", and sts-policy-max-age is "required once" with a numeric
// value. parseMTASTSPolicy accepted an enforce/testing policy with no mx
// pattern (which would then fail every MX match as a "valid" enforce policy)
// and silently clamped a malformed max_age to the 86400 default instead of
// rejecting the policy.

func TestParseMTASTSPolicyRequiresMXPattern(t *testing.T) {
	enforceNoMX := "version: STSv1\nmode: enforce\nmax_age: 604800\n"
	if _, err := parseMTASTSPolicy(enforceNoMX); err == nil {
		t.Errorf("FAIL: enforce policy without mx= accepted (RFC 8461 §4.2: mx required at least once unless mode is none)")
	} else {
		t.Logf("mx-less enforce policy rejected as expected: %v", err)
	}

	testingNoMX := "version: STSv1\nmode: testing\nmax_age: 604800\n"
	if _, err := parseMTASTSPolicy(testingNoMX); err == nil {
		t.Errorf("FAIL: testing policy without mx= accepted (RFC 8461 §4.2: mx required at least once unless mode is none)")
	}

	// Control: mode none legitimately has no mx patterns.
	if p, err := parseMTASTSPolicy("version: STSv1\nmode: none\nmax_age: 604800\n"); err != nil || p == nil {
		t.Errorf("mode-none policy without mx should parse (§4.2 exception), got err %v", err)
	}
}

func TestParseMTASTSPolicyRejectsMalformedMaxAge(t *testing.T) {
	if _, err := parseMTASTSPolicy("version: STSv1\nmode: enforce\nmx: mail.example.com\nmax_age: soon\n"); err == nil {
		t.Errorf("FAIL: malformed max_age accepted (RFC 8461 §4.2: max_age is required and numeric; the policy is invalid, not clampable)")
	}

	// Control: a conforming numeric max_age parses with its real value.
	p, err := parseMTASTSPolicy("version: STSv1\nmode: enforce\nmx: mail.example.com\nmax_age: 604800\n")
	if err != nil || p == nil || p.MaxAge != 604800 {
		t.Errorf("conforming policy parsed wrong: MaxAge=%d err=%v", p.MaxAge, err)
	}
}
