package storage

import (
	"testing"
	"time"
)

// Test GetOrCreateThreadID with nil database
func TestGetOrCreateThreadID_NoDB(t *testing.T) {
	db := &Database{bolt: nil}

	threadID, err := db.GetOrCreateThreadID("user@example.com", "INBOX", "Test Subject", "", nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if threadID == "" {
		t.Error("Expected non-empty thread ID")
	}
}

// Test GetOrCreateThreadID with In-Reply-To
func TestGetOrCreateThreadID_WithInReplyTo(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a message to reply to
	msg := &MessageMetadata{
		MessageID:    "original-msg@example.com",
		UID:          1,
		Subject:      "Original Subject",
		ThreadID:     "existing-thread-123",
		InternalDate: time.Now(),
	}

	err = db.StoreMessageMetadata("user@example.com", "INBOX", 1, msg)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Now create a reply
	threadID, err := db.GetOrCreateThreadID("user@example.com", "INBOX", "Re: Original Subject", "original-msg@example.com", nil)
	if err != nil {
		t.Errorf("GetOrCreateThreadID failed: %v", err)
	}

	if threadID != "existing-thread-123" {
		t.Errorf("Expected thread ID 'existing-thread-123', got %s", threadID)
	}
}

// Test GetOrCreateThreadID with References
func TestGetOrCreateThreadID_WithReferences(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a message
	msg := &MessageMetadata{
		MessageID:    "ref-msg@example.com",
		UID:          1,
		Subject:      "Reference Subject",
		ThreadID:     "ref-thread-456",
		InternalDate: time.Now(),
	}

	err = db.StoreMessageMetadata("user@example.com", "INBOX", 1, msg)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Now create a message with reference
	refs := []string{"ref-msg@example.com", "other@example.com"}
	threadID, err := db.GetOrCreateThreadID("user@example.com", "INBOX", "Re: Reference Subject", "", refs)
	if err != nil {
		t.Errorf("GetOrCreateThreadID failed: %v", err)
	}

	if threadID != "ref-thread-456" {
		t.Errorf("Expected thread ID 'ref-thread-456', got %s", threadID)
	}
}

// Test GetOrCreateThreadID with subject matching
func TestGetOrCreateThreadID_WithSubjectMatch(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a message with a thread
	msg := &MessageMetadata{
		MessageID:    "msg@example.com",
		UID:          1,
		Subject:      "Project Discussion",
		ThreadID:     "subject-thread-789",
		InternalDate: time.Now(),
	}

	err = db.StoreMessageMetadata("user@example.com", "INBOX", 1, msg)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Now create a message with similar subject (no references)
	threadID, err := db.GetOrCreateThreadID("user@example.com", "INBOX", "Re: Project Discussion", "", nil)
	if err != nil {
		t.Errorf("GetOrCreateThreadID failed: %v", err)
	}

	if threadID != "subject-thread-789" {
		t.Errorf("Expected thread ID 'subject-thread-789', got %s", threadID)
	}
}

// Test GetOrCreateThreadID creates new thread when no match
func TestGetOrCreateThreadID_NewThread(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a message with no threading headers and no subject match
	threadID, err := db.GetOrCreateThreadID("user@example.com", "INBOX", "Unique Subject XYZ", "", nil)
	if err != nil {
		t.Errorf("GetOrCreateThreadID failed: %v", err)
	}

	if threadID == "" {
		t.Error("Expected non-empty thread ID for new thread")
	}
}

// Test findThreadByReferences with nil database
func TestFindThreadByReferences_NoDB(t *testing.T) {
	db := &Database{bolt: nil}

	threadID, err := db.findThreadByReferences("user@example.com", "INBOX", "msg@example.com", nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if threadID != "" {
		t.Errorf("Expected empty thread ID, got %s", threadID)
	}
}

// Test findThreadBySubject with nil database
func TestFindThreadBySubject_NoDB(t *testing.T) {
	db := &Database{bolt: nil}

	threadID, err := db.findThreadBySubject("user@example.com", "INBOX", "Test Subject")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if threadID != "" {
		t.Errorf("Expected empty thread ID, got %s", threadID)
	}
}

// Test findThreadBySubject with empty subject
func TestFindThreadBySubject_EmptySubject(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	threadID, err := db.findThreadBySubject("user@example.com", "INBOX", "")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if threadID != "" {
		t.Errorf("Expected empty thread ID for empty subject, got %s", threadID)
	}
}

// Test findThreadBySubject with old thread (older than 30 days)
func TestFindThreadBySubject_OldThread(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create an old message
	msg := &MessageMetadata{
		MessageID:    "old-msg@example.com",
		UID:          1,
		Subject:      "Old Discussion",
		ThreadID:     "old-thread-999",
		InternalDate: time.Now().Add(-40 * 24 * time.Hour), // 40 days ago
	}

	err = db.StoreMessageMetadata("user@example.com", "INBOX", 1, msg)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Try to find by subject - should not match because thread is too old
	threadID, err := db.findThreadBySubject("user@example.com", "INBOX", "Old Discussion")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if threadID != "" {
		t.Errorf("Expected empty thread ID for old thread, got %s", threadID)
	}
}

// Test generateThreadID
func TestGenerateThreadID(t *testing.T) {
	// Generate two thread IDs with a small delay
	id1 := generateThreadID("Test Subject")
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	id2 := generateThreadID("Test Subject")
	id3 := generateThreadID("Different Subject")

	// All should be non-empty
	if id1 == "" || id2 == "" || id3 == "" {
		t.Error("Expected non-empty thread IDs")
	}

	// Same subject should generate different IDs due to timestamp
	if id1 == id2 {
		t.Error("Expected different thread IDs for sequential calls")
	}

	// Different subjects should generate different IDs
	if id1 == id3 {
		t.Error("Expected different thread IDs for different subjects")
	}

	// Should be 32 characters (16 bytes hex encoded)
	if len(id1) != 32 {
		t.Errorf("Expected 32 character thread ID, got %d", len(id1))
	}
}

// Test GetThreadMessages
func TestGetThreadMessages(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create messages in a thread
	threadID := "thread-messages-123"
	msgs := []*MessageMetadata{
		{
			MessageID:    "msg1@example.com",
			UID:          1,
			Subject:      "Thread Subject",
			ThreadID:     threadID,
			From:         "alice@example.com",
			To:           "bob@example.com",
			Flags:        []string{},
			InternalDate: time.Now(),
		},
		{
			MessageID:    "msg2@example.com",
			UID:          2,
			Subject:      "Re: Thread Subject",
			ThreadID:     threadID,
			From:         "bob@example.com",
			To:           "alice@example.com",
			Flags:        []string{`\Seen`},
			InternalDate: time.Now(),
		},
		{
			MessageID:    "msg3@example.com",
			UID:          3,
			Subject:      "Different Thread",
			ThreadID:     "other-thread",
			From:         "charlie@example.com",
			To:           "alice@example.com",
			Flags:        []string{},
			InternalDate: time.Now(),
		},
	}

	for i, msg := range msgs {
		err = db.StoreMessageMetadata("user@example.com", "INBOX", uint32(i+1), msg)
		if err != nil {
			t.Fatalf("Failed to store message: %v", err)
		}
	}

	// Get messages for thread
	messages, err := db.GetThreadMessages("user@example.com", "INBOX", threadID)
	if err != nil {
		t.Errorf("GetThreadMessages failed: %v", err)
	}

	if len(messages) != 2 {
		t.Errorf("Expected 2 messages in thread, got %d", len(messages))
	}

	// Verify message fields
	for _, msg := range messages {
		if msg.Mailbox != "INBOX" {
			t.Errorf("Expected mailbox INBOX, got %s", msg.Mailbox)
		}
		// Check IsRead flag
		if msg.MessageID == "msg2@example.com" && !msg.IsRead {
			t.Error("Expected msg2 to be marked as read")
		}
		if msg.MessageID == "msg1@example.com" && msg.IsRead {
			t.Error("Expected msg1 to be marked as unread")
		}
	}
}

// Test GetThreadMessages with nil database
func TestGetThreadMessages_NoDB(t *testing.T) {
	db := &Database{bolt: nil}

	messages, err := db.GetThreadMessages("user@example.com", "INBOX", "thread-123")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(messages))
	}
}

// Test GetThreadMessages with non-existent mailbox
func TestGetThreadMessages_NoMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	messages, err := db.GetThreadMessages("user@example.com", "NonExistent", "thread-123")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("Expected 0 messages for non-existent mailbox, got %d", len(messages))
	}
}
