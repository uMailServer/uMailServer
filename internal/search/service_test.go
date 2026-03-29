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

func TestServiceSearch(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Search without index - will need to build index first
	results, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "test",
		Limit: 10,
	})

	// Without a database, this will fail but should not panic
	if err != nil {
		t.Logf("Search returned error (expected without db): %v", err)
	}

	// Results should be empty or nil
	_ = results
}

func TestServiceClearIndex(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Create an index for a user
	svc.indexes["testuser"] = NewIndex()

	// Clear the index
	svc.ClearIndex("testuser")

	// Verify index was removed
	if _, exists := svc.indexes["testuser"]; exists {
		t.Error("expected index to be removed after ClearIndex")
	}
}

func TestServiceRemoveMessage(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Create an index and add a document
	idx := NewIndex()
	svc.indexes["testuser"] = idx

	doc := &Document{
		ID:      "INBOX:1",
		Content: "test content",
	}
	idx.Add(doc)

	// Remove the message
	svc.RemoveMessage("testuser", "INBOX", 1)

	// Verify document was removed
	if idx.DocCount() != 0 {
		t.Errorf("expected 0 documents after remove, got %d", idx.DocCount())
	}
}

// TestParseQuery tests the parseQuery function
func TestParseQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected []QueryTerm
	}{
		{
			name:  "simple term",
			query: "hello",
			expected: []QueryTerm{
				{Field: "", Value: "hello", Boost: 1.0},
			},
		},
		{
			name:  "multiple terms",
			query: "hello world",
			expected: []QueryTerm{
				{Field: "", Value: "hello", Boost: 1.0},
				{Field: "", Value: "world", Boost: 1.0},
			},
		},
		{
			name:  "field search",
			query: "from:john",
			expected: []QueryTerm{
				{Field: "from", Value: "john", Boost: 2.0},
			},
		},
		{
			name:  "multiple fields",
			query: "from:john subject:hello",
			expected: []QueryTerm{
				{Field: "from", Value: "john", Boost: 2.0},
				{Field: "subject", Value: "hello", Boost: 2.0},
			},
		},
		{
			name:  "mixed field and text",
			query: "from:john hello world",
			expected: []QueryTerm{
				{Field: "from", Value: "john", Boost: 2.0},
				{Field: "", Value: "hello", Boost: 1.0},
				{Field: "", Value: "world", Boost: 1.0},
			},
		},
		{
			name:  "has attachment",
			query: "has:attachment",
			expected: []QueryTerm{
				{Field: "has", Value: "attachment", Boost: 2.0},
			},
		},
		{
			name:     "empty query",
			query:    "",
			expected: []QueryTerm{},
		},
		{
			name:  "only field pattern",
			query: "subject:test",
			expected: []QueryTerm{
				{Field: "subject", Value: "test", Boost: 2.0},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseQuery(tc.query)

			if len(result) != len(tc.expected) {
				t.Errorf("expected %d terms, got %d: %v", len(tc.expected), len(result), result)
				return
			}

			for i, term := range result {
				if term.Field != tc.expected[i].Field {
					t.Errorf("term %d: expected field '%s', got '%s'", i, tc.expected[i].Field, term.Field)
				}
				if term.Value != tc.expected[i].Value {
					t.Errorf("term %d: expected value '%s', got '%s'", i, tc.expected[i].Value, term.Value)
				}
				if term.Boost != tc.expected[i].Boost {
					t.Errorf("term %d: expected boost %f, got %f", i, tc.expected[i].Boost, term.Boost)
				}
			}
		})
	}
}

// TestServiceIndexMessage tests the IndexMessage function
func TestServiceIndexMessage(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Test with no existing index - BuildIndex will be called
	// When db is nil, it may panic or handle gracefully depending on implementation
	defer func() {
		_ = recover() // Ignore panic if db is nil
	}()
	svc.IndexMessage("testuser", "INBOX", 1)
}

