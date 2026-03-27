package search

import (
	"testing"
)

func TestNewIndex(t *testing.T) {
	idx := NewIndex()
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

func TestIndexAdd(t *testing.T) {
	idx := NewIndex()

	doc := &Document{
		ID:      "INBOX:1",
		Content: "Hello world this is a test",
		Fields: map[string]string{
			"from":    "sender@example.com",
			"to":      "recipient@example.com",
			"subject": "Test Subject",
		},
	}

	idx.Add(doc)

	if idx.DocCount() != 1 {
		t.Errorf("expected doc count = 1, got %d", idx.DocCount())
	}
}

func TestIndexRemove(t *testing.T) {
	idx := NewIndex()

	doc := &Document{
		ID:      "INBOX:1",
		Content: "Hello world",
		Fields: map[string]string{
			"subject": "Test",
		},
	}

	idx.Add(doc)
	if idx.DocCount() != 1 {
		t.Fatal("expected 1 document after add")
	}

	idx.Remove("INBOX:1")
	if idx.DocCount() != 0 {
		t.Errorf("expected 0 documents after remove, got %d", idx.DocCount())
	}
}

func TestIndexClear(t *testing.T) {
	idx := NewIndex()

	// Add multiple documents
	for i := 1; i <= 5; i++ {
		doc := &Document{
			ID:      "INBOX:" + string(rune('0'+i)),
			Content: "Content " + string(rune('0'+i)),
		}
		idx.Add(doc)
	}

	if idx.DocCount() != 5 {
		t.Fatalf("expected 5 documents, got %d", idx.DocCount())
	}

	idx.Clear()

	if idx.DocCount() != 0 {
		t.Errorf("expected 0 documents after clear, got %d", idx.DocCount())
	}
}

func TestIndexSearch(t *testing.T) {
	idx := NewIndex()

	// Add documents
	docs := []*Document{
		{
			ID:      "INBOX:1",
			Content: "Hello world from golang",
			Fields:  map[string]string{"subject": "Greeting"},
		},
		{
			ID:      "INBOX:2",
			Content: "The quick brown fox",
			Fields:  map[string]string{"subject": "Animals"},
		},
		{
			ID:      "INBOX:3",
			Content: "Hello golang programming",
			Fields:  map[string]string{"subject": "Code"},
		},
	}

	for _, doc := range docs {
		idx.Add(doc)
	}

	tests := []struct {
		query         string
		minResults    int
		maxResults    int
		shouldContain string
	}{
		{"hello", 1, 3, "INBOX:1"},
		{"golang", 1, 2, ""},
		{"quick fox", 1, 2, ""},
		{"nonexistent", 0, 0, ""},
		{"", 0, 0, ""},
	}

	for _, tc := range tests {
		opts := SearchOptions{Limit: 10}
		results := idx.Search(tc.query, opts)

		if len(results) < tc.minResults {
			t.Errorf("query '%s': expected at least %d results, got %d", tc.query, tc.minResults, len(results))
		}
		if len(results) > tc.maxResults {
			t.Errorf("query '%s': expected at most %d results, got %d", tc.query, tc.maxResults, len(results))
		}

		if tc.shouldContain != "" {
			found := false
			for _, r := range results {
				if r.DocID == tc.shouldContain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("query '%s': expected results to contain %s", tc.query, tc.shouldContain)
			}
		}
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"Hello World", []string{"hello", "world"}},
		{"The Quick Brown Fox", []string{"quick", "brown", "fox"}}, // 'the' is stop word
		{"", []string{}},
		{"a an the", []string{}}, // all stop words
		{"Go Programming!!!", []string{"go", "programming"}},
	}

	for _, tc := range tests {
		tokens := tokenize(tc.input)
		if len(tokens) != len(tc.expected) {
			t.Errorf("tokenize('%s'): expected %v, got %v", tc.input, tc.expected, tokens)
			continue
		}
		for i, expected := range tc.expected {
			if tokens[i] != expected {
				t.Errorf("tokenize('%s')[%d]: expected %s, got %s", tc.input, i, expected, tokens[i])
			}
		}
	}
}

func TestParseDocID(t *testing.T) {
	tests := []struct {
		docID         string
		expectedFolder string
		expectedUID   uint32
		expectError   bool
	}{
		{"INBOX:1", "INBOX", 1, false},
		{"Sent:100", "Sent", 100, false},
		{"invalid", "", 0, true},
		{"folder:notanumber", "", 0, true},
		{"", "", 0, true},
	}

	for _, tc := range tests {
		folder, uid, err := parseDocID(tc.docID)
		if tc.expectError {
			if err == nil {
				t.Errorf("parseDocID('%s'): expected error", tc.docID)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDocID('%s'): unexpected error: %v", tc.docID, err)
			continue
		}
		if folder != tc.expectedFolder {
			t.Errorf("parseDocID('%s'): expected folder '%s', got '%s'", tc.docID, tc.expectedFolder, folder)
		}
		if uid != tc.expectedUID {
			t.Errorf("parseDocID('%s'): expected uid %d, got %d", tc.docID, tc.expectedUID, uid)
		}
	}
}

func TestGeneratePreview(t *testing.T) {
	tests := []struct {
		content string
		maxLen  int
		expectedLen int
	}{
		{"Hello World", 20, 11},
		{"This is a very long text", 10, 13}, // 10 + "..."
		{"", 10, 0},
		{"Short", 100, 5},
	}

	for _, tc := range tests {
		result := generatePreview(tc.content, tc.maxLen)
		if len(result) != tc.expectedLen {
			t.Errorf("generatePreview('%s', %d): expected len %d, got %d (%s)", tc.content, tc.maxLen, tc.expectedLen, len(result), result)
		}
	}
}

func TestExtractTextContent(t *testing.T) {
	// Test HTML content
	htmlContent := "Content-Type: text/html\r\n\r\n<html><body><p>Hello World</p></body></html>"
	result := extractTextContent([]byte(htmlContent))
	if result == "" {
		t.Error("expected non-empty extracted content")
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<p>Hello</p>", "Hello"},
		{"<div><span>Test</span></div>", "Test"},
		{"Plain text", "Plain text"},
		{"", ""},
		{"<br>", ""},
	}

	for _, tc := range tests {
		result := stripHTML(tc.input)
		if result != tc.expected {
			t.Errorf("stripHTML('%s'): expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestNewService(t *testing.T) {
	svc := NewService(nil, nil, nil)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}
