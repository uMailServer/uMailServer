package jmap

import (
	"testing"
)

// Test handleMailboxChanges
func TestHandleMailboxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/changes",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"sinceState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleMailboxChanges("user@example.com", call)

	if response.Name != "Mailbox/changes" {
		t.Errorf("Response.Name = %s, want Mailbox/changes", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}

	if args["oldState"] != "state-1234567890" {
		t.Errorf("oldState = %v, want state-1234567890", args["oldState"])
	}

	if _, ok := args["newState"].(string); !ok {
		t.Error("newState should be a string")
	}
}

func TestHandleMailboxChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/changes",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"sinceState": "state-1234567890",
			"maxChanges": float64(100),
		},
		ID: "call-1",
	}

	response := server.handleMailboxChanges("user@example.com", call)

	if response.Name != "Mailbox/changes" {
		t.Errorf("Response.Name = %s, want Mailbox/changes", response.Name)
	}
}

// Test handleMailboxQueryChanges
func TestHandleMailboxQueryChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleMailboxQueryChanges("user@example.com", call)

	if response.Name != "Mailbox/queryChanges" {
		t.Errorf("Response.Name = %s, want Mailbox/queryChanges", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleMailboxQueryChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
			"maxChanges":      float64(50),
		},
		ID: "call-1",
	}

	response := server.handleMailboxQueryChanges("user@example.com", call)

	if response.Name != "Mailbox/queryChanges" {
		t.Errorf("Response.Name = %s, want Mailbox/queryChanges", response.Name)
	}
}

// Test handleEmailQueryChanges
func TestHandleEmailQueryChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleEmailQueryChanges("user@example.com", call)

	if response.Name != "Email/queryChanges" {
		t.Errorf("Response.Name = %s, want Email/queryChanges", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleEmailQueryChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
			"maxChanges":      float64(100),
		},
		ID: "call-1",
	}

	response := server.handleEmailQueryChanges("user@example.com", call)

	if response.Name != "Email/queryChanges" {
		t.Errorf("Response.Name = %s, want Email/queryChanges", response.Name)
	}
}

// Test handleThreadQuery
func TestHandleThreadQuery(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleThreadQuery("user@example.com", call)

	if response.Name != "Thread/query" {
		t.Errorf("Response.Name = %s, want Thread/query", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}

	if _, ok := args["ids"]; !ok {
		t.Error("Response should contain ids")
	}
}

func TestHandleThreadQuery_WithFilter(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"filter": map[string]interface{}{
				"text": "search term",
			},
		},
		ID: "call-1",
	}

	response := server.handleThreadQuery("user@example.com", call)

	if response.Name != "Thread/query" {
		t.Errorf("Response.Name = %s, want Thread/query", response.Name)
	}
}

func TestHandleThreadQuery_WithSort(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"sort": []interface{}{
				map[string]interface{}{
					"property":    "receivedAt",
					"isAscending": false,
				},
			},
			"position": float64(0),
			"limit":    float64(30),
		},
		ID: "call-1",
	}

	response := server.handleThreadQuery("user@example.com", call)

	if response.Name != "Thread/query" {
		t.Errorf("Response.Name = %s, want Thread/query", response.Name)
	}
}

// Test handleThreadChanges
func TestHandleThreadChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/changes",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"sinceState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleThreadChanges("user@example.com", call)

	if response.Name != "Thread/changes" {
		t.Errorf("Response.Name = %s, want Thread/changes", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleThreadChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/changes",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"sinceState": "state-1234567890",
			"maxChanges": float64(100),
		},
		ID: "call-1",
	}

	response := server.handleThreadChanges("user@example.com", call)

	if response.Name != "Thread/changes" {
		t.Errorf("Response.Name = %s, want Thread/changes", response.Name)
	}
}

