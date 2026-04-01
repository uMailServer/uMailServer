package pop3

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMaildirStore_ListMessagesEmpty tests ListMessages when user has no messages.
func TestMaildirStore_ListMessagesEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msgs, err := store.ListMessages("user@example.com")
	if err != nil {
		t.Fatalf("ListMessages on empty store: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

// TestMaildirStore_ListMessagesWithFiles tests ListMessages with actual files.
func TestMaildirStore_ListMessagesWithFiles(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	user := "user_example.com"

	// Create new/ and cur/ directories
	newDir := filepath.Join(tmpDir, user, "new")
	curDir := filepath.Join(tmpDir, user, "cur")
	os.MkdirAll(newDir, 0755)
	os.MkdirAll(curDir, 0755)

	// Create test messages in new/
	os.WriteFile(filepath.Join(newDir, "1001.testhost"), []byte("msg1"), 0644)
	os.WriteFile(filepath.Join(newDir, "1002.testhost"), []byte("msg2"), 0644)
	// Create test message in cur/
	os.WriteFile(filepath.Join(curDir, "1003.testhost"), []byte("msg3"), 0644)

	msgs, err := store.ListMessages("user@example.com")
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

// TestMaildirStore_DeleteMessage tests DeleteMessage.
func TestMaildirStore_DeleteMessage(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	user := "user_example.com"

	// Create new/ directory with a file
	newDir := filepath.Join(tmpDir, user, "new")
	os.MkdirAll(newDir, 0755)
	os.WriteFile(filepath.Join(newDir, "1001.testhost"), []byte("msg1"), 0644)

	// List to get index
	msgs, _ := store.ListMessages("user@example.com")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message before delete, got %d", len(msgs))
	}

	// Delete it
	err := store.DeleteMessage("user@example.com", 0)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	// Verify it's gone
	msgs, _ = store.ListMessages("user@example.com")
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after delete, got %d", len(msgs))
	}
}

// TestMaildirStore_DeleteMessageFromCurDir tests deleting a message from cur/ directory.
func TestMaildirStore_DeleteMessageFromCurDir(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	user := "user_example.com"

	// Create cur/ directory with a file
	curDir := filepath.Join(tmpDir, user, "cur")
	os.MkdirAll(curDir, 0755)
	os.WriteFile(filepath.Join(curDir, "1001.testhost"), []byte("msg1"), 0644)

	msgs, _ := store.ListMessages("user@example.com")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	err := store.DeleteMessage("user@example.com", 0)
	if err != nil {
		t.Fatalf("DeleteMessage from cur failed: %v", err)
	}
}

// TestMaildirStore_DeleteMessageInvalidIndex tests DeleteMessage with invalid index.
func TestMaildirStore_DeleteMessageInvalidIndex(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.DeleteMessage("user@example.com", 99)
	if err == nil {
		t.Error("expected error for invalid index")
	}
}

// TestMaildirStore_GetMessageData tests reading message data.
func TestMaildirStore_GetMessageData(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	user := "user_example.com"

	// Create new/ with a message
	newDir := filepath.Join(tmpDir, user, "new")
	os.MkdirAll(newDir, 0755)
	content := []byte("Subject: Test\r\n\r\nHello World")
	os.WriteFile(filepath.Join(newDir, "1001.testhost"), content, 0644)

	data, err := store.GetMessageData("user@example.com", 0)
	if err != nil {
		t.Fatalf("GetMessageData failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("expected %q, got %q", content, data)
	}
}

// TestMaildirStore_GetMessageSize tests getting message size.
func TestMaildirStore_GetMessageSize(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	user := "user_example.com"

	newDir := filepath.Join(tmpDir, user, "new")
	os.MkdirAll(newDir, 0755)
	content := []byte("Subject: Test\r\n\r\nHello World")
	os.WriteFile(filepath.Join(newDir, "1001.testhost"), content, 0644)

	size, err := store.GetMessageSize("user@example.com", 0)
	if err != nil {
		t.Fatalf("GetMessageSize failed: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}
}

// TestMaildirStore_ReadMaildirWithSubdir tests that readMaildir skips directories.
func TestMaildirStore_ReadMaildirWithSubdir(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	dir := filepath.Join(tmpDir, "testdir")
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "file1"), []byte("data"), 0644)

	msgs, err := store.readMaildir(dir, "new")
	if err != nil {
		t.Fatalf("readMaildir failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message (skipping subdir), got %d", len(msgs))
	}
}

// TestMaildirStore_ReadMaildirNonexistent tests readMaildir on nonexistent path.
func TestMaildirStore_ReadMaildirNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msgs, err := store.readMaildir(filepath.Join(tmpDir, "nonexistent"), "new")
	if err != nil {
		t.Fatalf("readMaildir on nonexistent path: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

// TestMaildirStore_Authenticate tests user authentication.
func TestMaildirStore_Authenticate(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	user := "user_example.com"

	// User doesn't exist yet
	ok, err := store.Authenticate(user, "password")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if ok {
		t.Error("expected auth to fail for nonexistent user")
	}

	// Create user directory
	os.MkdirAll(filepath.Join(tmpDir, user), 0755)
	ok, err = store.Authenticate(user, "password")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if !ok {
		t.Error("expected auth to succeed for existing user")
	}
}
