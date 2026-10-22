package alert

import "testing"

// The IPv4-compatible IPv6 range ::/96 (deprecated by RFC 4291) embeds an
// IPv4 address in its low 32 bits and routes as ordinary global unicast on
// common stacks — the same embed-and-route-to-internal-IPv4 class as the
// blocked NAT64 and 6to4 forms. An alert webhook URL whose DNS resolves into
// ::/96 must therefore be rejected by dialControl's post-resolution SSRF
// check (F6500), not silently dialed.
func TestDialControlRejectsIPv4CompatibleV6(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	for _, addr := range []string{"[::127.0.0.1]:80", "[::10.0.0.5]:80", "[::a9fe:a9fe]:443"} {
		if err := m.dialControl("tcp", addr, nil); err == nil {
			t.Errorf("%s allowed (want rejected: deprecated IPv4-compatible form embeds a non-public IPv4 address)", addr)
		}
	}
	// The mapped (::ffff:0:0/96) forms keep working.
	for _, addr := range []string{"[::ffff:127.0.0.1]:80", "[::ffff:10.0.0.5]:80"} {
		if err := m.dialControl("tcp", addr, nil); err == nil {
			t.Errorf("%s allowed (mapped form must stay rejected)", addr)
		}
	}
	// Public space stays reachable.
	for _, addr := range []string{"[1.1.1.1]:80", "[2606:4700:4700::1111]:80"} {
		if err := m.dialControl("tcp", addr, nil); err != nil {
			t.Errorf("%s rejected (public addresses must stay reachable): %v", addr, err)
		}
	}
	// The explicit testing bypass is untouched.
	m.SetAllowPrivateIP(true)
	if err := m.dialControl("tcp", "[::127.0.0.1]:80", nil); err != nil {
		t.Errorf("allowPrivateIP bypass broken: %v", err)
	}
}
