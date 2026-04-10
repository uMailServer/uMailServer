package sieve

import (
	"testing"
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
