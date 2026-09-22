package jmap

import (
	"fmt"
	"time"

	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// setupTestServer creates a test server with temporary storage
func setupTestServer(t *testing.T) (*Server, *storage.Database, *storage.MessageStore, func()) {
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create message store: %v", err)
	}

	config := Config{JWTSecret: "test-secret"}
	server := NewServer(db, msgStore, nil, config)

	cleanup := func() {
		db.Close()
		msgStore.Close()
	}

	return server, db, msgStore, cleanup
}

// Test Mailbox Handlers
func TestHandleMailboxGet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleMailboxGet("user@example.com", call)

	if response.Name != "Mailbox/get" {
		t.Errorf("Response.Name = %s, want Mailbox/get", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}

	list, ok := args["list"].([]Mailbox)
	if !ok {
		t.Fatalf("list is not []Mailbox")
	}

	// Should have at least INBOX
	if len(list) == 0 {
		t.Error("Expected at least one mailbox")
	}
}

func TestHandleMailboxGet_WithIDs(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"inbox"},
		},
		ID: "call-1",
	}

	response := server.handleMailboxGet("user@example.com", call)

	if response.Name != "Mailbox/get" {
		t.Errorf("Response.Name = %s, want Mailbox/get", response.Name)
	}
}

func TestHandleMailboxQuery(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleMailboxQuery("user@example.com", call)

	if response.Name != "Mailbox/query" {
		t.Errorf("Response.Name = %s, want Mailbox/query", response.Name)
	}

	args := response.Args
	if _, ok := args["ids"]; !ok {
		t.Error("Response should contain ids")
	}
}

func TestHandleMailboxQuery_WithFilter(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"filter": map[string]interface{}{
				"name": "Inbox",
			},
		},
		ID: "call-1",
	}

	response := server.handleMailboxQuery("user@example.com", call)

	if response.Name != "Mailbox/query" {
		t.Errorf("Response.Name = %s, want Mailbox/query", response.Name)
	}
}

func TestHandleMailboxSet_Create(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"new-box": map[string]interface{}{
					"name": "New Box",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}

	args := response.Args
	if created, ok := args["created"]; ok {
		createdMap := created.(map[string]Mailbox)
		if _, hasNewBox := createdMap["new-box"]; !hasNewBox {
			t.Error("Expected new-box in created")
		}
	}
}

func TestHandleMailboxSet_Update(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// First create a mailbox
	createCall := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"test-box": map[string]interface{}{
					"name": "Test Box",
				},
			},
		},
		ID: "call-1",
	}
	server.handleMailboxSet("user@example.com", createCall)

	// Then update it
	updateCall := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"test-box": map[string]interface{}{
					"name": "Updated Box",
				},
			},
		},
		ID: "call-2",
	}

	response := server.handleMailboxSet("user@example.com", updateCall)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}
}

func TestHandleMailboxSet_Destroy(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"destroy":   []interface{}{"custom-box"},
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}
}

// Test Email Handlers
func TestHandleEmailGet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleEmailGet("user@example.com", call)

	if response.Name != "Email/get" {
		t.Errorf("Response.Name = %s, want Email/get", response.Name)
	}

	args := response.Args
	if _, ok := args["list"]; !ok {
		t.Error("Response should contain list")
	}
}

func TestHandleEmailGet_WithIDs(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"email-1", "email-2"},
		},
		ID: "call-1",
	}

	response := server.handleEmailGet("user@example.com", call)

	if response.Name != "Email/get" {
		t.Errorf("Response.Name = %s, want Email/get", response.Name)
	}

	args := response.Args
	if notFound, ok := args["notFound"].([]string); ok {
		if len(notFound) != 2 {
			t.Errorf("Expected 2 notFound, got %d", len(notFound))
		}
	}
}

func TestHandleEmailQuery(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleEmailQuery("user@example.com", call)

	if response.Name != "Email/query" {
		t.Errorf("Response.Name = %s, want Email/query", response.Name)
	}

	args := response.Args
	if _, ok := args["ids"]; !ok {
		t.Error("Response should contain ids")
	}
}

