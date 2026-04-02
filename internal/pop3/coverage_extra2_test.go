package pop3

import (
	"github.com/umailserver/umailserver/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestBboltStore_GetMessageDataBothFail(t *testing.T) {
	// Both ReadMessage and the INBOX/ fallback fail because the UID string
	// is short (< 4 chars) and ReadMessage requires >= 4 char IDs.
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	uid := uint32(42)
	// Store metadata with UID 42 -> UID string is "42" (< 4 chars)
	// ReadMessage will fail with "invalid message ID" for both paths
	meta := &storage.MessageMetadata{
		MessageID: "nonexistent1234",
		UID:       uid,
		Flags:     []string{},
		Size:      100,
		Subject:   "Ghost",
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	_, err = store.GetMessageData(user, 1)
	if err == nil {
		t.Error("Expected error when both read paths fail")
	}
}

func TestBboltStore_DeleteMessageWithPlainDeletedFlag(t *testing.T) {
	// Test the branch where the message has "Deleted" (without backslash) flag
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	uid := uint32(6)
	meta := &storage.MessageMetadata{
		MessageID: "plaindeleted12345",
		UID:       uid,
		Flags:     []string{"Deleted"},
		Size:      50,
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	err = store.DeleteMessage(user, 1)
	if err != nil {
		t.Fatalf("DeleteMessage with 'Deleted' flag failed: %v", err)
	}
}

func TestBboltStore_ListMessagesZeroSize(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	meta := &storage.MessageMetadata{
		UID:   1,
		Flags: []string{},
		Size:  0,
	}
	db.StoreMessageMetadata(user, "INBOX", 1, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	msgs, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message, got %d", len(msgs))
	}
}

func TestBboltStore_GetMessageDataINBOXFallback(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	uid := uint32(1000) // UID string "1000" has >= 4 chars for ReadMessage

	// Store metadata
	meta := &storage.MessageMetadata{
		MessageID: "test1000",
		UID:       uid,
		Flags:     []string{},
		Size:      20,
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	// The direct ReadMessage(user, "1000") will fail (no file there).
	// The fallback ReadMessage(user, "INBOX/1000") looks at:
	//   basePath/user/IN/BO/INBOX/1000
	// (first 2 chars of "INBOX/1000" = "IN", next 2 = "BO")
	inboxDir := filepath.Join(tmpDir, "messages", user, "IN", "BO", "INBOX")
	os.MkdirAll(inboxDir, 0755)
	testData := []byte("INBOX fallback data!!")
	os.WriteFile(filepath.Join(inboxDir, "1000"), testData, 0644)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	data, err := store.GetMessageData(user, 1)
	if err != nil {
		t.Fatalf("GetMessageData with INBOX fallback failed: %v", err)
	}
	if string(data) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(data))
	}
}
