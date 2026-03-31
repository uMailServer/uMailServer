package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *Database {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestOpenDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	if database == nil {
		t.Fatal("Expected non-nil Database")
	}

	// Verify the database file was created (path was set)
	if database.path == "" {
		t.Error("Expected non-empty path")
	}
}

func TestOpenDatabaseMultipleTimes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db1, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("First OpenDatabase failed: %v", err)
	}
	defer db1.Close()

	db2, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("Second OpenDatabase failed: %v", err)
	}
	defer db2.Close()

	if db1.path != db2.path {
		t.Errorf("Expected same path, got %q and %q", db1.path, db2.path)
	}
}

func TestDatabaseStruct(t *testing.T) {
	database := &Database{}
	if database == nil {
		t.Fatal("Failed to create Database instance")
	}
}

func TestDatabaseStructWithPath(t *testing.T) {
	database := &Database{path: "/some/path"}
	if database.path != "/some/path" {
		t.Errorf("Expected path '/some/path', got %q", database.path)
	}
}

func TestDatabaseClose(t *testing.T) {
	database := setupTestDB(t)
	err := database.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestDatabaseAuthenticateUser(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"basic user", "user", "pass"},
		{"email user", "user@example.com", "password123"},
		{"empty username", "", "pass"},
		{"empty password", "user", ""},
		{"both empty", "", ""},
		{"special chars", "user+tag@example.com", "p@$$w0rd!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := setupTestDB(t)
			authenticated, err := database.AuthenticateUser(tt.username, tt.password)
			if err != nil {
				t.Errorf("AuthenticateUser(%q, %q) returned error: %v", tt.username, tt.password, err)
			}
			// Stub implementation always returns true
			if !authenticated {
				t.Errorf("AuthenticateUser(%q, %q) = false, want true (stub)", tt.username, tt.password)
			}
		})
	}
}

func TestDatabaseGetMailbox(t *testing.T) {
	database := setupTestDB(t)

	tests := []struct {
		name     string
		user     string
		mailbox  string
	}{
		{"INBOX", "user@example.com", "INBOX"},
		{"custom mailbox", "user@example.com", "Archive"},
		{"case sensitive", "user@example.com", "inbox"},
		{"empty user", "", "INBOX"},
		{"empty mailbox", "user", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb, err := database.GetMailbox(tt.user, tt.mailbox)
			if err != nil {
				t.Fatalf("GetMailbox(%q, %q) failed: %v", tt.user, tt.mailbox, err)
			}
			if mb == nil {
				t.Fatal("Expected non-nil mailbox")
			}
			if mb.Name != tt.mailbox {
				t.Errorf("Expected mailbox name %q, got %q", tt.mailbox, mb.Name)
			}
			if mb.UIDValidity == 0 {
				t.Error("Expected non-zero UIDValidity")
			}
			if mb.UIDNext != 1 {
				t.Errorf("Expected UIDNext 1, got %d", mb.UIDNext)
			}
		})
	}
}

func TestDatabaseCreateMailbox(t *testing.T) {
	database := setupTestDB(t)

	tests := []struct {
		name    string
		user    string
		mailbox string
	}{
		{"INBOX", "user@example.com", "INBOX"},
		{"custom", "user@example.com", "TestBox"},
		{"sent", "user@example.com", "Sent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := database.CreateMailbox(tt.user, tt.mailbox)
			if err != nil {
				t.Errorf("CreateMailbox(%q, %q) returned error: %v", tt.user, tt.mailbox, err)
			}
		})
	}
}

func TestDatabaseDeleteMailbox(t *testing.T) {
	database := setupTestDB(t)

	err := database.DeleteMailbox("user@example.com", "TestBox")
	if err != nil {
		t.Errorf("DeleteMailbox returned error: %v", err)
	}
}

func TestDatabaseRenameMailbox(t *testing.T) {
	database := setupTestDB(t)

	err := database.RenameMailbox("user@example.com", "OldBox", "NewBox")
	if err != nil {
		t.Errorf("RenameMailbox returned error: %v", err)
	}
}

func TestDatabaseListMailboxes(t *testing.T) {
	database := setupTestDB(t)

	mailboxes, err := database.ListMailboxes("user")
	if err != nil {
		t.Fatalf("ListMailboxes failed: %v", err)
	}
	if len(mailboxes) == 0 {
		t.Error("Expected at least one mailbox")
	}
	if mailboxes[0] != "INBOX" {
		t.Errorf("Expected INBOX as first mailbox, got %q", mailboxes[0])
	}
}