func TestHandleEmailQuery_WithFilter(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"filter": map[string]interface{}{
				"inMailbox": "inbox",
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailQuery("user@example.com", call)

	if response.Name != "Email/query" {
		t.Errorf("Response.Name = %s, want Email/query", response.Name)
	}
}

func TestHandleEmailQuery_WithSort(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"sort": []interface{}{
				map[string]interface{}{
					"property":    "receivedAt",
					"isAscending": false,
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailQuery("user@example.com", call)

	if response.Name != "Email/query" {
		t.Errorf("Response.Name = %s, want Email/query", response.Name)
	}
}

func TestHandleEmailSet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"new-email": map[string]interface{}{
					"subject": "Test Subject",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}
}

func TestHandleEmailSet_Update(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"email-1": map[string]interface{}{
					"keywords": map[string]interface{}{
						"$seen": true,
					},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}
}

func TestHandleEmailSet_Destroy(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"destroy":   []interface{}{"email-1"},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}
}

func TestHandleEmailImport(t *testing.T) {
	server, _, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	// First store a message
	msgData := []byte("From: test@example.com\r\nSubject: Test\r\n\r\nBody")
	blobID, err := msgStore.StoreMessage("user@example.com", msgData)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emails": map[string]interface{}{
				"import-1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
					"keywords":   map[string]interface{}{},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailImport("user@example.com", call)

	if response.Name != "Email/import" {
		t.Errorf("Response.Name = %s, want Email/import", response.Name)
	}
}

// Test Thread Handlers
func TestHandleThreadGet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleThreadGet("user@example.com", call)

	if response.Name != "Thread/get" {
		t.Errorf("Response.Name = %s, want Thread/get", response.Name)
	}

	args := response.Args
	if _, ok := args["list"]; !ok {
		t.Error("Response should contain list")
	}
}

func TestHandleThreadGet_WithIDs(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"thread-1", "thread-2"},
		},
		ID: "call-1",
	}

	response := server.handleThreadGet("user@example.com", call)

	if response.Name != "Thread/get" {
		t.Errorf("Response.Name = %s, want Thread/get", response.Name)
	}
}

// Test SearchSnippet Handler
func TestHandleSearchSnippetGet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "SearchSnippet/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emailIds":  []interface{}{"email-1"},
			"filter": map[string]interface{}{
				"text": "test",
			},
		},
		ID: "call-1",
	}

	response := server.handleSearchSnippetGet("user@example.com", call)

	if response.Name != "SearchSnippet/get" {
		t.Errorf("Response.Name = %s, want SearchSnippet/get", response.Name)
	}

	args := response.Args
	if _, ok := args["list"]; !ok {
		t.Error("Response should contain list")
	}
}

// Test Identity Handlers
func TestHandleIdentityGet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleIdentityGet("user@example.com", call)

	if response.Name != "Identity/get" {
		t.Errorf("Response.Name = %s, want Identity/get", response.Name)
	}

	args := response.Args
	list, ok := args["list"].([]Identity)
	if !ok {
		t.Fatalf("list is not []Identity")
	}

	// Should have at least the default identity
	if len(list) == 0 {
		t.Error("Expected at least one identity")
	}
}

func TestHandleIdentityGet_WithIDs(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"identity-1"},
		},
		ID: "call-1",
	}

	response := server.handleIdentityGet("user@example.com", call)

	if response.Name != "Identity/get" {
		t.Errorf("Response.Name = %s, want Identity/get", response.Name)
	}
}

func TestHandleIdentitySet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"new-identity": map[string]interface{}{
					"name":  "Test User",
					"email": "test@example.com",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleIdentitySet("user@example.com", call)

	if response.Name != "Identity/set" {
		t.Errorf("Response.Name = %s, want Identity/set", response.Name)
	}
}

func TestHandleIdentitySet_Update(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"identity-1": map[string]interface{}{
					"name": "Updated Name",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleIdentitySet("user@example.com", call)

	if response.Name != "Identity/set" {
		t.Errorf("Response.Name = %s, want Identity/set", response.Name)
	}
}

func TestHandleIdentitySet_Destroy(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"destroy":   []interface{}{"identity-1"},
		},
		ID: "call-1",
	}

	response := server.handleIdentitySet("user@example.com", call)

	if response.Name != "Identity/set" {
		t.Errorf("Response.Name = %s, want Identity/set", response.Name)
	}
}

// Test Helper Functions
func TestGetMailboxIDFromName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"INBOX", "inbox"},
		{"Sent", "sent"},
		{"Drafts", "drafts"},
		{"Trash", "trash"},
		{"CustomFolder", "CustomFolder"},
	}

	for _, tt := range tests {
		id := getMailboxIDFromName(tt.name)
		if id != tt.expected {
			t.Errorf("getMailboxIDFromName(%q) = %q, want %q", tt.name, id, tt.expected)
		}
	}
}

