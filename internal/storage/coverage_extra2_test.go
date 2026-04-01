package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// TestCreateMailbox_Success tests the full CreateMailbox path including
// initial bucket creation with uidvalidity and uidnext.
func TestCreateMailbox_Success(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	err = database.CreateMailbox("testuser", "INBOX")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Verify the mailbox was created by reading it back
	mb, err := database.GetMailbox("testuser", "INBOX")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mb.Name != "INBOX" {
		t.Errorf("expected INBOX, got %s", mb.Name)
	}
	if mb.UIDValidity == 0 {
		t.Error("expected non-zero UIDValidity")
	}
	if mb.UIDNext == 0 {
		t.Error("expected non-zero UIDNext")
	}
}

// TestCreateMailbox_Idempotent tests that creating the same mailbox twice works.
func TestCreateMailbox_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	err = database.CreateMailbox("testuser", "INBOX")
	if err != nil {
		t.Fatalf("first CreateMailbox failed: %v", err)
	}

	// Second call should NOT overwrite existing uidvalidity/uidnext
	err = database.CreateMailbox("testuser", "INBOX")
	if err != nil {
		t.Fatalf("second CreateMailbox failed: %v", err)
	}

	// Verify uidvalidity was preserved (not overwritten)
	mb, err := database.GetMailbox("testuser", "INBOX")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mb.UIDValidity == 0 {
		t.Error("expected UIDValidity to be preserved after idempotent create")
	}
}

// TestCreateMailbox_NilBolt tests CreateMailbox with nil bolt (returns nil).
func TestCreateMailbox_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	err := database.CreateMailbox("testuser", "INBOX")
	if err != nil {
		t.Errorf("expected nil error with nil bolt, got %v", err)
	}
}

// TestCreateMailbox_AlreadyInitializedBucket tests that when a bucket
// already has uidvalidity set, the Put calls are skipped.
func TestCreateMailbox_AlreadyInitializedBucket(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// First creation sets uidvalidity and uidnext
	database.CreateMailbox("user1", "INBOX")

	// Get the original uidvalidity
	mb1, _ := database.GetMailbox("user1", "INBOX")
	originalValidity := mb1.UIDValidity

	// Second creation should skip the uidvalidity/uidnext init (branch: b.Get != nil)
	database.CreateMailbox("user1", "INBOX")

	mb2, _ := database.GetMailbox("user1", "INBOX")
	if mb2.UIDValidity != originalValidity {
		t.Errorf("uidvalidity changed from %d to %d on second create", originalValidity, mb2.UIDValidity)
	}
}

// TestGetNextUID_NewMailbox tests GetNextUID for a mailbox with no existing
// uidnext value (uid == 0, sets to 1).
func TestGetNextUID_NewMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Don't call CreateMailbox first - let GetNextUID create the bucket
	uid, err := database.GetNextUID("user1", "NewFolder")
	if err != nil {
		t.Fatalf("GetNextUID failed: %v", err)
	}
	if uid != 1 {
		t.Errorf("expected first UID to be 1 for new mailbox, got %d", uid)
	}

	// Verify it increments
	uid2, err := database.GetNextUID("user1", "NewFolder")
	if err != nil {
		t.Fatalf("GetNextUID (2nd) failed: %v", err)
	}
	if uid2 != 2 {
		t.Errorf("expected second UID to be 2, got %d", uid2)
	}
}

// TestGetNextUID_NilBolt tests GetNextUID with nil bolt (returns 1).
func TestGetNextUID_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	uid, err := database.GetNextUID("user", "INBOX")
	if err != nil {
		t.Errorf("expected nil error with nil bolt, got %v", err)
	}
	if uid != 1 {
		t.Errorf("expected UID 1, got %d", uid)
	}
}

// TestGetMessageMetadata_NonExistentMailbox tests GetMessageMetadata when
// the mailbox messages bucket does not exist (b == nil branch).
func TestGetMessageMetadata_NonExistentMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Don't create mailbox - the messages bucket won't exist
	meta, err := database.GetMessageMetadata("user1", "NoMailbox", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata should not error for non-existent mailbox: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.UID != 0 {
		t.Errorf("expected zero UID for non-existent mailbox, got %d", meta.UID)
	}
}

