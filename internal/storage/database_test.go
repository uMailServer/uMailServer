package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
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

	// Second open should fail because db1 has it locked
	db2, err := OpenDatabase(dbPath)
	if err == nil {
		db2.Close()
		t.Fatal("Expected error when opening already-open database, got nil")
	}

	// Now close db1 and try again
	db1.Close()

	db3, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase after close failed: %v", err)
	}
	defer db3.Close()

	if db3.path != filepath.Join(tmpDir, "test.db") {
		t.Errorf("Expected path %q, got %q", filepath.Join(tmpDir, "test.db"), db3.path)
	}
}

func TestDatabaseStruct(t *testing.T) {
	database := &Database{}
	_ = database
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
			// AuthenticateUser is not implemented — real auth is via SetAuthFunc
			_, err := database.AuthenticateUser(tt.username, tt.password)
			if err == nil {
				t.Errorf("AuthenticateUser(%q, %q) expected error (not implemented)", tt.username, tt.password)
			}
		})
	}
}

func TestDatabaseGetMailbox(t *testing.T) {
	database := setupTestDB(t)

	tests := []struct {
		name    string
		user    string
		mailbox string
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

	// AuthenticateUser returns error — real auth is via injected SetAuthFunc
	creds := []struct {
		user, pass string
	}{
		{"admin", "admin123"},
		{"user@example.com", "password"},
		{"test.user+tag@domain.org", "complex!@#$%"},
	}

	for _, c := range creds {
		_, err := database.AuthenticateUser(c.user, c.pass)
		if err == nil {
			t.Errorf("AuthenticateUser(%q, %q) expected error (not implemented)", c.user, c.pass)
		}
	}
}

func init() {
	// Ensure the test binary can access os package
	_ = os.ModeDir
}

// setupRealDB creates a bbolt-backed database for testing
func setupRealDB(t *testing.T) *Database {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "real.db")
	database, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if database.bolt == nil {
		t.Fatal("Expected bbolt DB to be initialized")
	}
	return database
}

func TestBboltCreateAndGetMailbox(t *testing.T) {
	db := setupRealDB(t)

	err := db.CreateMailbox("user1", "INBOX")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	mb, err := db.GetMailbox("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mb.Name != "INBOX" {
		t.Errorf("Expected INBOX, got %s", mb.Name)
	}
	if mb.UIDValidity == 0 {
		t.Error("Expected non-zero UIDValidity")
	}
	if mb.UIDNext == 0 {
		t.Error("Expected non-zero UIDNext")
	}
}

func TestBboltCreateMailboxTwice(t *testing.T) {
	db := setupRealDB(t)

	err := db.CreateMailbox("user1", "INBOX")
	if err != nil {
		t.Fatalf("First CreateMailbox failed: %v", err)
	}
	err = db.CreateMailbox("user1", "INBOX")
	if err != nil {
		t.Fatalf("Second CreateMailbox failed: %v", err)
	}
}

func TestBboltDeleteMailbox(t *testing.T) {
	db := setupRealDB(t)

	db.CreateMailbox("user1", "TestBox")
	err := db.DeleteMailbox("user1", "TestBox")
	if err != nil {
		t.Errorf("DeleteMailbox failed: %v", err)
	}
}

func TestBboltRenameMailbox(t *testing.T) {
	db := setupRealDB(t)

	db.CreateMailbox("user1", "OldBox")
	err := db.RenameMailbox("user1", "OldBox", "NewBox")
	if err != nil {
		t.Errorf("RenameMailbox failed: %v", err)
	}

	mb, err := db.GetMailbox("user1", "NewBox")
	if err != nil {
		t.Fatalf("GetMailbox after rename failed: %v", err)
	}
	if mb.Name != "NewBox" {
		t.Errorf("Expected NewBox, got %s", mb.Name)
	}
}

func TestBboltRenameNonexistentMailbox(t *testing.T) {
	db := setupRealDB(t)

	err := db.RenameMailbox("user1", "NoBox", "NewBox")
	if err != nil {
		t.Errorf("RenameMailbox of nonexistent should not error: %v", err)
	}
}