func TestGetMailboxNameFromID(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"inbox", "INBOX"},
		{"sent", "Sent"},
		{"drafts", "Drafts"},
		{"trash", "Trash"},
	}

	for _, tt := range tests {
		name := getMailboxNameFromID(tt.id)
		if name != tt.expected {
			t.Errorf("getMailboxNameFromID(%q) = %q, want %q", tt.id, name, tt.expected)
		}
	}
}

// Test matchesFilter function
func TestMatchesFilter_NilFilter(t *testing.T) {
	meta := &storage.MessageMetadata{
		Subject: "Test",
		From:    "test@example.com",
	}

	if !matchesFilter(meta, nil) {
		t.Error("matchesFilter should return true for nil filter")
	}
}

func TestMatchesFilter_Text(t *testing.T) {
	meta := &storage.MessageMetadata{
		Subject: "Hello World",
		From:    "sender@example.com",
		To:      "recipient@example.com",
	}

	filter := &FilterCondition{Text: "hello"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match text in subject")
	}

	filter = &FilterCondition{Text: "sender"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match text in from")
	}

	filter = &FilterCondition{Text: "nonexistent"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match nonexistent text")
	}
}

func TestMatchesFilter_Subject(t *testing.T) {
	meta := &storage.MessageMetadata{
		Subject: "Important Meeting",
	}

	filter := &FilterCondition{Subject: "meeting"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match subject")
	}

	filter = &FilterCondition{Subject: "hello"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match wrong subject")
	}
}

func TestMatchesFilter_From(t *testing.T) {
	meta := &storage.MessageMetadata{
		From: "john@example.com",
	}

	filter := &FilterCondition{From: "john"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match from")
	}

	filter = &FilterCondition{From: "jane"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match wrong from")
	}
}

func TestMatchesFilter_To(t *testing.T) {
	meta := &storage.MessageMetadata{
		To: "recipient@example.com",
	}

	filter := &FilterCondition{To: "recipient"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match to")
	}

	filter = &FilterCondition{To: "other"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match wrong to")
	}
}

func TestMatchesFilter_Size(t *testing.T) {
	meta := &storage.MessageMetadata{
		Subject: "Test",
		Size:    5000,
	}

	filter := &FilterCondition{MinSize: 1000}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match minSize")
	}

	filter = &FilterCondition{MinSize: 10000}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match when below minSize")
	}

	filter = &FilterCondition{MaxSize: 10000}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match maxSize")
	}

	filter = &FilterCondition{MaxSize: 1000}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match when above maxSize")
	}
}

func TestMatchesFilter_Unread(t *testing.T) {
	meta := &storage.MessageMetadata{
		Subject: "Test",
		Flags:   []string{},
	}

	filter := &FilterCondition{NotKeyword: "$seen"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match unread messages")
	}

	meta.Flags = []string{"\\Seen"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match read messages when filtering for unread")
	}
}

func TestMatchesFilter_Date(t *testing.T) {
	meta := &storage.MessageMetadata{
		Subject:      "Test",
		InternalDate: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
	}

	filter := &FilterCondition{After: "2024-01-01T00:00:00Z"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match after date")
	}

	filter = &FilterCondition{After: "2024-12-01T00:00:00Z"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match when before after date")
	}

	filter = &FilterCondition{Before: "2024-12-01T00:00:00Z"}
	if !matchesFilter(meta, filter) {
		t.Error("matchesFilter should match before date")
	}

	filter = &FilterCondition{Before: "2024-01-01T00:00:00Z"}
	if matchesFilter(meta, filter) {
		t.Error("matchesFilter should not match when after before date")
	}
}

// Test Email Query with sorting
func TestHandleEmailQuery_WithDifferentSorts(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some test messages
	for i := 0; i < 3; i++ {
		meta := &storage.MessageMetadata{
			UID:          uint32(i + 1),
			MessageID:    fmt.Sprintf("msg-%d", i),
			Subject:      fmt.Sprintf("Subject %d", i),
			From:         fmt.Sprintf("sender%d@example.com", i),
			To:           fmt.Sprintf("recipient%d@example.com", i),
			Size:         int64(1000 * (i + 1)),
			InternalDate: time.Now().Add(time.Duration(-i) * time.Hour),
			Flags:        []string{},
		}
		db.StoreMessageMetadata("user@example.com", "INBOX", uint32(i+1), meta)
	}

	sorts := []struct {
		property    string
		isAscending bool
	}{
		{"receivedAt", false},
		{"receivedAt", true},
		{"sentAt", false},
		{"from", true},
		{"to", true},
		{"subject", true},
		{"size", false},
	}

	for _, s := range sorts {
		call := MethodCall{
			Name: "Email/query",
			Args: map[string]interface{}{
				"accountId": "user@example.com",
				"sort": []interface{}{
					map[string]interface{}{
						"property":    s.property,
						"isAscending": s.isAscending,
					},
				},
			},
			ID: "call-1",
		}

		response := server.handleEmailQuery("user@example.com", call)
		if response.Name != "Email/query" {
			t.Errorf("Response.Name = %s for sort %s, want Email/query", response.Name, s.property)
		}
	}
}