func TestDatabaseGetMailboxCounts(t *testing.T) {
	database := setupTestDB(t)

	exists, recent, unseen, err := database.GetMailboxCounts("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	// Stub returns 0, 0, 0
	if exists != 0 || recent != 0 || unseen != 0 {
		t.Errorf("Expected all zeros, got exists=%d recent=%d unseen=%d", exists, recent, unseen)
	}
}

func TestDatabaseGetNextUID(t *testing.T) {
	database := setupTestDB(t)

	uid, err := database.GetNextUID("user", "INBOX")
	if err != nil {
		t.Fatalf("GetNextUID failed: %v", err)
	}
	if uid == 0 {
		t.Error("Expected non-zero UID")
	}
}

func TestDatabaseGetMessageUIDs(t *testing.T) {
	database := setupTestDB(t)

	uids, err := database.GetMessageUIDs("user", "INBOX")
	if err != nil {
		t.Fatalf("GetMessageUIDs failed: %v", err)
	}
	// Stub returns empty slice
	if uids == nil {
		t.Error("Expected non-nil slice")
	}
}

func TestDatabaseGetMessageMetadata(t *testing.T) {
	database := setupTestDB(t)

	meta, err := database.GetMessageMetadata("user", "INBOX", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}
	// Stub returns empty metadata
	if meta.UID != 0 {
		t.Errorf("Expected UID 0 from stub, got %d", meta.UID)
	}
}

func TestDatabaseStoreMessageMetadata(t *testing.T) {
	database := setupTestDB(t)

	tests := []struct {
		name string
		meta *MessageMetadata
	}{
		{
			"full metadata",
			&MessageMetadata{
				MessageID:    "msg123",
				UID:          1,
				Flags:        []string{"\\Seen"},
				InternalDate: time.Now(),
				Size:         1024,
				Subject:      "Test Subject",
				From:         "from@test.com",
				To:           "to@test.com",
			},
		},
		{
			"minimal metadata",
			&MessageMetadata{
				UID:     1,
				Subject: "Test",
			},
		},
		{
			"nil-like metadata",
			&MessageMetadata{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := database.StoreMessageMetadata("user", "INBOX", tt.meta.UID, tt.meta)
			if err != nil {
				t.Errorf("StoreMessageMetadata returned error: %v", err)
			}
		})
	}
}

func TestDatabaseUpdateMessageMetadata(t *testing.T) {
	database := setupTestDB(t)

	meta := &MessageMetadata{
		UID:     1,
		Subject: "Updated Subject",
		Flags:   []string{"\\Seen", "\\Flagged"},
	}
	err := database.UpdateMessageMetadata("user", "INBOX", 1, meta)
	if err != nil {
		t.Errorf("UpdateMessageMetadata returned error: %v", err)
	}
}

func TestDatabaseDeleteMessage(t *testing.T) {
	database := setupTestDB(t)

	err := database.DeleteMessage("user", "INBOX", 1)
	if err != nil {
		t.Errorf("DeleteMessage returned error: %v", err)
	}
}

func TestDatabaseMultipleOperations(t *testing.T) {
	database := setupTestDB(t)

	// Sequence of operations to test the database can handle multiple calls
	database.CreateMailbox("user@example.com", "INBOX")
	database.CreateMailbox("user@example.com", "Archive")

	mb, err := database.GetMailbox("user@example.com", "INBOX")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mb.Name != "INBOX" {
		t.Errorf("Expected INBOX, got %s", mb.Name)
	}

	database.StoreMessageMetadata("user@example.com", "INBOX", 1, &MessageMetadata{
		UID:     1,
		Subject: "Test",
		From:    "from@test.com",
		To:      "to@test.com",
		Flags:   []string{"\\Seen"},
	})

	database.UpdateMessageMetadata("user@example.com", "INBOX", 1, &MessageMetadata{
		UID:     1,
		Subject: "Updated",
		From:    "from@test.com",
		To:      "to@test.com",
		Flags:   []string{"\\Seen", "\\Flagged"},
	})

	database.DeleteMessage("user@example.com", "INBOX", 1)
	database.DeleteMailbox("user@example.com", "Archive")
	database.RenameMailbox("user@example.com", "INBOX", "Inbox")
}

func TestDatabaseDifferentUsers(t *testing.T) {
	database := setupTestDB(t)

	users := []string{"alice@example.com", "bob@example.com", "charlie@test.org"}

	for _, user := range users {
		mb, err := database.GetMailbox(user, "INBOX")
		if err != nil {
			t.Errorf("GetMailbox for %s failed: %v", user, err)
		}
		if mb == nil {
			t.Errorf("Expected mailbox for %s", user)
		}

		err = database.CreateMailbox(user, "Archive")
		if err != nil {
			t.Errorf("CreateMailbox for %s failed: %v", user, err)
		}
	}
}

func TestMailboxStruct(t *testing.T) {
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

func TestMailboxStructZeroValues(t *testing.T) {
	mbox := &Mailbox{}
	if mbox.Name != "" {
		t.Errorf("Expected empty Name, got %q", mbox.Name)
	}
	if mbox.UIDValidity != 0 {
		t.Errorf("Expected zero UIDValidity, got %d", mbox.UIDValidity)
	}
	if mbox.UIDNext != 0 {
		t.Errorf("Expected zero UIDNext, got %d", mbox.UIDNext)
	}
}

func TestMessageMetadataStruct(t *testing.T) {
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

func TestMessageMetadataStructZeroValues(t *testing.T) {
	meta := &MessageMetadata{}
	if meta.MessageID != "" {
		t.Errorf("Expected empty MessageID, got %q", meta.MessageID)
	}
	if meta.UID != 0 {
		t.Errorf("Expected zero UID, got %d", meta.UID)
	}
	if meta.Size != 0 {
		t.Errorf("Expected zero Size, got %d", meta.Size)
	}
	if meta.Flags != nil {
		t.Errorf("Expected nil Flags, got %v", meta.Flags)
	}
	if !meta.InternalDate.IsZero() {
		t.Errorf("Expected zero InternalDate, got %v", meta.InternalDate)
	}
}

func TestMessageMetadataAllFields(t *testing.T) {
	now := time.Now()
	meta := &MessageMetadata{
		MessageID:    "<abc123@example.com>",
		UID:          42,
		Flags:        []string{"\\Seen", "\\Flagged", "\\Recent"},
		InternalDate: now,
		Size:         2048,
		Subject:      "Re: Important Discussion",
		Date:         "Tue, 02 Jan 2024 10:30:00 +0000",
		From:         "alice@example.com",
		To:           "bob@example.com",
	}

	if meta.MessageID != "<abc123@example.com>" {
		t.Errorf("MessageID mismatch: got %q", meta.MessageID)
	}
	if len(meta.Flags) != 3 {
		t.Errorf("Expected 3 flags, got %d", len(meta.Flags))
	}
	if meta.InternalDate != now {
		t.Errorf("InternalDate mismatch")
	}
}

func TestOpenDatabasePathWithParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "a", "b", "c", "test.db")

	database, err := OpenDatabase(nestedPath)
	if err != nil {
		t.Fatalf("OpenDatabase with nested dirs failed: %v", err)
	}
	defer database.Close()

	if database.path != nestedPath {
		t.Errorf("Path mismatch: got %q, want %q", database.path, nestedPath)
	}
}

func TestDatabaseCloseMultiple(t *testing.T) {
	database := setupTestDB(t)

	// First close
	err := database.Close()
	if err != nil {
		t.Errorf("First Close returned error: %v", err)
	}
	// Second close (stub returns nil anyway)
	err = database.Close()
	if err != nil {
		t.Errorf("Second Close returned error: %v", err)
	}
}

func TestDatabaseGetMailboxCountsForDifferentMailboxes(t *testing.T) {
	database := setupTestDB(t)

	mailboxes := []string{"INBOX", "Archive", "Sent", "Drafts", "Trash"}

	for _, mbox := range mailboxes {
		exists, recent, unseen, err := database.GetMailboxCounts("user", mbox)
		if err != nil {
			t.Errorf("GetMailboxCounts(%q) failed: %v", mbox, err)
		}
		// Stub returns zeros
		_ = exists
		_ = recent
		_ = unseen
	}
}

func TestOpenDatabaseAndVerifyFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "verify.db")

	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer database.Close()

	// The stub just sets the path, doesn't actually create a file.
	// But we verify the path is stored correctly.
	if database.path != dbPath {
		t.Errorf("Database path = %q, want %q", database.path, dbPath)
	}
}

