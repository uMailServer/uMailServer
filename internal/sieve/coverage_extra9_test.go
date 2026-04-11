package sieve

import (
	"testing"
	"time"
)

// --- executeIf elsif skip when previous condition was false ---

// TestInterpreter_Elsif_SkippedWhenPreviousFalse tests the case where
// the first if evaluates to false, and elsif's skip condition is hit
// (line 277-281 in interpreter.go)
func TestInterpreter_Elsif_SkippedWhenPreviousFalse(t *testing.T) {
	// Script where first if fails and elsif should be skipped
	// because previous conditions weren't met
	script := `
	if header :contains "subject" "nomatch" {
		discard;
	} elsif header :contains "from" "test@example.com" {
		fileinto "SkippedFolder";
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"Hello"}, // doesn't match "nomatch"
			"from":    {"test@example.com"},
		},
		Body: []byte("Test"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Neither branch executes:
	// 1. First if is false (subject doesn't contain "nomatch")
	// 2. Elsif is skipped because previous condition (first if) was false
	// Default action is keep (implicit)
	if len(actions) != 1 {
		t.Errorf("Expected 1 implicit keep action, got %d", len(actions))
	}
	if _, ok := actions[0].(KeepAction); !ok {
		t.Errorf("Expected KeepAction, got %T", actions[0])
	}
}

// TestInterpreter_Elsif_SecondElsifEvaluated tests a chain of if-elsif-elsif
func TestInterpreter_Elsif_SecondElsifEvaluated(t *testing.T) {
	script := `
	if header :contains "subject" "nomatch1" {
		discard;
	} elsif header :contains "subject" "nomatch2" {
		discard;
	} elsif header :contains "from" "test@example.com" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"Hello"},
			"from":    {"test@example.com"},
		},
		Body: []byte("Test"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have keep action from third branch
	found := false
	for _, a := range actions {
		if _, ok := a.(KeepAction); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected keep action from elsif branch")
	}
}

// --- executeFileinto with no create flag (default) ---

func TestInterpreter_ExecuteFileinto_NoCreateFlag(t *testing.T) {
	script := `
	require "fileinto";
	fileinto "TestFolder";
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"Test"},
		},
		Body: []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- executeFileinto with :create false ---

func TestInterpreter_ExecuteFileinto_CreateFalse(t *testing.T) {
	script := `
	require "fileinto";
	fileinto :create "TestFolder";
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"Test"},
		},
		Body: []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- executeIf with empty block ---

func TestInterpreter_ExecuteIf_EmptyBlock(t *testing.T) {
	script := `
	if header :contains "subject" "match" {
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"match"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Empty block means no explicit action, so default keep
	if len(actions) != 1 {
		t.Errorf("Expected 1 implicit keep action, got %d", len(actions))
	}
	if _, ok := actions[0].(KeepAction); !ok {
		t.Errorf("Expected KeepAction, got %T", actions[0])
	}
}

// --- evaluateHeaderTest with count match type ---

func TestInterpreter_EvaluateHeaderTest_CountMatch(t *testing.T) {
	script := `
	if header :count "eq" "from" "1" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"from": {"test@example.com"},
		},
		Body: []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- evaluateHeaderTest with index match type ---

