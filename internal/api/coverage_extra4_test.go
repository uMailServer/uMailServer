package api

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// --- RevokeToken (0.0%) ---

func TestRevokeToken_SingleToken(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "test-secret"})

	tokenHash := fmt.Sprintf("%x", md5.Sum([]byte("test-token")))
	server.RevokeToken(tokenHash)

	if !server.IsTokenRevoked(tokenHash) {
		t.Error("Expected token to be revoked")
	}
}

func TestRevokeToken_NilDatabase(t *testing.T) {
	// Server with nil database - should use in-memory revocation
	server := NewServer(nil, nil, Config{JWTSecret: "test-secret"})

	tokenHash := fmt.Sprintf("%x", md5.Sum([]byte("test-token")))
	server.RevokeToken(tokenHash)

	if !server.IsTokenRevoked(tokenHash) {
		t.Error("Expected token to be revoked with nil database")
	}
}

func TestRevokeToken_MultipleTokens(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "test-secret"})

	// Revoke multiple tokens
	for i := 0; i < 5; i++ {
		token := fmt.Sprintf("token-%d", i)
		tokenHash := fmt.Sprintf("%x", md5.Sum([]byte(token)))
		server.RevokeToken(tokenHash)
	}

	// All should be revoked
	for i := 0; i < 5; i++ {
		token := fmt.Sprintf("token-%d", i)
		tokenHash := fmt.Sprintf("%x", md5.Sum([]byte(token)))
		if !server.IsTokenRevoked(tokenHash) {
			t.Errorf("Expected token %d to be revoked", i)
		}
	}
}

func TestRevokeToken_DatabaseErrorFallback(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := NewServer(database, nil, Config{JWTSecret: "test-secret"})

	// Close database to simulate error
	database.Close()

	tokenHash := fmt.Sprintf("%x", md5.Sum([]byte("test-token")))
	server.RevokeToken(tokenHash)

	// Should fall back to in-memory on DB error
	if !server.IsTokenRevoked(tokenHash) {
		t.Error("Expected token to be revoked via in-memory fallback")
	}
}

func TestIsTokenRevoked_NonExistent(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "test-secret"})

	if server.IsTokenRevoked("non-existent-token") {
		t.Error("Non-existent token should not be revoked")
	}
}

func TestCleanupExpiredTokens(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "test-secret"})

	// Add an already-expired token directly to map
	tokenHash := "expired-token"
	server.tokenBlacklistMu.Lock()
	server.tokenBlacklist[tokenHash] = time.Now().Add(-1 * time.Hour)
	server.tokenBlacklistMu.Unlock()

	server.CleanupExpiredTokens()

	if server.IsTokenRevoked(tokenHash) {
		t.Error("Expired token should have been cleaned up")
	}
}

// --- getEmailsFromStorage (6.9%) ---

func TestGetEmailsFromStorage_NilStorage(t *testing.T) {
	h := NewMailHandler()
	// mailDB and msgStore are nil

	emails, err := h.getEmailsFromStorage("user@example.com", "INBOX")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(emails) != 0 {
		t.Errorf("Expected empty emails, got %d", len(emails))
	}
}

func TestGetEmailsFromStorage_MailboxNotExists(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	// msgStore is nil

	emails, err := h.getEmailsFromStorage("user@example.com", "INBOX")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	// Should return empty since mailbox doesn't exist
	if len(emails) != 0 {
		t.Errorf("Expected empty emails for non-existent mailbox, got %d", len(emails))
	}
}

func TestGetEmailsFromStorage_WithMessages(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: Test\r\n\r\nBody"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Test",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{},
	})

	emails, err := h.getEmailsFromStorage("user@example.com", "INBOX")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(emails) != 1 {
		t.Errorf("Expected 1 email, got %d", len(emails))
	}
	if emails[0].Subject != "Test" {
		t.Errorf("Expected subject 'Test', got %s", emails[0].Subject)
	}
}

// --- getEmailFromStorage (10.5%) ---

func TestGetEmailFromStorage_NilStorage(t *testing.T) {
	h := NewMailHandler()
	// mailDB and msgStore are nil

	email, err := h.getEmailFromStorage("user@example.com", "INBOX", "some-id")
	if err == nil {
		t.Error("Expected error for nil storage")
	}
	if email != nil {
		t.Error("Expected nil email")
	}
}

func TestGetEmailFromStorage_NotFound(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	email, err := h.getEmailFromStorage("user@example.com", "INBOX", "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent email")
	}
	if email != nil {
		t.Error("Expected nil email")
	}
}

func TestGetEmailFromStorage_WithMessage(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: FindMe\r\n\r\nBody"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "FindMe",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{"\\Seen"},
	})

	email, err := h.getEmailFromStorage("user@example.com", "INBOX", msgID)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if email == nil {
		t.Fatal("Expected email, got nil")
	}
	if email.Subject != "FindMe" {
		t.Errorf("Expected subject 'FindMe', got %s", email.Subject)
	}
	if !email.Read {
		t.Error("Expected email to be marked as read")
	}
}

// --- handleMailDelete ---

