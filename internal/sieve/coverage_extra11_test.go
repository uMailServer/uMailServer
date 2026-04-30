package sieve

import (
	"fmt"
	"testing"
	"time"
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

// --- CheckAndRecordVacation tests ---

func TestManager_CheckAndRecordVacation_FirstTime(t *testing.T) {
	m := NewManager()
	if !m.CheckAndRecordVacation("sender@example.com", 7) {
		t.Error("Expected true for first-time sender")
	}
}

func TestManager_CheckAndRecordVacation_WithinInterval(t *testing.T) {
	m := NewManager()
	m.CheckAndRecordVacation("sender@example.com", 7)
	if m.CheckAndRecordVacation("sender@example.com", 7) {
		t.Error("Expected false for sender within interval")
	}
}

func TestManager_CheckAndRecordVacation_AfterInterval(t *testing.T) {
	m := NewManager()
	m.vacationCacheMu.Lock()
	m.vacationCache["sender@example.com"] = time.Now().Add(-48 * time.Hour)
	m.vacationCacheMu.Unlock()

	if !m.CheckAndRecordVacation("sender@example.com", 1) {
		t.Error("Expected true for sender after interval")
	}
}

func TestManager_CheckAndRecordVacation_MinInterval(t *testing.T) {
	m := NewManager()
	// days=0 should still enforce 1-day minimum
	m.CheckAndRecordVacation("sender@example.com", 0)
	if m.CheckAndRecordVacation("sender@example.com", 0) {
		t.Error("Expected false due to minimum 1-day interval")
	}
}

func TestManager_CheckAndRecordVacation_LRU(t *testing.T) {
	m := NewManager()
	m.vacationMaxSize = 4
	for i := 0; i < 5; i++ {
		m.CheckAndRecordVacation("sender"+string(rune('0'+i))+"@example.com", 1)
	}
	// After 5 inserts with max 4, oldest should have been evicted
	if len(m.vacationCache) != 4 {
		t.Errorf("Expected 4 entries after LRU eviction, got %d", len(m.vacationCache))
	}
}

func TestManager_RecordVacationSent_LRU(t *testing.T) {
	m := NewManager()
	m.vacationMaxSize = 4
	for i := 0; i < 5; i++ {
		m.RecordVacationSent("sender" + string(rune('0'+i)) + "@example.com")
	}
	if len(m.vacationCache) != 4 {
		t.Errorf("Expected 4 entries after LRU eviction, got %d", len(m.vacationCache))
	}
}

// --- evaluateTest default case ---

func TestEvaluateTest_UnknownType(t *testing.T) {
	interp := NewInterpreter(&Script{})
	result, err := interp.evaluateTest("not-a-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for unknown test type")
	}
}

// --- safeRegexMatch coverage ---

func TestSafeRegexMatch_SuspiciousPattern(t *testing.T) {
	_, err := safeRegexMatch("a++", "test", 1*time.Second)
	if err == nil {
		t.Error("expected error for suspicious pattern")
	}
}

func TestSafeRegexMatch_InvalidPattern(t *testing.T) {
	_, err := safeRegexMatch("[invalid", "test", 1*time.Second)
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestSafeRegexMatch_LRU_Eviction(t *testing.T) {
	// Fill cache beyond capacity to trigger LRU eviction
	for i := 0; i < 1100; i++ {
		_, err := safeRegexMatch(fmt.Sprintf("pattern%d", i), "test", 1*time.Second)
		if err != nil {
			t.Fatalf("safeRegexMatch failed: %v", err)
		}
	}
}

// --- executeFileinto uncovered paths ---

func TestExecuteFileinto_TagWithNonStringSecondArg(t *testing.T) {
	script := `require "fileinto"; fileinto :create 123;`
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
		t.Errorf("Expected 1 action (keep), got %d", len(actions))
	}
}

func TestExecuteFileinto_UnknownFirstArg(t *testing.T) {
	script := `require "fileinto"; fileinto 123;`
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
		t.Errorf("Expected 1 action (keep), got %d", len(actions))
	}
}

// --- executeIf uncovered paths ---

func TestExecuteIf_NoArguments(t *testing.T) {
	script := `if header :is "subject" "" { keep; }`
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

func TestExecuteIf_Elsif_PreviousFalse(t *testing.T) {
	script := `if header :is "subject" "nomatch" { keep; } elsif header :is "subject" "nomatch2" { keep; }`
	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	interp := NewInterpreter(s)
	msg := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{"Subject": {"RealSubject"}},
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