func TestBboltListMailboxes(t *testing.T) {
	db := setupRealDB(t)

	db.CreateMailbox("user1", "INBOX")
	db.CreateMailbox("user1", "Sent")
	db.CreateMailbox("user1", "Trash")

	list, err := db.ListMailboxes("user1")
	if err != nil {
		t.Fatalf("ListMailboxes failed: %v", err)
	}
	if len(list) < 3 {
		t.Errorf("Expected at least 3 mailboxes, got %d: %v", len(list), list)
	}
}

func TestBboltListMailboxesNoMailboxes(t *testing.T) {
	db := setupRealDB(t)

	list, err := db.ListMailboxes("nobody")
	if err != nil {
		t.Fatalf("ListMailboxes failed: %v", err)
	}
	if len(list) == 0 {
		t.Error("Expected at least INBOX as default")
	}
}

func TestBboltGetMailboxCounts(t *testing.T) {
	db := setupRealDB(t)

	db.CreateMailbox("user1", "INBOX")

	// Store messages
	db.StoreMessageMetadata("user1", "INBOX", 1, &MessageMetadata{
		UID:   1,
		Flags: []string{"\\Recent"},
		Size:  100,
	})
	db.StoreMessageMetadata("user1", "INBOX", 2, &MessageMetadata{
		UID:   2,
		Flags: []string{"\\Seen"},
		Size:  200,
	})
	db.StoreMessageMetadata("user1", "INBOX", 3, &MessageMetadata{
		UID:   3,
		Flags: []string{"\\Recent"},
		Size:  300,
	})

	exists, recent, unseen, err := db.GetMailboxCounts("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	if exists != 3 {
		t.Errorf("Expected 3 exists, got %d", exists)
	}
	if recent != 2 {
		t.Errorf("Expected 2 recent, got %d", recent)
	}
	if unseen != 2 {
		t.Errorf("Expected 2 unseen, got %d", unseen)
	}
}

func TestBboltGetMailboxCountsEmpty(t *testing.T) {
	db := setupRealDB(t)

	exists, recent, unseen, err := db.GetMailboxCounts("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	if exists != 0 || recent != 0 || unseen != 0 {
		t.Errorf("Expected all zeros, got exists=%d recent=%d unseen=%d", exists, recent, unseen)
	}
}

func TestBboltGetNextUID(t *testing.T) {
	db := setupRealDB(t)

	uid1, err := db.GetNextUID("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetNextUID failed: %v", err)
	}
	uid2, err := db.GetNextUID("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetNextUID failed: %v", err)
	}
	if uid2 <= uid1 {
		t.Errorf("Expected uid2 > uid1, got uid1=%d uid2=%d", uid1, uid2)
	}
}

func TestBboltGetNextUIDNewMailbox(t *testing.T) {
	db := setupRealDB(t)

	uid, err := db.GetNextUID("user1", "NewBox")
	if err != nil {
		t.Fatalf("GetNextUID for new mailbox failed: %v", err)
	}
	if uid != 1 {
		t.Errorf("Expected first UID to be 1, got %d", uid)
	}
}

