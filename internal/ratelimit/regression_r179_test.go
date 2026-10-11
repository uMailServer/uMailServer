package ratelimit

import "testing"

// F6570: per-user limits must not be bypassable by changing the case or
// surrounding whitespace of the account name.
func TestR179UserKeyCaseInsensitive(t *testing.T) {
	rl := New(nil, &Config{UserPerMinute: 2})
	defer rl.Stop()
	if !rl.CheckUser("Bob@Example.com").Allowed || !rl.CheckUser("bob@example.com").Allowed {
		t.Fatal("first two must pass")
	}
	if rl.CheckUser("BOB@EXAMPLE.COM").Allowed {
		t.Fatal("case variant bypassed the per-user limit")
	}
	if rl.CheckMessage(" bob@example.com ", "").Allowed {
		t.Fatal("whitespace variant bypassed the per-user limit")
	}
}

// F6571: host:port and IPv6 zone forms must aggregate with the bare address.
func TestR179IPKeyStripsPortAndZone(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:5678":      "1.2.3.4",
		"[2001:db8::1]:25":  "2001:db8::/64",
		"[2001:db8::1]":     "2001:db8::/64",
		"fe80::1%eth0":      "fe80::/64",
		"2001:db8:0:0:1::5": "2001:db8::/64",
		"::ffff:1.2.3.4":    "1.2.3.4",
		"not-an-ip":         "not-an-ip",
	}
	for in, want := range cases {
		if got := ipKey(in); got != want {
			t.Errorf("ipKey(%q)=%q want %q", in, got, want)
		}
	}
}
