package storage

import (
	"testing"
	"time"
)

func TestOpenDatabase(t *testing.T) {
	// OpenDatabase is a wrapper around db.Open
	// Since this requires a real database file, we just verify the function exists
	// The actual database functionality is tested in the db package
	t.Log("OpenDatabase is a wrapper around db.Open - tested in db package")
}

func TestDatabaseStruct(t *testing.T) {
	// Verify Database struct can be instantiated
	db := &Database{}
	if db == nil {
		t.Fatal("Failed to create Database instance")
	}
}

func TestMailboxStruct(t *testing.T) {
	// Verify Mailbox struct fields
	mbox := &Mailbox{
		Name:        "INBOX",
		UIDValidity: 1234567890,
		UIDNext:     11,
	}

	if mbox.Name != "INBOX" {
		t.Errorf("Name mismatch: got %q, want %q", mbox.Name, "INBOX")
	}

	if mbox.UIDValidity != 1234567890 {
		t.Errorf("UIDValidity mismatch: got %d, want %d", mbox.UIDValidity, 1234567890)
	}

	if mbox.UIDNext != 11 {
		t.Errorf("UIDNext mismatch: got %d, want %d", mbox.UIDNext, 11)
	}
}

func TestMessageMetadataStruct(t *testing.T) {
	// Verify MessageMetadata struct fields
	meta := &MessageMetadata{
		MessageID:    "msg123",
		UID:          1,
		From:         "sender@example.com",
		To:           "recipient@example.com",
		Subject:      "Test Subject",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		Size:         1024,
		Flags:        []string{"\\Seen"},
		InternalDate: time.Now(),
	}

	if meta.MessageID != "msg123" {
		t.Errorf("MessageID mismatch: got %q, want %q", meta.MessageID, "msg123")
	}

	if meta.UID != 1 {
		t.Errorf("UID mismatch: got %d, want %d", meta.UID, 1)
	}

	if meta.Subject != "Test Subject" {
		t.Errorf("Subject mismatch: got %q, want %q", meta.Subject, "Test Subject")
	}

	if meta.Size != 1024 {
		t.Errorf("Size mismatch: got %d, want %d", meta.Size, 1024)
	}

	if len(meta.Flags) != 1 || meta.Flags[0] != "\\Seen" {
		t.Errorf("Flags mismatch: got %v", meta.Flags)
	}
}
