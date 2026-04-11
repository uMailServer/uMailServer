package sieve

import (
	"testing"
)

// --- lookahead tests ---

func TestLookahead_LeftBrace(t *testing.T) {
	script := `{ keep; }`
	p := NewParser(script)
	tok := p.lookahead()
	if tok != TokLeftBrace {
		t.Errorf("Expected TokLeftBrace, got %v", tok)
	}
}

func TestLookahead_RightBrace(t *testing.T) {
	script := `} keep;`
	p := NewParser(script)
	tok := p.lookahead()
	if tok != TokRightBrace {
		t.Errorf("Expected TokRightBrace, got %v", tok)
	}
}

func TestLookahead_Semicolon(t *testing.T) {
	script := `; keep;`
	p := NewParser(script)
	tok := p.lookahead()
	if tok != TokSemicolon {
		t.Errorf("Expected TokSemicolon, got %v", tok)
	}
}

func TestLookahead_Tag(t *testing.T) {
	script := `:create "folder";`
	p := NewParser(script)
	tok := p.lookahead()
	if tok != TokTag {
		t.Errorf("Expected TokTag, got %v", tok)
	}
}

func TestLookahead_Identifier(t *testing.T) {
	script := `keep;`
	p := NewParser(script)
	tok := p.lookahead()
	if tok != TokIdentifier {
		t.Errorf("Expected TokIdentifier, got %v", tok)
	}
}

func TestLookahead_EOF(t *testing.T) {
	script := ``
	p := NewParser(script)
	tok := p.lookahead()
	if tok != TokEOF {
		t.Errorf("Expected TokEOF, got %v", tok)
	}
}

// --- parseStringList error cases ---

func TestParseStringList_Empty(t *testing.T) {
	script := `[]`
	p := NewParser(script)
	_, err := p.parseStringList()
	if err != nil {
		t.Fatalf("Unexpected error for empty list: %v", err)
	}
}

func TestParseStringList_WithStrings(t *testing.T) {
	script := `["a", "b", "c"]`
	p := NewParser(script)
	list, err := p.parseStringList()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(list.Values) != 3 {
		t.Errorf("Expected 3 values, got %d", len(list.Values))
	}
}

func TestParseStringList_WithBareWords(t *testing.T) {
	script := `[a, b, c]`
	p := NewParser(script)
	list, err := p.parseStringList()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(list.Values) != 3 {
		t.Errorf("Expected 3 values, got %d", len(list.Values))
	}
}

func TestParseStringList_Unclosed(t *testing.T) {
	script := `[a, b`
	p := NewParser(script)
	_, err := p.parseStringList()
	if err == nil {
		t.Error("Expected error for unclosed string list")
	}
}

func TestParseStringList_NotAtStart(t *testing.T) {
	script := `keep;`
	p := NewParser(script)
	_, err := p.parseStringList()
	if err == nil {
		t.Error("Expected error when not at string list")
	}
}

// --- parseNumber edge cases ---

func TestParseNumber_Negative(t *testing.T) {
	script := `-42`
	p := NewParser(script)
	num, err := p.parseNumber()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if num.Value != -42 {
		t.Errorf("Expected -42, got %d", num.Value)
	}
}

func TestParseNumber_Zero(t *testing.T) {
	script := `0`
	p := NewParser(script)
	num, err := p.parseNumber()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if num.Value != 0 {
		t.Errorf("Expected 0, got %d", num.Value)
	}
}
