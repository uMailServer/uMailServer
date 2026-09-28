package storage

// Regression tests for NormalizeSubject prefix stripping.
//
// NormalizeSubject is used by findThreadBySubject to group messages into
// conversations: any two messages of the same conversation must normalize to
// the same string, regardless of how many Re:/Fwd: reply/forward prefixes they
// carry and in whatever order those prefixes appear.
//
// Bug (internal/storage/database.go NormalizeSubject): the function removed
// Re:/Re[n]: prefixes in one loop to exhaustion, THEN removed Fwd:/FW:
// prefixes in a second loop. Because the Re-loop finished before the Fwd-loop
// began, a subject with an Fwd prefix in FRONT of a Re prefix (e.g.
// "Fwd: Re: Topic") kept its inner Re: prefix and normalized to "Re: Topic",
// which differs from "Re: Topic" and "Topic" — fragmenting the thread. The fix
// strips any leading Re/Fwd prefix in a single loop so order does not matter.

import "testing"

// TestNormalizeSubject_StripsInterleavedPrefixes asserts that all leading
// Re/Fwd prefixes are stripped regardless of their order.
func TestNormalizeSubject_StripsInterleavedPrefixes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"fwd then re", "Fwd: Re: Topic", "Topic"},
		{"fwd then re lower", "Fwd: Re: Project Update", "Project Update"},
		{"fw then re", "FW: Re: Topic", "Topic"},
		{"fwd then re bracketed", "Fwd: Re[2]: Topic", "Topic"},
		{"fwd then re repeated", "Fwd: Re: Re: Topic", "Topic"},
		{"re then fwd", "Re: Fwd: Topic", "Topic"},
		{"fwd repeated", "Fwd: Fwd: Topic", "Topic"},
		{"re repeated", "Re: Re: Topic", "Topic"},
		{"fwd fw re mixed", "Fwd: FW: Re: Topic", "Topic"},
		{"only prefixes", "Fwd: Re:", ""},
		{"whitespace between", "Fwd:  Re:   Topic", "Topic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSubject(tt.in); got != tt.want {
				t.Errorf("NormalizeSubject(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormalizeSubject_PreservesNonPrefixes is a boundary/control: subjects
// without leading Re/Fwd markers (or where the marker lacks its colon) must be
// returned unchanged.
func TestNormalizeSubject_PreservesNonPrefixes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Topic", "Topic"},
		{"RE meeting notes", "RE meeting notes"},           // "RE" without ':' is not a prefix
		{"report: re: review", "report: re: review"},       // prefix not at start
		{"Re[broken Topic", "Re[broken Topic"},             // malformed Re[n] (no "]:")
		{"My Fwd: notes", "My Fwd: notes"},                // not at start
		{"", ""},                                           // empty
	}
	for _, tt := range tests {
		if got := NormalizeSubject(tt.in); got != tt.want {
			t.Errorf("NormalizeSubject(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestNormalizeSubject_SameConversationCollapses asserts the actual threading
// contract: every prefix variant of one conversation normalizes identically, so
// findThreadBySubject will group them into a single thread.
func TestNormalizeSubject_SameConversationCollapses(t *testing.T) {
	variants := []string{
		"Quarterly Report",
		"Re: Quarterly Report",
		"Fwd: Quarterly Report",
		"FW: Quarterly Report",
		"Re[2]: Quarterly Report",
		"Re: Fwd: Quarterly Report",
		"Fwd: Re: Quarterly Report",
		"FW: Re: Re: Quarterly Report",
	}
	want := NormalizeSubject("Quarterly Report")
	for _, v := range variants {
		if got := NormalizeSubject(v); got != want {
			t.Errorf("NormalizeSubject(%q) = %q, want %q (same conversation must normalize identically)",
				v, got, want)
		}
	}
}