// TestGetMessageMetadata_NonExistentMessage tests GetMessageMetadata when
// the bucket exists but the specific UID does not (data == nil branch).
func TestGetMessageMetadata_NonExistentMessage(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Create the mailbox and store one message
	database.CreateMailbox("user1", "INBOX")
	database.StoreMessageMetadata("user1", "INBOX", 1, &MessageMetadata{
		UID:     1,
		Subject: "First message",
	})

	// Query for UID 99 which doesn't exist (data == nil path)
	meta, err := database.GetMessageMetadata("user1", "INBOX", 99)
	if err != nil {
		t.Fatalf("GetMessageMetadata should not error for non-existent UID: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.UID != 0 {
		t.Errorf("expected zero UID for non-existent message, got %d", meta.UID)
	}
}

// TestGetMessageMetadata_NilBolt tests GetMessageMetadata with nil bolt.
func TestGetMessageMetadata_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	meta, err := database.GetMessageMetadata("user", "INBOX", 1)
	if err != nil {
		t.Errorf("expected nil error with nil bolt, got %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
}

// TestStoreMessageMetadata_NilBolt tests StoreMessageMetadata with nil bolt.
func TestStoreMessageMetadata_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	err := database.StoreMessageMetadata("user", "INBOX", 1, &MessageMetadata{UID: 1})
	if err != nil {
		t.Errorf("expected nil error with nil bolt, got %v", err)
	}
}

// TestStoreMessageMetadata_RoundTrip verifies StoreMessageMetadata and
// GetMessageMetadata round-trip correctly for all fields.
func TestStoreMessageMetadata_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	now := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)
	meta := &MessageMetadata{
		MessageID:    "<test@example.com>",
		UID:          42,
		Flags:        []string{"\\Seen", "\\Flagged"},
		InternalDate: now,
		Size:         2048,
		Subject:      "Round Trip Test",
		Date:         "Sat, 15 Mar 2025 12:00:00 +0000",
		From:         "from@example.com",
		To:           "to@example.com",
	}

	err = database.StoreMessageMetadata("user1", "INBOX", 42, meta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	got, err := database.GetMessageMetadata("user1", "INBOX", 42)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}

	if got.MessageID != meta.MessageID {
		t.Errorf("MessageID mismatch: got %q, want %q", got.MessageID, meta.MessageID)
	}
	if got.Subject != meta.Subject {
		t.Errorf("Subject mismatch: got %q, want %q", got.Subject, meta.Subject)
	}
	if got.Size != meta.Size {
		t.Errorf("Size mismatch: got %d, want %d", got.Size, meta.Size)
	}
	if got.From != meta.From {
		t.Errorf("From mismatch: got %q, want %q", got.From, meta.From)
	}
	if got.To != meta.To {
		t.Errorf("To mismatch: got %q, want %q", got.To, meta.To)
	}
	if len(got.Flags) != len(meta.Flags) {
		t.Errorf("Flags len mismatch: got %d, want %d", len(got.Flags), len(meta.Flags))
	}
	if !got.InternalDate.Equal(meta.InternalDate) {
		t.Errorf("InternalDate mismatch: got %v, want %v", got.InternalDate, meta.InternalDate)
	}
}

// TestStoreMessageMetadata_ClosedDB tests StoreMessageMetadata on a closed database.
func TestStoreMessageMetadata_ClosedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	err = database.StoreMessageMetadata("user", "INBOX", 1, &MessageMetadata{UID: 1})
	if err == nil {
		t.Error("expected error when storing metadata on closed database")
	}
}

// TestCreateMailbox_ClosedDB tests CreateMailbox on a closed database.
func TestCreateMailbox_ClosedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	err = database.CreateMailbox("user", "INBOX")
	if err == nil {
		t.Error("expected error when creating mailbox on closed database")
	}
}

// TestGetNextUID_ClosedDB tests GetNextUID on a closed database.
func TestGetNextUID_ClosedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	_, err = database.GetNextUID("user", "INBOX")
	if err == nil {
		t.Error("expected error when getting next UID on closed database")
	}
}

// TestGetMessageMetadata_ClosedDB tests GetMessageMetadata on a closed database.
func TestGetMessageMetadata_ClosedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	_, err = database.GetMessageMetadata("user", "INBOX", 1)
	if err == nil {
		t.Error("expected error when getting metadata on closed database")
	}
}

// TestBboltCreateMailbox_BucketAlreadyHasUID tests the branch in CreateMailbox
// where uidvalidity already exists in the bucket (skipping the Put calls).
func TestBboltCreateMailbox_BucketAlreadyHasUID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// First create
	database.CreateMailbox("user1", "INBOX")

	// Read back to get original validity
	mb1, _ := database.GetMailbox("user1", "INBOX")
	if mb1.UIDValidity == 0 {
		t.Fatal("expected non-zero UIDValidity after first create")
	}

	// Second create should NOT overwrite (b.Get("uidvalidity") != nil branch)
	database.CreateMailbox("user1", "INBOX")

	mb2, _ := database.GetMailbox("user1", "INBOX")
	if mb2.UIDValidity != mb1.UIDValidity {
		t.Errorf("UIDValidity changed: was %d, now %d", mb1.UIDValidity, mb2.UIDValidity)
	}
}

