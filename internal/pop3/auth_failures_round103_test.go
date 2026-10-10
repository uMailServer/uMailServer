package pop3

import (
	"testing"
	"time"
)

// F5853: isAuthLockedOut stored an empty slice for every IP it checked, so
// each distinct client IP that attempted PASS leaked a map entry forever.
func TestF5853_AuthFailuresMapDoesNotLeakIdleIPs(t *testing.T) {
	s := &Server{authFailures: make(map[string][]time.Time)}
	s.SetAuthLimits(3, time.Minute)
	for i := 0; i < 100; i++ {
		s.isAuthLockedOut("10.0.0." + string(rune('0'+i%10)) + string(rune('a'+i%26)))
	}
	s.authFailures["old"] = []time.Time{time.Now().Add(-time.Hour)}
	s.isAuthLockedOut("old")
	if n := len(s.authFailures); n != 0 {
		t.Fatalf("authFailures retained %d empty entries", n)
	}
}
