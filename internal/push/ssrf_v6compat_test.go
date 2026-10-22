package push

import (
	"net/netip"
	"testing"
)

// The IPv4-compatible IPv6 range ::/96 (deprecated by RFC 4291) embeds an
// IPv4 address in its low 32 bits and routes as ordinary global unicast on
// common stacks — exactly the embed-and-route-to-internal-IPv4 class the
// NAT64 and 6to4 prefixes are blocked for. A subscriber whose authoritative
// DNS answers with an AAAA in ::/96 must therefore not slip past the
// dial-time SSRF control: isForbiddenAddr is the predicate newPushTransport's
// Control applies to every resolved address (F5760).
func TestIsForbiddenAddrBlocksIPv4CompatibleV6(t *testing.T) {
	for _, a := range []string{"::127.0.0.1", "::10.0.0.5", "::169.254.169.254", "::a00:1"} {
		if !isForbiddenAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s allowed (want forbidden: deprecated IPv4-compatible form embeds a non-public IPv4 address)", a)
		}
	}
	// The mapped (::ffff:0:0/96) forms keep working.
	for _, a := range []string{"::ffff:127.0.0.1", "::ffff:10.0.0.5"} {
		if !isForbiddenAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s allowed (mapped form must stay forbidden)", a)
		}
	}
	// Public space stays reachable: the block must not extend beyond ::/96.
	for _, a := range []string{"1.1.1.1", "2606:4700:4700::1111", "::ffff:1.1.1.1"} {
		if isForbiddenAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s forbidden (public addresses must stay reachable)", a)
		}
	}
}
