package db

import (
	"testing"
)

// TestSplitEmail exercises all branches of the unexported splitEmail function.
func TestSplitEmail(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		wantLocal string
		wantDom   string
	}{
		{"normal address", "user@example.com", "user", "example.com"},
		{"no @ sign", "userexample", "userexample", ""},
		{"multiple @ signs uses last", "first@middle@domain.com", "first@middle", "domain.com"},
		{"empty string", "", "", ""},
		{"@ at start", "@domain.com", "", "domain.com"},
		{"@ at end", "user@", "user", ""},
		{"subaddressed", "user+tag@domain.com", "user+tag", "domain.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, domain := splitEmail(tt.email)
			if local != tt.wantLocal {
				t.Errorf("splitEmail(%q) local = %q, want %q", tt.email, local, tt.wantLocal)
			}
			if domain != tt.wantDom {
				t.Errorf("splitEmail(%q) domain = %q, want %q", tt.email, domain, tt.wantDom)
			}
		})
	}
}
