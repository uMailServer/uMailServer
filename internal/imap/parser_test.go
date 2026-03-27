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

func TestNewParser(t *testing.T) {
	parser := NewParser("test input")
	if parser == nil {
		t.Fatal("expected non-nil parser")
	}
	if parser.input != "test input" {
		t.Errorf("expected input 'test input', got %s", parser.input)
	}
	if parser.pos != 0 {
		t.Errorf("expected pos 0, got %d", parser.pos)
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

func TestParseFetchItemsBody(t *testing.T) {
	items, err := ParseFetchItems("BODY[]")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestParseSearchCriteria(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(*SearchCriteria) bool
	}{
		{
			name:  "ALL",
			input: "ALL",
			check: func(c *SearchCriteria) bool { return c.All },
		},
		{
			name:  "SEEN",
			input: "SEEN",
			check: func(c *SearchCriteria) bool { return c.Seen },
		},
		{
			name:  "UNSEEN",
			input: "UNSEEN",
			check: func(c *SearchCriteria) bool { return c.Unseen },
		},
		{
			name:  "FLAGGED",
			input: "FLAGGED",
			check: func(c *SearchCriteria) bool { return c.Flagged },
		},
		{
			name:  "DELETED",
			input: "DELETED",
			check: func(c *SearchCriteria) bool { return c.Deleted },
		},
		{
			name:  "ANSWERED",
			input: "ANSWERED",
			check: func(c *SearchCriteria) bool { return c.Answered },
		},
		{
			name:  "FROM",
			input: "FROM test@example.com",
			check: func(c *SearchCriteria) bool { return c.From == "test@example.com" },
		},
		{
			name:  "TO",
			input: "TO recipient@example.com",
			check: func(c *SearchCriteria) bool { return c.To == "recipient@example.com" },
		},
		{
			name:  "SUBJECT",
			input: "SUBJECT test",
			check: func(c *SearchCriteria) bool { return c.Subject == "test" },
		},
		{
			name:  "BODY",
			input: "BODY content",
			check: func(c *SearchCriteria) bool { return c.Body == "content" },
		},
		{
			name:  "TEXT",
			input: "TEXT search",
			check: func(c *SearchCriteria) bool { return c.Text == "search" },
		},
		{
			name:  "UID",
			input: "UID 100:110",
			check: func(c *SearchCriteria) bool { return c.UIDSet == "100:110" },
		},
		{
			name:  "LARGER",
			input: "LARGER 1024",
			check: func(c *SearchCriteria) bool { return c.Larger == 1024 },
		},
		{
			name:  "SMALLER",
			input: "SMALLER 1048576",
			check: func(c *SearchCriteria) bool { return c.Smaller == 1048576 },
		},
		{
			name:  "NEW",
			input: "NEW",
			check: func(c *SearchCriteria) bool { return c.New },
		},
		{
			name:  "OLD",
			input: "OLD",
			check: func(c *SearchCriteria) bool { return c.Old },
		},
		{
			name:  "RECENT",
			input: "RECENT",
			check: func(c *SearchCriteria) bool { return c.Recent },
		},
		{
			name:  "UNANSWERED",
			input: "UNANSWERED",
			check: func(c *SearchCriteria) bool { return c.Unanswered },
		},
		{
			name:  "UNDELETED",
			input: "UNDELETED",
			check: func(c *SearchCriteria) bool { return c.Undeleted },
		},
		{
			name:  "UNFLAGGED",
			input: "UNFLAGGED",
			check: func(c *SearchCriteria) bool { return c.Unflagged },
		},
		{
			name:  "DRAFT",
			input: "DRAFT",
			check: func(c *SearchCriteria) bool { return c.Draft },
		},
		{
			name:  "UNDRAFT",
			input: "UNDRAFT",
			check: func(c *SearchCriteria) bool { return c.Undraft },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			criteria, err := ParseSearchCriteria(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.check(criteria) {
				t.Errorf("check failed for input %q", tt.input)
			}
		})
	}
}

func TestParseSearchCriteriaNOT(t *testing.T) {
	criteria, err := ParseSearchCriteria("NOT SEEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Not == nil {
		t.Fatal("expected Not to be set")
	}
	if !criteria.Not.Seen {
		t.Error("expected inner criteria Seen to be true")
	}
}

func TestParseSearchCriteriaOR(t *testing.T) {
	criteria, err := ParseSearchCriteria("OR SEEN FLAGGED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Or[0] == nil || criteria.Or[1] == nil {
		t.Fatal("expected both Or criteria to be set")
	}
}

func TestParseSearchCriteriaHeader(t *testing.T) {
	criteria, err := ParseSearchCriteria("HEADER X-Priority 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Header == nil {
		t.Fatal("expected Header map to be set")
	}
	if criteria.Header["X-Priority"] != "1" {
		t.Errorf("expected X-Priority 1, got %s", criteria.Header["X-Priority"])
	}
}

func TestParseSearchCriteriaQuoted(t *testing.T) {
	criteria, err := ParseSearchCriteria(`FROM "test user"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.From != "test user" {
		t.Errorf("expected From 'test user', got %s", criteria.From)
	}
}

func TestParseSearchCriteriaSeqSet(t *testing.T) {
	criteria, err := ParseSearchCriteria("1:10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.SeqSet != "1:10" {
		t.Errorf("expected SeqSet '1:10', got %s", criteria.SeqSet)
	}
}

func TestParseFlagsNotParenthesized(t *testing.T) {
	_, err := ParseFlags("\\Seen")
	if err == nil {
		t.Error("expected error for flags without parentheses")
	}
}

func TestSeqRangeContainsSwapped(t *testing.T) {
	// Test with start > end (should be swapped)
	r := SeqRange{Start: 10, End: 5}
	if !r.Contains(7, 10) {
		t.Error("expected 7 to be in range 10:5")
	}
}

func TestParseFetchItemsNested(t *testing.T) {
	items, err := ParseFetchItems("(FLAGS (RFC822.SIZE UID))")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Error("expected items")
	}
}