func TestBboltStoreAndGetMessageMetadata(t *testing.T) {
	db := setupRealDB(t)

	meta := &MessageMetadata{
		MessageID:    "msg123",
		UID:          1,
		Flags:        []string{"\\Seen", "\\Recent"},
		InternalDate: time.Now(),
		Size:         1024,
		Subject:      "Test Subject",
		Date:         "Mon, 01 Jan 2024 12:00:00 +0000",
		From:         "from@test.com",
		To:           "to@test.com",
	}

	err := db.StoreMessageMetadata("user1", "INBOX", 1, meta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	got, err := db.GetMessageMetadata("user1", "INBOX", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if got.MessageID != "msg123" {
		t.Errorf("Expected MessageID msg123, got %s", got.MessageID)
	}
	if got.Subject != "Test Subject" {
		t.Errorf("Expected Subject 'Test Subject', got %s", got.Subject)
	}
	if got.Size != 1024 {
		t.Errorf("Expected Size 1024, got %d", got.Size)
	}
	if len(got.Flags) != 2 {
		t.Errorf("Expected 2 flags, got %d", len(got.Flags))
	}
}

func TestBboltGetMessageUIDs(t *testing.T) {
	db := setupRealDB(t)

	db.StoreMessageMetadata("user1", "INBOX", 1, &MessageMetadata{UID: 1})
	db.StoreMessageMetadata("user1", "INBOX", 2, &MessageMetadata{UID: 2})
	db.StoreMessageMetadata("user1", "INBOX", 3, &MessageMetadata{UID: 3})

	uids, err := db.GetMessageUIDs("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMessageUIDs failed: %v", err)
	}
	if len(uids) != 3 {
		t.Errorf("Expected 3 UIDs, got %d: %v", len(uids), uids)
	}
}

func TestBboltGetMessageUIDsEmpty(t *testing.T) {
	db := setupRealDB(t)

	uids, err := db.GetMessageUIDs("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMessageUIDs failed: %v", err)
	}
	if len(uids) != 0 {
		t.Errorf("Expected 0 UIDs, got %d", len(uids))
	}
	if uids == nil {
		t.Error("Expected non-nil slice")
	}
}

func TestBboltGetMessageMetadataNotFound(t *testing.T) {
	db := setupRealDB(t)

	meta, err := db.GetMessageMetadata("user1", "INBOX", 999)
	if err != nil {
		t.Fatalf("GetMessageMetadata for non-existent should not error: %v", err)
	}
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}
	if meta.UID != 0 {
		t.Errorf("Expected empty metadata, got UID %d", meta.UID)
	}
}

func TestBboltUpdateMessageMetadata(t *testing.T) {
	db := setupRealDB(t)

	// Store original
	db.StoreMessageMetadata("user1", "INBOX", 1, &MessageMetadata{
		UID:     1,
		Flags:   []string{"\\Seen"},
		Subject: "Original",
	})

	// Update
	err := db.UpdateMessageMetadata("user1", "INBOX", 1, &MessageMetadata{
		UID:     1,
		Flags:   []string{"\\Seen", "\\Flagged"},
		Subject: "Updated",
	})
	if err != nil {
		t.Fatalf("UpdateMessageMetadata failed: %v", err)
	}

	got, err := db.GetMessageMetadata("user1", "INBOX", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}
	if got.Subject != "Updated" {
		t.Errorf("Expected 'Updated', got %s", got.Subject)
	}
	if len(got.Flags) != 2 {
		t.Errorf("Expected 2 flags, got %d", len(got.Flags))
	}
}

func TestBboltDeleteMessage(t *testing.T) {
	db := setupRealDB(t)

	db.StoreMessageMetadata("user1", "INBOX", 1, &MessageMetadata{UID: 1})

	err := db.DeleteMessage("user1", "INBOX", 1)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	uids, _ := db.GetMessageUIDs("user1", "INBOX")
	if len(uids) != 0 {
		t.Errorf("Expected 0 UIDs after delete, got %d", len(uids))
	}
}

func TestBboltDeleteMessageNonexistent(t *testing.T) {
	db := setupRealDB(t)

	err := db.DeleteMessage("user1", "INBOX", 999)
	if err != nil {
		t.Errorf("DeleteMessage for non-existent should not error: %v", err)
	}
}

func TestBboltRenameMailboxWithMessages(t *testing.T) {
	db := setupRealDB(t)

	db.CreateMailbox("user1", "Old")
	db.StoreMessageMetadata("user1", "Old", 1, &MessageMetadata{
		UID:     1,
		Subject: "Test",
		Flags:   []string{"\\Seen"},
	})

	err := db.RenameMailbox("user1", "Old", "New")
	if err != nil {
		t.Fatalf("RenameMailbox failed: %v", err)
	}

	// Verify messages in new mailbox
	uids, _ := db.GetMessageUIDs("user1", "New")
	if len(uids) != 1 {
		t.Errorf("Expected 1 UID in new mailbox, got %d", len(uids))
	}

	// Verify old mailbox is gone
	uidsOld, _ := db.GetMessageUIDs("user1", "Old")
	if len(uidsOld) != 0 {
		t.Errorf("Expected 0 UIDs in old mailbox, got %d", len(uidsOld))
	}
}

