package jmap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/storage"
)

func TestNewServer(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	if server.logger == nil {
		t.Error("Server logger should not be nil")
	}

	if server.sessions == nil {
		t.Error("Server sessions should not be nil")
	}
}

func TestNewServerWithConfig(t *testing.T) {
	config := Config{
		JWTSecret:   "test-secret",
		TokenExpiry: time.Hour,
	}
	server := NewServer(nil, nil, nil, config)

	if server.config.JWTSecret != "test-secret" {
		t.Error("JWTSecret not set correctly")
	}
}

func TestAuthenticate(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	// Create a valid token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/session", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	user, ok := server.authenticate(req)
	if !ok {
		t.Error("Authentication should succeed with valid token")
	}
	if user != "user@example.com" {
		t.Errorf("User = %s, want user@example.com", user)
	}
}

func TestAuthenticate_NoAuth(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("GET", "/jmap/session", nil)

	_, ok := server.authenticate(req)
	if ok {
		t.Error("Authentication should fail without auth header")
	}
}

func TestAuthenticate_InvalidToken(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{JWTSecret: "test-secret"})

	req := httptest.NewRequest("GET", "/jmap/session", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	_, ok := server.authenticate(req)
	if ok {
		t.Error("Authentication should fail with invalid token")
	}
}

