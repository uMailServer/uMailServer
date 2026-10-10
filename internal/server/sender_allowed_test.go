package server

import "testing"

// F6041: an authenticated submission user may only use their own address or
// an alias that routes to their mailbox as MAIL FROM.
func TestSenderAllowed(t *testing.T) {
	s := newDeliveryAuditServer(t)
	mkAlias(t, s, "info", "bob@test.com")

	cases := []struct {
		user, from string
		want       bool
	}{
		{"bob@test.com", "bob@test.com", true},
		{"Bob@TEST.com", "bob@test.com", true},
		{"bob@test.com", "info@test.com", true},
		{"bob@test.com", "", true},
		{"bob@test.com", "alice@test.com", false},
		{"bob@test.com", "ceo@other.org", false},
	}
	for _, c := range cases {
		if got := s.senderAllowed(c.user, c.from); got != c.want {
			t.Errorf("senderAllowed(%q,%q)=%v want %v", c.user, c.from, got, c.want)
		}
	}
}
