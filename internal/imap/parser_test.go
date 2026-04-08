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

func TestParseSequenceSetInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "invalid range",
			input: "1:2:3",
		},
		{
			name:  "invalid number",
			input: "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSequenceSet(tt.input)
			if err == nil {
				t.Error("expected error for invalid sequence set")
			}
		})
	}
}

func TestParseSequenceSetEmpty(t *testing.T) {
	ranges, err := ParseSequenceSet("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(ranges) != 0 {
		t.Errorf("expected 0 ranges, got %d", len(ranges))
	}
}

func TestSeqRangeContainsSwapped(t *testing.T) {
	// Test with start > end (should be swapped)
	r := SeqRange{Start: 10, End: 5}
	if !r.Contains(7, 10) {
		t.Error("expected 7 to be in range 10:5")
	}
}