// Test storageToJMAPEmail

// Test storageToJMAPEmail
func TestStorageToJMAPEmail(t *testing.T) {
	meta := &storage.MessageMetadata{
		MessageID:    "msg-123",
		ThreadID:     "thread-123",
		Subject:      "Test Subject",
		From:         "sender@example.com",
		To:           "recipient@example.com",
		Size:         1024,
		InternalDate: time.Now(),
		Flags:        []string{},
	}

	email := storageToJMAPEmail(meta, nil, "INBOX")

	if email.ID != "msg-123" {
		t.Errorf("ID = %q, want msg-123", email.ID)
	}
	if email.Subject != "Test Subject" {
		t.Errorf("Subject = %q, want Test Subject", email.Subject)
	}
	if len(email.From) != 1 || email.From[0].Email != "sender@example.com" {
		t.Errorf("From = %v, want sender@example.com", email.From)
	}
	if len(email.To) != 1 || email.To[0].Email != "recipient@example.com" {
		t.Errorf("To = %v, want recipient@example.com", email.To)
	}
}

// Test storageToJMAPEmail with all flags
func TestStorageToJMAPEmail_AllFlags(t *testing.T) {
	meta := &storage.MessageMetadata{
		MessageID:    "msg-all-flags",
		Subject:      "All Flags Test",
		InternalDate: time.Now(),
		Flags:        []string{},
	}

	email := storageToJMAPEmail(meta, nil, "INBOX")

	if email.Keywords == nil {
		t.Error("Keywords should not be nil")
	}
}

// Test parseFilter function
func TestParseFilter(t *testing.T) {
	result := parseFilter(nil)
	if result != nil {
		t.Errorf("Expected nil for nil filter, got %v", result)
	}

	filterMap := map[string]interface{}{
		"inMailbox": "inbox",
		"text":      "search",
	}
	result = parseFilter(filterMap)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.InMailbox != "inbox" {
		t.Errorf("InMailbox = %q, want inbox", result.InMailbox)
	}
	if result.Text != "search" {
		t.Errorf("Text = %q, want search", result.Text)
	}
}

// Test handleEmailSet with existing message
func TestHandleEmailSet_UpdateKeywords(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a mailbox and store a message
	db.CreateMailbox("user@example.com", "INBOX")
	meta := &storage.MessageMetadata{
		UID:          1,
		MessageID:    "email-123",
		Subject:      "Test",
		InternalDate: time.Now(),
		Flags:        []string{},
	}
	db.StoreMessageMetadata("user@example.com", "INBOX", 1, meta)

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"email-123": map[string]interface{}{
					"keywords": map[string]interface{}{
						"$seen": true,
					},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}
}

func TestHandleEmailSet_MoveMessage(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create mailboxes and store a message
	db.CreateMailbox("user@example.com", "INBOX")
	db.CreateMailbox("user@example.com", "Archive")
	meta := &storage.MessageMetadata{
		UID:          1,
		MessageID:    "email-move",
		Subject:      "Test",
		InternalDate: time.Now(),
		Flags:        []string{},
	}
	db.StoreMessageMetadata("user@example.com", "INBOX", 1, meta)

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"email-move": map[string]interface{}{
					"mailboxIds": map[string]interface{}{
						"archive": true,
					},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}
}

func TestHandleEmailSet_DestroyNotFound(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"destroy":   []interface{}{"nonexistent-email"},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}

	// Should have notDestroyed for non-existent email
	args := response.Args
	if notDestroyed, ok := args["notDestroyed"]; ok {
		notDestroyedMap := notDestroyed.(map[string]interface{})
		if _, hasIt := notDestroyedMap["nonexistent-email"]; !hasIt {
			t.Error("Expected nonexistent-email in notDestroyed")
		}
	}
}

func TestHandleEmailSet_CreateNotSupported(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"new-email": map[string]interface{}{
					"subject": "Test",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}

	// Should have notCreated since create is not supported
	args := response.Args
	if notCreated, ok := args["notCreated"]; ok {
		notCreatedMap := notCreated.(map[string]interface{})
		if _, hasIt := notCreatedMap["new-email"]; !hasIt {
			t.Error("Expected new-email in notCreated")
		}
	}

}