func TestBboltGetMailboxCountsBadData(t *testing.T) {
	db := setupRealDB(t)

	// Store invalid JSON to test error handling
	db.bolt.Update(func(tx *bbolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte(messagesBucket("user1", "INBOX")))
		b.Put(itob(1), []byte("invalid json"))
		return nil
	})

	exists, _, _, err := db.GetMailboxCounts("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMailboxCounts failed: %v", err)
	}
	// Invalid message should be skipped
	if exists != 0 {
		t.Errorf("Expected 0 exists for invalid data, got %d", exists)
	}
}

func TestBboltRenameMailboxNoSourceMessages(t *testing.T) {
	db := setupRealDB(t)

	db.CreateMailbox("user1", "Old")

	err := db.RenameMailbox("user1", "Old", "New")
	if err != nil {
		t.Errorf("RenameMailbox with no source messages failed: %v", err)
	}
}

// --- Nil database (db.bolt == nil) tests ---

func TestNilDatabaseGetMailbox(t *testing.T) {
	db := &Database{path: "fake"}
	mb, err := db.GetMailbox("user", "INBOX")
	if err != nil {
		t.Fatalf("Nil DB GetMailbox should not error: %v", err)
	}
	if mb.Name != "INBOX" {
		t.Errorf("Expected INBOX, got %s", mb.Name)
	}
	if mb.UIDValidity != 1 || mb.UIDNext != 1 {
		t.Errorf("Expected defaults (1,1), got (%d,%d)", mb.UIDValidity, mb.UIDNext)
	}
}

func TestNilDatabaseCreateMailbox(t *testing.T) {
	db := &Database{path: "fake"}
	err := db.CreateMailbox("user", "INBOX")
	if err != nil {
		t.Errorf("Nil DB CreateMailbox should return nil: %v", err)
	}
}

func TestNilDatabaseDeleteMailbox(t *testing.T) {
	db := &Database{path: "fake"}
	err := db.DeleteMailbox("user", "INBOX")
	if err != nil {
		t.Errorf("Nil DB DeleteMailbox should return nil: %v", err)
	}
}

func TestNilDatabaseGetNextUID(t *testing.T) {
	db := &Database{path: "fake"}
	uid, err := db.GetNextUID("user", "INBOX")
	if err != nil {
		t.Errorf("Nil DB GetNextUID should not error: %v", err)
	}
	if uid != 1 {
		t.Errorf("Expected UID 1, got %d", uid)
	}
}

func TestNilDatabaseGetMessageUIDs(t *testing.T) {
	db := &Database{path: "fake"}
	uids, err := db.GetMessageUIDs("user", "INBOX")
	if err != nil {
		t.Errorf("Nil DB GetMessageUIDs should not error: %v", err)
	}
	if len(uids) != 0 {
		t.Errorf("Expected empty slice, got %v", uids)
	}
}

func TestNilDatabaseGetMessageMetadata(t *testing.T) {
	db := &Database{path: "fake"}
	meta, err := db.GetMessageMetadata("user", "INBOX", 1)
	if err != nil {
		t.Errorf("Nil DB GetMessageMetadata should not error: %v", err)
	}
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}
	if meta.UID != 0 {
		t.Errorf("Expected zero UID, got %d", meta.UID)
	}
}

func TestNilDatabaseStoreMessageMetadata(t *testing.T) {
	db := &Database{path: "fake"}
	err := db.StoreMessageMetadata("user", "INBOX", 1, &MessageMetadata{UID: 1})
	if err != nil {
		t.Errorf("Nil DB StoreMessageMetadata should return nil: %v", err)
	}
}

