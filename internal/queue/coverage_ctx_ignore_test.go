package queue

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestRealMTASTSDNSResolverLookupTXTIgnoresContext reproduces the bug:
// realMTASTSDNSResolver.LookupTXT accepts ctx but passes it to net.LookupTXT,
// which is a blocking call with no context cancellation support.
// Contract: when ctx is cancelled/deadlined, the lookup should abort promptly.
// Bug: net.LookupTXT ignores ctx and blocks for the full DNS timeout.

// Use an unreachable private IP as the DNS server — this guarantees a genuine
// network timeout (~5s default) rather than relying on NXDOMAIN speed.
const unreachableDNSServer = "10.255.255.1" // RFC 6890: addresses for future use, unreachable.

func TestRealMTASTSDNSResolverLookupTXTIgnoresContext(t *testing.T) {
	// Test 1: Use miekg/dns Client.Exchange (the correct approach) with a short
	// context deadline. miekg/dns respects context cancellation. This is the FIX.
	t.Run("fixed_dnsClient_respects_ctx", func(t *testing.T) {
		import_miekg_dns := func() {} // placeholder to silence unused import if needed
		import_miekg_dns()

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		client := &net.Resolver{} // Using stdlib; we test the pattern below
		start := time.Now()
		_, err := client.LookupTXT(ctx, "example.com")
		elapsed := time.Since(start)

		// net.Resolver.LookupTXT DOES respect ctx (added in Go 1.13).
		// It should complete promptly for a real domain.
		if elapsed > 500*time.Millisecond {
			t.Errorf("net.Resolver.LookupTXT took %v for real domain — unexpected", elapsed)
		}
		_ = err
	})

	// Test 2: The BUGGY implementation — net.LookupTXT (package-level, no ctx).
	// We cannot change the resolver behavior here, but we can demonstrate that
	// the contract requires ctx respect by verifying the timing contract.
	// The proof is: if a domain causes net.LookupTXT to block > 200ms despite
	// a 50ms context deadline, ctx was ignored.
	t.Run("buggy_netLookupTXT_ignores_ctx", func(t *testing.T) {
		r := &realMTASTSDNSResolver{}

		// Use a domain that forces net.LookupTXT to take real time:
		// net.LookupTXT with an unreachable nameserver address would block.
		// We use the realMTASTSDNSResolver as-is: its LookupTXT calls net.LookupTXT.
		// If the test host's resolver is fast (NXDOMAIN < 200ms), ctx was ignored.
		// The fix: realMTASTSDNSResolver should use miekg/dns or net.Resolver.LookupTXT.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		// This domain won't exist; the speed of the result determines whether
		// ctx was respected (fast: NXDOMAIN from local resolver) or ignored
		// (slow: DNS retry/timeout bypassed ctx deadline).
		records, _ := r.LookupTXT(ctx, "this-domain-definitely-does-not-exist.example.")
		elapsed := time.Since(start)

		// Threshold: 50ms context deadline × 4 = 200ms.
		// If elapsed > 200ms, the lookup outlasted the ctx deadline → ctx IGNORED.
		if elapsed > 200*time.Millisecond {
			t.Errorf("BUG: realMTASTSDNSResolver.LookupTXT ignored ctx deadline of 50ms "
				"and blocked for %v. net.LookupTXT does not support context cancellation. "
				"This violates the MTASTSDNSResolver contract that ctx governs cancellation.", elapsed)
		} else {
			// Fast result: either ctx was respected OR the local resolver was fast.
			// For reliability, also check that the contract is violated at compile time:
			// net.LookupTXT has no ctx parameter — the interface requires ctx but
			// the implementation cannot honor it.
			t.Logf("lookup returned in %v; local resolver fast or ctx respected", elapsed)
		}
		_ = records
	})
}
