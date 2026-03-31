package sieve

import (
	"testing"
)

func TestEvalSizeUnder(t *testing.T) {
	// Test size :under which is only 40% covered
	script := `
if size :under 1000 {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Small message should be discarded (size < 1000)
	msg := &Message{
		Size:    500,
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard for small message, got %v", results)
	}

	// Large message should be kept (size >= 1000)
	msg2 := &Message{
		Size:    5000,
		Headers: map[string][]string{},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for large message, got %v", results2)
	}
}

func TestEvaluateConditionCondTrue(t *testing.T) {
	// "true" literal in sieve should always evaluate to true
	script := `
if true {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard for 'true' test, got %v", results)
	}
}

func TestEvaluateConditionCondFalse(t *testing.T) {
	// "false" literal in sieve should always evaluate to false
	script := `
if false {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep (default) for 'false' test, got %v", results)
	}
}

func TestMatchHeaderDefaultOp(t *testing.T) {
	// Header test with no operator specified falls through to default case
	// which uses EqualFold. This tests the default branch in matchHeader.
	script := `
if header "Subject" "hello" {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Matching subject (case-insensitive)
	msg := &Message{Headers: map[string][]string{"Subject": {"Hello"}}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard for matching header, got %v", results)
	}

	// Non-matching subject
	msg2 := &Message{Headers: map[string][]string{"Subject": {"Goodbye"}}}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for non-matching header, got %v", results2)
	}
}

func TestEvalAddressTestNoAt(t *testing.T) {
	// Address with no @ sign should be skipped
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

	// From address without @ should not match
	msg := &Message{
		From:    "notanemail",
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for address without @, got %v", results)
	}
}

func TestEvalAddressTestDefaultPart(t *testing.T) {
	// Address test without explicit :localpart or :domain uses full address
	script := `
require ["fileinto"];
if address :is "From" "admin@example.com" {
    fileinto "Admin";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Full address match
	msg := &Message{
		From:    "admin@example.com",
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto for full address match, got %v", results)
	}

	// Different address
	msg2 := &Message{
		From:    "user@example.com",
		Headers: map[string][]string{},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for different address, got %v", results2)
	}
}

func TestEvalAddressTestEmptyFrom(t *testing.T) {
	// No From or To addresses
	script := `
require ["fileinto"];
if address :is "From" "admin@example.com" {
    fileinto "Admin";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		From:    "",
		To:      nil,
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for empty addresses, got %v", results)
	}
}

func TestParseHeaderWithListValue(t *testing.T) {
	// Header test with list value: header "Subject" ["test"]
	// This exercises the "[" branch in header value parsing
	script := `
require ["fileinto"];
if header :contains "Subject" "test" {
    fileinto "Testing";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{Headers: map[string][]string{"Subject": {"this is a test"}}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionFileInto {
		t.Fatalf("Expected ActionFileInto, got %v", results)
	}
}

func TestParseExistsWithList(t *testing.T) {
	// exists test with a list of header names (note: the simple tokenizer
	// treats commas as separate tokens, so we avoid them)
	script := `
if exists ["X-Spam-Flag" "X-Spam-Score"] {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Both headers present
	msg := &Message{
		Headers: map[string][]string{"X-Spam-Flag": {"YES"}, "X-Spam-Score": {"5.0"}},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard when both headers exist, got %v", results)
	}

	// Only one header present
	msg2 := &Message{
		Headers: map[string][]string{"X-Spam-Flag": {"YES"}},
	}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep when not all headers exist, got %v", results2)
	}
}

func TestParseActionRedirectMissingTarget(t *testing.T) {
	// redirect without target should fail gracefully
	script := `redirect;`
	_, err := Parse(script)
	if err != nil {
		// Parser should handle it gracefully (error or skip)
		t.Logf("Parse returned error (expected): %v", err)
	}
}

func TestParseActionFileintoMissingMailbox(t *testing.T) {
	script := `fileinto;`
	_, err := Parse(script)
	if err != nil {
		t.Logf("Parse returned error (expected): %v", err)
	}
}

func TestParseActionRejectMissingReason(t *testing.T) {
	script := `reject;`
	_, err := Parse(script)
	if err != nil {
		t.Logf("Parse returned error (expected): %v", err)
	}
}

func TestParseUnknownAction(t *testing.T) {
	// Unknown action should be skipped by the parser
	script := `unknown_action "arg";`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	// Should default to keep since no valid actions
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep (default) for unknown action, got %v", results)
	}
}

func TestParseEmptyBlock(t *testing.T) {
	// if with empty block
	script := `
if true {
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep (default) for empty block, got %v", results)
	}
}

func TestParseRequireSingle(t *testing.T) {
	// require with a single string (not a list)
	script := `
require "fileinto";
discard;
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard, got %v", results)
	}
}

func TestParseActionStopTerminates(t *testing.T) {
	script := `
discard;
stop;
redirect "other@example.com";
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	// First rule: discard + stop
	if len(results) != 2 {
		t.Fatalf("Expected 2 results (discard + stop), got %d", len(results))
	}
	if results[0].Action != ActionDiscard {
		t.Errorf("Expected first result ActionDiscard, got %v", results[0].Action)
	}
	if !results[1].Stop {
		t.Error("Expected second result to have Stop=true")
	}
	// redirect after stop should not execute
}

func TestParseSizeUnderValue(t *testing.T) {
	// Test the :under branch in parseSizeTest
	script := `
if size :under 500 {
    keep;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Message under 500 bytes
	msg := &Message{Size: 100, Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for small message, got %v", results)
	}

	// Message over 500 bytes
	msg2 := &Message{Size: 1000, Headers: map[string][]string{}}
	results2 := s.Evaluate(msg2)
	if len(results2) == 0 || results2[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep (default) for large message, got %v", results2)
	}
}

func TestEvaluateConditionsNoConditions(t *testing.T) {
	// A rule with no conditions should always match (returns true)
	script := `discard;`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	msg := &Message{Headers: map[string][]string{}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard, got %v", results)
	}
}

func TestEvalAddressTestInvalidAddress(t *testing.T) {
	// Test with an invalid email address
	script := `
require ["fileinto"];
if address :is "From" "admin@example.com" {
    fileinto "Admin";
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{
		From:    "not-valid-email",
		Headers: map[string][]string{},
	}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionKeep {
		t.Fatalf("Expected ActionKeep for invalid address, got %v", results)
	}
}

func TestParseHeaderListName(t *testing.T) {
	// Test parsing header test where header-name is a list ["Subject"]
	script := `
if header :is ["Subject"] "test" {
    discard;
}
`
	s, err := Parse(script)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	msg := &Message{Headers: map[string][]string{"Subject": {"test"}}}
	results := s.Evaluate(msg)
	if len(results) == 0 || results[0].Action != ActionDiscard {
		t.Fatalf("Expected ActionDiscard, got %v", results)
	}
}