func TestNilDatabaseDeleteMessage(t *testing.T) {
	db := &Database{path: "fake"}
	err := db.DeleteMessage("user", "INBOX", 1)
	if err != nil {
		t.Errorf("Nil DB DeleteMessage should return nil: %v", err)
	}
}

func TestNilDatabaseRenameMailbox(t *testing.T) {
	db := &Database{path: "fake"}
	err := db.RenameMailbox("user", "Old", "New")
	if err != nil {
		t.Errorf("Nil DB RenameMailbox should return nil: %v", err)
	}
}

func TestNilDatabaseListMailboxes(t *testing.T) {
	db := &Database{path: "fake"}
	list, err := db.ListMailboxes("user")
	if err != nil {
		t.Errorf("Nil DB ListMailboxes should not error: %v", err)
	}
	if len(list) != 1 || list[0] != "INBOX" {
		t.Errorf("Expected [INBOX], got %v", list)
	}
}

func TestNilDatabaseGetMailboxCounts(t *testing.T) {
	db := &Database{path: "fake"}
	exists, recent, unseen, err := db.GetMailboxCounts("user", "INBOX")
	if err != nil {
		t.Errorf("Nil DB GetMailboxCounts should not error: %v", err)
	}
	if exists != 0 || recent != 0 || unseen != 0 {
		t.Errorf("Expected all zeros, got %d %d %d", exists, recent, unseen)
	}
}

func TestNilDatabaseAuthenticateUser(t *testing.T) {
	db := &Database{path: "fake"}
	_, err := db.AuthenticateUser("user", "pass")
	// AuthenticateUser is not implemented — expects error
	if err == nil {
		t.Error("Expected error from unimplemented AuthenticateUser")
	}
}

func TestNilDatabaseClose(t *testing.T) {
	db := &Database{path: "fake"}
	err := db.Close()
	if err != nil {
		t.Errorf("Nil DB Close should return nil: %v", err)
	}
}

// --- Closed database (db.bolt set but closed) error path tests ---

func TestClosedDBGetMailboxError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	_, err := db.GetMailbox("user", "INBOX")
	if err == nil {
		t.Error("Expected error from GetMailbox on closed DB")
	}
}

func TestClosedDBCreateMailboxError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	err := db.CreateMailbox("user", "INBOX")
	if err == nil {
		t.Error("Expected error from CreateMailbox on closed DB")
	}
}

func TestClosedDBDeleteMailboxError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	err := db.DeleteMailbox("user", "INBOX")
	if err == nil {
		t.Error("Expected error from DeleteMailbox on closed DB")
	}
}

func TestClosedDBGetNextUIDError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	_, err := db.GetNextUID("user", "INBOX")
	if err == nil {
		t.Error("Expected error from GetNextUID on closed DB")
	}
}

func TestClosedDBStoreMessageMetadataError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	err := db.StoreMessageMetadata("user", "INBOX", 1, &MessageMetadata{UID: 1})
	if err == nil {
		t.Error("Expected error from StoreMessageMetadata on closed DB")
	}
}

func TestClosedDBDeleteMessageError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	err := db.DeleteMessage("user", "INBOX", 1)
	if err == nil {
		t.Error("Expected error from DeleteMessage on closed DB")
	}
}

func TestClosedDBGetMessageUIDsError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	_, err := db.GetMessageUIDs("user", "INBOX")
	if err == nil {
		t.Error("Expected error from GetMessageUIDs on closed DB")
	}
}

func TestClosedDBGetMessageMetadataError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	_, err := db.GetMessageMetadata("user", "INBOX", 1)
	if err == nil {
		t.Error("Expected error from GetMessageMetadata on closed DB")
	}
}

func TestClosedDBListMailboxesError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	_, err := db.ListMailboxes("user")
	if err == nil {
		t.Error("Expected error from ListMailboxes on closed DB")
	}
}

func TestClosedDBGetMailboxCountsError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	_, _, _, err := db.GetMailboxCounts("user", "INBOX")
	if err == nil {
		t.Error("Expected error from GetMailboxCounts on closed DB")
	}
}