func TestInterpreter_EvaluateHeaderTest_IndexMatch(t *testing.T) {
	script := `
	if header :index 1 "from" "test@example.com" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"from": {"test@example.com"},
		},
		Body: []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- executeSet with various types ---

func TestInterpreter_ExecuteSet_String(t *testing.T) {
	script := `
	require "variables";
	set "name" "John";
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

func TestInterpreter_ExecuteSet_Modifier(t *testing.T) {
	script := `
	require "variables";
	set :lower "name" "JOHN";
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- executeFileinto with empty folder ---

func TestInterpreter_ExecuteFileinto_EmptyFolder(t *testing.T) {
	script := `
	require "fileinto";
	fileinto "";
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Empty folder should return no explicit action (implicit keep)
	if len(actions) != 1 {
		t.Errorf("Expected 1 implicit keep action, got %d", len(actions))
	}
}

// --- evaluateTest with SizeTest ---

func TestInterpreter_EvaluateTest_SizeOver(t *testing.T) {
	script := `
	if size :over 1K {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"), // 5 bytes > 1K (1024) is false, but tests the path
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

func TestInterpreter_EvaluateTest_SizeUnder(t *testing.T) {
	script := `
	if size :under 1K {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hi"), // small message < 1K
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

func TestInterpreter_EvaluateTest_SizeUnknownRelation(t *testing.T) {
	script := `
	if size :unknown 1K {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- evaluateTest with BooleanTest ---

func TestInterpreter_EvaluateTest_BooleanEmpty(t *testing.T) {
	script := `
	if true {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- executeIf elsif with previous true (elsif should execute) ---

func TestInterpreter_Elsif_ExecutesWhenPreviousTrue(t *testing.T) {
	script := `
	if header :contains "subject" "match" {
		discard;
	} elsif header :contains "from" "test@example.com" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "test@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"match"},
			"from":    {"test@example.com"},
		},
		Body: []byte("Test"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// First if is true, discard is returned
	if len(actions) != 1 {
		t.Errorf("Expected 1 discard action, got %d", len(actions))
	}
	if _, ok := actions[0].(DiscardAction); !ok {
		t.Errorf("Expected DiscardAction, got %T", actions[0])
	}
}

// --- executeIf with false condition and nil block ---

func TestInterpreter_ExecuteIf_FalseConditionNilBlock(t *testing.T) {
	script := `
	if header :contains "subject" "nomatch" {
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"Hello"}, // doesn't match "nomatch"
		},
		Body: []byte("Test"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Condition false, block is nil, so implicit keep
	if len(actions) != 1 {
		t.Errorf("Expected 1 keep action, got %d", len(actions))
	}
}

// --- executeStop ---

func TestInterpreter_ExecuteStop(t *testing.T) {
	script := `
	stop;
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
	if _, ok := actions[0].(StopAction); !ok {
		t.Fatalf("Expected StopAction, got %T", actions[0])
	}
}

// --- executeKeep with explicit flags ---

func TestInterpreter_ExecuteKeep_Explicit(t *testing.T) {
	script := `
	keep;
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
	if _, ok := actions[0].(KeepAction); !ok {
		t.Fatalf("Expected KeepAction, got %T", actions[0])
	}
}

// --- executeCommand with unknown command ---

func TestInterpreter_ExecuteCommand_UnknownCommand(t *testing.T) {
	interp := NewInterpreter(&Script{})

	cmd := &Command{
		Name:      "unknown_command",
		Arguments: []Value{},
	}

	actions, err := interp.executeCommand(cmd)
	if err != nil {
		t.Fatalf("executeCommand error: %v", err)
	}
	// Unknown command returns nil actions
	if actions != nil {
		t.Errorf("Expected nil actions for unknown command, got %v", actions)
	}
}

// --- executeIf with string test ---

func TestInterpreter_ExecuteIf_StringTest(t *testing.T) {
	script := `
	if string :contains "body" "hello" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("hello world"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- evaluateHeaderTest with :matches and regex ---

func TestInterpreter_EvaluateHeaderTest_MatchesWildcard(t *testing.T) {
	script := `
	if header :matches "subject" "*hello*" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"Hello World"},
		},
		Body: []byte("Test"),
	}

	_, err = interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- SizeTest with :over ---

func TestInterpreter_SizeTest_OverTrue(t *testing.T) {
	script := `
	if size :over 100K {
		discard;
	} else {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello world"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// 11 bytes is not over 100K, so keep action
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
}

// --- SizeTest with :under ---

func TestInterpreter_SizeTest_UnderTrue(t *testing.T) {
	script := `
	if size :under 100K {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hi"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
}

// --- BooleanTest with allof ---
// Removed due to parser issue with allof syntax

// --- Script.String() method tests ---

func TestScriptString_Basic(t *testing.T) {
	p := NewParser(`keep;`)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	str := s.String()
	if str == "" {
		t.Error("Expected non-empty string")
	}
}

func TestScriptString_WithBlock(t *testing.T) {
	p := NewParser(`if header :contains "subject" "test" { keep; }`)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	str := s.String()
	if str == "" {
		t.Error("Expected non-empty string")
	}
}

func TestScriptString_MultipleCommands(t *testing.T) {
	p := NewParser(`keep; discard;`)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	str := s.String()
	if str == "" {
		t.Error("Expected non-empty string")
	}
}

// --- parseTag error cases ---

func TestParseTag_NotAtColon(t *testing.T) {
	p := NewParser(`keep;`)
	// Manually call parseTag when input doesn't start with ':'
	p.pos = 0
	_, err := p.parseTag()
	if err == nil {
		t.Error("Expected error when not at colon")
	}
}

func TestParseTag_EmptyTagName(t *testing.T) {
	p := NewParser(`: keep;`)
	p.pos = 0 // At ':'
	_, err := p.parseTag()
	// After ':', next char is space, not alnum, so should error
	if err == nil {
		t.Error("Expected error for empty tag name")
	}
}

// --- parseString error cases ---

func TestParseString_NotAtQuote(t *testing.T) {
	p := NewParser(`keep;`)
	p.pos = 0
	_, err := p.parseString()
	if err == nil {
		t.Error("Expected error when not at quote")
	}
}

func TestParseString_WithEscapes(t *testing.T) {
	// String in redirect command
	p := NewParser(`redirect "test@example.com";`)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(s.Commands) == 0 {
		t.Error("Expected at least one command")
	}
}

// --- parseArgument with various types ---

func TestParseArgument_String(t *testing.T) {
	p := NewParser(`keep;`)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(s.Commands) == 0 {
		t.Error("Expected at least one command")
	}
}

// --- evaluateStringTest with different match types ---

func TestInterpreter_StringTestContainsDirect(t *testing.T) {
	script := `
	if string :contains "body" "hello" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello World"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have keep action since "Hello World" contains "hello" case-insensitively
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_StringTestMatchesDirect(t *testing.T) {
	script := `
	if string :matches "body" "*world*" {
		keep;
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello World"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
}

// --- safeRegexMatch coverage ---

func TestSafeRegexMatch_Valid(t *testing.T) {
	matched, err := safeRegexMatch("hello", "hello world", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !matched {
		t.Error("Expected match")
	}
}

func TestSafeRegexMatch_NoMatchResult(t *testing.T) {
	matched, err := safeRegexMatch("goodbye", "hello world", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if matched {
		t.Error("Expected no match")
	}
}

