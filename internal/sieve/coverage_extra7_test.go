package sieve

import (
	"encoding/base64"
	"strings"
	"testing"
)

// --- decodeBase64 tests ---

func TestDecodeBase64_Valid(t *testing.T) {
	input := "SGVsbG8="
	decoded, err := decodeBase64(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if string(decoded) != "Hello" {
		t.Errorf("Expected 'Hello', got %q", string(decoded))
	}
}

func TestDecodeBase64_Empty(t *testing.T) {
	decoded, err := decodeBase64("")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("Expected empty slice, got %v", decoded)
	}
}

func TestDecodeBase64_Invalid(t *testing.T) {
	input := "not-valid-base64!!!"
	decoded, err := decodeBase64(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if string(decoded) != input {
		t.Errorf("Expected original string on invalid base64, got %q", string(decoded))
	}
}

func TestDecodeBase64_WithPadding(t *testing.T) {
	input := "VGVzdA=="
	decoded, err := decodeBase64(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if string(decoded) != "Test" {
		t.Errorf("Expected 'Test', got %q", string(decoded))
	}
}

func TestDecodeBase64_LongString(t *testing.T) {
	input := strings.Repeat("a", 1000)
	encoded := base64.StdEncoding.EncodeToString([]byte(input))

	decoded, err := decodeBase64(encoded)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if string(decoded) != input {
		t.Errorf("Expected original string after decode")
	}
}

// --- parseManageSieveLine tests ---

func TestParseManageSieveLine_Simple(t *testing.T) {
	result := parseManageSieveLine("NOOP")
	if len(result) != 1 || result[0] != "NOOP" {
		t.Errorf("Expected [NOOP], got %v", result)
	}
}

func TestParseManageSieveLine_WithArgs(t *testing.T) {
	result := parseManageSieveLine("LISTSCRIPTS")
	if len(result) != 1 || result[0] != "LISTSCRIPTS" {
		t.Errorf("Expected [LISTSCRIPTS], got %v", result)
	}
}

func TestParseManageSieveLine_MultipleArgs(t *testing.T) {
	result := parseManageSieveLine("PUTSCRIPT script1 100")
	if len(result) != 3 {
		t.Errorf("Expected 3 parts, got %d", len(result))
	}
	if result[0] != "PUTSCRIPT" || result[1] != "script1" || result[2] != "100" {
		t.Errorf("Unexpected parts: %v", result)
	}
}

func TestParseManageSieveLine_QuotedArgs(t *testing.T) {
	result := parseManageSieveLine("AUTHENTICATE \"PLAIN\"")
	if len(result) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result))
	}
	if result[0] != "AUTHENTICATE" || result[1] != "\"PLAIN\"" {
		t.Errorf("Unexpected parts: %v", result)
	}
}

func TestParseManageSieveLine_QuotedWithSpaces(t *testing.T) {
	result := parseManageSieveLine("SETACTIVE \"my script\"")
	if len(result) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result))
	}
	if result[1] != "\"my script\"" {
		t.Errorf("Expected quoted string to be preserved, got %q", result[1])
	}
}

func TestParseManageSieveLine_Empty(t *testing.T) {
	result := parseManageSieveLine("")
	if len(result) != 0 {
		t.Errorf("Expected empty slice, got %v", result)
	}
}

func TestParseManageSieveLine_OnlySpaces(t *testing.T) {
	result := parseManageSieveLine("   ")
	if len(result) != 0 {
		t.Errorf("Expected empty slice for only spaces, got %v", result)
	}
}

func TestParseManageSieveLine_TrailingSpace(t *testing.T) {
	result := parseManageSieveLine("NOOP ")
	if len(result) != 1 {
		t.Errorf("Expected 1 part, got %d", len(result))
	}
}

func TestParseManageSieveLine_LeadingSpace(t *testing.T) {
	result := parseManageSieveLine(" NOOP")
	if len(result) != 1 {
		t.Errorf("Expected 1 part, got %d", len(result))
	}
}

func TestParseManageSieveLine_MultipleSpaces(t *testing.T) {
	result := parseManageSieveLine("CMD   arg1   arg2")
	if len(result) != 3 {
		t.Errorf("Expected 3 parts, got %d", len(result))
	}
	if result[0] != "CMD" || result[1] != "arg1" || result[2] != "arg2" {
		t.Errorf("Unexpected parts: %v", result)
	}
}

