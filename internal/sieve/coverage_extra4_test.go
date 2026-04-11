package sieve

import (
	"testing"
)

// --- executeFileinto with :create tag ---

func TestInterpreter_FileintoWithCreateTag(t *testing.T) {
	// Test fileinto with :create flag
	script := `fileinto :create "Trash";`

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
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
	if _, ok := actions[0].(FileintoAction); !ok {
		t.Errorf("Expected FileintoAction, got %T", actions[0])
	}
}

// --- executeIf with elsif false condition skip ---

func TestInterpreter_ElsifConditionSkipped(t *testing.T) {
	// Test elsif when parent condition was true
	// First if matches, elsif should be skipped
	script := `
	if header :contains "subject" "match" {
		keep;
	} elsif header :contains "subject" "nomatch" {
		fileinto "Skipped";
	}
	`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From: "sender@example.com",
		To:   []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"match"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have keep action from first if branch
	found := false
	for _, a := range actions {
		if _, ok := a.(KeepAction); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected keep action from first if branch")
	}
}