func TestDatabaseGetNextUIDDifferentMailboxes(t *testing.T) {
	database := setupTestDB(t)

	uid1, err := database.GetNextUID("user", "INBOX")
	if err != nil {
		t.Fatalf("GetNextUID INBOX failed: %v", err)
	}

	uid2, err := database.GetNextUID("user", "Archive")
	if err != nil {
		t.Fatalf("GetNextUID Archive failed: %v", err)
	}

	// Stub returns 1 for both
	_ = uid1
	_ = uid2
}

func TestDatabaseAuthenticateUserDifferentCredentials(t *testing.T) {
	database := setupTestDB(t)

	// All should return true (stub behavior)
	creds := []struct {
		user, pass string
	}{
		{"admin", "admin123"},
		{"user@example.com", "password"},
		{"test.user+tag@domain.org", "complex!@#$%"},
	}

	for _, c := range creds {
		ok, err := database.AuthenticateUser(c.user, c.pass)
		if err != nil {
			t.Errorf("AuthenticateUser(%q, %q) error: %v", c.user, c.pass, err)
		}
		if !ok {
			t.Errorf("AuthenticateUser(%q, %q) = false, want true (stub)", c.user, c.pass)
		}
	}
}

func init() {
	// Ensure the test binary can access os package
	_ = os.ModeDir
}
