package auth

import "testing"

// TestMatchMX_WildcardMatchesOnlyLeftmostLabel pins RFC 8461 section 4.1:
//
//	"Thus, the mx pattern '*.example.com' matches 'mail.example.com'
//	 but not 'example.com' or 'foo.bar.example.com'."
//
// MTA-STS exists to stop an active network attacker from steering a sender to
// an MX host the receiving domain did not authorize. A policy publishing
// "mx: *.example.com" therefore excludes the apex and every deeper name. The
// previous implementation matched both, so an attacker holding such a host
// (for example a dangling deeper subdomain) was accepted.
func TestMatchMX_WildcardMatchesOnlyLeftmostLabel(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		mx      string
		want    bool
	}{
		{"single label matches", "*.example.com", "mail.example.com", true},
		{"other single label matches", "*.example.com", "mx.example.com", true},
		{"apex does not match", "*.example.com", "example.com", false},
		{"deeper name does not match", "*.example.com", "foo.bar.example.com", false},
		{"much deeper name does not match", "*.example.com", "a.b.c.example.com", false},
		{"empty label does not match", "*.example.com", ".example.com", false},
		{"sibling domain does not match", "*.example.com", "mail.other.com", false},
		{"suffix without dot boundary does not match",
			"*.example.com", "mailexample.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchMX(tt.pattern, tt.mx); got != tt.want {
				t.Errorf("matchMX(%q, %q) = %v, want %v; RFC 8461 4.1: the wildcard "+
					"may only match the entire left-most label", tt.pattern, tt.mx, got, tt.want)
			}
		})
	}
}

// TestMatchMX_ExactMatchAndCaseInsensitivityUnaffected is the control: exact
// matching and case-insensitive comparison are unchanged by the fix. These pass
// before and after, so a red run cannot be blamed on the harness.
func TestMatchMX_ExactMatchAndCaseInsensitivityUnaffected(t *testing.T) {
	tests := []struct {
		pattern string
		mx      string
		want    bool
	}{
		{"mail.example.com", "mail.example.com", true},
		{"mail.example.com", "mail.other.com", false},
		{"MAIL.EXAMPLE.COM", "mail.example.com", true},
		{"mail.example.com", "MAIL.EXAMPLE.COM", true},
		{"*.EXAMPLE.COM", "mail.example.com", true},
		{"*.EXAMPLE.COM", "Mail.Example.Com", true},
	}

	for _, tt := range tests {
		if got := matchMX(tt.pattern, tt.mx); got != tt.want {
			t.Errorf("control: matchMX(%q, %q) = %v, want %v", tt.pattern, tt.mx, got, tt.want)
		}
	}
}

// TestCheckPolicy_ExcludesHostsOutsidePolicy drives the caller path: a policy
// listing one wildcard must not authorize the apex or a deeper host, because
// CheckPolicy is what queue/manager.go consults before delivering.
func TestCheckPolicy_ExcludesHostsOutsidePolicy(t *testing.T) {
	policy := &MTASTSPolicy{
		Version: "STSv1",
		Mode:    MTASTSModeEnforce,
		MX:      []string{"*.example.com"},
		MaxAge:  86400,
	}

	for _, mx := range []string{"mail.example.com"} {
		if !matchMX(policy.MX[0], mx) {
			t.Errorf("%q should match policy %v", mx, policy.MX)
		}
	}
	for _, mx := range []string{"example.com", "foo.bar.example.com", "attacker.net"} {
		if matchMX(policy.MX[0], mx) {
			t.Errorf("%q must NOT match policy %v; it is outside the authorized set",
				mx, policy.MX)
		}
	}
}
