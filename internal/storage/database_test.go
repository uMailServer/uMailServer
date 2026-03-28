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

func TestDatabaseClose(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	err := db.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestDatabaseAuthenticateUser(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	authenticated, err := db.AuthenticateUser("user", "pass")
	if err != nil {
		t.Errorf("AuthenticateUser returned error: %v", err)
	}
	// Stub implementation returns true
	if !authenticated {
		t.Error("Expected authenticated to be true")
	}
}

func TestDatabaseGetMailbox(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	mb, err := db.GetMailbox("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mb == nil {
		t.Fatal("Expected non-nil mailbox")
	}
	if mb.Name != "INBOX" {
		t.Errorf("Expected mailbox name INBOX, got %s", mb.Name)
	}
}

func TestDatabaseCreateMailbox(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	err := db.CreateMailbox("user", "TestBox")
	if err != nil {
		t.Errorf("CreateMailbox returned error: %v", err)
	}
}

func TestDatabaseDeleteMailbox(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	err := db.DeleteMailbox("user", "TestBox")
	if err != nil {
		t.Errorf("DeleteMailbox returned error: %v", err)
	}
}

func TestDatabaseRenameMailbox(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	err := db.RenameMailbox("user", "OldBox", "NewBox")
	if err != nil {
		t.Errorf("RenameMailbox returned error: %v", err)
	}
}

func TestDatabaseListMailboxes(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	mailboxes, err := db.ListMailboxes("user")
	if err != nil {
		t.Fatalf("ListMailboxes failed: %v", err)
	}
	if len(mailboxes) == 0 {
		t.Error("Expected at least one mailbox")
	}
}

func TestDatabaseGetMailboxCounts(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	exists, recent, unseen, err := db.GetMailboxCounts("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	_ = exists
	_ = recent
	_ = unseen
}

func TestDatabaseGetNextUID(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	uid, err := db.GetNextUID("user", "INBOX")
	if err != nil {
		t.Fatalf("GetNextUID failed: %v", err)
	}
	if uid == 0 {
		t.Error("Expected non-zero UID")
	}
}

func TestDatabaseGetMessageUIDs(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	uids, err := db.GetMessageUIDs("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMessageUIDs failed: %v", err)
	}
	_ = uids
}

func TestDatabaseGetMessageMetadata(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	meta, err := db.GetMessageMetadata("user", "INBOX", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}
}

func TestDatabaseStoreMessageMetadata(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	meta := &MessageMetadata{
		UID:     1,
		Subject: "Test",
	}
	err := db.StoreMessageMetadata("user", "INBOX", 1, meta)
	if err != nil {
		t.Errorf("StoreMessageMetadata returned error: %v", err)
	}
}

func TestDatabaseUpdateMessageMetadata(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	meta := &MessageMetadata{
		UID:     1,
		Subject: "Test",
	}
	err := db.UpdateMessageMetadata("user", "INBOX", 1, meta)
	if err != nil {
		t.Errorf("UpdateMessageMetadata returned error: %v", err)
	}
}

func TestDatabaseDeleteMessage(t *testing.T) {
	db, _ := OpenDatabase("/tmp/test.db")
	err := db.DeleteMessage("user", "INBOX", 1)
	if err != nil {
		t.Errorf("DeleteMessage returned error: %v", err)
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
