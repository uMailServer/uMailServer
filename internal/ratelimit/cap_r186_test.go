package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

// F6630: counter maps must stay bounded under a flood of distinct keys, and
// admitting a fresh key must never be rejected because the map is full.
func TestCountersBoundedUnderKeyFlood_F6630(t *testing.T) {
	rl := New(nil, DefaultConfig())
	defer rl.Stop()
	clk := time.Unix(1_000_000, 0)
	rl.nowFn = func() time.Time { return clk }
	rl.maxEntries = 50

	for i := 0; i < 1000; i++ {
		clk = clk.Add(time.Second)
		if r := rl.CheckIP(fmt.Sprintf("10.0.%d.%d", i/250, i%250+1)); !r.Allowed {
			t.Fatalf("fresh ip rejected: %+v", r)
		}
		if r := rl.CheckUser(fmt.Sprintf("u%d@x.test", i)); !r.Allowed {
			t.Fatalf("fresh user rejected: %+v", r)
		}
		if r := rl.CheckConnection(fmt.Sprintf("10.1.%d.%d", i/250, i%250+1)); !r.Allowed {
			t.Fatalf("fresh conn rejected: %+v", r)
		}
		if n := len(rl.ipCounters); n > 50 {
			t.Fatalf("ipCounters=%d > cap", n)
		}
		if n := len(rl.userCounters); n > 50 {
			t.Fatalf("userCounters=%d > cap", n)
		}
		if n := len(rl.connLimits); n > 50 {
			t.Fatalf("connLimits=%d > cap", n)
		}
	}
}

// Eviction prefers expired entries over live ones and idle conns over busy.
func TestEvictionPrefersExpiredAndIdle_F6630(t *testing.T) {
	rl := New(nil, DefaultConfig())
	defer rl.Stop()
	clk := time.Unix(1_000_000, 0)
	rl.nowFn = func() time.Time { return clk }
	rl.maxEntries = 10

	rl.CheckConnection("1.1.1.1") // busy (count 1)
	for i := 0; i < 8; i++ {
		ip := fmt.Sprintf("2.2.2.%d", i)
		rl.CheckConnection(ip)
		rl.ReleaseConnection(ip) // idle
	}
	rl.CheckConnection("3.3.3.3")
	rl.CheckConnection("4.4.4.4") // triggers eviction
	if _, ok := rl.connLimits["1.1.1.1"]; !ok {
		t.Fatal("busy connection slot evicted while idle entries existed")
	}
}