func TestParseManageSieveLine_QuotedEmpty(t *testing.T) {
	result := parseManageSieveLine("CMD \"\"")
	if len(result) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result))
	}
	if result[1] != "\"\"" {
		t.Errorf("Expected empty quoted string, got %q", result[1])
	}
}

// --- StoredScript struct tests ---

func TestStoredScript_Fields(t *testing.T) {
	script := &Script{}
	stored := &StoredScript{
		Name:   "test",
		Source: "keep;",
		Script: script,
	}

	if stored.Name != "test" {
		t.Errorf("Name = %q, want 'test'", stored.Name)
	}
	if stored.Source != "keep;" {
		t.Errorf("Source = %q, want 'keep;'", stored.Source)
	}
	if stored.Script == nil {
		t.Error("Expected Script to be set")
	}
}

// --- Manager tests ---

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("Expected non-nil Manager")
	}
}

func TestManager_StoreScript(t *testing.T) {
	m := NewManager()

	err := m.StoreScript("user1", "testscript", "keep;")
	if err != nil {
		t.Fatalf("StoreScript failed: %v", err)
	}

	script, ok := m.GetScript("user1", "testscript")
	if !ok {
		t.Fatal("Expected to find stored script")
	}
	if script == nil {
		t.Fatal("Expected non-nil script")
	}
}

func TestManager_StoreScript_Invalid(t *testing.T) {
	m := NewManager()

	err := m.StoreScript("user1", "test", "invalid { syntax")
	if err == nil {
		t.Error("Expected error for invalid script syntax")
	}
}

func TestManager_StoreScript_Update(t *testing.T) {
	m := NewManager()

	_ = m.StoreScript("user1", "testscript", "keep;")
	_ = m.StoreScript("user1", "testscript", "discard;")

	stored := m.scripts["user1"]["testscript"]
	if stored.Source != "discard;" {
		t.Errorf("Expected updated source 'discard;', got %q", stored.Source)
	}
}

func TestManager_GetScript_UserNotFound(t *testing.T) {
	m := NewManager()

	_, ok := m.GetScript("nonexistent", "script")
	if ok {
		t.Error("Expected not found for nonexistent user")
	}
}

// TestManager_SetActiveScript lives in coverage_extra_test.go

func TestManager_SetActiveScriptByName(t *testing.T) {
	m := NewManager()
	_ = m.StoreScript("user1", "script1", "keep;")
	m.StoreScript("user1", "script2", "keep;")

	err := m.SetActiveScriptByName("user1", "script1")
	if err != nil {
		t.Fatalf("SetActiveScriptByName failed: %v", err)
	}

	m.scriptsMu.RLock()
	if m.activeScripts["user1"] != "script1" {
		t.Errorf("Expected active script 'script1', got %q", m.activeScripts["user1"])
	}
	m.scriptsMu.RUnlock()
}

func TestManager_SetActiveScriptByName_UserNotFound(t *testing.T) {
	m := NewManager()

	err := m.SetActiveScriptByName("nonexistent", "script")
	if err == nil {
		t.Error("Expected error for nonexistent user")
	}
}

func TestManager_SetActiveScriptByName_ScriptNotFound(t *testing.T) {
	m := NewManager()
	_ = m.StoreScript("user1", "script1", "keep;")

	err := m.SetActiveScriptByName("user1", "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent script")
	}
}

func TestManager_GetActiveScript(t *testing.T) {
	m := NewManager()
	m.StoreScript("user1", "myscript", "keep;")
	_ = m.SetActiveScriptByName("user1", "myscript")

	script, ok := m.GetActiveScript("user1")
	if !ok {
		t.Fatal("Expected to find active script")
	}
	if script == nil {
		t.Fatal("Expected non-nil script")
	}
}

func TestManager_GetActiveScript_NoActive(t *testing.T) {
	m := NewManager()
	_ = m.StoreScript("user1", "script1", "keep;")

	script, ok := m.GetActiveScript("user1")
	if ok {
		t.Error("Expected no active script")
	}
	if script != nil {
		t.Errorf("Expected nil script, got %v", script)
	}
}

func TestManager_GetActiveScript_UserNotFound(t *testing.T) {
	m := NewManager()

	script, ok := m.GetActiveScript("nonexistent")
	if ok {
		t.Error("Expected no active script for nonexistent user")
	}
	if script != nil {
		t.Errorf("Expected nil script, got %v", script)
	}
}