// Test handleThreadQueryChanges
func TestHandleThreadQueryChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleThreadQueryChanges("user@example.com", call)

	if response.Name != "Thread/queryChanges" {
		t.Errorf("Response.Name = %s, want Thread/queryChanges", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleThreadQueryChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Thread/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
			"maxChanges":      float64(100),
		},
		ID: "call-1",
	}

	response := server.handleThreadQueryChanges("user@example.com", call)

	if response.Name != "Thread/queryChanges" {
		t.Errorf("Response.Name = %s, want Thread/queryChanges", response.Name)
	}
}

// Test handleIdentityChanges
func TestHandleIdentityChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/changes",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"sinceState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleIdentityChanges("user@example.com", call)

	if response.Name != "Identity/changes" {
		t.Errorf("Response.Name = %s, want Identity/changes", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleIdentityChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/changes",
		Args: map[string]interface{}{
			"accountId":  "user@example.com",
			"sinceState": "state-1234567890",
			"maxChanges": float64(50),
		},
		ID: "call-1",
	}

	response := server.handleIdentityChanges("user@example.com", call)

	if response.Name != "Identity/changes" {
		t.Errorf("Response.Name = %s, want Identity/changes", response.Name)
	}
}

// Test handleIdentityQuery
func TestHandleIdentityQuery(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "call-1",
	}

	response := server.handleIdentityQuery("user@example.com", call)

	if response.Name != "Identity/query" {
		t.Errorf("Response.Name = %s, want Identity/query", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}

	if _, ok := args["ids"]; !ok {
		t.Error("Response should contain ids")
	}
}

func TestHandleIdentityQuery_WithFilter(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"filter": map[string]interface{}{
				"email": "test@example.com",
			},
		},
		ID: "call-1",
	}

	response := server.handleIdentityQuery("user@example.com", call)

	if response.Name != "Identity/query" {
		t.Errorf("Response.Name = %s, want Identity/query", response.Name)
	}
}

func TestHandleIdentityQuery_WithSort(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"sort": []interface{}{
				map[string]interface{}{
					"property":    "name",
					"isAscending": true,
				},
			},
		},
		ID: "call-1",
	}

	response := server.handleIdentityQuery("user@example.com", call)

	if response.Name != "Identity/query" {
		t.Errorf("Response.Name = %s, want Identity/query", response.Name)
	}
}

// Test handleIdentityQueryChanges
func TestHandleIdentityQueryChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
		},
		ID: "call-1",
	}

	response := server.handleIdentityQueryChanges("user@example.com", call)

	if response.Name != "Identity/queryChanges" {
		t.Errorf("Response.Name = %s, want Identity/queryChanges", response.Name)
	}

	args := response.Args
	if args["accountId"] != "user@example.com" {
		t.Errorf("accountId = %v, want user@example.com", args["accountId"])
	}
}

func TestHandleIdentityQueryChanges_WithMaxChanges(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Identity/queryChanges",
		Args: map[string]interface{}{
			"accountId":       "user@example.com",
			"sinceQueryState": "state-1234567890",
			"maxChanges":      float64(100),
		},
		ID: "call-1",
	}

	response := server.handleIdentityQueryChanges("user@example.com", call)

	if response.Name != "Identity/queryChanges" {
		t.Errorf("Response.Name = %s, want Identity/queryChanges", response.Name)
	}
}

// Test generateSearchSnippet
func TestGenerateSearchSnippet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Test basic search snippet generation with proper email format
	emailData := "Subject: Test Email\nFrom: sender@example.com\n\nThis is a test message with some content"
	result := server.generateSearchSnippet(emailData, "test")

	if result.Preview == "" {
		t.Error("Expected non-empty snippet preview")
	}
	if result.Subject == "" {
		t.Error("Expected non-empty subject")
	}

	// Test with empty text
	result = server.generateSearchSnippet("", "test")
	if result.Preview != "" {
		t.Errorf("Expected empty snippet preview for empty text, got %q", result.Preview)
	}

	// Test with empty query
	result = server.generateSearchSnippet("some content", "")
	if result.Preview != "" {
		t.Errorf("Expected empty snippet preview for empty query, got %q", result.Preview)
	}
}