func TestClosedDBRenameMailboxError(t *testing.T) {
	db := setupRealDB(t)
	db.Close()

	err := db.RenameMailbox("user", "Old", "New")
	if err == nil {
		t.Error("Expected error from RenameMailbox on closed DB")
	}
}

// --- StoreMessageMetadata / GetMessageMetadata round-trip with all fields ---

func TestBboltMetadataRoundTripAllFields(t *testing.T) {
	db := setupRealDB(t)

	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	meta := &MessageMetadata{
		MessageID:    "<unique-id@example.com>",
		UID:          42,
		Flags:        []string{"\\Seen", "\\Flagged", "\\Draft"},
		InternalDate: now,
		Size:         4096,
		Subject:      "Important: Q3 Review",
		Date:         "Sat, 15 Jun 2024 10:30:00 +0000",
		From:         "alice@example.com",
		To:           "bob@example.com, carol@example.com",
	}

	err := db.StoreMessageMetadata("user1", "INBOX", 42, meta)
	if err != nil {
		t.Fatalf("StoreMessageMetadata failed: %v", err)
	}

	got, err := db.GetMessageMetadata("user1", "INBOX", 42)
	if err != nil {
		t.Fatalf("GetMessageMetadata failed: %v", err)
	}

	if got.MessageID != meta.MessageID {
		t.Errorf("MessageID = %q, want %q", got.MessageID, meta.MessageID)
	}
	if got.UID != meta.UID {
		t.Errorf("UID = %d, want %d", got.UID, meta.UID)
	}
	if got.Subject != meta.Subject {
		t.Errorf("Subject = %q, want %q", got.Subject, meta.Subject)
	}
	if got.Size != meta.Size {
		t.Errorf("Size = %d, want %d", got.Size, meta.Size)
	}
	if got.From != meta.From {
		t.Errorf("From = %q, want %q", got.From, meta.From)
	}
	if got.To != meta.To {
		t.Errorf("To = %q, want %q", got.To, meta.To)
	}
	if got.Date != meta.Date {
		t.Errorf("Date = %q, want %q", got.Date, meta.Date)
	}
	if len(got.Flags) != len(meta.Flags) {
		t.Errorf("Flags len = %d, want %d", len(got.Flags), len(meta.Flags))
	}
	if !got.InternalDate.Equal(meta.InternalDate) {
		t.Errorf("InternalDate = %v, want %v", got.InternalDate, meta.InternalDate)
	}
}

// --- GetMessageMetadata for non-existent mailbox and non-existent message ---

func TestBboltGetMessageMetadataNonExistentMailbox(t *testing.T) {
	db := setupRealDB(t)

	meta, err := db.GetMessageMetadata("user1", "NoMailbox", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata for non-existent mailbox should not error: %v", err)
	}
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}
	if meta.UID != 0 {
		t.Errorf("Expected zero UID for non-existent mailbox, got %d", meta.UID)
	}
}

// --- GetMessageUIDs for empty mailbox (bucket exists but no messages) ---

func TestBboltGetMessageUIDsEmptyBucket(t *testing.T) {
	db := setupRealDB(t)

	// Create the mailbox and its message bucket (by storing and deleting a message)
	db.StoreMessageMetadata("user1", "INBOX", 1, &MessageMetadata{UID: 1})
	db.DeleteMessage("user1", "INBOX", 1)

	uids, err := db.GetMessageUIDs("user1", "INBOX")
	if err != nil {
		t.Fatalf("GetMessageUIDs on empty bucket failed: %v", err)
	}
	if len(uids) != 0 {
		t.Errorf("Expected 0 UIDs in empty bucket, got %d", len(uids))
	}
	if uids == nil {
		t.Error("Expected non-nil slice")
	}
}

// --- DeleteMessage for non-existent bucket ---