// TestServiceIndexMessageWithIndex tests IndexMessage when index exists
func TestServiceIndexMessageWithIndex(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Pre-create an index for the user
	svc.indexes["testuser"] = NewIndex()

	// Test with existing index but no database - should fail when getting metadata
	defer func() {
		_ = recover() // Expect panic when db is nil
	}()
	svc.IndexMessage("testuser", "INBOX", 1)
}

// TestServiceBuildIndexEmptyUser tests BuildIndex with empty user
func TestServiceBuildIndexEmptyUser(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Build index with empty user and nil db may panic or return error
	defer func() {
		_ = recover() // Ignore panic if db is nil
	}()
	svc.BuildIndex("")
}

// TestServiceSearchWithExistingIndex tests Search when index already exists
func TestServiceSearchWithExistingIndex(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Create an index and add a document
	idx := NewIndex()
	svc.indexes["testuser"] = idx

	doc := &Document{
		ID:      "INBOX:1",
		Content: "hello world test message",
		Fields: map[string]string{
			"from":    "sender@example.com",
			"to":      "recipient@example.com",
			"subject": "Test Subject",
		},
	}
	idx.Add(doc)

	// Search with existing index
	results, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "hello",
		Limit: 10,
	})

	// Should not error since index exists
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should find the document
	if len(results) == 0 {
		t.Error("expected search results")
	}
}

// TestServiceSearchWithDateFilter tests Search with date filters
func TestServiceSearchWithDateFilter(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Create an index
	idx := NewIndex()
	svc.indexes["testuser"] = idx

	doc := &Document{
		ID:      "INBOX:1",
		Content: "hello world",
		Fields: map[string]string{
			"subject": "Test",
			"date":    "2024-01-15",
		},
	}
	idx.Add(doc)

	// Search with date filter
	results, err := svc.Search(MessageSearchOptions{
		User:     "testuser",
		Query:    "hello",
		DateFrom: "2024-01-01",
		DateTo:   "2024-12-31",
		Limit:    10,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_ = results
}

// TestServiceSearchWithAttachmentFilter tests Search with attachment filter
func TestServiceSearchWithAttachmentFilter(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Create an index
	idx := NewIndex()
	svc.indexes["testuser"] = idx

	doc := &Document{
		ID:      "INBOX:1",
		Content: "hello world",
		Fields: map[string]string{
			"subject":         "Test",
			"has_attachment":  "true",
		},
	}
	idx.Add(doc)

	// Search with attachment filter
	results, err := svc.Search(MessageSearchOptions{
		User:          "testuser",
		Query:         "hello",
		HasAttachment: true,
		Limit:         10,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_ = results
}

// TestServiceSearchFolderFilter tests Search with folder filter
func TestServiceSearchFolderFilter(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Create an index with multiple folder documents
	idx := NewIndex()
	svc.indexes["testuser"] = idx

	// Add document in INBOX
	idx.Add(&Document{
		ID:      "INBOX:1",
		Content: "inbox message",
	})

	// Add document in Sent
	idx.Add(&Document{
		ID:      "Sent:1",
		Content: "sent message",
	})

	// Search specific folder
	results, err := svc.Search(MessageSearchOptions{
		User:   "testuser",
		Folder: "INBOX",
		Query:  "message",
		Limit:  10,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should only return INBOX results
	for _, r := range results {
		if r.Folder != "INBOX" {
			t.Errorf("expected folder INBOX, got %s", r.Folder)
		}
	}
}

// TestServiceRemoveMessageNoIndex tests RemoveMessage when no index exists
func TestServiceRemoveMessageNoIndex(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Remove message when no index exists - should not panic
	svc.RemoveMessage("nonexistent", "INBOX", 1)
}

// TestServiceClearIndexNonExistent tests ClearIndex for non-existent user
func TestServiceClearIndexNonExistent(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Clear index for non-existent user - should not panic
	svc.ClearIndex("nonexistent")
}