// TestBboltGetMessageMetadata_BadJSON tests GetMessageMetadata when the stored
// data is not valid JSON (exercises json.Unmarshal error path).
func TestBboltGetMessageMetadata_BadJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Create the mailbox first so the messages bucket exists
	database.CreateMailbox("user1", "INBOX")

	// Inject bad JSON directly into the messages bucket
	database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(messagesBucket("user1", "INBOX")))
		if b == nil {
			t.Fatal("messages bucket should exist")
		}
		return b.Put(itob(999), []byte("not valid json"))
	})

	// GetMessageMetadata should return an error for invalid JSON
	_, err = database.GetMessageMetadata("user1", "INBOX", 999)
	if err == nil {
		t.Error("expected error for invalid JSON data in message metadata")
	}
}

// TestMessageMetadataJSON_RoundTrip verifies that MessageMetadata survives
// JSON marshaling and unmarshaling correctly.
func TestMessageMetadataJSON_RoundTrip(t *testing.T) {
	now := time.Now()
	meta := &MessageMetadata{
		MessageID:    "<roundtrip@test.com>",
		UID:          7,
		Flags:        []string{"\\Seen", "\\Recent"},
		InternalDate: now,
		Size:         512,
		Subject:      "JSON Round Trip",
		Date:         "Mon, 01 Jan 2024 00:00:00 +0000",
		From:         "a@b.com",
		To:           "c@d.com",
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var got MessageMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if got.MessageID != meta.MessageID {
		t.Errorf("MessageID: got %q, want %q", got.MessageID, meta.MessageID)
	}
	if got.UID != meta.UID {
		t.Errorf("UID: got %d, want %d", got.UID, meta.UID)
	}
	if got.Size != meta.Size {
		t.Errorf("Size: got %d, want %d", got.Size, meta.Size)
	}
}

// TestMessageStore_StoreAndRead tests StoreMessage and ReadMessage round trip.
func TestMessageStore_StoreAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	data := []byte("Subject: Test\r\n\r\nHello World")
	msgID, err := store.StoreMessage("user@example.com", data)
	if err != nil {
		t.Fatalf("StoreMessage failed: %v", err)
	}
	if msgID == "" {
		t.Error("expected non-empty message ID")
	}

	// Read back
	got, err := store.ReadMessage("user@example.com", msgID)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

// TestMessageStore_StoreDuplicate tests that storing the same message twice
// returns the same ID (content-hash-based dedup).
func TestMessageStore_StoreDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	data := []byte("Subject: Test\r\n\r\nHello World")
	id1, _ := store.StoreMessage("user@example.com", data)
	id2, _ := store.StoreMessage("user@example.com", data)

	if id1 != id2 {
		t.Errorf("expected same ID for duplicate content, got %q and %q", id1, id2)
	}
}

// TestMessageStore_MessageExists tests MessageExists.
func TestMessageStore_MessageExists(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	data := []byte("Subject: Test\r\n\r\nHello World")
	id, _ := store.StoreMessage("user@example.com", data)

	if !store.MessageExists("user@example.com", id) {
		t.Error("expected message to exist")
	}
	if store.MessageExists("user@example.com", "nonexistent") {
		t.Error("expected nonexistent message to not exist")
	}
	// Short ID
	if store.MessageExists("user@example.com", "ab") {
		t.Error("expected short ID to not exist")
	}
}

// TestMessageStore_DeleteMessage tests DeleteMessage.
func TestMessageStore_DeleteMessage(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	data := []byte("Subject: Test\r\n\r\nHello World")
	id, _ := store.StoreMessage("user@example.com", data)

	if err := store.DeleteMessage("user@example.com", id); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	if store.MessageExists("user@example.com", id) {
		t.Error("expected message to be deleted")
	}
}

// TestMessageStore_ReadMessageInvalidID tests ReadMessage with short ID.
func TestMessageStore_ReadMessageInvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	_, err = store.ReadMessage("user@example.com", "ab")
	if err == nil {
		t.Error("expected error for short message ID")
	}
}

// TestMessageStore_DeleteMessageInvalidID tests DeleteMessage with short ID.
func TestMessageStore_DeleteMessageInvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	err = store.DeleteMessage("user@example.com", "ab")
	if err == nil {
		t.Error("expected error for short message ID")
	}
}

// TestNewMessageStore_MkdirError tests NewMessageStore when MkdirAll fails.
func TestNewMessageStore_MkdirError(t *testing.T) {
	// Create a file at the base path to prevent MkdirAll
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "store")
	os.WriteFile(filePath, []byte("x"), 0644)

	_, err := NewMessageStore(filePath + "/sub")
	if err == nil {
		t.Error("expected error when base path conflicts with file")
	}
}
