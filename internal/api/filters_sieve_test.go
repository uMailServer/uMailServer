package api

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

func runFilterScript(t *testing.T, filters []*EmailFilter, msg string) []sieve.Action {
	t.Helper()
	src := FiltersToSieve(filters)
	if src == "" {
		t.Fatal("FiltersToSieve returned no script")
	}
	script, err := sieve.Compile(src)
	if err != nil {
		t.Fatalf("generated script does not compile: %v\n%s", err, src)
	}
	headers := map[string][]string{}
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		if k, v, ok := strings.Cut(line, ": "); ok {
			headers[k] = append(headers[k], v)
		}
	}
	actions, err := sieve.NewInterpreter(script).Execute(&sieve.MessageContext{
		From: "s@x.example", To: []string{"alice@test.com"}, Headers: headers,
		Body: []byte(msg), Size: int64(len(msg)), BodyIsFullMessage: true,
	})
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, src)
	}
	return actions
}

func fileintoTargets(actions []sieve.Action) []string {
	var out []string
	for _, a := range actions {
		if f, ok := a.(sieve.FileintoAction); ok {
			out = append(out, f.Folder)
		}
	}
	return out
}

func moveFilter(field, op, value, target string) *EmailFilter {
	return &EmailFilter{
		Enabled:    true,
		Conditions: []FilterCondition{{Field: field, Operator: op, Value: value, HeaderName: "X-List"}},
		Actions:    []FilterAction{{Type: "move", Target: target}},
	}
}

const filterTestMsg = "From: Bob <bob@ext.example>\r\nTo: alice@test.com\r\nCc: team@test.com\r\n" +
	"Subject: Report *final* \"Q3\"\r\nX-List: dev\r\n\r\nquarterly numbers inside\r\n"

func TestFiltersToSieveOperatorsAndFields(t *testing.T) {
	cases := []struct {
		name  string
		f     *EmailFilter
		match bool
	}{
		{"from contains", moveFilter("from", "contains", "BOB@ext", "W"), true},
		{"to covers cc", moveFilter("to", "equals", "team@test.com", "W"), true},
		{"subject startsWith literal star", moveFilter("subject", "startsWith", "report *final*", "W"), true},
		{"startsWith is not a glob", moveFilter("subject", "startsWith", "Report *x", "W"), false},
		{"subject endsWith quote", moveFilter("subject", "endsWith", `"Q3"`, "W"), true},
		{"header equals", moveFilter("header", "equals", "dev", "W"), true},
		{"body contains", moveFilter("body", "contains", "numbers", "W"), true},
		{"regex", moveFilter("subject", "matches", `^report \*final\*`, "W"), true},
		{"no match", moveFilter("subject", "contains", "invoice", "W"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fileintoTargets(runFilterScript(t, []*EmailFilter{tc.f}, filterTestMsg))
			if (len(got) == 1) != tc.match {
				t.Fatalf("fileinto = %v, want match=%v\n%s", got, tc.match, FiltersToSieve([]*EmailFilter{tc.f}))
			}
		})
	}
}

func TestFiltersToSieveMatchAllAndDisabled(t *testing.T) {
	all := &EmailFilter{
		Enabled:  true,
		MatchAll: true,
		Conditions: []FilterCondition{
			{Field: "from", Operator: "contains", Value: "bob"},
			{Field: "subject", Operator: "contains", Value: "invoice"},
		},
		Actions: []FilterAction{{Type: "move", Target: "W"}},
	}
	if got := fileintoTargets(runFilterScript(t, []*EmailFilter{all}, filterTestMsg)); len(got) != 0 {
		t.Fatalf("matchAll with one failing condition filed into %v", got)
	}
	all.MatchAll = false
	if got := fileintoTargets(runFilterScript(t, []*EmailFilter{all}, filterTestMsg)); len(got) != 1 {
		t.Fatalf("matchAny filed into %v, want W", got)
	}

	all.Enabled = false
	if src := FiltersToSieve([]*EmailFilter{all}); src != "" {
		t.Fatalf("disabled filter produced a script:\n%s", src)
	}

	// A condition that cannot be expressed must not widen a matchAll filter.
	bad := &EmailFilter{
		Enabled: true, MatchAll: true,
		Conditions: []FilterCondition{
			{Field: "from", Operator: "contains", Value: "bob"},
			{Field: "subject", Operator: "matches", Value: "("},
		},
		Actions: []FilterAction{{Type: "move", Target: "W"}},
	}
	if src := FiltersToSieve([]*EmailFilter{bad}); src != "" {
		t.Fatalf("matchAll filter with an invalid regex produced a script:\n%s", src)
	}
}

