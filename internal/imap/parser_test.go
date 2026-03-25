package imap

import (
	"testing"
)

func TestParseSequenceSet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxSeq   uint32
		expected []uint32
		wantErr  bool
	}{
		{
			name:     "single number",
			input:    "1",
			maxSeq:   10,
			expected: []uint32{1},
		},
		{
			name:     "range",
			input:    "1:3",
			maxSeq:   10,
			expected: []uint32{1, 2, 3},
		},
		{
			name:     "star",
			input:    "*",
			maxSeq:   10,
			expected: []uint32{10},
		},
		{
			name:     "mixed",
			input:    "1,3:5,7",
			maxSeq:   10,
			expected: []uint32{1, 3, 4, 5, 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranges, err := ParseSequenceSet(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Build expected from ranges
			var got []uint32
			for seq := uint32(1); seq <= tt.maxSeq; seq++ {
				for _, r := range ranges {
					if r.Contains(seq, tt.maxSeq) {
						got = append(got, seq)
						break
					}
				}
			}

			if len(got) != len(tt.expected) {
				t.Errorf("Got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSeqRangeContains(t *testing.T) {
	tests := []struct {
		name   string
		start  uint32
		end    uint32
		seqNum uint32
		maxSeq uint32
		want   bool
	}{
		{"in range", 1, 5, 3, 10, true},
		{"at start", 1, 5, 1, 10, true},
		{"at end", 1, 5, 5, 10, true},
		{"before range", 1, 5, 0, 10, false},
		{"after range", 1, 5, 6, 10, false},
		{"star resolved to max", 0, 0, 10, 10, true}, // *:* with max 10
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := SeqRange{Start: tt.start, End: tt.end}
			got := r.Contains(tt.seqNum, tt.maxSeq)
			if got != tt.want {
				t.Errorf("Contains(%d, %d) = %v, want %v", tt.seqNum, tt.maxSeq, got, tt.want)
			}
		})
	}
}

func TestParseFetchItems(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "ALL macro",
			input:    "ALL",
			expected: []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE"},
		},
		{
			name:     "FAST macro",
			input:    "FAST",
			expected: []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE"},
		},
		{
			name:     "FULL macro",
			input:    "FULL",
			expected: []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODY"},
		},
		{
			name:     "individual items",
			input:    "(FLAGS UID RFC822.SIZE)",
			expected: []string{"FLAGS", "UID", "RFC822.SIZE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFetchItems(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Errorf("Got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "empty flags",
			input:    "()",
			expected: []string{},
		},
		{
			name:     "single flag",
			input:    "(\\Seen)",
			expected: []string{"\\Seen"},
		},
		{
			name:     "multiple flags",
			input:    "(\\Seen \\Answered \\Deleted)",
			expected: []string{"\\Seen", "\\Answered", "\\Deleted"},
		},
		{
			name:    "not parenthesized",
			input:   "\\Seen",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFlags(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Errorf("Got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFormatFlags(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{[]string{}, "()"},
		{[]string{"\\Seen"}, "(\\Seen)"},
		{[]string{"\\Seen", "\\Answered"}, "(\\Seen \\Answered)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := FormatFlags(tt.input)
			if got != tt.expected {
				t.Errorf("FormatFlags(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseStatusItems(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []StatusItem
		wantErr  bool
	}{
		{
			name:     "single item",
			input:    "MESSAGES",
			expected: []StatusItem{StatusMessages},
		},
		{
			name:     "parenthesized list",
			input:    "(MESSAGES UIDNEXT UNSEEN)",
			expected: []StatusItem{StatusMessages, StatusUIDNext, StatusUnseen},
		},
		{
			name:    "unknown item",
			input:   "UNKNOWN",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatusItems(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Errorf("Got %v, want %v", got, tt.expected)
			}
		})
	}
}