// Test handleEmailImport error cases
func TestHandleEmailImport_InvalidData(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emails": map[string]interface{}{
				"import-1": "invalid-data",
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailImport("user@example.com", call)

	if response.Name != "Email/import" {
		t.Errorf("Response.Name = %s, want Email/import", response.Name)
	}

	args := response.Args
	if notCreated, ok := args["notCreated"]; ok {
		notCreatedMap := notCreated.(map[string]interface{})
		if _, hasIt := notCreatedMap["import-1"]; !hasIt {
			t.Error("Expected import-1 in notCreated for invalid data")
		}
	}
}

func TestHandleEmailImport_MissingBlobId(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emails": map[string]interface{}{
				"import-1": map[string]interface{}{
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailImport("user@example.com", call)

	if response.Name != "Email/import" {
		t.Errorf("Response.Name = %s, want Email/import", response.Name)
	}

	args := response.Args
	if notCreated, ok := args["notCreated"]; ok {
		notCreatedMap := notCreated.(map[string]interface{})
		if _, hasIt := notCreatedMap["import-1"]; !hasIt {
			t.Error("Expected import-1 in notCreated for missing blobId")
		}
	}
}

func TestHandleEmailImport_BlobNotFound(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emails": map[string]interface{}{
				"import-1": map[string]interface{}{
					"blobId":     "nonexistent-blob",
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailImport("user@example.com", call)

	if response.Name != "Email/import" {
		t.Errorf("Response.Name = %s, want Email/import", response.Name)
	}

	args := response.Args
	if notCreated, ok := args["notCreated"]; ok {
		notCreatedMap := notCreated.(map[string]interface{})
		if _, hasIt := notCreatedMap["import-1"]; !hasIt {
			t.Error("Expected import-1 in notCreated for nonexistent blob")
		}
	}
}

func TestHandleEmailImport_WithKeywords(t *testing.T) {
	server, _, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	// Store a message
	msgData := []byte("From: test@example.com\r\nSubject: Test\r\n\r\nBody")
	blobID, err := msgStore.StoreMessage("user@example.com", msgData)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emails": map[string]interface{}{
				"import-1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
					"keywords": map[string]interface{}{
						"$seen":     true,
						"$answered": true,
						"$flagged":  true,
						"$draft":    true,
					},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailImport("user@example.com", call)

	if response.Name != "Email/import" {
		t.Errorf("Response.Name = %s, want Email/import", response.Name)
	}

	args := response.Args
	if created, ok := args["created"]; ok {
		createdMap := created.(map[string]Email)
		if email, hasIt := createdMap["import-1"]; hasIt {
			if !email.Keywords["$seen"] {
				t.Error("Expected $seen keyword")
			}
			if !email.Keywords["$answered"] {
				t.Error("Expected $answered keyword")
			}
			if !email.Keywords["$flagged"] {
				t.Error("Expected $flagged keyword")
			}
			if !email.Keywords["$draft"] {
				t.Error("Expected $draft keyword")
			}
		} else {
			t.Error("Expected import-1 in created")
		}
	}
}

func TestHandleEmailImport_WithReceivedAt(t *testing.T) {
	server, _, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	// Store a message
	msgData := []byte("From: test@example.com\r\nSubject: Test\r\n\r\nBody")
	blobID, err := msgStore.StoreMessage("user@example.com", msgData)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	receivedAt := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"emails": map[string]interface{}{
				"import-1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
					"receivedAt": receivedAt,
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailImport("user@example.com", call)

	if response.Name != "Email/import" {
		t.Errorf("Response.Name = %s, want Email/import", response.Name)
	}

	args := response.Args
	if created, ok := args["created"]; ok {
		createdMap := created.(map[string]Email)
		if _, hasIt := createdMap["import-1"]; !hasIt {
			t.Error("Expected import-1 in created")
		}
	}
}

// Test handleEmailGet with messages in database
func TestHandleEmailGet_WithStoredMessages(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create mailbox and store messages
	db.CreateMailbox("user@example.com", "INBOX")
	for i := 1; i <= 3; i++ {
		meta := &storage.MessageMetadata{
			UID:          uint32(i),
			MessageID:    fmt.Sprintf("email-%d", i),
			Subject:      fmt.Sprintf("Subject %d", i),
			From:         fmt.Sprintf("sender%d@example.com", i),
			To:           fmt.Sprintf("recipient%d@example.com", i),
			InternalDate: time.Now(),
			Flags:        []string{},
		}
		db.StoreMessageMetadata("user@example.com", "INBOX", uint32(i), meta)
	}

	call := MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"email-1", "email-2", "email-3"},
		},
		ID: "call-1",
	}

	response := server.handleEmailGet("user@example.com", call)

	if response.Name != "Email/get" {
		t.Errorf("Response.Name = %s, want Email/get", response.Name)
	}

	args := response.Args
	list, ok := args["list"].([]Email)
	if !ok {
		t.Fatalf("list is not []Email")
	}

	if len(list) != 3 {
		t.Errorf("Expected 3 emails, got %d", len(list))
	}

	// Check notFound is empty
	notFound, ok := args["notFound"].([]string)
	if ok && len(notFound) > 0 {
		t.Errorf("Expected no notFound, got %v", notFound)
	}
}

