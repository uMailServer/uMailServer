package sieve

import (
	"testing"
)

func TestParser_BasicScript(t *testing.T) {
	script := `
require ["fileinto", "vacation"];

if header :contains "subject" "invoice" {
    fileinto "Invoices";
} elsif header :is "from" "boss@example.com" {
    fileinto "Work";
} else {
    keep;
}
`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(s.Commands) < 2 {
		t.Fatalf("Expected at least 2 commands, got %d", len(s.Commands))
	}

	// First command should be require
	if s.Commands[0].Name != "require" {
		t.Errorf("First command should be 'require', got %q", s.Commands[0].Name)
	}

	// Second command should be if
	if s.Commands[1].Name != "if" {
		t.Errorf("Second command should be 'if', got %q", s.Commands[1].Name)
	}
}

func TestParser_StringValues(t *testing.T) {
	script := `reject "This message was rejected";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(s.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(s.Commands))
	}

	cmd := s.Commands[0]
	if cmd.Name != "reject" {
		t.Errorf("Expected 'reject', got %q", cmd.Name)
	}
}

func TestParser_Fileinto(t *testing.T) {
	script := `fileinto "Trash";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	cmd := s.Commands[0]
	if cmd.Name != "fileinto" {
		t.Errorf("Expected 'fileinto', got %q", cmd.Name)
	}
}

func TestParser_Keep(t *testing.T) {
	script := `keep;`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	cmd := s.Commands[0]
	if cmd.Name != "keep" {
		t.Errorf("Expected 'keep', got %q", cmd.Name)
	}
}

func TestParser_Redirect(t *testing.T) {
	script := `redirect "test@example.com";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	cmd := s.Commands[0]
	if cmd.Name != "redirect" {
		t.Errorf("Expected 'redirect', got %q", cmd.Name)
	}
}

func TestInterpreter_KeepAction(t *testing.T) {
	script := `keep;`

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
		t.Errorf("Expected KeepAction, got %T", actions[0])
	}
}

func TestInterpreter_DiscardAction(t *testing.T) {
	script := `discard;`

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

	if _, ok := actions[0].(DiscardAction); !ok {
		t.Errorf("Expected DiscardAction, got %T", actions[0])
	}
}

func TestInterpreter_StopAction(t *testing.T) {
	script := `stop;`

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
		t.Errorf("Expected StopAction, got %T", actions[0])
	}
}

func TestInterpreter_HeaderContains(t *testing.T) {
	script := `
if header :contains "subject" "invoice" {
    fileinto "Invoices";
}
`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Debug: print parsed script
	t.Logf("Script has %d commands", len(s.Commands))
	for i, cmd := range s.Commands {
		t.Logf("Command %d: name=%q, tag=%q, args=%d, hasBlock=%v",
			i, cmd.Name, cmd.Tag, len(cmd.Arguments), cmd.Block != nil)
		if cmd.Block != nil {
			t.Logf("  Block has %d commands", len(cmd.Block.Commands))
		}
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From: "sender@example.com",
		To:   []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Subject": {"Invoice #123"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have fileinto action
	found := false
	for _, a := range actions {
		if fa, ok := a.(FileintoAction); ok && fa.Folder == "Invoices" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected FileintoAction for Invoices")
	}
}

func TestInterpreter_NoMatch(t *testing.T) {
	script := `
if header :contains "subject" "invoice" {
    fileinto "Invoices";
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
			"Subject": {"Hello World"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have keep action since no condition matched
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
	if _, ok := actions[0].(KeepAction); !ok {
		t.Errorf("Expected KeepAction when no match, got %T", actions[0])
	}
}

func TestManager_ValidateScript(t *testing.T) {
	m := NewManager()

	// Valid script
	err := m.ValidateScript(`keep;`)
	if err != nil {
		t.Errorf("Expected valid script to pass, got: %v", err)
	}

	// Invalid script (unclosed block)
	err = m.ValidateScript(`if header :contains "test" {`)
	if err == nil {
		t.Error("Expected invalid script to fail")
	}
}
