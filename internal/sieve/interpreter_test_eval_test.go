package sieve

import "testing"

// TestInterpreter_TestsAreNotImplicitlyTrue guards F5006: "false", a size
// test without a quantifier, and unparseable tests used to evaluate as true,
// so e.g. `if size :over 1000000 { discard; }` discarded every message.
func TestInterpreter_TestsAreNotImplicitlyTrue(t *testing.T) {
	cases := []struct {
		src     string
		size    int64
		discard bool
		wantErr bool
	}{
		{`if false { discard; }`, 100, false, false},
		{`if true { discard; }`, 100, true, false},
		{`if size :over 1000000 { discard; }`, 100, false, false},
		{`if size :over 1000000 { discard; }`, 1000001, true, false},
		{`if size :over 1K { discard; }`, 1024, false, false},
		{`if size :over 1K { discard; }`, 1025, true, false},
		{`if size :under 1M { discard; }`, 100, true, false},
		{`if bogus { discard; }`, 100, false, true},
		{`if size :over { discard; }`, 100, false, true},
	}
	for _, c := range cases {
		s, err := NewParser(c.src).Parse()
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		acts, err := NewInterpreter(s).Execute(&MessageContext{Headers: map[string][]string{}, Size: c.size})
		if (err != nil) != c.wantErr {
			t.Errorf("%q size=%d: err=%v, wantErr=%v", c.src, c.size, err, c.wantErr)
			continue
		}
		discarded := false
		for _, a := range acts {
			if _, ok := a.(DiscardAction); ok {
				discarded = true
			}
		}
		if discarded != c.discard {
			t.Errorf("%q size=%d: discarded=%v, want %v", c.src, c.size, discarded, c.discard)
		}
	}
}