func TestHandleEmailGet_MessageNotFound(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"nonexistent-email"},
		},
		ID: "call-1",
	}

	response := server.handleEmailGet("user@example.com", call)

	if response.Name != "Email/get" {
		t.Errorf("Response.Name = %s, want Email/get", response.Name)
	}

	args := response.Args
	notFound, ok := args["notFound"].([]string)
	if !ok || len(notFound) != 1 || notFound[0] != "nonexistent-email" {
		t.Errorf("Expected nonexistent-email in notFound, got %v", notFound)
	}
}

func TestHandleEmailGet_MultipleMailboxes(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create multiple mailboxes with messages
	db.CreateMailbox("user@example.com", "INBOX")
	db.CreateMailbox("user@example.com", "Sent")

	meta1 := &storage.MessageMetadata{
		UID:          1,
		MessageID:    "inbox-email",
		Subject:      "Inbox Message",
		InternalDate: time.Now(),
		Flags:        []string{},
	}
	db.StoreMessageMetadata("user@example.com", "INBOX", 1, meta1)

	meta2 := &storage.MessageMetadata{
		UID:          1,
		MessageID:    "sent-email",
		Subject:      "Sent Message",
		InternalDate: time.Now(),
		Flags:        []string{},
	}
	db.StoreMessageMetadata("user@example.com", "Sent", 1, meta2)

	call := MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"inbox-email", "sent-email"},
		},
		ID: "call-1",
	}

	response := server.handleEmailGet("user@example.com", call)

	if response.Name != "Email/get" {
		t.Errorf("Response.Name = %s, want Email/get", response.Name)
	}

	args := response.Args
	list, ok := args["list"].([]Email)
	if !ok {
		t.Fatalf("list is not []Email")
	}

	if len(list) != 2 {
		t.Errorf("Expected 2 emails, got %d", len(list))
	}
}

// Test handleMailboxSet with empty arguments
func TestHandleMailboxSet_Empty(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}

	// Should succeed with empty results
	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleMailboxSet_UpdateInvalidData(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"mailbox-1": "invalid-update-data",
			},
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}

	// Should have notUpdated for invalid data
	args := response.Args
	if notUpdated, ok := args["notUpdated"]; ok {
		notUpdatedMap := notUpdated.(map[string]interface{})
		if _, hasIt := notUpdatedMap["mailbox-1"]; !hasIt {
			t.Error("Expected mailbox-1 in notUpdated for invalid data")
		}
	}
}

func TestHandleMailboxSet_CreateInvalidData(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"new-box": "invalid-create-data",
			},
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}

	// Should have notCreated for invalid data
	args := response.Args
	if notCreated, ok := args["notCreated"]; ok {
		notCreatedMap := notCreated.(map[string]interface{})
		if _, hasIt := notCreatedMap["new-box"]; !hasIt {
			t.Error("Expected new-box in notCreated for invalid data")
		}
	}
}

