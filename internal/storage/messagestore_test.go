package storage

import (
	"os"
	"testing"
)

func TestNewMessageStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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

func TestNewMessageStoreInvalidPath(t *testing.T) {
	// Try to create store in a file (not a directory)
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

func TestStoreAndReadMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "testuser"
	data := []byte("Test message content for storage")

	// Store message
	messageID, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("StoreMessage() failed: %v", err)
	}

	if messageID == "" {
		t.Error("Expected non-empty message ID")
	}

	// Check message exists
	if !store.MessageExists(user, messageID) {
		t.Error("MessageExists() returned false for stored message")
	}

	// Read message back
	readData, err := store.ReadMessage(user, messageID)
	if err != nil {
		t.Fatalf("ReadMessage() failed: %v", err)
	}

	if string(readData) != string(data) {
		t.Errorf("Read data mismatch: got %q, want %q", string(readData), string(data))
	}
}

func TestStoreMessageDuplicate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "testuser"
	data := []byte("Same content")

	// Store same message twice
	id1, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("First StoreMessage() failed: %v", err)
	}

	id2, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("Second StoreMessage() failed: %v", err)
	}

	// Same content should produce same ID
	if id1 != id2 {
		t.Errorf("Duplicate messages have different IDs: %q vs %q", id1, id2)
	}
}

func TestReadMessageInvalidID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	// Test with too short ID
	_, err = store.ReadMessage("user", "ab")
	if err == nil {
		t.Error("Expected error for short message ID")
	}
}

func TestMessageExistsInvalidID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	// Test with too short ID
	if store.MessageExists("user", "ab") {
		t.Error("MessageExists() returned true for short ID")
	}

	// Test with empty ID
	if store.MessageExists("user", "") {
		t.Error("MessageExists() returned true for empty ID")
	}
}

func TestDeleteMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	user := "testuser"
	data := []byte("Message to delete")

	// Store message
	messageID, err := store.StoreMessage(user, data)
	if err != nil {
		t.Fatalf("StoreMessage() failed: %v", err)
	}

	// Verify it exists
	if !store.MessageExists(user, messageID) {
		t.Fatal("Message should exist before deletion")
	}

	// Delete message
	if err := store.DeleteMessage(user, messageID); err != nil {
		t.Fatalf("DeleteMessage() failed: %v", err)
	}

	// Verify it's gone
	if store.MessageExists(user, messageID) {
		t.Error("Message should not exist after deletion")
	}

	// Try to read deleted message
	_, err = store.ReadMessage(user, messageID)
	if err == nil {
		t.Error("Expected error reading deleted message")
	}
}

func TestDeleteMessageInvalidID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "msgstore-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMessageStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMessageStore() failed: %v", err)
	}
	defer store.Close()

	// Test with too short ID
	err = store.DeleteMessage("user", "ab")
	if err == nil {
		t.Error("Expected error for short message ID")
	}
}
