package imap

import (
	"fmt"
	"testing"
	"time"
)

// F6340: per-account lockout must be case/space-insensitive and independent
// of the source IP; tracker memory must stay bounded.
func TestF6340_UserLockoutCaseInsensitive(t *testing.T) {
	s := &Server{maxLoginAttempts: 2, lockoutDuration: time.Minute, auth: NewAuthTracker()}
	variants := []string{"Bob@x.com", "bob@X.com", " BOB@x.com", "bob@x.com", "BOB@X.COM", "bOb@x.com"}
	for _, v := range variants {
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
	s := &Server{maxLoginAttempts: 5, lockoutDuration: time.Hour, auth: NewAuthTracker()}
	for i := 0; i < maxTrackedAuthKeys+1000; i++ {
		s.recordAuthFailure(fmt.Sprintf("ip%d", i))
	}
	if n := len(s.auth.failures); n > maxTrackedAuthKeys {
		t.Fatalf("tracker grew to %d", n)
	}
}