func TestHandleMailboxSet_CreateEmptyName(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"create": map[string]interface{}{
				"new-box": map[string]interface{}{
					"name": "",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}

	// Should have notCreated for empty name
	args := response.Args
	if notCreated, ok := args["notCreated"]; ok {
		notCreatedMap := notCreated.(map[string]interface{})
		if _, hasIt := notCreatedMap["new-box"]; !hasIt {
			t.Error("Expected new-box in notCreated for empty name")
		}
	}
}

func TestHandleMailboxSet_Rename(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a mailbox first
	db.CreateMailbox("user@example.com", "OldName")

	call := MethodCall{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"oldname": map[string]interface{}{
					"name": "NewName",
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleMailboxSet("user@example.com", call)

	if response.Name != "Mailbox/set" {
		t.Errorf("Response.Name = %s, want Mailbox/set", response.Name)
	}
}

// Test parseEmailMetadata
func TestParseEmailMetadata(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		messageID  string
		expectFrom string
		expectSubj string
	}{
		{
			name:       "Simple email",
			data:       []byte("From: sender@example.com\r\nSubject: Test Subject\r\n\r\nBody"),
			messageID:  "msg-1",
			expectFrom: "sender@example.com",
			expectSubj: "Test Subject",
		},
		{
			name:       "Empty data",
			data:       []byte{},
			messageID:  "msg-empty",
			expectFrom: "",
			expectSubj: "",
		},
		{
			name:       "Email with To header",
			data:       []byte("From: from@example.com\r\nTo: to@example.com\r\nSubject: To Test\r\n\r\nBody"),
			messageID:  "msg-to",
			expectFrom: "from@example.com",
			expectSubj: "To Test",
		},
		{
			name:       "Email with Date header",
			data:       []byte("From: sender@test.com\r\nDate: Mon, 01 Jan 2024 12:00:00 +0000\r\nSubject: Dated\r\n\r\nBody"),
			messageID:  "msg-date",
			expectFrom: "sender@test.com",
			expectSubj: "Dated",
		},
		{
			name:       "Email with References header",
			data:       []byte("From: a@b.com\r\nReferences: <ref1@example.com> <ref2@example.com>\r\nSubject: Refs\r\n\r\nBody"),
			messageID:  "msg-refs",
			expectFrom: "a@b.com",
			expectSubj: "Refs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := parseEmailMetadata(tt.data, tt.messageID)

			if meta.MessageID != tt.messageID {
				t.Errorf("MessageID = %q, want %q", meta.MessageID, tt.messageID)
			}
			if meta.From != tt.expectFrom {
				t.Errorf("From = %q, want %q", meta.From, tt.expectFrom)
			}
			if meta.Subject != tt.expectSubj {
				t.Errorf("Subject = %q, want %q", meta.Subject, tt.expectSubj)
			}
		})
	}
}

func TestParseEmailMetadata_InReplyTo(t *testing.T) {
	data := []byte("From: a@b.com\r\nIn-Reply-To: <original@example.com>\r\nSubject: Reply\r\n\r\nBody")
	meta := parseEmailMetadata(data, "msg-reply")

	if meta.InReplyTo != "<original@example.com>" {
		t.Errorf("InReplyTo = %q, want <original@example.com>", meta.InReplyTo)
	}
}

func TestStorageToJMAPEmail_Flags(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		expected map[string]bool
	}{
		{
			name:     "Seen flag",
			flags:    []string{`\Seen`},
			expected: map[string]bool{"$seen": true},
		},
		{
			name:     "Answered flag",
			flags:    []string{`\Answered`},
			expected: map[string]bool{"$answered": true},
		},
		{
			name:     "Flagged flag",
			flags:    []string{`\Flagged`},
			expected: map[string]bool{"$flagged": true},
		},
		{
			name:     "Draft flag",
			flags:    []string{`\Draft`},
			expected: map[string]bool{"$draft": true},
		},
		{
			name:  "All flags",
			flags: []string{`\Seen`, `\Answered`, `\Flagged`, `\Draft`},
			expected: map[string]bool{
				"$seen":     true,
				"$answered": true,
				"$flagged":  true,
				"$draft":    true,
			},
		},
		{
			name:     "Unknown flag",
			flags:    []string{`\Unknown`},
			expected: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &storage.MessageMetadata{
				MessageID:    "msg-123",
				InternalDate: time.Now(),
				Flags:        tt.flags,
			}

			email := storageToJMAPEmail(meta, nil, "INBOX")

			for keyword, expected := range tt.expected {
				if email.Keywords[keyword] != expected {
					t.Errorf("Keywords[%s] = %v, want %v", keyword, email.Keywords[keyword], expected)
				}
			}
		})
	}
}

// Test storageToJMAPEmail with different mailboxes
func TestStorageToJMAPEmail_Mailboxes(t *testing.T) {
	tests := []struct {
		mailbox  string
		expected string
	}{
		{"INBOX", "inbox"},
		{"Sent", "sent"},
		{"Drafts", "drafts"},
		{"Trash", "trash"},
	}

	for _, tt := range tests {
		t.Run(tt.mailbox, func(t *testing.T) {
			meta := &storage.MessageMetadata{
				MessageID:    "msg-123",
				InternalDate: time.Now(),
			}

			email := storageToJMAPEmail(meta, nil, tt.mailbox)

			if !email.MailboxIDs[tt.expected] {
				t.Errorf("MailboxIDs[%s] should be true", tt.expected)
			}
		})
	}
}

