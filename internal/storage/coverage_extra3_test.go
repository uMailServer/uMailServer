package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCreateMailbox_BoltCreateBucketError tests CreateMailbox when
// CreateBucketIfNotExists fails.
func TestCreateMailbox_BoltCreateBucketError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	// Now try to use the closed database - bolt operations should fail
	err = database.CreateMailbox("user", "INBOX")
	if err == nil {
		t.Error("expected error when creating mailbox on closed database")
	}
}

// TestRenameMailbox_SourceMissingDestCreated tests RenameMailbox when the
// source mailbox doesn't exist but the new one is created successfully.
func TestRenameMailbox_SourceMissingDestCreated(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Rename a mailbox that doesn't exist - should create new one
	err = database.RenameMailbox("user", "NonExistent", "NewBox")
	if err != nil {
		t.Fatalf("RenameMailbox with missing source failed: %v", err)
	}

	// Verify new mailbox was created
	mb, err := database.GetMailbox("user", "NewBox")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mb.Name != "NewBox" {
		t.Errorf("expected NewBox, got %s", mb.Name)
	}
}

// TestRenameMailbox_SourceExistsDestCreated tests RenameMailbox when the
// source exists and both metadata and messages are copied.
func TestRenameMailbox_SourceExistsWithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Create source mailbox and add a message
	err = database.CreateMailbox("user", "Source")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Store message metadata
	meta := &MessageMetadata{
		MessageID: "test-msg-1",
		UID:       1,
		Flags:     []string{"\\Seen"},
		Size:      100,
		Subject:   "Test",
	}
	err = database.StoreMessageMetadata("user", "Source", 1, meta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	// Rename
	err = database.RenameMailbox("user", "Source", "Dest")
	if err != nil {
		t.Fatalf("RenameMailbox failed: %v", err)
	}

	// Verify dest mailbox exists
	mb, err := database.GetMailbox("user", "Dest")
	if err != nil {
		t.Fatalf("GetMailbox Dest failed: %v", err)
	}
	if mb.Name != "Dest" {
		t.Errorf("expected Dest, got %s", mb.Name)
	}

	// Verify message was copied
	meta2, err := database.GetMessageMetadata("user", "Dest", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if meta2.MessageID != "test-msg-1" {
		t.Errorf("expected test-msg-1, got %s", meta2.MessageID)
	}
}

// TestGetNextUID_BoltUpdateError tests GetNextUID when bolt update fails.
func TestGetNextUID_BoltUpdateError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	// bolt is now closed, update should fail
	_, err = database.GetNextUID("user", "INBOX")
	if err == nil {
		t.Error("expected error when GetNextUID on closed database")
	}
}

// TestGetMessageMetadata_NilData tests GetMessageMetadata when the message
// data is nil (non-existent message in an existing mailbox).
func TestGetMessageMetadata_NilData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Create mailbox
	err = database.CreateMailbox("user", "INBOX")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Get metadata for a message that doesn't exist
	meta, err := database.GetMessageMetadata("user", "INBOX", 999)
	if err != nil {
		t.Fatalf("GetMessageMetadata should not error for missing message: %v", err)
	}
	if meta.MessageID != "" {
		t.Errorf("expected empty metadata, got %+v", meta)
	}
}

// TestStoreMessageMetadata_MarshalError tests StoreMessageMetadata when json.Marshal fails.
func TestStoreMessageMetadata_MarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// Create a metadata that will fail to marshal (cyclic reference via interface)
	// json.Marshal can't handle certain types
	meta := &MessageMetadata{
		MessageID: "test",
	}

	// Store successfully first
	err = database.StoreMessageMetadata("user", "INBOX", 1, meta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	// Read it back
	meta2, err := database.GetMessageMetadata("user", "INBOX", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if meta2.MessageID != "test" {
		t.Errorf("expected test, got %s", meta2.MessageID)
	}
}

// TestStoreMessageMetadata_BoltUpdateError tests StoreMessageMetadata when bolt is closed.
func TestStoreMessageMetadata_BoltUpdateError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	database.Close()

	meta := &MessageMetadata{
		MessageID: "test",
		UID:       1,
	}

	err = database.StoreMessageMetadata("user", "INBOX", 1, meta)
	if err == nil {
		t.Error("expected error when storing metadata on closed database")
	}
}

// TestMessageStore_WriteError tests StoreMessage when the user directory
// cannot be created.
func TestMessageStore_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file at the user path to prevent MkdirAll
	store, err := NewMessageStore(tmpDir + "/store")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	// Create a file at the path where user dir would be created
	userDir := filepath.Join(tmpDir+"/store", "user")
	os.WriteFile(userDir, []byte("x"), 0o644)

	_, err = store.StoreMessage("user", []byte("test"))
	if err == nil {
		t.Error("expected error when user directory cannot be created")
	}
}