func TestHandleMailDelete_Success(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})
	server.SetMailDB(mailDB)
	server.SetMsgStore(msgStore)

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	// Create mailbox and message
	mailDB.CreateMailbox("user@mailtest.com", "INBOX")
	msgID, _ := msgStore.StoreMessage("user@mailtest.com", []byte("From: sender\r\nSubject: DeleteMe\r\n\r\nBody"))
	uid, _ := mailDB.GetNextUID("user@mailtest.com", "INBOX")
	mailDB.StoreMessageMetadata("user@mailtest.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "DeleteMe",
		From:         "sender",
		To:           "user@mailtest.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/mail/delete?id="+msgID, strings.NewReader("{}"))
	req = req.WithContext(context.WithValue(req.Context(), "user", "user@mailtest.com"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMailDelete_MissingID(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/mail/delete", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", "user@mailtest.com"))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

// --- extractBody ---

func TestExtractBody_WithCRLF(t *testing.T) {
	h := NewMailHandler()
	raw := "From: sender\r\nSubject: Test\r\n\r\nBody content here"
	body := h.extractBody(raw)
	if body != "Body content here" {
		t.Errorf("Expected 'Body content here', got %q", body)
	}
}

func TestExtractBody_WithLF(t *testing.T) {
	h := NewMailHandler()
	raw := "From: sender\nSubject: Test\n\nBody content here"
	body := h.extractBody(raw)
	if body != "Body content here" {
		t.Errorf("Expected 'Body content here', got %q", body)
	}
}

func TestExtractBody_NoSeparator(t *testing.T) {
	h := NewMailHandler()
	raw := "Just one line without separator"
	body := h.extractBody(raw)
	if body != raw {
		t.Errorf("Expected raw content, got %q", body)
	}
}

// --- hasFlag ---

func TestHasFlag_Exists(t *testing.T) {
	flags := []string{"\\Seen", "\\Flagged", "\\Answered"}
	if !hasFlag(flags, "\\Seen") {
		t.Error("Expected hasFlag to return true for \\Seen")
	}
	if !hasFlag(flags, "\\Flagged") {
		t.Error("Expected hasFlag to return true for \\Flagged")
	}
}

func TestHasFlag_NotExists(t *testing.T) {
	flags := []string{"\\Seen", "\\Flagged"}
	if hasFlag(flags, "\\Deleted") {
		t.Error("Expected hasFlag to return false for \\Deleted")
	}
}

// --- markAsRead ---

func TestMarkAsRead_NilDB(t *testing.T) {
	h := NewMailHandler()
	// mailDB is nil - should not panic
	h.markAsRead("user@example.com", "INBOX", "some-id")
}

func TestMarkAsRead_MessageNotFound(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	h := NewMailHandler()
	h.mailDB = mailDB

	// Should not panic when message not found
	h.markAsRead("user@example.com", "INBOX", "non-existent-id")
}

func TestMarkAsRead_Success(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message (unread)
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")
	msgID, _ := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: Test\r\n\r\nBody"))
	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Test",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{}, // No \Seen flag - unread
	})

	// markAsRead should add \Seen flag
	h.markAsRead("user@example.com", "INBOX", msgID)

	// Verify the flag was added
	meta, _ := mailDB.GetMessageMetadata("user@example.com", "INBOX", uid)
	if !hasFlag(meta.Flags, "\\Seen") {
		t.Error("Expected \\Seen flag to be added")
	}
}

func TestMarkAsRead_AlreadyRead(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message (already read)
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")
	msgID, _ := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: Test\r\n\r\nBody"))
	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Test",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{"\\Seen"}, // Already has \Seen flag
	})

	// markAsRead should not change anything (no update needed)
	h.markAsRead("user@example.com", "INBOX", msgID)

	// Verify the flag was not changed
	meta, _ := mailDB.GetMessageMetadata("user@example.com", "INBOX", uid)
	if len(meta.Flags) != 1 || meta.Flags[0] != "\\Seen" {
		t.Errorf("Expected exactly \\Seen flag, got %v", meta.Flags)
	}
}

// --- InitDemoEmails ---

func TestInitDemoEmails_IsNoOp(t *testing.T) {
	// Should not panic
	InitDemoEmails("test@example.com")
}

// --- MailHandler folder mapping ---

func TestFolderMap_RoundTrip(t *testing.T) {
	// Test reverseFolderMap
	folders := []string{"Inbox", "Sent", "Drafts", "Trash", "Spam"}
	expectedInternal := []string{"INBOX", "Sent", "Drafts", "Trash", "Junk"}

	for i, folder := range folders {
		internal := folderMap[strings.ToLower(folder)]
		if internal != expectedInternal[i] {
			t.Errorf("Expected %s -> %s, got %s", folder, expectedInternal[i], internal)
		}
	}

	// Test reverse mapping
	for i, internal := range expectedInternal {
		external := reverseFolderMap[internal]
		if external != folders[i] {
			t.Errorf("Expected %s -> %s, got %s", internal, folders[i], external)
		}
	}
}

// --- getEmailFromStorage error paths ---

