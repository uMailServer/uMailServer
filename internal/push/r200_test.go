package push

import (
	"net/netip"
	"testing"
)

// F6773: non-routable IPv4 ranges are refused as push targets.
func TestR200ForbiddenNonRoutableRanges(t *testing.T) {
	for _, a := range []string{"198.18.0.1", "198.19.255.254", "192.0.0.8", "240.0.0.1", "255.255.255.255", "::ffff:198.18.0.1", "100.64.0.1"} {
		if !isForbiddenAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s allowed", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "198.17.255.255", "198.20.0.0", "192.0.1.1", "2606:4700:4700::1111"} {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		if isForbiddenAddr(ip) {
			t.Errorf("%s forbidden", a)
		}
	}
}