// Test parseEmailMetadata with threading headers
func TestParseEmailMetadata_Threading(t *testing.T) {
	// Email with threading headers
	data := []byte("From: a@b.com\r\nSubject: Thread Test\r\nMessage-Id: \u003cmsg-123@example.com\u003e\r\n\r\nBody")
	meta := parseEmailMetadata(data, "msg-123")

	// Should have parsed the subject
	if meta.Subject != "Thread Test" {
		t.Errorf("Subject = %q, want Thread Test", meta.Subject)
	}
}

// Test getMailboxIDFromName with unknown mailboxes
func TestHandleEmailSet_DestroyExisting(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create mailbox and store message
	db.CreateMailbox("user@example.com", "INBOX")
	meta := &storage.MessageMetadata{
		UID:          1,
		MessageID:    "email-to-delete",
		Subject:      "Delete Me",
		InternalDate: time.Now(),
		Flags:        []string{},
	}
	db.StoreMessageMetadata("user@example.com", "INBOX", 1, meta)

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"destroy":   []interface{}{"email-to-delete"},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}

	// Check destroyed list
	args := response.Args
	if destroyed, ok := args["destroyed"].([]string); ok {
		found := false
		for _, id := range destroyed {
			if id == "email-to-delete" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected email-to-delete in destroyed list")
		}
	}
}

// Test handleEmailSet update with invalid email ID
func TestHandleEmailSet_UpdateNotFound(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"nonexistent-email": map[string]interface{}{
					"keywords": map[string]interface{}{
						"$seen": true,
					},
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}

	// Check notUpdated
	args := response.Args
	if notUpdated, ok := args["notUpdated"]; ok {
		notUpdatedMap := notUpdated.(map[string]interface{})
		if _, hasIt := notUpdatedMap["nonexistent-email"]; !hasIt {
			t.Error("Expected nonexistent-email in notUpdated")
		}
	}
}

// Test handleEmailSet update with invalid data type
func TestHandleEmailSet_UpdateInvalidType(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"update": map[string]interface{}{
				"email-1": "invalid-update-data",
			},
		},
		ID: "call-1",
	}

	response := server.handleEmailSet("user@example.com", call)

	if response.Name != "Email/set" {
		t.Errorf("Response.Name = %s, want Email/set", response.Name)
	}
}

// Test handleThreadGet with messages in database
func TestHandleThreadGet_WithMessages(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create mailbox and store messages with thread IDs
	db.CreateMailbox("user@example.com", "INBOX")
	for i := 1; i <= 3; i++ {
		meta := &storage.MessageMetadata{
			UID:          uint32(i),
			MessageID:    fmt.Sprintf("email-%d", i),
			ThreadID:     "thread-123",
			Subject:      "Thread Subject",
			InternalDate: time.Now(),
			Flags:        []string{},
		}
		db.StoreMessageMetadata("user@example.com", "INBOX", uint32(i), meta)
	}

	call := MethodCall{
		Name: "Thread/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"ids":       []interface{}{"thread-123"},
		},
		ID: "call-1",
	}

	response := server.handleThreadGet("user@example.com", call)

	if response.Name != "Thread/get" {
		t.Errorf("Response.Name = %s, want Thread/get", response.Name)
	}

	args := response.Args
	list, ok := args["list"].([]Thread)
	if !ok {
		t.Fatalf("list is not []Thread")
	}

	if len(list) == 0 {
		t.Error("Expected at least one thread")
	}
}

// Test handleMailboxGet with properties filter
func TestHandleMailboxGet_WithProperties(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some mailboxes
	db.CreateMailbox("user@example.com", "INBOX")
	db.CreateMailbox("user@example.com", "Sent")
	db.CreateMailbox("user@example.com", "Drafts")

	call := MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"properties": []interface{}{"id", "name"},
		},
		ID: "call-1",
	}

	response := server.handleMailboxGet("user@example.com", call)

	if response.Name != "Mailbox/get" {
		t.Errorf("Response.Name = %s, want Mailbox/get", response.Name)
	}

	args := response.Args
	list, ok := args["list"].([]Mailbox)
	if !ok {
		t.Fatalf("list is not []Mailbox")
	}

	if len(list) == 0 {
		t.Error("Expected at least one mailbox")
	}
}

// Test parseEmailMetadata with References header
