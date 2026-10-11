package webhook

import "testing"

// F6774: a prefix wildcard of any length, including "m*", matches by prefix.
func TestR200EventMatchesShortWildcard(t *testing.T) {
	m := &Manager{}
	cases := []struct {
		pat, ev string
		want    bool
	}{
		{"m*", "mail.received", true}, {"m*", "queue.sent", false},
		{"mail.*", "mail.received", true}, {"mail.*", "mail", false},
		{"*", "anything", true}, {"", "x", false}, {"mail.received", "mail.received", true},
	}
	for _, c := range cases {
		if got := m.eventMatches([]string{c.pat}, c.ev); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pat, c.ev, got, c.want)
		}
	}
}
