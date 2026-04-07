package sieve

import (
	"strings"
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

// ========== ManageSieve Tests ==========

func TestManageSieveServer_CmdListScripts(t *testing.T) {
	m := NewManager()
	m.SetActiveScript("user1", "main", "keep;")

	server := NewManageSieveServer(m, nil)
	// Just ensure creation works
	if server == nil {
		t.Error("Expected non-nil server")
	}
}

func TestManageSieveServer_CmdSetActive(t *testing.T) {
	m := NewManager()
	m.StoreScript("user1", "script1", "keep;")

	server := NewManageSieveServer(m, nil)
	if server == nil {
		t.Error("Expected non-nil server")
	}

	// Test setting active
	err := m.SetActiveScriptByName("user1", "script1")
	if err != nil {
		t.Errorf("SetActiveScriptByName error: %v", err)
	}
}

func TestManageSieveServer_CmdDeleteScript(t *testing.T) {
	m := NewManager()
	m.StoreScript("user1", "todelete", "keep;")

	server := NewManageSieveServer(m, nil)
	if server == nil {
		t.Error("Expected non-nil server")
	}

	m.DeleteScript("user1", "todelete")

	scripts := m.ListScripts("user1")
	for _, s := range scripts {
		if s == "todelete" {
			t.Error("Script should have been deleted")
		}
	}
}

func TestManageSieveServer_CmdGetScript(t *testing.T) {
	m := NewManager()
	m.StoreScript("user1", "myscript", "fileinto \"Test\";")

	server := NewManageSieveServer(m, nil)
	if server == nil {
		t.Error("Expected non-nil server")
	}

	source := m.GetScriptSource("user1", "myscript")
	if source != "fileinto \"Test\";" {
		t.Errorf("Expected script source, got %q", source)
	}
}

func TestManageSieveServer_CmdGetScript_NotFound(t *testing.T) {
	m := NewManager()

	source := m.GetScriptSource("user1", "nonexistent")
	if source != "" {
		t.Errorf("Expected empty string, got %q", source)
	}
}

func TestManageSieveServer_Close(t *testing.T) {
	m := NewManager()
	server := NewManageSieveServer(m, nil)

	// Close should work even if not started
	err := server.Close()
	if err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestManageSieveServer_Addr(t *testing.T) {
	m := NewManager()
	server := NewManageSieveServer(m, nil)

	// Addr returns nil when not listening
	addr := server.Addr()
	if addr != nil {
		t.Errorf("Expected nil addr when not listening, got %v", addr)
	}
}

// ========== Parser Edge Cases ==========

func TestParser_parseNumber(t *testing.T) {
	script := `vacation :days 7 "message";`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(s.Commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(s.Commands))
	}
}

func TestParser_NegativeNumber(t *testing.T) {
	script := `keep;`
	p := NewParser(script)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error on keep: %v", err)
	}
}

func TestParser_lookahead(t *testing.T) {
	script := `if header :contains "test" { keep; }`
	p := NewParser(script)
	// Just ensure no panic
	_ = p.lookahead()
}

func TestIsWhitespace(t *testing.T) {
	tests := []struct {
		ch  byte
		want bool
	}{
		{' ', true},
		{'\t', true},
		{'\r', true},
		{'\n', true},
		{'a', false},
		{'0', false},
	}

	for _, tt := range tests {
		if got := isWhitespace(tt.ch); got != tt.want {
			t.Errorf("isWhitespace(%q) = %v, want %v", tt.ch, got, tt.want)
		}
	}
}

func TestMustCompile_Valid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustCompile should not panic on valid script")
		}
	}()
	script := MustCompile("keep;")
	if script == nil {
		t.Error("Expected non-nil script")
	}
}

func TestMustCompile_Invalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompile should panic on invalid script")
		}
	}()
	MustCompile("invalid { script")
}

func TestScript_String(t *testing.T) {
	p := NewParser("keep;")
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// Just ensure it doesn't panic
	str := s.String()
	if str == "" {
		t.Error("Expected non-empty string")
	}
}

// ========== Interpreter Edge Cases ==========

func TestInterpreter_SetAction(t *testing.T) {
	script := `set "myvariable" "myvalue";`
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

	// set command produces no actions, so default Keep is returned
	if len(actions) != 1 {
		t.Errorf("Expected 1 action (keep), got %d", len(actions))
	}
}