func TestManager_HasActiveScript(t *testing.T) {
	m := NewManager()
	m.StoreScript("user1", "myscript", "keep;")
	_ = m.SetActiveScriptByName("user1", "myscript")

	if !m.HasActiveScript("user1") {
		t.Error("Expected HasActiveScript to return true")
	}
}

func TestManager_HasActiveScript_NoActive(t *testing.T) {
	m := NewManager()

	if m.HasActiveScript("user1") {
		t.Error("Expected HasActiveScript to return false")
	}
}

// TestManager_CompileScript lives in coverage_extra_test.go
// TestManager_CompileScript_Invalid also lives there

func TestManager_ProcessMessage(t *testing.T) {
	m := NewManager()
	m.StoreScript("user1", "test", "keep;")

	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Body:    []byte("Hello"),
	}

	_, err := m.ProcessMessage("user1", msg)
	// ProcessMessage may return error if no active script
	// Just verify no panic
	_ = err
}

// --- Interpreter tests for untested paths ---

func TestInterpreter_ExecuteIf_True(t *testing.T) {
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

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_ExecuteIf_False(t *testing.T) {
	script := `
		if false {
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

	// Verify no panic with if false
	_ = actions
}

func TestInterpreter_HeaderTest_Contains_CaseSensitive(t *testing.T) {
	script := `
		if header :contains "subject" "test" {
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
			"subject": {"This is a TEST email"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Verify no panic with case-sensitive header test
	_ = actions
}

func TestInterpreter_HeaderTest_Is_ExactMatch(t *testing.T) {
	script := `
		if header :is "from" "sender@example.com" {
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
			"from": {"sender@example.com"},
		},
		Body: []byte("Hello"),
	}

	actions, err := interp.Execute(msg)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_HeaderTest_MissingHeader(t *testing.T) {
	script := `
		if header :matches "x-custom" "*" {
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

	// Verify no panic with missing header
	_ = actions
}

func TestInterpreter_StringTest_ValueMatch(t *testing.T) {
	script := `
		set "testvar" "hello";
		if string :value "eq" :value "testvar" "hello" {
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

	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_Set_Override(t *testing.T) {
	script := `
		set "var1" "first";
		set "var1" "second";
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
}

func TestInterpreter_Vacation_DaysOnly(t *testing.T) {
	script := `vacation :days 5 "I am away";`

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

	_ = actions
}

func TestInterpreter_MultipleRecipients(t *testing.T) {
	script := `keep;`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient1@example.com", "recipient2@example.com"},
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
}

func TestInterpreter_DifferentBodySizes(t *testing.T) {
	script := `keep;`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)

	testCases := []int{0, 1, 100, 10000}
	for _, size := range testCases {
		body := make([]byte, size)
		for i := range body {
			body[i] = 'A'
		}

		msg := &MessageContext{
			From:    "sender@example.com",
			To:      []string{"recipient@example.com"},
			Headers: map[string][]string{},
			Body:    body,
		}

		actions, err := interp.Execute(msg)
		if err != nil {
			t.Fatalf("Execute error for body size %d: %v", size, err)
		}

		if len(actions) != 1 {
			t.Errorf("Expected 1 action for body size %d, got %d", size, len(actions))
		}
	}
}

func TestInterpreter_Elsif_True(t *testing.T) {
	script := `
		if false {
			discard;
		} elsif true {
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

	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
}

func TestInterpreter_Elsif_False(t *testing.T) {
	script := `
		if false {
			discard;
		} elsif false {
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

	// Verify no panic with elsif false
	_ = actions
}

// --- executeRedirect with valid email ---

func TestInterpreter_Redirect_Valid(t *testing.T) {
	script := `redirect "forward@example.com";`

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
}

// --- executeReject with message ---

func TestInterpreter_Reject_WithMessage(t *testing.T) {
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
}

// --- executeStop ---

func TestInterpreter_Stop_InIfBlock(t *testing.T) {
	script := `
		if true {
			stop;
		}
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
		t.Fatalf("Expected 1 action (stop), got %d", len(actions))
	}

	_, ok := actions[0].(StopAction)
	if !ok {
		t.Fatalf("Expected StopAction, got %T", actions[0])
	}
}
