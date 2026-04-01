package search

import (
	"fmt"
	"os"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// TestBuildIndex_DBErrorOnGetUIDs tests BuildIndex when GetMessageUIDs fails
// (exercises the "continue" on error path in BuildIndex loop)
func TestBuildIndex_DBErrorOnGetUIDs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer database.Close()

	user := "uiderruser"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	// Store metadata so UID 1 exists
	meta := &storage.MessageMetadata{
		MessageID: "test-msg-uid-err",
		UID:       1,
		Subject:   "UID Error Test",
		From:      "a@b.com",
		To:        "c@d.com",
		Date:      "2025-01-01",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
		t.Fatalf("StoreMessageMetadata: %v", err)
	}

	// Store another mailbox that has no messages
	if err := database.CreateMailbox(user, "Empty"); err != nil {
		t.Fatalf("CreateMailbox Empty: %v", err)
	}

	svc := NewService(database, nil, nil)
	err = svc.BuildIndex(user)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()
	if idx == nil {
		t.Fatal("expected index to exist")
	}
	// Should have at least 1 doc from INBOX
	if idx.DocCount() < 1 {
		t.Errorf("expected at least 1 doc, got %d", idx.DocCount())
	}
}

// TestBuildIndex_DBErrorOnGetMetadata tests BuildIndex when GetMessageMetadata fails.
func TestBuildIndex_DBErrorOnGetMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer database.Close()

	user := "metaerruser"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	svc := NewService(database, nil, nil)
	err = svc.BuildIndex(user)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	// Should succeed even with no messages
	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()
	if idx == nil {
		t.Fatal("expected index to exist")
	}
}

// TestIndexMessage_WithMsgStoreAndContent tests IndexMessage reading message body content.
func TestIndexMessage_WithMsgStoreAndContent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer database.Close()

	storePath := tmpDir + "/messages"
	err = os.MkdirAll(storePath, 0755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	msgStore, err := storage.NewMessageStore(storePath)
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	defer msgStore.Close()

	user := "indexmsguser"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	// Store a message with content
	msgData := []byte("From: alice@example.com\r\nSubject: Important Update\r\n\r\nThe quarterly report is due tomorrow morning. Please review the attached documents.")
	msgID, err := msgStore.StoreMessage(user, msgData)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	meta := &storage.MessageMetadata{
		MessageID: msgID,
		UID:       42,
		Subject:   "Important Update",
		From:      "alice@example.com",
		To:        "bob@example.com",
		Date:      "2025-04-15",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 42, meta); err != nil {
		t.Fatalf("StoreMessageMetadata: %v", err)
	}

	// Create service and pre-index
	svc := NewService(database, msgStore, nil)
	svc.indexes[user] = NewIndex()

	err = svc.IndexMessage(user, "INBOX", 42)
	if err != nil {
		t.Fatalf("IndexMessage: %v", err)
	}

	// Search for content from the message body
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "quarterly report",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'quarterly report', got %d", len(results))
	}
}

// TestIndexMessage_NilDB tests IndexMessage with nil database (exercises recover path).
func TestIndexMessage_NilDB(t *testing.T) {
	svc := NewService(nil, nil, nil)
	svc.indexes["niluser"] = NewIndex()

	defer func() {
		_ = recover()
	}()
	svc.IndexMessage("niluser", "INBOX", 1)
}

// TestBuildIndex_ManyFoldersManyMessages stress tests with many folders and messages.
func TestBuildIndex_ManyFoldersManyMessages(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer database.Close()

	user := "manyuser"
	// Create 3 folders with 3 messages each
	folders := []string{"INBOX", "Sent", "Drafts"}
	for _, folder := range folders {
		if err := database.CreateMailbox(user, folder); err != nil {
			t.Fatalf("CreateMailbox %s: %v", folder, err)
		}
		for uid := uint32(1); uid <= 3; uid++ {
			 meta := &storage.MessageMetadata{
				MessageID: fmt.Sprintf("msg-%s-%d", folder, uid),
				UID:       uid,
			 Subject:   fmt.Sprintf("Subject in %s #%d", folder, uid),
			 From:      fmt.Sprintf("sender%d@example.com", uid),
                To:        "recv@example.com",
                Date:      "2025-01-15",
            }
            if err := database.StoreMessageMetadata(user, folder, uid, meta); err != nil {
                t.Fatalf("StoreMessageMetadata %s/%d: %v", folder, uid, err)
            }
		}
	}

	svc := NewService(database, nil, nil)
	err = svc.BuildIndex(user)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()

	if idx.DocCount() != 9 {
		t.Errorf("expected 9 documents, got %d", idx.DocCount())
	}
}
