package sieve

import (
	"testing"
)

func TestParser_Elsif(t *testing.T) {
	script := `
if header :contains "subject" "test" {
    keep;
} elsif header :contains "from" "boss" {
    fileinto "Work";
} else {
    discard;
}
`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Parser creates flat list of commands: if, elsif, else
	if len(s.Commands) < 1 {
		t.Fatalf("Expected at least 1 command, got %d", len(s.Commands))
	}

	cmd := s.Commands[0]
	if cmd.Name != "if" {
		t.Errorf("Expected first command 'if', got %q", cmd.Name)
	}
}

func TestParser_Vacation(t *testing.T) {
	script := `vacation "I'm on vacation";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	cmd := s.Commands[0]
	if cmd.Name != "vacation" {
		t.Errorf("Expected 'vacation', got %q", cmd.Name)
	}
}

func TestParser_Reject(t *testing.T) {
	script := `reject "Message rejected";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	cmd := s.Commands[0]
	if cmd.Name != "reject" {
		t.Errorf("Expected 'reject', got %q", cmd.Name)
	}
}

func TestParser_MultipleCommands(t *testing.T) {
	script := `
require "fileinto";
if header :contains "subject" "test" {
    fileinto "Test";
}
keep;
`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(s.Commands) != 3 {
		t.Fatalf("Expected 3 commands, got %d", len(s.Commands))
	}
}

func TestInterpreter_RedirectAction(t *testing.T) {
	script := `redirect "test@example.com";`

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

	ra, ok := actions[0].(RedirectAction)
	if !ok {
		t.Fatalf("Expected RedirectAction, got %T", actions[0])
	}

	if ra.Address != "test@example.com" {
		t.Errorf("Expected 'test@example.com', got %q", ra.Address)
	}
}

func TestInterpreter_RejectAction(t *testing.T) {
	script := `reject "Message rejected";`

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

	if ra.Message != "Message rejected" {
		t.Errorf("Expected 'Message rejected', got %q", ra.Message)
	}
}

func TestInterpreter_VacationAction(t *testing.T) {
	script := `vacation "Subject: Away" "Message: I am on vacation";`

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

	// Vacation with subject and body should return action
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}

	_, ok := actions[0].(VacationAction)
	if !ok {
		t.Fatalf("Expected VacationAction, got %T", actions[0])
	}
}

func TestManager_CompileScript(t *testing.T) {
	m := NewManager()

	script, err := m.CompileScript(`keep;`)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	if script == nil {
		t.Fatal("Expected script, got nil")
	}
}

func TestManager_SetActiveScript(t *testing.T) {
	m := NewManager()

	err := m.SetActiveScript("user1", "main", `keep;`)
	if err != nil {
		t.Fatalf("SetActiveScript error: %v", err)
	}

	if !m.HasActiveScript("user1") {
		t.Error("Expected user1 to have active script")
	}

	script, ok := m.GetActiveScript("user1")
	if !ok {
		t.Error("Expected to get active script")
	}
	if script == nil {
		t.Error("Expected script, got nil")
	}
}

func TestManager_DeleteScript(t *testing.T) {
	m := NewManager()

	m.SetActiveScript("user1", "main", `keep;`)
	m.DeleteScript("user1", "main")

	if m.HasActiveScript("user1") {
		t.Error("Expected user1 to not have active script after delete")
	}
}

func TestManager_InvalidScript(t *testing.T) {
	m := NewManager()

	err := m.SetActiveScript("user1", "main", `if header {`)
	if err == nil {
		t.Error("Expected error for invalid script")
	}
}
