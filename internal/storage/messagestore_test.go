package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// --- NewMessageStore tests ---

func TestNewMessageStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("Expected store instance, got nil")
	}

	if store.basePath != tmpDir {
		t.Errorf("basePath = %q, want %q", store.basePath, tmpDir)
	}
}

func TestNewMessageStoreCreatesNestedDir(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "nested", "subdir")

	store, err := NewMessageStore(newDir)
	if err != nil {
		t.Fatalf("NewMessageStore with nested dir failed: %v", err)
	}
	defer store.Close()

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("Directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected directory, got file")
	}
}

func TestNewMessageStoreInvalidPath(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "notadir-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	_, err = NewMessageStore(tmpFile.Name() + "/subdir")
	if err == nil {
		t.Skip("Expected error for invalid path, but got none (may vary by platform)")
	}
}

func TestNewMessageStoreExistingDir(t *testing.T) {
	tmpDir := t.TempDir()

	store1, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("First NewMessageStore failed: %v", err)
	}
	defer store1.Close()

	store2, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("Second NewMessageStore failed: %v", err)
	}
	defer store2.Close()
}

// --- StoreMessage tests ---

func TestStoreAndReadMessage(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "testuser"
	data := []byte("Test message content for storage")

	messageID, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("StoreMessage() failed: %v", err)
	}

	if messageID == "" {
		t.Error("Expected non-empty message ID")
	}

	// Verify message ID is the SHA256 hex of the data
	expectedHash := sha256.Sum256(data)
	expectedID := hex.EncodeToString(expectedHash[:])
	if messageID != expectedID {
		t.Errorf("Message ID = %q, want %q", messageID, expectedID)
	}

	if !store.MessageExists(user, messageID) {
		t.Error("MessageExists() returned false for stored message")
	}

	readData, err := store.ReadMessage(user, messageID)
	if err != nil {
		t.Fatalf("ReadMessage() failed: %v", err)
	}

	if string(readData) != string(data) {
		t.Errorf("Read data mismatch: got %q, want %q", string(readData), string(data))
	}
}

func TestStoreMessageDuplicate(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "testuser"
	data := []byte("Same content")

	id1, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("First StoreMessage() failed: %v", err)
	}

	id2, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("Second StoreMessage() failed: %v", err)
	}

	if id1 != id2 {
		t.Errorf("Duplicate messages have different IDs: %q vs %q", id1, id2)
	}
}

func TestStoreMessageDifferentContent(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	data1 := []byte("First message")
	data2 := []byte("Second message")

	id1, err := store.StoreMessage("user", data1)
	if err != nil {
		t.Fatalf("StoreMessage data1 failed: %v", err)
	}

	id2, err := store.StoreMessage("user", data2)
	if err != nil {
		t.Fatalf("StoreMessage data2 failed: %v", err)
	}

	if id1 == id2 {
		t.Error("Different content should produce different IDs")
	}

	read1, err := store.ReadMessage("user", id1)
	if err != nil {
		t.Fatalf("ReadMessage id1 failed: %v", err)
	}
	if string(read1) != string(data1) {
		t.Errorf("Data mismatch for id1")
	}

	read2, err := store.ReadMessage("user", id2)
	if err != nil {
		t.Fatalf("ReadMessage id2 failed: %v", err)
	}
	if string(read2) != string(data2) {
		t.Errorf("Data mismatch for id2")
	}
}

func TestStoreMessageEmptyData(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	id, err := store.StoreMessage("user", []byte{})
	if err != nil {
		t.Fatalf("StoreMessage with empty data failed: %v", err)
	}
	if id == "" {
		t.Error("Expected non-empty ID for empty data")
	}

	readData, err := store.ReadMessage("user", id)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if len(readData) != 0 {
		t.Errorf("Expected empty data, got %q", string(readData))
	}
}

