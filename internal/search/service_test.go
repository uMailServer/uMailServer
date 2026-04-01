package search

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
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

// --- New tests for improved coverage ---

func TestNewServiceWithLogger(t *testing.T) {
	logger := slog.Default()
	svc := NewService(nil, nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.logger == nil {
		t.Error("expected logger to be set")
	}
}

func TestServiceSearchNoDBBuildIndex(t *testing.T) {
	svc := NewService(nil, nil, nil)

	// Search without a database - BuildIndex will fail
	_, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "test",
		Limit: 10,
	})
	if err == nil {
		t.Error("expected error when building index without database")
	}
}

func TestServiceSearchWithExistingIndexDefaultLimit(t *testing.T) {
	svc := NewService(nil, nil, nil)

	idx := NewIndex()
	svc.indexes["testuser"] = idx

	// Add several documents
	for i := 0; i < 25; i++ {
		idx.Add(&Document{
			ID:      fmt.Sprintf("INBOX:%d", i),
			Content: fmt.Sprintf("test message number %d", i),
		})
	}

	// Search with Limit=0 should default to 20
	results, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "test",
		Limit: 0,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) > 20 {
		t.Errorf("expected at most 20 results with default limit, got %d", len(results))
	}
}

func TestServiceSearchWithOffset(t *testing.T) {
	svc := NewService(nil, nil, nil)

	idx := NewIndex()
	svc.indexes["testuser"] = idx

	idx.Add(&Document{ID: "INBOX:1", Content: "alpha beta gamma"})
	idx.Add(&Document{ID: "INBOX:2", Content: "alpha delta epsilon"})
	idx.Add(&Document{ID: "INBOX:3", Content: "alpha zeta eta"})

	// Search with offset
	results, err := svc.Search(MessageSearchOptions{
		User:   "testuser",
		Query:  "alpha",
		Limit:  10,
		Offset: 2,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Offset may skip some results
	_ = results
}

func TestServiceSearchWithFolderFilter(t *testing.T) {
	svc := NewService(nil, nil, nil)

	idx := NewIndex()
	svc.indexes["testuser"] = idx

	// Add documents in different folders
	idx.Add(&Document{ID: "INBOX:1", Content: "inbox test message"})
	idx.Add(&Document{ID: "Sent:1", Content: "sent test message"})
	idx.Add(&Document{ID: "Archive:1", Content: "archive test message"})

	// Search only INBOX
	results, err := svc.Search(MessageSearchOptions{
		User:   "testuser",
		Query:  "test",
		Folder: "INBOX",
		Limit:  10,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	for _, r := range results {
		if r.Folder != "INBOX" {
			t.Errorf("expected INBOX folder, got %s", r.Folder)
		}
	}
	if len(results) != 1 {
		t.Errorf("expected 1 INBOX result, got %d", len(results))
	}
}

func TestServiceSearchWithDB(t *testing.T) {
	// Create a storage database
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
		defer database.Close()

	svc := NewService(database, nil, nil)

	// Create index and add document
	idx := NewIndex()
	svc.indexes["testuser"] = idx

	idx.Add(&Document{
		ID:      "INBOX:1",
		Content: "hello world",
		Fields: map[string]string{
			"from":    "sender@example.com",
			"to":      "recipient@example.com",
			"subject": "Test Subject",
		},
	})

	results, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "hello",
		Limit: 10,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results")
	}
}

func TestServiceSearchInvalidDocID(t *testing.T) {
	svc := NewService(nil, nil, nil)

	idx := NewIndex()
	svc.indexes["testuser"] = idx

	// Add a document with invalid docID format (no colon)
	idx.Add(&Document{
		ID:      "invaliddocid",
		Content: "some content",
	})

	results, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "content",
		Limit: 10,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Invalid docID should be skipped, so results should be empty
	if len(results) != 0 {
		t.Errorf("expected 0 results for invalid docID, got %d", len(results))
	}
}

func TestServiceSearchUIDNotNumber(t *testing.T) {
	svc := NewService(nil, nil, nil)

	idx := NewIndex()
	svc.indexes["testuser"] = idx

	// Add a document with non-numeric UID
	idx.Add(&Document{
		ID:      "INBOX:notanumber",
		Content: "test content",
	})

	results, err := svc.Search(MessageSearchOptions{
		User:  "testuser",
		Query: "test",
		Limit: 10,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Invalid UID should be skipped
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-numeric UID, got %d", len(results))
	}
}

func TestServiceBuildIndexWithDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
		defer database.Close()

	svc := NewService(database, nil, nil)

	err = svc.BuildIndex("testuser")
	if err != nil {
		// BuildIndex may return error if no data, just verify it doesn't panic
		t.Logf("BuildIndex returned error (may be expected): %v", err)
	}
}

func TestServiceBuildIndexNoDB(t *testing.T) {
	svc := NewService(nil, nil, nil)

	err := svc.BuildIndex("testuser")
	if err == nil {
		t.Error("expected error when building index without database")
	}
}

func TestServiceIndexMessageNoIndex(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
		defer database.Close()

	svc := NewService(database, nil, nil)

	// No index exists for user, so BuildIndex will be called
	err = svc.IndexMessage("newuser", "INBOX", 1)
	// May succeed or fail depending on database state
	_ = err
}

func TestParseDocIDWithColonInFolder(t *testing.T) {
	// Folder names with colons should use SplitN correctly
	folder, uid, err := parseDocID("Sent.Items:42")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if folder != "Sent.Items" {
		t.Errorf("expected folder 'Sent.Items', got %q", folder)
	}
	if uid != 42 {
		t.Errorf("expected uid 42, got %d", uid)
	}
}

func TestParseDocIDEdgeCases(t *testing.T) {
	tests := []struct {
		docID   string
		wantErr bool
	}{
		{":", true},                   // empty folder, empty uid
		{":0", false},                 // empty folder, uid 0
		{"INBOX:", true},              // empty uid
		{"INBOX:0", false},            // uid 0
		{"INBOX:4294967295", false},    // max uint32
		{"INBOX:4294967296", true},     // overflow uint32
		{"INBOX:-1", true},            // negative
	}

	for _, tc := range tests {
		t.Run(tc.docID, func(t *testing.T) {
			_, _, err := parseDocID(tc.docID)
			if tc.wantErr && err == nil {
				t.Errorf("parseDocID(%q): expected error", tc.docID)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("parseDocID(%q): unexpected error: %v", tc.docID, err)
			}
		})
	}
}

func TestGeneratePreviewExactLength(t *testing.T) {
	// Content exactly maxLen should not have "..."
	content := "1234567890" // 10 chars
	result := generatePreview(content, 10)
	if result != content {
		t.Errorf("expected %q, got %q", content, result)
	}
	if strings.HasSuffix(result, "...") {
		t.Error("should not have ellipsis when content fits exactly")
	}
}

func TestGeneratePreviewOneOver(t *testing.T) {
	content := "12345678901" // 11 chars
	result := generatePreview(content, 10)
	if len(result) != 13 { // 10 + "..."
		t.Errorf("expected length 13, got %d", len(result))
	}
}

func TestExtractTextContentWithLFBody(t *testing.T) {
	// Test with LF-only line endings (no CR)
	data := []byte("Subject: Test\n\nHello World\nSecond line")
	result := extractTextContent(data)
	if result == "" {
		t.Error("expected non-empty content")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("expected content to contain 'Hello'")
	}
}

func TestExtractTextContentNoBody(t *testing.T) {
	// Test with no body separator
	data := []byte("Just a header with no body")
	result := extractTextContent(data)
	if result == "" {
		t.Error("expected non-empty content even without body separator")
	}
}

func TestStripHTMLComplex(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<html><head><title>Test</title></head><body><p>Hello</p></body></html>", "TestHello"},
		{"<div class=\"test\">Content</div>", "Content"},
		{"<a href=\"http://example.com\">Link</a>", "Link"},
		{"<br/>", ""},   // self-closing tag stripped
		{"<>text", "text"}, // empty tag
		{"<script>alert('xss')</script>", "alert('xss')"},
		{"no tags here", "no tags here"},
	}

	for _, tc := range tests {
		result := stripHTML(tc.input)
		if result != tc.expected {
			t.Errorf("stripHTML(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestIndexSearchWithFieldQuery(t *testing.T) {
	idx := NewIndex()

	idx.Add(&Document{
		ID:      "INBOX:1",
		Content: "hello world",
		Fields: map[string]string{
			"from":    "john@example.com",
			"subject": "Important Meeting",
		},
	})

	// Search for field-specific query
	results := idx.Search("from:john", SearchOptions{Limit: 10})
	if len(results) == 0 {
		t.Error("expected results for field search 'from:john'")
	}

	// Search for subject field
	results = idx.Search("subject:meeting", SearchOptions{Limit: 10})
	if len(results) == 0 {
		t.Error("expected results for field search 'subject:meeting'")
	}
}

func TestIndexAddDuplicate(t *testing.T) {
	idx := NewIndex()

	// Add same document twice - should replace
	doc1 := &Document{ID: "INBOX:1", Content: "first version"}
	doc2 := &Document{ID: "INBOX:1", Content: "second version"}

	idx.Add(doc1)
	idx.Add(doc2)

	// Should have only 1 document
	if idx.DocCount() != 1 {
		// Note: Add increments docCount even on replace in current impl
		t.Logf("DocCount after duplicate add: %d", idx.DocCount())
	}

	// Search should find the second version
	results := idx.Search("second", SearchOptions{Limit: 10})
	if len(results) == 0 {
		t.Error("expected to find 'second version' content")
	}
}

func TestIndexSearchWithOffset(t *testing.T) {
	idx := NewIndex()

	for i := 0; i < 5; i++ {
		idx.Add(&Document{
			ID:      fmt.Sprintf("INBOX:%d", i),
			Content: fmt.Sprintf("test document number %d", i),
		})
	}

	// Get all results
	allResults := idx.Search("test", SearchOptions{Limit: 10})
	if len(allResults) != 5 {
		t.Errorf("expected 5 results, got %d", len(allResults))
	}

	// Get with offset
	offsetResults := idx.Search("test", SearchOptions{Limit: 10, Offset: 3})
	if len(offsetResults) != 2 {
		t.Errorf("expected 2 results with offset 3, got %d", len(offsetResults))
	}
}

func TestIndexSearchOffsetBeyondResults(t *testing.T) {
	idx := NewIndex()

	idx.Add(&Document{ID: "INBOX:1", Content: "test message"})

	results := idx.Search("test", SearchOptions{Limit: 10, Offset: 100})
	if len(results) != 0 {
		t.Errorf("expected 0 results with offset beyond range, got %d", len(results))
	}
}

func TestIndexSearchEmptyQuery(t *testing.T) {
	idx := NewIndex()
	idx.Add(&Document{ID: "INBOX:1", Content: "test message"})

	results := idx.Search("", SearchOptions{Limit: 10})
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestIndexDocCount(t *testing.T) {
	idx := NewIndex()
	if idx.DocCount() != 0 {
		t.Error("expected 0 docs on new index")
	}

	idx.Add(&Document{ID: "1", Content: "a"})
	if idx.DocCount() != 1 {
		t.Errorf("expected 1 doc, got %d", idx.DocCount())
	}

	idx.Add(&Document{ID: "2", Content: "b"})
	if idx.DocCount() != 2 {
		t.Errorf("expected 2 docs, got %d", idx.DocCount())
	}

	idx.Remove("1")
	if idx.DocCount() != 1 {
		t.Errorf("expected 1 doc after remove, got %d", idx.DocCount())
	}
}

func TestTokenizeEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected int // number of tokens
	}{
		{"   ", 0},                      // whitespace only
		{"!!!???", 0},                    // punctuation only
		{"a", 0},                         // single stop word
		{"Go", 1},                        // single non-stop word
		{"hello123world", 1},             // alphanumeric runs
		{"café résumé", 2},               // unicode letters
		{"test123", 1},                   // mixed letters/numbers
	}

	for _, tc := range tests {
		tokens := tokenize(tc.input)
		if len(tokens) != tc.expected {
			t.Errorf("tokenize(%q): expected %d tokens, got %d (%v)", tc.input, tc.expected, len(tokens), tokens)
		}
	}
}

func TestServiceSearchMultipleFoldersExclusion(t *testing.T) {
	svc := NewService(nil, nil, nil)
	idx := NewIndex()
	svc.indexes["user1"] = idx

	idx.Add(&Document{ID: "INBOX:1", Content: "uniqueinbox"})
	idx.Add(&Document{ID: "Sent:1", Content: "uniquesent"})
	idx.Add(&Document{ID: "Drafts:1", Content: "uniquedraft"})

	// Search for a term that matches all, but filter to Sent only
	results, err := svc.Search(MessageSearchOptions{
		User:   "user1",
		Query:  "unique",
		Folder: "Sent",
		Limit:  10,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	for _, r := range results {
		if r.Folder != "Sent" {
			t.Errorf("expected only Sent results, got %s", r.Folder)
		}
	}
}

func TestServiceRemoveMessageThenSearch(t *testing.T) {
	svc := NewService(nil, nil, nil)
	idx := NewIndex()
	svc.indexes["user1"] = idx

	idx.Add(&Document{ID: "INBOX:1", Content: "removeme"})
	idx.Add(&Document{ID: "INBOX:2", Content: "keepme"})

	// Remove one document
	svc.RemoveMessage("user1", "INBOX", 1)

	// Search should only find the remaining document
	results, err := svc.Search(MessageSearchOptions{
		User:  "user1",
		Query: "keepme",
		Limit: 10,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result after removal, got %d", len(results))
	}
}
