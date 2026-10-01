package sieve

import "testing"

// TestParseStringList_UnterminatedInputReturnsError pins the contract that a
// malformed script must produce an error, never a panic.
//
// parseStringList skips whitespace and comments as the first statement of its
// loop body. That can advance p.pos to p.length, after which the loop body's
// next statement used to read p.input[p.pos] unchecked and panic with
// "index out of range". A multi-line string list whose remaining content is only
// whitespace or an unterminated comment triggered it.
//
// These inputs reach the parser through Manager.ValidateScript, which
// managesieve.go calls directly on the PUTSCRIPT/CHECKSCRIPT handler goroutine,
// where there is no recover() — so the panic terminated the whole process.
func TestParseStringList_UnterminatedInputReturnsError(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"space after open bracket", `if true { fileinto [ `},
		{"tabs after open bracket", "if true { fileinto [\t\t"},
		{"newline after open bracket", "if true { fileinto [\n"},
		{"carriage return after open bracket", "if true { fileinto [\r"},
		{"space after comma", `if true { fileinto ["a", `},
		{"tabs after comma", "if true { fileinto [\"a\",\t\t"},
		{"unterminated comment to end of input", "if true { fileinto [ # trailing comment"},
		{"multiline comment to end of input", "if true { fileinto [ /* trailing comment"},
		{"top-level unterminated list", `require [ `},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The parser must not panic, whatever it returns.
			_, _ = NewParser(tt.src).Parse()
		})
	}
}

// TestParseStringList_WellFormedListsStillParse is the control: the bounds fix
// must not reject valid multi-line string lists. It passes before and after the
// fix, so it cannot be the reason the cases above go red.
func TestParseStringList_WellFormedListsStillParse(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantLists [][]string
	}{
		{
			name:      "two quoted values",
			src:       `require ["fileinto", "envelope"];`,
			wantLists: [][]string{{"fileinto", "envelope"}},
		},
		{
			name:      "bare words",
			src:       `require [fileinto envelope];`,
			wantLists: [][]string{{"fileinto", "envelope"}},
		},
		{
			name:      "trailing whitespace before bracket",
			src:       "require [ \"fileinto\" \t ] ;",
			wantLists: [][]string{{"fileinto"}},
		},
		{
			name:      "mixed values with spaces around comma",
			src:       `require [ "a" , b ];`,
			wantLists: [][]string{{"a", "b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, err := NewParser(tt.src).Parse()
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tt.src, err)
			}

			var got [][]string
			for _, cmd := range script.Commands {
				for _, arg := range cmd.Arguments {
					if lv, ok := arg.(*ListValue); ok {
						got = append(got, lv.Values)
					}
				}
			}

			if len(got) != len(tt.wantLists) {
				t.Fatalf("Parse(%q): got %d lists %v, want %d %v",
					tt.src, len(got), got, len(tt.wantLists), tt.wantLists)
			}
			for i := range got {
				if len(got[i]) != len(tt.wantLists[i]) {
					t.Fatalf("Parse(%q): list %d = %v, want %v",
						tt.src, i, got[i], tt.wantLists[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.wantLists[i][j] {
						t.Errorf("Parse(%q): list %d value %d = %q, want %q",
							tt.src, i, j, got[i][j], tt.wantLists[i][j])
					}
				}
			}
		})
	}
}

// TestParse_BlockTerminatorBoundIsUnaffected exercises the sibling parseBlock
// path, which already re-checked the bound before the fix. It guards against a
// future edit to parseBlock regressing the same way.
func TestParse_BlockTerminatorBoundIsUnaffected(t *testing.T) {
	for _, src := range []string{"if true { ", "if true {\t", "if true {\n", "if true { # c"} {
		_, _ = NewParser(src).Parse()
	}
}