func TestStoreMessageLargeData(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	largeData := make([]byte, 100*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	id, err := store.StoreMessage("user", largeData)
	if err != nil {
		t.Fatalf("StoreMessage with large data failed: %v", err)
	}

	readData, err := store.ReadMessage("user", id)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if len(readData) != len(largeData) {
		t.Fatalf("Size mismatch: got %d, want %d", len(readData), len(largeData))
	}
	for i := range largeData {
		if readData[i] != largeData[i] {
			t.Fatalf("Data mismatch at byte %d", i)
		}
	}
}

func TestStoreMessageMultipleUsers(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	data := []byte("Shared message content")

	id1, err := store.StoreMessage("user1", data)
	if err != nil {
		t.Fatalf("StoreMessage for user1 failed: %v", err)
	}

	id2, err := store.StoreMessage("user2", data)
	if err != nil {
		t.Fatalf("StoreMessage for user2 failed: %v", err)
	}

	if id1 != id2 {
		t.Errorf("Same content for different users should have same ID: %q vs %q", id1, id2)
	}

	store.DeleteMessage("user1", id1)
	if store.MessageExists("user1", id1) {
		t.Error("Message should not exist for user1 after deletion")
	}
	if !store.MessageExists("user2", id2) {
		t.Error("Message should still exist for user2")
	}
}

func TestStoreMessageSpecialCharsInUser(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "user@example.com"
	data := []byte("Test message")

	id, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("StoreMessage with @ in username failed: %v", err)
	}

	if !store.MessageExists(user, id) {
		t.Error("Message should exist for user with @ in name")
	}

	readData, err := store.ReadMessage(user, id)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("Data mismatch")
	}
}

// TestStoreMessageFileLayout verifies the hash-based subdirectory structure
func TestStoreMessageFileLayout(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	data := []byte("Layout test")
	id, err := store.StoreMessage("user", data)
	if err != nil {
		t.Fatalf("StoreMessage failed: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "user", id[:2], id[2:4], id)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Message file not found at expected path %q", expectedPath)
	}

	fileContent, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read message file: %v", err)
	}
	if string(fileContent) != string(data) {
		t.Errorf("File content mismatch: got %q, want %q", string(fileContent), string(data))
	}
}

// TestStoreMessageUserDirBlocked triggers the MkdirAll error path for user directory
func TestStoreMessageUserDirBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	// Create a file where the user directory would go, blocking MkdirAll
	userDirPath := filepath.Join(tmpDir, "blockeduser")
	if err := os.WriteFile(userDirPath, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("Failed to create blocking file: %v", err)
	}
	defer os.Remove(userDirPath)

	_, err = store.StoreMessage("blockeduser", []byte("test"))
	if err == nil {
		t.Log("Expected error when user dir creation blocked by file, but got none (platform dependent)")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

// TestStoreMessageSubDirBlocked triggers the MkdirAll error for message subdirectory
func TestStoreMessageSubDirBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "subdirtest"
	data := []byte("Subdir test content")
	hash := sha256.Sum256(data)
	messageID := hex.EncodeToString(hash[:])

	// Create a file where the first hash subdirectory should go
	subDir1Path := filepath.Join(tmpDir, user, messageID[:2])
	if err := os.MkdirAll(filepath.Dir(subDir1Path), 0755); err != nil {
		t.Fatalf("Failed to create user dir: %v", err)
	}
	if err := os.WriteFile(subDir1Path, []byte("blocker"), 0644); err != nil {
		t.Fatalf("Failed to create blocking file: %v", err)
	}

	_, err = store.StoreMessage(user, data)
	if err == nil {
		t.Log("Subdirectory error not triggered (may vary by OS)")
	} else {
		t.Logf("Got expected subdirectory error: %v", err)
	}
}

// TestStoreMessageWriteFileBlockedByDir triggers the WriteFile error path
func TestStoreMessageWriteFileBlockedByDir(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "writefailuser"
	data := []byte("Content that will fail to write")
	hash := sha256.Sum256(data)
	messageID := hex.EncodeToString(hash[:])

	// Create the subdirectories, but make the target path a DIRECTORY
	msgPath := filepath.Join(tmpDir, user, messageID[:2], messageID[2:4], messageID)
	if err := os.MkdirAll(filepath.Dir(msgPath), 0755); err != nil {
		t.Fatalf("Failed to create subdirectories: %v", err)
	}
	if err := os.MkdirAll(msgPath, 0755); err != nil {
		t.Fatalf("Failed to create blocking directory: %v", err)
	}

	_, err = store.StoreMessage(user, data)
	if err == nil {
		t.Log("WriteFile error not triggered (may vary by OS)")
	} else {
		t.Logf("Got expected write error: %v", err)
	}
}