func TestHandleWellKnown(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("GET", "/.well-known/jmap", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := response["capabilities"]; !ok {
		t.Error("Response should have capabilities")
	}
}

func TestHandleWellKnown_MethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("POST", "/.well-known/jmap", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleSession(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	// Create a valid token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/session", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var response SessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Username != "user@example.com" {
		t.Errorf("Username = %s, want user@example.com", response.Username)
	}

	if response.APIURL != "/jmap/api" {
		t.Errorf("APIURL = %s, want /jmap/api", response.APIURL)
	}
}

func TestHandleSession_Unauthorized(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("GET", "/jmap/session", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSession_MethodNotAllowed(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("POST", "/jmap/session", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleAPI(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	request := Request{
		Using: []string{"urn:ietf:params:jmap:core"},
		MethodCalls: []MethodCall{
			{
				Name: "Core/echo",
				Args: map[string]interface{}{"hello": "world"},
				ID:   "test-1",
			},
		},
	}

	body, _ := json.Marshal(request)
	req := httptest.NewRequest("POST", "/jmap/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var response ResponseObject
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.MethodResponses) != 1 {
		t.Errorf("MethodResponses count = %d, want 1", len(response.MethodResponses))
	}
}

func TestHandleAPI_Unauthorized(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("POST", "/jmap/api", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleAPI_InvalidJSON(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("POST", "/jmap/api", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpload(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	data := []byte("test upload data")
	req := httptest.NewRequest("POST", "/jmap/upload", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}

	var response UploadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Size != len(data) {
		t.Errorf("Size = %d, want %d", response.Size, len(data))
	}
}

func TestHandleUpload_Unauthorized(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("POST", "/jmap/upload", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleDownload_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	msgStore, err := storage.NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create message store: %v", err)
	}

	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, msgStore, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/download/user@example.com/blob123/test.eml", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDownload_Forbidden(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/download/other@example.com/blob123/test.eml", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleEvents(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/jmap/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// We expect the request to be interrupted by context timeout
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", contentType)
	}
}

func TestCORSHeaders(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	req := httptest.NewRequest("OPTIONS", "/jmap/api", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, w.Code)
	}

	// With secure CORS, no header is set when no origins configured
	allowOrigin := w.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "" {
		t.Errorf("Access-Control-Allow-Origin = %s, want empty (secure default)", allowOrigin)
	}
}

func TestProcessMethodCall_UnknownMethod(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	call := MethodCall{
		Name: "Unknown/method",
		Args: map[string]interface{}{},
		ID:   "test-1",
	}

	response := server.processMethodCall("user@example.com", call)

	if response.Name != "error" {
		t.Errorf("Response.Name = %s, want error", response.Name)
	}

	if response.Args["type"] != "unknownMethod" {
		t.Errorf("Error type = %v, want unknownMethod", response.Args["type"])
	}
}

func TestGetOrCreateSession(t *testing.T) {
	server := NewServer(nil, nil, nil, Config{})

	// Create new session
	session1 := server.getOrCreateSession("user@example.com")
	if session1 == nil {
		t.Fatal("getOrCreateSession returned nil")
	}
	if session1.User != "user@example.com" {
		t.Errorf("User = %s, want user@example.com", session1.User)
	}

	// Get existing session
	session2 := server.getOrCreateSession("user@example.com")
	if session2.ID != session1.ID {
		t.Error("Should return same session")
	}

	// Create different session for different user
	session3 := server.getOrCreateSession("other@example.com")
	if session3.ID == session1.ID {
		t.Error("Different users should have different sessions")
	}
}

func TestGenerateBlobID(t *testing.T) {
	id1 := generateBlobID([]byte("test1"))
	id2 := generateBlobID([]byte("test2"))

	if id1 == "" {
		t.Error("generateBlobID should not return empty string")
	}

	if id1 == id2 {
		t.Error("Different data should generate different blob IDs")
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	if id1 == "" {
		t.Error("generateSessionID should not return empty string")
	}

	if id1 == id2 {
		t.Error("Should generate unique session IDs")
	}
}

func TestRequestStruct(t *testing.T) {
	req := Request{
		Using: []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"},
		MethodCalls: []MethodCall{
			{Name: "Mailbox/get", Args: map[string]interface{}{"accountId": "user"}, ID: "call-1"},
		},
		CreatedIDs: map[string]string{"temp-1": "real-1"},
	}

	if len(req.Using) != 2 {
		t.Errorf("Using count = %d, want 2", len(req.Using))
	}

	if len(req.MethodCalls) != 1 {
		t.Errorf("MethodCalls count = %d, want 1", len(req.MethodCalls))
	}
}

func TestResponseObjectStruct(t *testing.T) {
	resp := ResponseObject{
		SessionState: "session-123",
		MethodResponses: []Response{
			{Name: "Mailbox/get", Args: map[string]interface{}{"list": []string{}}, ID: "call-1"},
		},
	}

	if resp.SessionState != "session-123" {
		t.Errorf("SessionState = %s, want session-123", resp.SessionState)
	}

	if len(resp.MethodResponses) != 1 {
		t.Errorf("MethodResponses count = %d, want 1", len(resp.MethodResponses))
	}
}

func TestSessionResponseStruct(t *testing.T) {
	session := SessionResponse{
		Capabilities: map[string]interface{}{
			"urn:ietf:params:jmap:core": CoreCapabilities{
				MaxSizeUpload: 50000000,
			},
		},
		Accounts: map[string]Account{
			"user@example.com": {Name: "user@example.com", IsPrimary: true},
		},
		PrimaryAccounts: map[string]string{
			"urn:ietf:params:jmap:mail": "user@example.com",
		},
		Username:       "user@example.com",
		APIURL:         "/jmap/api",
		DownloadURL:    "/jmap/download/{accountId}/{blobId}/{name}",
		UploadURL:      "/jmap/upload/{accountId}",
		EventSourceURL: "/jmap/events",
		State:          "state-123",
	}

	if session.Username != "user@example.com" {
		t.Errorf("Username = %s, want user@example.com", session.Username)
	}

	if session.APIURL != "/jmap/api" {
		t.Errorf("APIURL = %s, want /jmap/api", session.APIURL)
	}
}

func TestCoreCapabilitiesStruct(t *testing.T) {
	caps := CoreCapabilities{
		MaxSizeUpload:         50 * 1024 * 1024,
		MaxConcurrentUpload:   4,
		MaxSizeRequest:        10 * 1024 * 1024,
		MaxConcurrentRequests: 4,
		MaxCallsInRequest:     16,
		MaxObjectsInGet:       256,
		MaxObjectsInSet:       128,
		CollationAlgorithms:   []string{"i;unicode-casemap"},
	}

	if caps.MaxSizeUpload != 52428800 {
		t.Errorf("MaxSizeUpload = %d, want 52428800", caps.MaxSizeUpload)
	}

	if caps.MaxCallsInRequest != 16 {
		t.Errorf("MaxCallsInRequest = %d, want 16", caps.MaxCallsInRequest)
	}
}

func TestMailCapabilitiesStruct(t *testing.T) {
	caps := MailCapabilities{
		MaxMailboxesPerEmail:       100,
		MaxMailboxDepth:            10,
		MaxSizeMailboxName:         256,
		MaxSizeAttachmentsPerEmail: 100 * 1024 * 1024,
		EmailQuerySortOptions:      []string{"receivedAt", "sentAt"},
	}

	if caps.MaxMailboxDepth != 10 {
		t.Errorf("MaxMailboxDepth = %d, want 10", caps.MaxMailboxDepth)
	}

	if caps.MaxSizeAttachmentsPerEmail != 104857600 {
		t.Errorf("MaxSizeAttachmentsPerEmail = %d, want 104857600", caps.MaxSizeAttachmentsPerEmail)
	}
}

func TestMailboxStruct(t *testing.T) {
	mailbox := Mailbox{
		ID:            "mailbox-123",
		Name:          "Inbox",
		Role:          "inbox",
		SortOrder:     1,
		TotalEmails:   100,
		UnreadEmails:  5,
		TotalThreads:  80,
		UnreadThreads: 3,
		MyRights:      MailboxRights{MayReadItems: true, MayAddItems: true},
		IsSubscribed:  true,
	}

	if mailbox.Name != "Inbox" {
		t.Errorf("Name = %s, want Inbox", mailbox.Name)
	}

	if mailbox.TotalEmails != 100 {
		t.Errorf("TotalEmails = %d, want 100", mailbox.TotalEmails)
	}

	if !mailbox.MyRights.MayReadItems {
		t.Error("MayReadItems should be true")
	}
}

func TestEmailStruct(t *testing.T) {
	email := Email{
		ID:         "email-123",
		BlobID:     "blob-123",
		ThreadID:   "thread-123",
		MailboxIDs: map[string]bool{"mailbox-1": true},
		Keywords:   map[string]bool{"$seen": true},
		Size:       1024,
		Subject:    "Test Subject",
		From:       []EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:         []EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		Preview:    "Email preview text...",
	}

	if email.Subject != "Test Subject" {
		t.Errorf("Subject = %s, want Test Subject", email.Subject)
	}

	if email.Size != 1024 {
		t.Errorf("Size = %d, want 1024", email.Size)
	}

	if len(email.From) != 1 {
		t.Errorf("From count = %d, want 1", len(email.From))
	}
}

func TestIdentityStruct(t *testing.T) {
	identity := Identity{
		ID:            "identity-123",
		Name:          "John Doe",
		Email:         "john@example.com",
		ReplyTo:       []EmailAddress{{Email: "reply@example.com"}},
		TextSignature: "--\nJohn Doe",
		HTMLSignature: "<p>--<br>John Doe</p>",
		MayDelete:     true,
	}

	if identity.Name != "John Doe" {
		t.Errorf("Name = %s, want John Doe", identity.Name)
	}

	if identity.Email != "john@example.com" {
		t.Errorf("Email = %s, want john@example.com", identity.Email)
	}
}

func TestFilterConditionStruct(t *testing.T) {
	filter := FilterCondition{
		InMailbox:     "mailbox-123",
		Before:        "2024-01-01T00:00:00Z",
		After:         "2023-01-01T00:00:00Z",
		MinSize:       1000,
		MaxSize:       1000000,
		HasKeyword:    "$seen",
		HasAttachment: true,
		Text:          "search text",
		From:          "sender@example.com",
	}

	if filter.InMailbox != "mailbox-123" {
		t.Errorf("InMailbox = %s, want mailbox-123", filter.InMailbox)
	}

	if filter.MinSize != 1000 {
		t.Errorf("MinSize = %d, want 1000", filter.MinSize)
	}
}

func TestGetRequestStruct(t *testing.T) {
	req := GetRequest{
		AccountID:  "user@example.com",
		IDs:        []string{"id-1", "id-2"},
		Properties: []string{"id", "name"},
	}

	if req.AccountID != "user@example.com" {
		t.Errorf("AccountID = %s, want user@example.com", req.AccountID)
	}

	if len(req.IDs) != 2 {
		t.Errorf("IDs count = %d, want 2", len(req.IDs))
	}
}

func TestQueryRequestStruct(t *testing.T) {
	req := QueryRequest{
		AccountID:      "user@example.com",
		Position:       0,
		Limit:          50,
		CalculateTotal: true,
		Sort: []Comparator{
			{Property: "receivedAt", IsAscending: false},
		},
	}

	if req.Limit != 50 {
		t.Errorf("Limit = %d, want 50", req.Limit)
	}

	if !req.CalculateTotal {
		t.Error("CalculateTotal should be true")
	}
}

func TestSetRequestStruct(t *testing.T) {
	req := SetRequest{
		AccountID: "user@example.com",
		IfInState: "state-123",
		Create:    map[string]interface{}{"temp-1": map[string]interface{}{"name": "Test"}},
		Update:    map[string]interface{}{"id-1": map[string]interface{}{"name": "Updated"}},
		Destroy:   []string{"id-2"},
	}

	if req.IfInState != "state-123" {
		t.Errorf("IfInState = %s, want state-123", req.IfInState)
	}

	if len(req.Destroy) != 1 {
		t.Errorf("Destroy count = %d, want 1", len(req.Destroy))
	}
}

func TestEmailImportStruct(t *testing.T) {
	imp := EmailImport{
		BlobID:     "blob-123",
		MailboxIDs: map[string]bool{"mailbox-1": true},
		Keywords:   map[string]bool{"$seen": true},
		ReceivedAt: "2024-01-01T00:00:00Z",
	}

	if imp.BlobID != "blob-123" {
		t.Errorf("BlobID = %s, want blob-123", imp.BlobID)
	}

	if _, ok := imp.MailboxIDs["mailbox-1"]; !ok {
		t.Error("MailboxIDs should contain mailbox-1")
	}
}

func TestHandleUpload_MethodNotAllowed(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/upload", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleDownload_MethodNotAllowed(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("POST", "/jmap/download/user@example.com/blob123/test.eml", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleDownload_InvalidPath(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	// Path too short - less than 6 parts
	req := httptest.NewRequest("GET", "/jmap/download/user", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleEvents_Unauthorized(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	req := httptest.NewRequest("GET", "/jmap/events", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleEvents_MethodNotAllowed(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("POST", "/jmap/events", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestSendError(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	w := httptest.NewRecorder()

	server.sendError(w, http.StatusBadRequest, "testError", map[string]interface{}{"detail": "test"})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", contentType)
	}
}

func TestSendJSON(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	w := httptest.NewRecorder()

	server.sendJSON(w, http.StatusOK, map[string]string{"test": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", contentType)
	}
}

func TestGetMailboxIDFromName_ArchiveJunk(t *testing.T) {
	// Test Archive and Junk cases specifically
	if getMailboxIDFromName("Archive") != "archive" {
		t.Error("Archive should map to archive")
	}
	if getMailboxIDFromName("Junk") != "junk" {
		t.Error("Junk should map to junk")
	}
}

func TestGetMailboxNameFromID_ArchiveJunk(t *testing.T) {
	// Test archive and junk cases specifically
	if getMailboxNameFromID("archive") != "Archive" {
		t.Error("archive should map to Archive")
	}
	if getMailboxNameFromID("junk") != "Junk" {
		t.Error("junk should map to Junk")
	}
}

func TestHandleAPI_MethodNotAllowed(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/api", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestAuthenticate_WrongSigningMethod(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	// Create token with wrong signing method (HS256 expected, using none)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	// Sign with different secret to trigger error
	tokenString, _ := token.SignedString([]byte("wrong-secret"))

	req := httptest.NewRequest("GET", "/jmap/session", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	_, ok := server.authenticate(req)
	if ok {
		t.Error("Authentication should fail with wrong secret")
	}
}

func TestAuthenticate_ExpiredToken(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	// Create expired token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(-time.Hour).Unix(), // expired
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/jmap/session", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	_, ok := server.authenticate(req)
	if ok {
		t.Error("Authentication should fail with expired token")
	}
}

func TestAuthenticate_MalformedHeader(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	tests := []struct {
		name   string
		header string
	}{
		{"no space", "BearerToken"},
		{"wrong type", "Basic token"},
		{"empty bearer", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/jmap/session", nil)
			req.Header.Set("Authorization", tt.header)

			_, ok := server.authenticate(req)
			if ok {
				t.Errorf("Authentication should fail for %s", tt.name)
			}
		})
	}
}

func TestHandleDownload_Unauthorized(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	req := httptest.NewRequest("GET", "/jmap/download/user@example.com/blob123/test.eml", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestProcessMethodCall_MailboxGet(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "test-1",
	}

	response := server.processMethodCall("user@example.com", call)

	if response.Name != "Mailbox/get" {
		t.Errorf("Response.Name = %s, want Mailbox/get", response.Name)
	}
}

func TestProcessMethodCall_MailboxQuery(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "test-1",
	}

	response := server.processMethodCall("user@example.com", call)

	if response.Name != "Mailbox/query" {
		t.Errorf("Response.Name = %s, want Mailbox/query", response.Name)
	}
}

func TestProcessMethodCall_EmailQuery(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	call := MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
		},
		ID: "test-1",
	}

	response := server.processMethodCall("user@example.com", call)

	if response.Name != "Email/query" {
		t.Errorf("Response.Name = %s, want Email/query", response.Name)
	}
}

func TestHandleAPI_UnknownMethod(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	request := Request{
		Using: []string{"urn:ietf:params:jmap:core"},
		MethodCalls: []MethodCall{
			{
				Name: "Unknown/method",
				Args: map[string]interface{}{},
				ID:   "test-1",
			},
		},
	}

	body, _ := json.Marshal(request)
	req := httptest.NewRequest("POST", "/jmap/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_RouteNotFound(t *testing.T) {
	config := Config{JWTSecret: "test-secret"}
	server := NewServer(nil, nil, nil, config)

	req := httptest.NewRequest("GET", "/nonexistent/path", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
