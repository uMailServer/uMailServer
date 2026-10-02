package pop3

import (
	"bytes"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
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
	// Test the branch where the message has "Deleted" (without backslash) flag.
	// Since the RFC 1939 §4 fix, messages flagged \Deleted (either spelling) are
	// excluded from the POP3 maildrop view; an unflagged message stays visible.
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
	kept := &storage.MessageMetadata{
		MessageID: "keptmessage123456",
		UID:       7,
		Flags:     []string{"\\Seen"},
		Size:      60,
	}
	db.StoreMessageMetadata(user, "INBOX", kept.UID, kept)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	messages, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(messages) != 1 || messages[0].UID != "7" {
		t.Fatalf("DeleteMessage-flag spelling filter: expected only UID 7 visible, got %v", messages)
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

func TestBboltStore_GetMessageDataReadsBlobAddressedData(t *testing.T) {
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
	uid := uint32(1000)

	// Raw octets are content-hash-addressed in the MessageStore; the blob id
	// lives in the message metadata (MessageID) and GetMessageData resolves
	// through it.
	testData := []byte("blob addressed message data")
	blobID, err := msgStore.StoreMessage(user, testData)
	if err != nil {
		t.Fatalf("StoreMessage failed: %v", err)
	}
	meta := &storage.MessageMetadata{
		MessageID: blobID,
		UID:       uid,
		Flags:     []string{},
		Size:      int64(len(testData)),
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	data, err := store.GetMessageData(user, 1)
	if err != nil {
		t.Fatalf("GetMessageData failed: %v", err)
	}
	if !bytes.Equal(data, testData) {
		t.Fatalf("GetMessageData returned %q, want %q", string(data), string(testData))
	}
}
