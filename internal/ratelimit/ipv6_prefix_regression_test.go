package ratelimit

// Regression tests for finding F5106: IPv6 limits are keyed by /64 so interface-ID rotation cannot bypass them.

import (
	"fmt"
	"testing"
)

func v5106RL() *RateLimiter {
	c := DefaultConfig()
	c.IPPerMinute, c.IPPerHour, c.IPPerDay = 1, 0, 0
	return New(nil, c)
}

func TestF5106Reproduction(t *testing.T) {
	rl := v5106RL()
	defer rl.Stop()
	if !rl.CheckIP("2001:db8:1:2::1").Allowed || rl.CheckIP("2001:db8:1:2::ffff").Allowed {
		t.Fatal("same /64 not grouped")
	}
	for i := 0; i < 1000; i++ {
		rl.CheckIP(fmt.Sprintf("2001:db8:1:2:%x::1", i))
	}
	if n := len(rl.ipCounters); n != 1 {
		t.Fatalf("buckets=%d", n)
	}
}

// Edge: a different /64 and IPv4 addresses keep separate buckets; IPv4-mapped
// IPv6 shares the IPv4 bucket; unparseable keys are used verbatim.
func TestF5106Edges(t *testing.T) {
	rl := v5106RL()
	defer rl.Stop()
	if !rl.CheckIP("2001:db8:1:2::1").Allowed || !rl.CheckIP("2001:db8:1:3::1").Allowed {
		t.Fatal("distinct /64 grouped")
	}
	if !rl.CheckIP("192.0.2.1").Allowed || !rl.CheckIP("192.0.2.2").Allowed {
		t.Fatal("distinct IPv4 grouped")
	}
	if rl.CheckIP("::ffff:192.0.2.1").Allowed {
		t.Fatal("IPv4-mapped not grouped with IPv4")
	}
	if !rl.CheckIP("not-an-ip").Allowed || rl.CheckIP("not-an-ip").Allowed {
		t.Fatal("verbatim key")
	}
	if s := rl.GetIPStats("2001:db8:1:2::abcd"); s["minute_count"] != 1 {
		t.Fatalf("stats not grouped: %v", s)
	}
}

func TestF5106Connections(t *testing.T) {
	c := DefaultConfig()
	c.IPConnections = 1
	rl := New(nil, c)
	defer rl.Stop()
	if !rl.CheckConnection("2001:db8::1").Allowed || rl.CheckConnection("2001:db8::2").Allowed {
		t.Fatal("connection limit not grouped by /64")
	}
	rl.ReleaseConnection("2001:db8::2")
	if !rl.CheckConnection("2001:db8::3").Allowed {
		t.Fatal("release not grouped by /64")
	}
}
