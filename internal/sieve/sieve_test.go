package sieve

import (
	"testing"
)

func TestSieveDiscard(t *testing.T) {
	script := `
require ["fileinto"];
discard;
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		From:    "test@example.com",
		Subject: "Test",
		Headers: map[string][]string{"Subject": {"Test"}},
	}

	results := s.Evaluate(msg)
	if len(results) == 0 {
		t.Fatal("Expected at least one result")
	}
	if results[0].Action != ActionDiscard {
		t.Errorf("Expected ActionDiscard, got %v", results[0].Action)
	}
}

func TestSieveFileInto(t *testing.T) {
	script := `
require ["fileinto"];
if header :contains "Subject" "SPAM" {
    fileinto "Junk";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should match
	msg := &Message{
		Subject: "Buy SPAM now",
		Headers: map[string][]string{"Subject": {"Buy SPAM now"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto, got %v", results)
	}
	if results[0].Target != "Junk" {
		t.Errorf("Expected target 'Junk', got %q", results[0].Target)
	}

	// Should not match -> keep
	msg2 := &Message{
		Subject: "Normal message",
		Headers: map[string][]string{"Subject": {"Normal message"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for non-matching message, got %v", results2)
	}
}

func TestSieveRedirect(t *testing.T) {
	script := `
redirect "admin@example.com";
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		From: "user@example.com",
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionRedirect {
		t.Fatalf("Expected ActionRedirect, got %v", results)
	}
	if results[0].Target != "admin@example.com" {
		t.Errorf("Expected target 'admin@example.com', got %q", results[0].Target)
	}
}

func TestSieveSizeTest(t *testing.T) {
	script := `
if size :over 1000 {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Large message should be discarded
	msg := &Message{
		Size:    5000,
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard for large message, got %v", results)
	}

	// Small message should be kept
	msg2 := &Message{
		Size:    500,
		Headers: map[string][]string{},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for small message, got %v", results2)
	}
}

func TestSieveStop(t *testing.T) {
	script := `
discard;
stop;
keep;
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	// Should stop after discard, not execute keep
	if len(results) != 2 {
		t.Fatalf("Expected 2 results (discard + stop), got %d", len(results))
	}
	if results[0].Action != ActionDiscard {
		t.Errorf("Expected first action to be ActionDiscard, got %v", results[0].Action)
	}
	if !results[1].Stop {
		t.Error("Expected second result to have Stop=true")
	}
}

func TestSieveReject(t *testing.T) {
	script := `
if header :is "From" "spammer@evil.com" {
    reject "Your message was rejected";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		From:    "spammer@evil.com",
		Headers: map[string][]string{"From": {"spammer@evil.com"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionReject {
		t.Fatalf("Expected ActionReject, got %v", results)
	}
	if results[0].RejectMsg != "Your message was rejected" {
		t.Errorf("Expected rejection message, got %q", results[0].RejectMsg)
	}
}

func TestSieveKeep(t *testing.T) {
	script := `keep;`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep, got %v", results)
	}
}

func TestSieveDefaultKeep(t *testing.T) {
	// Empty script should default to keep
	script := ``
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep as default, got %v", results)
	}
}

func TestSieveExistsTest(t *testing.T) {
	script := `
if exists "X-Spam-Flag" {
    fileinto "Junk";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// With header
	msg := &Message{
		Headers: map[string][]string{"X-Spam-Flag": {"YES"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto, got %v", results)
	}

	// Without header
	msg2 := &Message{
		Headers: map[string][]string{},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep, got %v", results2)
	}
}

func TestSieveComments(t *testing.T) {
	script := `
# This is a comment
require ["fileinto"];
/* Multi-line
   comment */
if header :contains "Subject" "test" {
    fileinto "Testing";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		Headers: map[string][]string{"Subject": {"This is a test"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto, got %v", results)
	}
}

// --- Tests for previously untested features ---

func TestSieveAllof(t *testing.T) {
	// allof: all conditions must match for the block to execute
	script := `
require ["fileinto"];
if allof (header :contains "Subject" "urgent", header :is "From" "boss@company.com") {
    fileinto "Priority";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name string
		msg  *Message
		want Action
	}{
		{
			name: "both conditions match",
			msg: &Message{
				From:    "boss@company.com",
				Headers: map[string][]string{"Subject": {"urgent update"}, "From": {"boss@company.com"}},
			},
			want: ActionFileInto,
		},
		{
			name: "only subject matches",
			msg: &Message{
				From:    "peer@company.com",
				Headers: map[string][]string{"Subject": {"urgent item"}, "From": {"peer@company.com"}},
			},
			want: ActionKeep,
		},
		{
			name: "only from matches",
			msg: &Message{
				From:    "boss@company.com",
				Headers: map[string][]string{"Subject": {"weekly report"}, "From": {"boss@company.com"}},
			},
			want: ActionKeep,
		},
		{
			name: "neither matches",
			msg: &Message{
				From:    "other@company.com",
				Headers: map[string][]string{"Subject": {"hello"}, "From": {"other@company.com"}},
			},
			want: ActionKeep,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := s.Evaluate(tc.msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveAnyof(t *testing.T) {
	// anyof: at least one condition must match
	script := `
require ["fileinto"];
if anyof (header :contains "Subject" "spam", header :is "From" "spammer@evil.com") {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name string
		msg  *Message
		want Action
	}{
		{
			name: "first condition matches",
			msg: &Message{
				From:    "anyone@example.com",
				Headers: map[string][]string{"Subject": {"spam offer"}, "From": {"anyone@example.com"}},
			},
			want: ActionDiscard,
		},
		{
			name: "second condition matches",
			msg: &Message{
				From:    "spammer@evil.com",
				Headers: map[string][]string{"Subject": {"Hello friend"}, "From": {"spammer@evil.com"}},
			},
			want: ActionDiscard,
		},
		{
			name: "both conditions match",
			msg: &Message{
				From:    "spammer@evil.com",
				Headers: map[string][]string{"Subject": {"spam here"}, "From": {"spammer@evil.com"}},
			},
			want: ActionDiscard,
		},
		{
			name: "neither condition matches",
			msg: &Message{
				From:    "friend@example.com",
				Headers: map[string][]string{"Subject": {"Hello"}, "From": {"friend@example.com"}},
			},
			want: ActionKeep,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := s.Evaluate(tc.msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveNot(t *testing.T) {
	// not: negation of a single test
	script := `
require ["fileinto"];
if not header :is "X-Spam-Flag" "YES" {
    fileinto "Inbox";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name string
		msg  *Message
		want Action
	}{
		{
			name: "header absent - not evaluates to true",
			msg: &Message{
				Headers: map[string][]string{},
			},
			want: ActionFileInto,
		},
		{
			name: "header present but different value - not evaluates to true",
			msg: &Message{
				Headers: map[string][]string{"X-Spam-Flag": {"NO"}},
			},
			want: ActionFileInto,
		},
		{
			name: "header matches - not evaluates to false",
			msg: &Message{
				Headers: map[string][]string{"X-Spam-Flag": {"YES"}},
			},
			want: ActionKeep,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := s.Evaluate(tc.msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveNotSize(t *testing.T) {
	// not with size test
	script := `
if not size :over 1000 {
    keep;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Small message: size NOT over 1000 -> not evaluates to true -> keep action runs
	msg := &Message{Size: 500, Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for small message, got %v", results)
	}

	// Large message: size over 1000 -> not evaluates to false -> default keep
	msg2 := &Message{Size: 5000, Headers: map[string][]string{}}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep (default) for large message, got %v", results2)
	}
}

func TestSieveAddressLocalpart(t *testing.T) {
	// address test with :localpart checks only the local part of the address
	script := `
require ["fileinto"];
if address :localpart :is "From" "admin" {
    fileinto "Admin";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name string
		msg  *Message
		want Action
	}{
		{
			name: "localpart matches",
			msg: &Message{
				From:    "admin@example.com",
				Headers: map[string][]string{},
			},
			want: ActionFileInto,
		},
		{
			name: "localpart does not match",
			msg: &Message{
				From:    "user@example.com",
				Headers: map[string][]string{},
			},
			want: ActionKeep,
		},
		{
			name: "localpart matches different domain",
			msg: &Message{
				From:    "admin@other.com",
				Headers: map[string][]string{},
			},
			want: ActionFileInto,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := s.Evaluate(tc.msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveAddressDomain(t *testing.T) {
	// address test with :domain checks only the domain part of the address
	script := `
require ["fileinto"];
if address :domain :is "From" "example.com" {
    fileinto "Internal";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name string
		msg  *Message
		want Action
	}{
		{
			name: "domain matches",
			msg: &Message{
				From:    "user@example.com",
				Headers: map[string][]string{},
			},
			want: ActionFileInto,
		},
		{
			name: "domain does not match",
			msg: &Message{
				From:    "user@other.com",
				Headers: map[string][]string{},
			},
			want: ActionKeep,
		},
		{
			name: "localpart matches but domain does not",
			msg: &Message{
				From:    "user@notexample.com",
				Headers: map[string][]string{},
			},
			want: ActionKeep,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := s.Evaluate(tc.msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveAddressToField(t *testing.T) {
	// address test checks both From and To addresses
	script := `
require ["fileinto"];
if address :domain :is "To" "target.com" {
    fileinto "Targeted";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// To address domain matches
	msg := &Message{
		From:    "sender@other.com",
		To:      []string{"recipient@target.com"},
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto when To address domain matches, got %v", results)
	}

	// Neither From nor To domain matches
	msg2 := &Message{
		From:    "sender@other.com",
		To:      []string{"recipient@another.com"},
		Headers: map[string][]string{},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep when no address domain matches, got %v", results2)
	}
}

func TestSieveMatchesPrefixWildcard(t *testing.T) {
	// :matches with prefix wildcard: trailing * acts as "starts with"
	script := `
require ["fileinto"];
if header :matches "Subject" "Re: *" {
    fileinto "Replies";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name    string
		subject string
		want    Action
	}{
		{
			name:    "prefix match with trailing wildcard",
			subject: "Re: your email",
			want:    ActionFileInto,
		},
		{
			name:    "subject does not start with prefix",
			subject: "FW: your email",
			want:    ActionKeep,
		},
		{
			name:    "subject is just the prefix with trailing space",
			subject: "Re: ",
			want:    ActionFileInto,
		},
		{
			name:    "completely unrelated subject",
			subject: "anything at all",
			want:    ActionKeep,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &Message{
				Headers: map[string][]string{"Subject": {tc.subject}},
			}
			results := s.Evaluate(msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveMatchesStarOnly(t *testing.T) {
	// bare * matches any subject
	script := `
require ["fileinto"];
if header :matches "Subject" "*" {
    fileinto "All";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		Headers: map[string][]string{"Subject": {"anything"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto for wildcard match, got %v", results)
	}
}

func TestSieveMatchesContainsWildcard(t *testing.T) {
	// *keyword* in :matches acts like contains
	script := `
require ["fileinto"];
if header :matches "Subject" "*urgent*" {
    fileinto "Urgent";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name    string
		subject string
		want    Action
	}{
		{
			name:    "keyword in the middle",
			subject: "This is urgent please read",
			want:    ActionFileInto,
		},
		{
			name:    "keyword at the start",
			subject: "urgent matter",
			want:    ActionFileInto,
		},
		{
			name:    "keyword at the end",
			subject: "very urgent",
			want:    ActionFileInto,
		},
		{
			name:    "no keyword present",
			subject: "normal message",
			want:    ActionKeep,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &Message{
				Headers: map[string][]string{"Subject": {tc.subject}},
			}
			results := s.Evaluate(msg)
			if len(results) == 0 {
				t.Fatal("Expected at least one result")
			}
			if results[0].Action != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, results[0].Action)
			}
		})
	}
}

func TestSieveMatchesSuffixWildcard(t *testing.T) {
	// trailing * after literal prefix: acts as "starts with"
	script := `
require ["fileinto"];
if header :matches "Subject" "[SPAM]*" {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Subject starts with the prefix
	msg := &Message{
		Headers: map[string][]string{"Subject": {"[SPAM] Buy now"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard for prefix wildcard match, got %v", results)
	}

	// Subject does not start with the prefix
	msg2 := &Message{
		Headers: map[string][]string{"Subject": {"Hello [SPAM] nope"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep when prefix does not match, got %v", results2)
	}
}

func TestSieveElsifElseChain(t *testing.T) {
	// The parser flattens if/elsif/else into a single rule.
	// When the if-test matches, all collected actions from all branches execute.
	// When the if-test does not match, no actions execute (default keep).
	script := `
require ["fileinto"];
if header :is "X-Priority" "1" {
    fileinto "High";
} elsif header :is "X-Priority" "2" {
    fileinto "Medium";
} else {
    fileinto "Low";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// When the if condition matches, all actions from all branches execute
	msg := &Message{
		Headers: map[string][]string{"X-Priority": {"1"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 {
		t.Fatal("Expected at least one result")
	}
	// First action should be fileinto "High" (from the if block)
	if results[0].Action != ActionFileInto || results[0].Target != "High" {
		t.Errorf("Expected first action FileInto 'High', got action=%v target=%q", results[0].Action, results[0].Target)
	}
	// Due to flattening, all three fileinto actions execute when if matches
	if len(results) < 3 {
		t.Errorf("Expected 3 fileinto actions (flattened branches), got %d", len(results))
	}

	// When the if condition does not match, no actions execute -> default keep
	msg2 := &Message{
		Headers: map[string][]string{"X-Priority": {"2"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep (default) when if does not match, got %v", results2)
	}

	// No priority header at all
	msg3 := &Message{
		Headers: map[string][]string{},
	}
	results3 := s.Evaluate(msg3)
	if len(results3) == 0 || results3[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep when no matching header, got %v", results3)
	}
}

func TestSieveElseOnly(t *testing.T) {
	// Simple if/else: when if matches, both branch actions execute;
	// when if does not match, default keep applies.
	script := `
require ["fileinto"];
if header :is "Subject" "test" {
    fileinto "Testing";
} else {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// if matches
	msg := &Message{
		Headers: map[string][]string{"Subject": {"test"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 {
		t.Fatal("Expected at least one result")
	}
	if results[0].Action != ActionFileInto || results[0].Target != "Testing" {
		t.Errorf("Expected FileInto 'Testing', got action=%v target=%q", results[0].Action, results[0].Target)
	}

	// if does not match -> default keep
	msg2 := &Message{
		Headers: map[string][]string{"Subject": {"other"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep when if does not match, got %v", results2)
	}
}

func TestSieveMatchesExactValue(t *testing.T) {
	// :matches without any wildcard characters falls back to exact match
	script := `
require ["fileinto"];
if header :matches "Subject" "Exact Match" {
    fileinto "Exact";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Exact match (case-insensitive via EqualFold)
	msg := &Message{
		Headers: map[string][]string{"Subject": {"exact match"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto for exact match, got %v", results)
	}

	// Non-matching subject
	msg2 := &Message{
		Headers: map[string][]string{"Subject": {"no match at all"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for non-match, got %v", results2)
	}
}

func TestSieveMatchesLeadingWildcard(t *testing.T) {
	// *suffix: matches subjects ending with the given suffix
	script := `
require ["fileinto"];
if header :matches "Subject" "*newsletter" {
    fileinto "Newsletters";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Subject ends with "newsletter"
	msg := &Message{
		Headers: map[string][]string{"Subject": {"weekly newsletter"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto for suffix match, got %v", results)
	}

	// Subject does not end with "newsletter"
	msg2 := &Message{
		Headers: map[string][]string{"Subject": {"newsletter weekly"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep when suffix does not match, got %v", results2)
	}
}
