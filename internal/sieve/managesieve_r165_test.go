package sieve

import "testing"

// F6451: per-account lockout checked after the username is known; success
// clears only that account and never the per-IP counter.
func TestF6451_PerAccountLockout(t *testing.T) {
	srv := NewManageSieveServer(NewManager(), nil)
	srv.SetAuthHandler(func(u, p string) bool { return u == "user" && p == "pass" })
	srv.SetAuthLimits(3, 1<<40)
	for i := 0; i < 3; i++ {
		if ok, err := srv.checkedAuth(" Victim ", "bad"); ok || err != nil {
			t.Fatalf("attempt %d: ok=%v err=%v", i, ok, err)
		}
	}
	if _, err := srv.checkedAuth("VICTIM", "bad"); err == nil {
		t.Fatal("account not locked after limit")
	}
	if ok, err := srv.checkedAuth("user", "pass"); !ok || err != nil {
		t.Fatalf("other account affected: %v %v", ok, err)
	}
	// success must not reset IP-level failures
	srv.recordAuthFailure("9.9.9.9")
	srv.recordAuthFailure("9.9.9.9")
	srv.recordAuthFailure("9.9.9.9")
	srv.checkedAuth("user", "pass")
	if !srv.authLocked("9.9.9.9") {
		t.Fatal("success cleared IP counter")
	}
}