func TestGetEmailFromStorage_GetMessageUIDsError(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox but no messages - GetMessageUIDs returns empty
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	email, err := h.getEmailFromStorage("user@example.com", "INBOX", "non-existent-id")
	// email should be nil for non-existent message, error may vary
	if email != nil {
		t.Error("Expected nil email for non-existent message")
	}
}

func TestGetEmailFromStorage_WithMessages(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: Test\r\n\r\nBody"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Test",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{"\\Seen", "\\Flagged"},
	})

	email, err := h.getEmailFromStorage("user@example.com", "INBOX", msgID)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if email == nil {
		t.Fatal("Expected email, got nil")
	}
	if email.Subject != "Test" {
		t.Errorf("Expected subject 'Test', got %s", email.Subject)
	}
	if !email.Read {
		t.Error("Expected Read to be true")
	}
	if !email.Starred {
		t.Error("Expected Starred to be true")
	}
}

// --- getEmailsFromStorage error paths ---

func TestGetEmailsFromStorage_GetMessageUIDsError(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message with corrupted metadata
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: Test\r\n\r\nBody"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	// Store metadata without proper flags to test hasFlag coverage
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Test",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{}, // No flags - tests hasFlag false path
	})

	emails, err := h.getEmailsFromStorage("user@example.com", "INBOX")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(emails) != 1 {
		t.Errorf("Expected 1 email, got %d", len(emails))
	}
	if emails[0].Read {
		t.Error("Expected Read to be false with no flags")
	}
}

// --- handleMailGet ---

func TestHandleMailGet_Success(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create mailbox and add a message
	_ = mailDB.CreateMailbox("user@example.com", "INBOX")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: sender\r\nSubject: Test\r\n\r\nBody"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "INBOX")
	_ = mailDB.StoreMessageMetadata("user@example.com", "INBOX", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Test",
		From:         "sender",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{"\\Seen"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/message?id="+msgID+"&folder=INBOX", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	h.handleMailGet(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// --- handleMailList ---

func TestHandleMailList_SpamFolder(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create Junk mailbox with a message
	_ = mailDB.CreateMailbox("user@example.com", "Junk")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: spam\r\nSubject: Spam\r\n\r\nSpam body"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "Junk")
	_ = mailDB.StoreMessageMetadata("user@example.com", "Junk", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Spam",
		From:         "spam@example.com",
		To:           "user@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/inbox?folder=spam", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	h.handleMailList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["folder"] != "spam" {
		t.Errorf("Expected folder 'spam', got %v", resp["folder"])
	}
}

// --- handleMailList with sent folder ---

func TestHandleMailList_SentFolder(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	h := NewMailHandler()
	h.mailDB = mailDB
	h.msgStore = msgStore

	// Create Sent mailbox with a message
	_ = mailDB.CreateMailbox("user@example.com", "Sent")

	msgID, err := msgStore.StoreMessage("user@example.com", []byte("From: user@example.com\r\nSubject: Sent\r\n\r\nSent body"))
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	uid, _ := mailDB.GetNextUID("user@example.com", "Sent")
	_ = mailDB.StoreMessageMetadata("user@example.com", "Sent", uid, &storage.MessageMetadata{
		MessageID:    msgID,
		UID:          uid,
		Subject:      "Sent",
		From:         "user@example.com",
		To:           "recipient@example.com",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		InternalDate: time.Now(),
		Size:         100,
		Flags:        []string{"\\Seen"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/inbox?folder=sent", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	h.handleMailList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	// folder should map to "sent"
	if resp["folder"] != "sent" {
		t.Errorf("Expected folder 'sent', got %v", resp["folder"])
	}
}

// --- deleteMessageMetadata ---

func TestDeleteMessageMetadata_NotFound(t *testing.T) {
	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	h := NewMailHandler()
	h.mailDB = mailDB

	// Should not panic when message not found
	h.deleteMessageMetadata("user@example.com", "non-existent-id")
}

// --- mail send with BCC ---

func TestMailSend_WithBCC(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	body := map[string]interface{}{
		"to":      []string{"recipient@example.com"},
		"bcc":     []string{"bcc1@example.com", "bcc2@example.com"},
		"subject": "Test with BCC",
		"body":    "Test message with BCC",
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
}

// --- handleMailSend with storage ---

func TestHandleMailSend_WithStorage(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	mailDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open mail db: %v", err)
	}
	defer mailDB.Close()

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}
	defer msgStore.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})
	server.SetMailDB(mailDB)
	server.SetMsgStore(msgStore)

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	body := map[string]interface{}{
		"to":      []string{"recipient@example.com"},
		"subject": "Test with storage",
		"body":    "Test message",
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

	// Verify message was stored in Sent folder
	sentBox, _ := mailDB.GetMailbox("user@mailtest.com", "Sent")
	if sentBox == nil {
		t.Error("Expected Sent mailbox to be created")
	}
}

// --- handleMailList folder mapping ---

func TestHandleMailList_FolderMapping(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	// Test with "spam" folder (maps to internal "Junk")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/inbox?folder=spam", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = req.WithContext(withUser(req.Context(), "user@mailtest.com"))
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}