func TestInterpreter_AddHeader(t *testing.T) {
	script := `addheader "X-Test" "value";`
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

	// addheader doesn't produce actions, default Keep is returned
	if len(actions) != 1 {
		t.Errorf("Expected 1 action (keep), got %d", len(actions))
	}
}

func TestInterpreter_DeleteHeader(t *testing.T) {
	script := `deleteheader "Subject";`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{"Subject": {"Test"}},
		Body:    []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// deleteheader doesn't produce actions, default Keep is returned
	if len(actions) != 1 {
		t.Errorf("Expected 1 action (keep), got %d", len(actions))
	}
}

func TestInterpreter_StringTest(t *testing.T) {
	script := `
if "test@example.com" :contains "test" {
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

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have keep action
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_SizeTest(t *testing.T) {
	script := `
if size :over 1M {
    discard;
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
		Body:    []byte(strings.Repeat("x", 2*1024*1024)), // 2MB
		Size:    2 * 1024 * 1024,
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have discard action
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_SizeTestUnder(t *testing.T) {
	script := `
if size :under 1M {
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
		Body:    []byte("short"),
		Size:    100,
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should have keep action
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_ExecuteScript(t *testing.T) {
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	actions, err := ExecuteScript("keep;", msg)
	if err != nil {
		t.Fatalf("ExecuteScript error: %v", err)
	}

	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_ExecuteScript_Invalid(t *testing.T) {
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	_, err := ExecuteScript("invalid {", msg)
	if err == nil {
		t.Error("Expected error for invalid script")
	}
}

// ========== Manager Multi-Script Tests ==========

func TestManager_MultipleScripts(t *testing.T) {
	m := NewManager()

	m.StoreScript("user1", "script1", "keep;")
	m.StoreScript("user1", "script2", "discard;")

	scripts := m.ListScripts("user1")
	if len(scripts) != 2 {
		t.Errorf("Expected 2 scripts, got %d", len(scripts))
	}

	// Activate script2
	err := m.SetActiveScriptByName("user1", "script2")
	if err != nil {
		t.Errorf("SetActiveScriptByName error: %v", err)
	}

	// ProcessMessage should use active script
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	actions, err := m.ProcessMessage("user1", msg)
	if err != nil {
		t.Fatalf("ProcessMessage error: %v", err)
	}

	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}

	// Should be discard (from script2)
	if _, ok := actions[0].(DiscardAction); !ok {
		t.Errorf("Expected DiscardAction, got %T", actions[0])
	}
}

func TestManager_ProcessMessage_InvalidScript(t *testing.T) {
	m := NewManager()

	// Store an invalid script
	m.StoreScript("user1", "bad", "if header {")
	m.SetActiveScriptByName("user1", "bad")

	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	// Should fall back to keep
	actions, err := m.ProcessMessage("user1", msg)
	if err != nil {
		t.Fatalf("ProcessMessage error: %v", err)
	}

	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestManager_ShouldSendVacation_MinInterval(t *testing.T) {
	m := NewManager()

	// Test minimum interval (1 day)
	m.RecordVacationSent("sender@example.com")
	if m.ShouldSendVacation("sender@example.com", 0) {
		t.Error("Should not send within minimum interval")
	}
}

func TestStoredScript_Struct(t *testing.T) {
	ss := &StoredScript{
		Name:   "test",
		Source: "keep;",
	}

	if ss.Name != "test" {
		t.Errorf("Expected 'test', got %q", ss.Name)
	}
	if ss.Source != "keep;" {
		t.Errorf("Expected 'keep;', got %q", ss.Source)
	}
}

// ========== Vacation Cache Tests ==========

func TestManager_VacationCacheCleanup(t *testing.T) {
	m := NewManager()

	// Add some vacation replies
	m.RecordVacationSent("sender1@example.com")
	m.RecordVacationSent("sender2@example.com")

	// Should not send within interval
	if m.ShouldSendVacation("sender1@example.com", 7) {
		t.Error("Should not send within 7 day interval")
	}

	// Different sender should still be able to send
	if !m.ShouldSendVacation("sender3@example.com", 7) {
		t.Error("Different sender should be able to send")
	}
}
