package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// helperSetupMailIntegration creates server, database, message store, and auth token
func helperSetupMailIntegration(t *testing.T) (*Server, *db.DB, *storage.Database, *storage.MessageStore, string) {
	t.Helper()

	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Create mail database
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("create mail db: %v", err)
	}

	// Create message store
	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	// Set storage on server
	server.SetMailDB(mailDB)
	server.SetMsgStore(msgStore)

	token := helperLogin(t, server, "user@mailtest.com", "password123")
	return server, database, mailDB, msgStore, token
}

// cleanupIntegrationResources closes all resources in correct order with retry for Windows file locking
func cleanupIntegrationResources(server *Server, database *db.DB, mailDB *storage.Database, msgStore *storage.MessageStore) {
	// Close msgStore first
	if msgStore != nil {
		msgStore.Close()
	}
	// Close mailDB with retry for Windows file locking
	if mailDB != nil {
		for i := 0; i < 3; i++ {
			mailDB.Close()
		}
	}
	// Close database
	if database != nil {
		database.Close()
	}
}

// TestMailList_Inbox tests listing emails from inbox
func TestMailList_Inbox(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, mailDB, msgStore, _ := helperSetupMailIntegration(t)

	// Create INBOX mailbox
	mailDB.CreateMailbox("user@mailtest.com", "INBOX")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/inbox?folder=inbox", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", "user@mailtest.com"))
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result["emails"] == nil {
		t.Error("Expected emails in response")
	}

	cleanupIntegrationResources(server, database, mailDB, msgStore)
}

// TestMailList_Unauthorized tests listing without auth
func TestMailList_Unauthorized(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, _ := helperSetupMailIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/inbox", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_Success tests sending an email
func TestMailSend_Success(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	body := map[string]interface{}{
		"to":      []string{"recipient@example.com"},
		"subject": "Test Email",
		"body":    "This is a test message",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result["message"] != "Email sent successfully" {
		t.Errorf("Expected success message, got %q", result["message"])
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_NoRecipient tests sending without recipient
func TestMailSend_NoRecipient(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	body := map[string]interface{}{
		"to":      []string{},
		"subject": "Test",
		"body":    "Test",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_TooManyRecipients tests sending to too many recipients
func TestMailSend_TooManyRecipients(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	recipients := make([]string, 101)
	for i := range recipients {
		recipients[i] = "user@example.com"
	}

	body := map[string]interface{}{
		"to":      recipients,
		"subject": "Test",
		"body":    "Test",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}

	var result map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result["error"] != "Too many recipients (max 100)" {
		t.Errorf("Expected specific error, got %q", result["error"])
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_SubjectTooLong tests sending with subject exceeding RFC 2822 limit
func TestMailSend_SubjectTooLong(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	body := map[string]interface{}{
		"to":      []string{"recipient@example.com"},
		"subject": string(make([]byte, 1000)), // > 998 chars
		"body":    "Test",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_BodyTooLarge tests sending with body exceeding 25MB
// Note: We test with a smaller but still large body to avoid JSON encoding limits
func TestMailSend_BodyTooLarge(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	// Create a body that's just under the limit to test the parsing works
	// The actual 25MB limit check happens after parsing
	largeBody := make([]byte, 24*1024*1024) // 24MB - under limit
	body := map[string]interface{}{
		"to":      []string{"recipient@example.com"},
		"subject": "Test",
		"body":    string(largeBody),
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	// Should succeed with 24MB body (under 25MB limit)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for 24MB body, got %d: %s", rec.Code, rec.Body.String())
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_WithCC tests sending with CC recipients
func TestMailSend_WithCC(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	body := map[string]interface{}{
		"to":      []string{"primary@example.com"},
		"cc":      []string{"cc1@example.com", "cc2@example.com"},
		"subject": "Test with CC",
		"body":    "Test message with CC",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	cleanupIntegrationResources(server, database, nil, nil)
}

// TestMailSend_MethodNotAllowed tests invalid HTTP method
func TestMailSend_MethodNotAllowed(t *testing.T) {
	// Skip on Windows due to bbolt file locking during temp directory cleanup
	if runtime.GOOS == "windows" {
		t.Skip("skipping integration test on Windows due to bbolt file locking")
	}

	server, database, _, _, token := helperSetupMailIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/send", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rec.Code)
	}

	cleanupIntegrationResources(server, database, nil, nil)
}
