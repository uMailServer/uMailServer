package auth

// Regression tests for ARC chain set selection.
//
// RFC 8617 §5.1: an ARC chain is a sequence of ARC sets identified by instance
// number i=N. The validator must evaluate every set actually present.
//
// Bug (internal/auth/arc.go Validate): the loop iterated
// `for i := 1; i <= len(arcSets); i++` over arcSets — a map[int]ARCSet keyed by
// instance number. len(arcSets) is the COUNT of sets, not the highest instance,
// so a chain with gaps fabricated empty phantom sets for missing instances and
// skipped the real higher-instance sets entirely.

import (
	"context"
	"fmt"
	"testing"
)

// arcRegressionHeaders builds a headers map containing ARC sets for the given
// instance numbers. The tag-values carry no b= signature, so validateAMS /
// validateAS short-circuit to (false, nil) with no DNS or crypto — the defect
// is purely in set selection, keeping these tests deterministic.
func arcRegressionHeaders(instances ...int) map[string][]string {
	var aar, ams, as []string
	for _, n := range instances {
		aar = append(aar, fmt.Sprintf("i=%d; spf=pass smtp.mailfrom=x@example.com", n))
		ams = append(ams, fmt.Sprintf("i=%d; a=rsa-sha256; d=example.com; s=sel", n))
		as = append(as, fmt.Sprintf("i=%d; a=rsa-sha256; d=example.com; s=sel; cv=pass", n))
	}
	return map[string][]string{
		"ARC-Authentication-Results": aar,
		"ARC-Message-Signature":      ams,
		"ARC-Seal":                   as,
	}
}

func arcWalkedInstances(chain *ARCChain) []int {
	out := make([]int, 0, len(chain.Sets))
	for _, s := range chain.Sets {
		out = append(out, s.Instance)
	}
	return out
}

func arcHasPhantomSet(chain *ARCChain) bool {
	for _, s := range chain.Sets {
		if s.AMS == "" && s.AS == "" {
			return true
		}
	}
	return false
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertARCWalk(t *testing.T, chain *ARCChain, err error, want []int, label string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: Validate returned error: %v", label, err)
	}
	got := arcWalkedInstances(chain)
	if !intSliceEqual(got, want) {
		t.Fatalf("%s: Validate walked instances %v, want %v (every real ARC set must be validated, "+
			"and no phantom set may be fabricated for a missing instance)", label, got, want)
	}
	if arcHasPhantomSet(chain) {
		t.Fatalf("%s: Validate fabricated a phantom empty ARC set; walked %v", label, got)
	}
}

// TestARCValidate_NonContiguousChain: instances 1,2,4 (3 absent) must be
// validated as 1,2,4 — the real instance-4 set examined, no phantom instance 3.
func TestARCValidate_NonContiguousChain(t *testing.T) {
	v := NewARCValidator(newMockDNSResolver())
	chain, err := v.Validate(context.Background(), arcRegressionHeaders(1, 2, 4), []byte("body"))
	assertARCWalk(t, chain, err, []int{1, 2, 4}, "non-contiguous 1,2,4")
	if chain.ChainLength != 3 {
		t.Fatalf("non-contiguous 1,2,4: ChainLength = %d, want 3", chain.ChainLength)
	}
}

// TestARCValidate_ContiguousChain (control): a well-formed contiguous chain
// 1,2,3 must validate exactly those three real sets.
func TestARCValidate_ContiguousChain(t *testing.T) {
	v := NewARCValidator(newMockDNSResolver())
	chain, err := v.Validate(context.Background(), arcRegressionHeaders(1, 2, 3), []byte("body"))
	assertARCWalk(t, chain, err, []int{1, 2, 3}, "contiguous 1,2,3")
}

// TestARCValidate_SingleInstance boundary: a one-set chain must validate just
// that set.
func TestARCValidate_SingleInstance(t *testing.T) {
	v := NewARCValidator(newMockDNSResolver())
	chain, err := v.Validate(context.Background(), arcRegressionHeaders(1), []byte("body"))
	assertARCWalk(t, chain, err, []int{1}, "single instance 1")
}

// TestARCValidate_ChainStartingAboveOne boundary: a chain whose lowest instance
// is 2 (instance 1 absent) must validate instance 2, not fabricate instance 1.
func TestARCValidate_ChainStartingAboveOne(t *testing.T) {
	v := NewARCValidator(newMockDNSResolver())
	chain, err := v.Validate(context.Background(), arcRegressionHeaders(2, 3), []byte("body"))
	assertARCWalk(t, chain, err, []int{2, 3}, "chain starting at 2,3")
}

// TestARCValidate_HighInstanceIsValidated: a single real set at a high instance
// number must still be validated.
func TestARCValidate_HighInstanceIsValidated(t *testing.T) {
	v := NewARCValidator(newMockDNSResolver())
	chain, err := v.Validate(context.Background(), arcRegressionHeaders(7), []byte("body"))
	assertARCWalk(t, chain, err, []int{7}, "single high instance 7")
}