func TestFiltersToSieveActions(t *testing.T) {
	f := &EmailFilter{
		Enabled:    true,
		Conditions: []FilterCondition{{Field: "from", Operator: "contains", Value: "bob"}},
		Actions: []FilterAction{
			{Type: "copy", Target: "Archive"},
			{Type: "flag"},
			{Type: "forward", ForwardTo: "carol@ext.example"},
			{Type: "delete"},
		},
	}
	actions := runFilterScript(t, []*EmailFilter{f}, filterTestMsg)
	var redirect bool
	for _, a := range actions {
		switch v := a.(type) {
		case sieve.FileintoAction:
			if !strings.Contains(v.Flags, `\Flagged`) {
				t.Errorf("fileinto %s flags = %q, want \\Flagged", v.Folder, v.Flags)
			}
		case sieve.RedirectAction:
			redirect = v.Address == "carol@ext.example" && v.Copy
		}
	}
	if got := fileintoTargets(actions); len(got) != 2 || got[0] != "Archive" || got[1] != "Trash" {
		t.Fatalf("fileinto = %v, want [Archive Trash]", got)
	}
	if !redirect {
		t.Fatalf("forward did not redirect a copy: %#v", actions)
	}
}

func TestValidateFilterRule(t *testing.T) {
	ok := []FilterCondition{{Field: "from", Operator: "contains", Value: "x"}}
	for name, tc := range map[string]struct {
		conds   []FilterCondition
		actions []FilterAction
	}{
		"unknown field":    {[]FilterCondition{{Field: "date", Operator: "contains", Value: "x"}}, nil},
		"unknown operator": {[]FilterCondition{{Field: "from", Operator: "like", Value: "x"}}, nil},
		"bad regex":        {[]FilterCondition{{Field: "from", Operator: "matches", Value: "("}}, nil},
		"bad header name":  {[]FilterCondition{{Field: "header", HeaderName: "X Bad:", Operator: "contains", Value: "x"}}, nil},
		"unknown action":   {ok, []FilterAction{{Type: "explode"}}},
		"move no target":   {ok, []FilterAction{{Type: "move"}}},
		"bad forward":      {ok, []FilterAction{{Type: "forward", ForwardTo: "not-an-address"}}},
	} {
		if msg := validateFilterRule(tc.conds, tc.actions); msg == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	if msg := validateFilterRule(ok, []FilterAction{{Type: "move", Target: "Work"}, {Type: "forward", ForwardTo: "a@b.example"}}); msg != "" {
		t.Errorf("valid rule rejected: %s", msg)
	}
}

// F7777: equals/startsWith/endsWith on From/To compared the whole header, so
// "Bob <bob@ext.example>" never matched an address condition.
func TestFiltersFromToMatchTheAddress(t *testing.T) {
	msg := "From: Bob <bob@ext.example>\r\nTo: Alice <alice@test.com>, carol@test.com\r\nCc: \"Team\" <team@corp.example>\r\nSubject: s\r\n\r\nbody\r\n"
	for _, c := range []struct {
		field, op, value string
		match            bool
	}{
		{"from", "endsWith", "@ext.example", true},
		{"from", "endsWith", "@EXT.example", true},
		{"from", "equals", "bob@ext.example", true},
		{"from", "startsWith", "bob@", true},
		{"from", "endsWith", "@other.example", false},
		{"from", "equals", "Bob", false},
		{"from", "contains", "Bob", true}, // the display name is still searchable
		{"to", "equals", "alice@test.com", true},
		{"to", "equals", "carol@test.com", true},
		{"to", "endsWith", "@corp.example", true}, // Cc
		{"to", "startsWith", "nobody@", false},
	} {
		got := fileintoTargets(runFilterScript(t, []*EmailFilter{moveFilter(c.field, c.op, c.value, "W")}, msg))
		if (len(got) == 1) != c.match {
			t.Errorf("%s %s %q: fileinto %v, want match=%v", c.field, c.op, c.value, got, c.match)
		}
	}
}

// F7778: one filter whose escaped :matches key exceeded the Sieve pattern
// limit made the user's whole script fail to compile, disabling every filter.
func TestFilterOverlongPatternDoesNotDisableOthers(t *testing.T) {
	good := moveFilter("from", "contains", "bob", "W")
	long := moveFilter("subject", "startsWith", strings.Repeat("*", 600), "X")
	if msg := validateFilterRule(long.Conditions, long.Actions); msg == "" {
		t.Fatal("an over-long startsWith value was accepted")
	}
	// A filter stored before the check is skipped; the others still apply.
	if got := fileintoTargets(runFilterScript(t, []*EmailFilter{long, good}, filterTestMsg)); len(got) != 1 || got[0] != "W" {
		t.Fatalf("fileinto %v, want [W]", got)
	}
	// Just under the limit is accepted and compiles.
	ok := moveFilter("subject", "startsWith", strings.Repeat("a", 1000), "X")
	if msg := validateFilterRule(ok.Conditions, ok.Actions); msg != "" {
		t.Fatalf("a 1000-byte plain value was rejected: %s", msg)
	}
	runFilterScript(t, []*EmailFilter{ok, good}, filterTestMsg)
}
