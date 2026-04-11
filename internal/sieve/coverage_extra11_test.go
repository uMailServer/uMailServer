package sieve

import (
	"testing"
)

// --- executeSet tests for coverage ---

func TestExecuteSet_NoArguments(t *testing.T) {
	script := `set "var" "value";`
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
	// set with no action should return just keep (default)
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestExecuteSet_NonTagArgument(t *testing.T) {
	// Test executeSet when first arg is not a TagValue
	// This requires creating a script with a bare word instead of :name
	script := `set "variable" "value";`
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
	// Should still work - variable not set but no error
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}