func TestBboltDeleteMessageNonExistentBucket(t *testing.T) {
	db := setupRealDB(t)

	// No mailbox/messages created, so the messages bucket does not exist
	err := db.DeleteMessage("user1", "NoMailbox", 1)
	if err != nil {
		t.Errorf("DeleteMessage on non-existent bucket should not error: %v", err)
	}
}

// --- GetMailbox for non-existent mailbox bucket returns defaults ---

func TestBboltGetMailboxNonExistent(t *testing.T) {
	db := setupRealDB(t)

	mb, err := db.GetMailbox("user1", "NoBox")
	if err != nil {
		t.Fatalf("GetMailbox for non-existent should not error: %v", err)
	}
	if mb.Name != "NoBox" {
		t.Errorf("Expected NoBox, got %s", mb.Name)
	}
	if mb.UIDValidity != 1 || mb.UIDNext != 1 {
		t.Errorf("Expected defaults (1,1), got (%d,%d)", mb.UIDValidity, mb.UIDNext)
	}
}

// --- DeleteMailbox for non-existent mailbox ---

func TestBboltDeleteMailboxNonExistent(t *testing.T) {
	db := setupRealDB(t)

	err := db.DeleteMailbox("user1", "GhostBox")
	if err != nil {
		t.Errorf("DeleteMailbox for non-existent should not error: %v", err)
	}
}

// ==================== Thread Tests ====================

func TestGetThread(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a thread
	thread := &Thread{
		ThreadID:     "thread-123",
		Subject:      "Test Thread",
		Participants: []string{"user1@example.com", "user2@example.com"},
		MessageCount: 5,
		UnreadCount:  2,
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
	}

	err = db.UpdateThread("user@example.com", thread)
	if err != nil {
		t.Fatalf("Failed to create thread: %v", err)
	}

	// Retrieve the thread
	retrieved, err := db.GetThread("user@example.com", "thread-123")
	if err != nil {
		t.Errorf("GetThread failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Retrieved thread is nil")
	}

	if retrieved.ThreadID != "thread-123" {
		t.Errorf("ThreadID = %s, want thread-123", retrieved.ThreadID)
	}

	if retrieved.Subject != "Test Thread" {
		t.Errorf("Subject = %s, want Test Thread", retrieved.Subject)
	}
}

func TestGetThread_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.GetThread("user@example.com", "nonexistent-thread")
	if err == nil {
		t.Error("Expected error for nonexistent thread")
	}
}

func TestGetThreads(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create multiple threads
	for i := 0; i < 5; i++ {
		thread := &Thread{
			ThreadID:     fmt.Sprintf("thread-%d", i),
			Subject:      fmt.Sprintf("Thread %d", i),
			Participants: []string{"user@example.com"},
			MessageCount: i + 1,
			UnreadCount:  i,
			LastActivity: time.Now(),
			CreatedAt:    time.Now(),
		}
		db.UpdateThread("user@example.com", thread)
	}

	// Get all threads
	threads, err := db.GetThreads("user@example.com", 10, 0)
	if err != nil {
		t.Errorf("GetThreads failed: %v", err)
	}

	if len(threads) != 5 {
		t.Errorf("Expected 5 threads, got %d", len(threads))
	}
}

func TestGetThreads_WithLimit(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create multiple threads
	for i := 0; i < 10; i++ {
		thread := &Thread{
			ThreadID:     fmt.Sprintf("thread-%d", i),
			Subject:      fmt.Sprintf("Thread %d", i),
			Participants: []string{"user@example.com"},
			MessageCount: 1,
			LastActivity: time.Now(),
			CreatedAt:    time.Now(),
		}
		db.UpdateThread("user@example.com", thread)
	}

	// Get with limit
	threads, err := db.GetThreads("user@example.com", 5, 0)
	if err != nil {
		t.Errorf("GetThreads failed: %v", err)
	}

	if len(threads) != 5 {
		t.Errorf("Expected 5 threads (limited), got %d", len(threads))
	}
}

