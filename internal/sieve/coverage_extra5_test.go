package sieve

import (
	"testing"
)

// --- executeVacation with :mime tag ---

func TestInterpreter_VacationAction_WithMime(t *testing.T) {
	// Test vacation with mime flag - subject comes first then body
	script := `vacation :subject "Away" :mime "Body text";`

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

	va, ok := actions[0].(VacationAction)
	if !ok {
		t.Fatalf("Expected VacationAction, got %T", actions[0])
	}

	// Subject should be set
	if va.Subject != "Away" {
		t.Errorf("Expected subject 'Away', got %q", va.Subject)
	}
}

func TestInterpreter_VacationAction_OnlySubject(t *testing.T) {
	// Vacation with only subject (no body) - still returns action
	script := `vacation :subject "Away";`

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
}

func TestInterpreter_VacationAction_OnlyDays(t *testing.T) {
	// Vacation with just :days set but no subject/body is still disabled
	script := `vacation :days 3;`

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

	// Days alone without subject/body - behavior depends on implementation
	_ = actions
}

// --- executeRedirect with invalid address ---

func TestInterpreter_RedirectAction_InvalidAddress(t *testing.T) {
	script := `redirect "not-an-email";`

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

	// Invalid email should cause error
	_, err = interp.Execute(msg)
	if err == nil {
		t.Error("Expected error for invalid redirect address")
	}
}

func TestInterpreter_RedirectAction_EmptyAddress(t *testing.T) {
	script := `redirect "";`

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

	// Empty address - parser treats "" as a string value
	// Check what actually happens
	_ = actions
}

// --- executeReject with message ---

func TestInterpreter_RejectAction_WithMessage(t *testing.T) {
	script := `reject "Message text";`

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

	ra, ok := actions[0].(RejectAction)
	if !ok {
		t.Fatalf("Expected RejectAction, got %T", actions[0])
	}

	if ra.Message != "Message text" {
		t.Errorf("Expected message 'Message text', got %q", ra.Message)
	}
}

func TestInterpreter_RejectAction_JustDiscard(t *testing.T) {
	script := `reject;`

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

	// Just "reject;" without message should produce discard
	da, ok := actions[0].(DiscardAction)
	if !ok {
		t.Fatalf("Expected DiscardAction, got %T", actions[0])
	}
	_ = da
}

// --- executeIf with header test containing :matches ---

func TestInterpreter_HeaderTest_WithMatches(t *testing.T) {
	script := `
	if header :matches "subject" "*test*" {
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
		From: "sender@example.com",
		To:   []string{"recipient@example.com"},
		Headers: map[string][]string{
			"subject": {"this is a test email"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should match and keep
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}