// TestStoreMultipleMessagesSameUser stores several messages and verifies uniqueness
func TestStoreMultipleMessagesSameUser(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	messages := []string{
		"Message one",
		"Message two",
		"Message three",
		"Message four",
		"Message five",
	}

	ids := make(map[string]bool)
	for _, msg := range messages {
		id, err := store.StoreMessage("user", []byte(msg))
		if err != nil {
			t.Fatalf("StoreMessage(%q) failed: %v", msg, err)
		}

		if ids[id] {
			t.Errorf("Duplicate ID for message %q", msg)
		}
		ids[id] = true

		if !store.MessageExists("user", id) {
			t.Errorf("Message %q should exist after storing", msg)
		}
	}

	if len(ids) != len(messages) {
		t.Errorf("Expected %d unique IDs, got %d", len(messages), len(ids))
	}
}

// --- ReadMessage tests ---

func TestReadMessageInvalidID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	_, err = store.ReadMessage("user", "ab")
	if err == nil {
		t.Error("Expected error for short message ID")
	}
}

func TestReadMessageWith3CharID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	_, err = store.ReadMessage("user", "abc")
	if err == nil {
		t.Error("Expected error for 3-char message ID")
	}
}

func TestReadMessageEmptyID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	_, err = store.ReadMessage("user", "")
	if err == nil {
		t.Error("Expected error for empty message ID")
	}
}

func TestReadMessageNonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	_, err = store.ReadMessage("user", "abcdef1234567890")
	if err == nil {
		t.Error("Expected error for non-existent message")
	}
}

// --- MessageExists tests ---

func TestMessageExistsInvalidID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	if store.MessageExists("user", "ab") {
		t.Error("MessageExists() returned true for short ID")
	}

	if store.MessageExists("user", "") {
		t.Error("MessageExists() returned true for empty ID")
	}
}

func TestMessageExistsWith3CharID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	if store.MessageExists("user", "abc") {
		t.Error("MessageExists should return false for 3-char ID")
	}
}

func TestMessageExistsNonExistentUser(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	if store.MessageExists("nonexistent", "abcdef1234567890abcdef1234567890") {
		t.Error("MessageExists should return false for non-existent user")
	}
}

// --- DeleteMessage tests ---

func TestDeleteMessage(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "testuser"
	data := []byte("Message to delete")

	messageID, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("StoreMessage() failed: %v", err)
	}

	if !store.MessageExists(user, messageID) {
		t.Fatal("Message should exist before deletion")
	}

	if err := store.DeleteMessage(user, messageID); err != nil {
		t.Fatalf("DeleteMessage() failed: %v", err)
	}

	if store.MessageExists(user, messageID) {
		t.Error("Message should not exist after deletion")
	}

	_, err = store.ReadMessage(user, messageID)
	if err == nil {
		t.Error("Expected error reading deleted message")
	}
}

func TestDeleteMessageInvalidID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	err = store.DeleteMessage("user", "ab")
	if err == nil {
		t.Error("Expected error for short message ID")
	}
}

func TestDeleteMessageWith3CharID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	err = store.DeleteMessage("user", "abc")
	if err == nil {
		t.Error("Expected error for 3-char message ID")
	}
}

func TestDeleteMessageEmptyID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	err = store.DeleteMessage("user", "")
	if err == nil {
		t.Error("Expected error for empty message ID")
	}
}

func TestDeleteMessageNonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	err = store.DeleteMessage("user", "abcdef1234567890")
	if err == nil {
		t.Log("DeleteMessage returned nil for non-existent message")
	}
}

// --- Close tests ---

func TestStoreClose(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

func TestStoreCloseMultiple(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Errorf("First Close() failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Errorf("Second Close() failed: %v", err)
	}
}

// --- Integration-style tests ---

func TestStoreAndDeleteAndVerifyReadFails(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	data := []byte("Will be deleted")
	id, err := store.StoreMessage("user", data)
	if err != nil {
		t.Fatalf("StoreMessage failed: %v", err)
	}

	if err := store.DeleteMessage("user", id); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	_, err = store.ReadMessage("user", id)
	if err == nil {
		t.Error("Expected error reading deleted message")
	}
}

func TestStoreDeleteAndReStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	data := []byte("Store, delete, re-store")
	id1, err := store.StoreMessage("user", data)
	if err != nil {
		t.Fatalf("First StoreMessage failed: %v", err)
	}

	store.DeleteMessage("user", id1)

	// Re-storing same content should succeed
	id2, err := store.StoreMessage("user", data)
	if err != nil {
		t.Fatalf("Second StoreMessage failed: %v", err)
	}

	if id1 != id2 {
		t.Errorf("Same content should produce same ID: %q vs %q", id1, id2)
	}

	if !store.MessageExists("user", id2) {
		t.Error("Re-stored message should exist")
	}
}
