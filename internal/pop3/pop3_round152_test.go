package pop3

import (
	"fmt"
	"testing"
	"time"
)

func TestF6340_UserLockoutCaseInsensitive(t *testing.T) {
	s := &Server{maxLoginAttempts: 2, lockoutDuration: time.Minute, authFailures: map[string][]time.Time{}}
	for _, v := range []string{"Bob@x.com", "bob@X.com", " BOB@x.com", "bob@x.com", "BOB@X.COM", "bOb@x.com"} {
		s.recordUserFailure(v)
	}
	if !s.isUserLockedOut("bob@x.com") {
		t.Fatal("case variants bypassed per-user lockout")
	}
	s.clearUserFailures("BOB@x.com")
	if s.isUserLockedOut("bob@x.com") {
		t.Fatal("success did not reset counter")
	}
}

func TestF6340_TrackerBounded(t *testing.T) {
	s := &Server{maxLoginAttempts: 5, lockoutDuration: time.Hour, authFailures: map[string][]time.Time{}}
	for i := 0; i < maxTrackedAuthKeys+1000; i++ {
		s.recordAuthFailure(fmt.Sprintf("ip%d", i))
	}
	if n := len(s.authFailures); n > maxTrackedAuthKeys {
		t.Fatalf("tracker grew to %d", n)
	}
}
