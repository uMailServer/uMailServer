package vacation

import (
	"testing"
	"time"
)

// F6441: the shared Eligible rules refuse self, automated and null senders and
// default a non-positive interval to 7 days.
func TestR163EligibleShared(t *testing.T) {
	c := &Config{Enabled: true}
	now := time.Now()
	for _, s := range []string{"", "<>", "ME@a.com", "<me@a.com>", "list-request@x.org", "Mailer-Daemon@x.org"} {
		if _, ok := Eligible(c, "me@a.com", s, nil, now); ok {
			t.Errorf("sender %q eligible", s)
		}
	}
	eff, ok := Eligible(c, "me@a.com", "b@x.org", map[string]string{"x": "y"}, now)
	if !ok || eff.SendInterval != 7*24*time.Hour {
		t.Fatalf("eligible=%v interval=%v", ok, eff)
	}
	if _, ok := Eligible(c, "me@a.com", "b@x.org", map[string]string{"auto-submitted": "auto-generated"}, now); ok {
		t.Error("auto-submitted answered")
	}
	for _, s := range []string{"Postmaster@x.org", "<no-reply@x.org>", "", "owner-x@y"} {
		if !IsAutomatedSender(s) {
			t.Errorf("%q not automated", s)
		}
	}
}