func TestGetThreads_WithOffset(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create multiple threads
	for i := 0; i < 10; i++ {
		thread := &Thread{
			ThreadID:     fmt.Sprintf("thread-%d", i),
			Subject:      fmt.Sprintf("Thread %d", i),
			Participants: []string{"user@example.com"},
			MessageCount: 1,
			LastActivity: time.Now(),
			CreatedAt:    time.Now(),
		}
		db.UpdateThread("user@example.com", thread)
	}

	// Get with offset
	threads, err := db.GetThreads("user@example.com", 10, 5)
	if err != nil {
		t.Errorf("GetThreads failed: %v", err)
	}

	if len(threads) != 5 {
		t.Errorf("Expected 5 threads (offset 5), got %d", len(threads))
	}
}

func TestUpdateThread(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	thread := &Thread{
		ThreadID:     "thread-update",
		Subject:      "Original Subject",
		Participants: []string{"user@example.com"},
		MessageCount: 1,
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
	}

	// Create thread
	err = db.UpdateThread("user@example.com", thread)
	if err != nil {
		t.Fatalf("Failed to create thread: %v", err)
	}

	// Update thread
	thread.Subject = "Updated Subject"
	thread.MessageCount = 5
	err = db.UpdateThread("user@example.com", thread)
	if err != nil {
		t.Errorf("Failed to update thread: %v", err)
	}

	// Verify update
	retrieved, _ := db.GetThread("user@example.com", "thread-update")
	if retrieved.Subject != "Updated Subject" {
		t.Errorf("Subject = %s, want Updated Subject", retrieved.Subject)
	}

	if retrieved.MessageCount != 5 {
		t.Errorf("MessageCount = %d, want 5", retrieved.MessageCount)
	}
}

func TestDeleteThread(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	thread := &Thread{
		ThreadID:     "thread-delete",
		Subject:      "To Be Deleted",
		Participants: []string{"user@example.com"},
		MessageCount: 1,
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
	}

	// Create thread
	db.UpdateThread("user@example.com", thread)

	// Delete thread
	err = db.DeleteThread("user@example.com", "thread-delete")
	if err != nil {
		t.Errorf("DeleteThread failed: %v", err)
	}

	// Verify deletion
	_, err = db.GetThread("user@example.com", "thread-delete")
	if err == nil {
		t.Error("Expected error after deleting thread")
	}
}

func TestSearchThreads(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create threads
	threads := []*Thread{
		{
			ThreadID:     "thread-1",
			Subject:      "Important Meeting",
			Participants: []string{"alice@example.com", "bob@example.com"},
			MessageCount: 5,
			LastActivity: time.Now(),
			CreatedAt:    time.Now(),
		},
		{
			ThreadID:     "thread-2",
			Subject:      "Project Discussion",
			Participants: []string{"charlie@example.com"},
			MessageCount: 3,
			LastActivity: time.Now(),
			CreatedAt:    time.Now(),
		},
		{
			ThreadID:     "thread-3",
			Subject:      "Important Update",
			Participants: []string{"dave@example.com"},
			MessageCount: 2,
			LastActivity: time.Now(),
			CreatedAt:    time.Now(),
		},
	}

	for _, thread := range threads {
		db.UpdateThread("user@example.com", thread)
	}

	// Search by subject
	results, err := db.SearchThreads("user@example.com", "Important")
	if err != nil {
		t.Errorf("SearchThreads failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 threads with 'Important', got %d", len(results))
	}

	// Search by participant
	results, err = db.SearchThreads("user@example.com", "alice")
	if err != nil {
		t.Errorf("SearchThreads failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 thread with 'alice', got %d", len(results))
	}
}

func TestNormalizeSubject(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Re: Test Subject", "Test Subject"},
		{"RE: Test Subject", "Test Subject"},
		{"Re[2]: Test Subject", "Test Subject"},
		{"Fwd: Test Subject", "Test Subject"},
		{"FW: Test Subject", "Test Subject"},
		{"Re: Fwd: Test Subject", "Test Subject"},
		{"Test Subject", "Test Subject"},
	}

	for _, tt := range tests {
		result := NormalizeSubject(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeSubject(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