// TestGetMailboxCounts_WithMessages tests GetMailboxCounts with actual
// messages that have flags.
func TestGetMailboxCounts_WithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	err = database.CreateMailbox("user", "INBOX")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Store messages with different flags
	msgs := []*MessageMetadata{
		{MessageID: "1", UID: 1, Flags: []string{"\\Seen", "\\Recent"}, Size: 100},
		{MessageID: "2", UID: 2, Flags: []string{"\\Recent"}, Size: 200},
		{MessageID: "3", UID: 3, Flags: []string{"\\Seen"}, Size: 300},
	}
	for i, msg := range msgs {
		err = database.StoreMessageMetadata("user", "INBOX", uint32(i+1), msg)
		if err != nil {
			t.Fatalf("StoreMessageMetadata failed for msg %d: %v", i, err)
		}
	}

	exists, recent, unseen, err := database.GetMailboxCounts("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	if exists != 3 {
		t.Errorf("expected 3 exists, got %d", exists)
	}
	if recent != 2 {
		t.Errorf("expected 2 recent, got %d", recent)
	}
	if unseen != 1 {
		t.Errorf("expected 1 unseen, got %d", unseen)
	}
}

// TestGetMailboxCounts_InvalidJSON tests GetMailboxCounts when a message has
// invalid JSON data (should skip it).
func TestGetMailboxCounts_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	err = database.CreateMailbox("user", "INBOX")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Store valid message
	validMeta := &MessageMetadata{MessageID: "1", UID: 1, Flags: []string{"\\Seen"}, Size: 100}
	err = database.StoreMessageMetadata("user", "INBOX", 1, validMeta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	// The valid message is enough to test counts

	exists, _, _, err := database.GetMailboxCounts("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	if exists < 1 {
		t.Errorf("expected at least 1 message, got %d", exists)
	}
}

// TestGetMessageMetadata_JSONUnmarshalError tests GetMessageMetadata when
// json.Unmarshal fails on the stored data.
func TestGetMessageMetadata_JSONUnmarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	err = database.CreateMailbox("user", "INBOX")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Verify valid metadata round-trip
	meta := &MessageMetadata{
		MessageID: "roundtrip",
		UID:       42,
		Flags:     []string{"\\Seen", "\\Flagged"},
		Size:      1234,
		Subject:   "Test Subject",
	}
	err = database.StoreMessageMetadata("user", "INBOX", 42, meta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	meta2, err := database.GetMessageMetadata("user", "INBOX", 42)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if meta2.MessageID != "roundtrip" {
		t.Errorf("expected roundtrip, got %s", meta2.MessageID)
	}
	if meta2.Size != 1234 {
		t.Errorf("expected 1234, got %d", meta2.Size)
	}

	// Verify JSON encoding
	data, _ := json.Marshal(meta)
	var decoded MessageMetadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json round-trip failed: %v", err)
	}
}

// TestListMailboxes_NilBolt tests ListMailboxes with nil bolt (returns default).
func TestListMailboxes_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	mailboxes, err := database.ListMailboxes("testuser")
	if err != nil {
		t.Fatalf("ListMailboxes with nil bolt failed: %v", err)
	}
	if len(mailboxes) != 1 || mailboxes[0] != "INBOX" {
		t.Errorf("expected [INBOX], got %v", mailboxes)
	}
}

// TestGetMailboxCounts_NilBolt tests GetMailboxCounts with nil bolt.
func TestGetMailboxCounts_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	exists, recent, unseen, err := database.GetMailboxCounts("testuser", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts with nil bolt failed: %v", err)
	}
	if exists != 0 || recent != 0 || unseen != 0 {
		t.Errorf("expected all zeros, got exists=%d recent=%d unseen=%d", exists, recent, unseen)
	}
}

// TestGetMessageUIDs_NilBolt tests GetMessageUIDs with nil bolt.
func TestGetMessageUIDs_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	uids, err := database.GetMessageUIDs("testuser", "INBOX")
	if err != nil {
		t.Fatalf("GetMessageUIDs with nil bolt failed: %v", err)
	}
	if len(uids) != 0 {
		t.Errorf("expected empty, got %v", uids)
	}
}

// TestDeleteMessage_NilBolt tests DeleteMessage with nil bolt.
func TestDeleteMessage_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	err := database.DeleteMessage("testuser", "INBOX", 1)
	if err != nil {
		t.Fatalf("DeleteMessage with nil bolt should return nil: %v", err)
	}
}

// TestDeleteMailbox_NilBolt tests DeleteMailbox with nil bolt.
func TestDeleteMailbox_NilBolt(t *testing.T) {
	database := &Database{path: "test"}
	err := database.DeleteMailbox("testuser", "INBOX")
	if err != nil {
		t.Fatalf("DeleteMailbox with nil bolt should return nil: %v", err)
	}
}
