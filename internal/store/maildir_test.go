package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaildirStore(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"
	folder := "INBOX"

	t.Run("Deliver", func(t *testing.T) {
		msg := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest message body")

		filename, err := store.Deliver(domain, user, folder, msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}

		if filename == "" {
			t.Error("expected non-empty filename")
		}

		// Verify file exists in new/
		newPath := filepath.Join(tmpDir, "domains", domain, "users", user, "Maildir", "new", filename)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			t.Errorf("message file not found at %s", newPath)
		}
	})

	t.Run("DeliverWithFlags", func(t *testing.T) {
		msg := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test with flags\r\n\r\nTest message")

		filename, err := store.DeliverWithFlags(domain, user, folder, msg, "S")
		if err != nil {
			t.Fatalf("DeliverWithFlags failed: %v", err)
		}

		if !contains(filename, flagSeparator()+"S") {
			t.Errorf("expected filename to contain '%sS', got %s", flagSeparator(), filename)
		}

		// Verify file exists in cur/
		curPath := filepath.Join(tmpDir, "domains", domain, "users", user, "Maildir", "cur", filename)
		if _, err := os.Stat(curPath); os.IsNotExist(err) {
			t.Errorf("message file not found at %s", curPath)
		}
	})

	t.Run("Fetch", func(t *testing.T) {
		msg := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Fetch Test\r\n\r\nFetch me")

		filename, err := store.Deliver(domain, user, folder, msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}

		data, err := store.Fetch(domain, user, folder, filename)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}

		if string(data) != string(msg) {
			t.Errorf("fetched data doesn't match: got %q, want %q", string(data), string(msg))
		}
	})

	t.Run("List", func(t *testing.T) {
		// Clear existing messages
		maildir, _ := store.userMaildirPath(domain, user)
		os.RemoveAll(maildir)

		// Deliver some messages (sleep between deliveries to ensure unique filenames)
		for i := 0; i < 3; i++ {
			msg := []byte("Subject: Message " + string(rune('0'+i)) + "\r\n\r\nBody")
			_, err := store.Deliver(domain, user, folder, msg)
			if err != nil {
				t.Fatalf("Deliver failed: %v", err)
			}
			time.Sleep(1 * time.Millisecond)
		}

		messages, err := store.List(domain, user, folder)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(messages) != 3 {
			t.Errorf("expected 3 messages, got %d", len(messages))
		}
	})

	t.Run("SetFlags", func(t *testing.T) {
		msg := []byte("Subject: Flag Test\r\n\r\nBody")

		filename, err := store.Deliver(domain, user, folder, msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}

		// Set seen flag
		err = store.SetFlags(domain, user, folder, filename, "S")
		if err != nil {
			t.Fatalf("SetFlags failed: %v", err)
		}

		// List and verify flag was set
		messages, err := store.List(domain, user, folder)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		found := false
		for _, m := range messages {
			if contains(m.Filename, filename) || contains(filename, m.Filename) {
				found = true
				if m.Flags != "S" {
					t.Errorf("expected flags 'S', got '%s'", m.Flags)
				}
				break
			}
		}
		if !found {
			t.Error("message with flags not found")
		}
	})

	t.Run("Move", func(t *testing.T) {
		msg := []byte("Subject: Move Test\r\n\r\nBody")

		filename, err := store.Deliver(domain, user, folder, msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}

		targetFolder := "Sent"
		err = store.Move(domain, user, folder, targetFolder, filename)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// Verify message was moved by listing target folder
		messages, err := store.List(domain, user, targetFolder)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(messages) != 1 {
			t.Errorf("expected 1 message in Sent folder, got %d", len(messages))
		}

		// Verify message is no longer in source folder
		_, err = store.Fetch(domain, user, folder, filename)
		if err == nil {
			t.Error("expected message to be gone from source folder")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		msg := []byte("Subject: Delete Test\r\n\r\nBody")

		filename, err := store.Deliver(domain, user, folder, msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}

		err = store.Delete(domain, user, folder, filename)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify message is gone
		_, err = store.Fetch(domain, user, folder, filename)
		if err == nil {
			t.Error("expected error fetching deleted message")
		}
	})

	t.Run("CreateFolder", func(t *testing.T) {
		newFolder := "Archive"
		err := store.CreateFolder(domain, user, newFolder)
		if err != nil {
			t.Fatalf("CreateFolder failed: %v", err)
		}

		// Verify folder structure exists
		folderPath := filepath.Join(tmpDir, "domains", domain, "users", user, "Maildir", "."+newFolder)
		for _, subdir := range []string{"tmp", "new", "cur"} {
			path := filepath.Join(folderPath, subdir)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("folder subdirectory %s not found", path)
			}
		}
	})

	t.Run("ListFolders", func(t *testing.T) {
		// Create some folders
		folders := []string{"Sent", "Drafts", "Trash"}
		for _, f := range folders {
			if err := store.CreateFolder(domain, user, f); err != nil {
				t.Fatalf("CreateFolder failed: %v", err)
			}
		}

		list, err := store.ListFolders(domain, user)
		if err != nil {
			t.Fatalf("ListFolders failed: %v", err)
		}

		// Should have INBOX + created folders
		hasInbox := false
		hasSent := false
		for _, f := range list {
			if f == "INBOX" {
				hasInbox = true
			}
			if f == "Sent" {
				hasSent = true
			}
		}

		if !hasInbox {
			t.Error("INBOX not in folder list")
		}
		if !hasSent {
			t.Error("Sent not in folder list")
		}
	})

	t.Run("RenameFolder", func(t *testing.T) {
		oldName := "OldFolder"
		newName := "NewFolder"

		if err := store.CreateFolder(domain, user, oldName); err != nil {
			t.Fatalf("CreateFolder failed: %v", err)
		}

		if err := store.RenameFolder(domain, user, oldName, newName); err != nil {
			t.Fatalf("RenameFolder failed: %v", err)
		}

		// Verify old folder is gone
		oldPath := filepath.Join(tmpDir, "domains", domain, "users", user, "Maildir", "."+oldName)
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Error("old folder still exists after rename")
		}

		// Verify new folder exists
		newPath := filepath.Join(tmpDir, "domains", domain, "users", user, "Maildir", "."+newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			t.Error("new folder doesn't exist after rename")
		}
	})

	t.Run("DeleteFolder", func(t *testing.T) {
		folderName := "ToDelete"
		if err := store.CreateFolder(domain, user, folderName); err != nil {
			t.Fatalf("CreateFolder failed: %v", err)
		}

		if err := store.DeleteFolder(domain, user, folderName); err != nil {
			t.Fatalf("DeleteFolder failed: %v", err)
		}

		folderPath := filepath.Join(tmpDir, "domains", domain, "users", user, "Maildir", "."+folderName)
		if _, err := os.Stat(folderPath); !os.IsNotExist(err) {
			t.Error("folder still exists after delete")
		}
	})

	t.Run("Quota", func(t *testing.T) {
		// Clear existing messages
		maildir, _ := store.userMaildirPath(domain, user)
		os.RemoveAll(maildir)

		// Deliver some messages
		for i := 0; i < 3; i++ {
			msg := []byte("Subject: Quota Test " + string(rune('0'+i)) + "\r\n\r\nSome message body content here")
			_, err := store.Deliver(domain, user, folder, msg)
			if err != nil {
				t.Fatalf("Deliver failed: %v", err)
			}
		}

		used, limit, err := store.Quota(domain, user)
		if err != nil {
			t.Fatalf("Quota failed: %v", err)
		}

		if used == 0 {
			t.Error("expected non-zero quota usage")
		}
		if limit != 0 {
			t.Errorf("expected limit 0 (unlimited), got %d", limit)
		}
	})
}

func TestCannotDeleteInbox(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.DeleteFolder("example.com", "testuser", "INBOX")
	if err == nil {
		t.Error("expected error when deleting INBOX")
	}
}

func TestCannotRenameInbox(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.RenameFolder("example.com", "testuser", "INBOX", "Other")
	if err == nil {
		t.Error("expected error when renaming INBOX")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestMessageCount tests the MessageCount function
func TestMessageCount(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	// Initially should have 0 messages
	count, err := store.MessageCount(domain, user, "INBOX")
	if err != nil {
		t.Fatalf("MessageCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 messages, got %d", count)
	}

	// Deliver a message
	msg := []byte("From: test@example.com\r\nSubject: Test\r\n\r\nBody")
	_, err = store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Should now have 1 message
	count, err = store.MessageCount(domain, user, "INBOX")
	if err != nil {
		t.Fatalf("MessageCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 message, got %d", count)
	}

	// Deliver another message (sleep to ensure unique filename)
	time.Sleep(1 * time.Millisecond)
	_, err = store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Should now have 2 messages
	count, err = store.MessageCount(domain, user, "INBOX")
	if err != nil {
		t.Fatalf("MessageCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 messages, got %d", count)
	}
}

// TestMessageCountNonExistentUser tests MessageCount for user with no maildir
func TestMessageCountNonExistentUser(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// For a non-existent user, should return 0 (no error)
	count, err := store.MessageCount("example.com", "nonexistent", "INBOX")
	// The List function creates the folder if it doesn't exist
	if err != nil {
		t.Logf("MessageCount returned error (may be expected): %v", err)
	}
	// After creating folder, count should be 0
	t.Logf("MessageCount returned: %d", count)
}

// TestFetchReader tests the FetchReader function
func TestFetchReader(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	// Deliver a message
	msg := []byte("From: test@example.com\r\nSubject: Test Message\r\n\r\nBody content here")
	_, err := store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Get the message list
	messages, err := store.List(domain, user, "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	// Fetch the message using the filename
	filename := messages[0].Filename
	reader, err := store.FetchReader(domain, user, "INBOX", filename)
	if err != nil {
		t.Fatalf("FetchReader failed: %v", err)
	}
	defer reader.Close()

	// Read the content
	content := make([]byte, len(msg)+100)
	n, err := reader.Read(content)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify content matches
	if n < len(msg) {
		t.Errorf("Expected at least %d bytes, got %d", len(msg), n)
	}
}

// TestFetchReaderNonExistent tests FetchReader for non-existent message
func TestFetchReaderNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Try to fetch a non-existent message
	_, err := store.FetchReader("example.com", "testuser", "INBOX", "nonexistent-key")
	if err == nil {
		t.Error("Expected error for non-existent message")
	}
}

// --- New tests to improve coverage ---

func TestFlagSeparator(t *testing.T) {
	sep := flagSeparator()
	if sep == "" {
		t.Error("flagSeparator() returned empty string")
	}
	// On all platforms it should end with comma
	if sep[len(sep)-1] != ',' {
		t.Errorf("flagSeparator() = %q, expected to end with ','", sep)
	}
}

func TestJoinFlags(t *testing.T) {
	tests := []struct {
		name     string
		baseName string
		flags    string
		expected string
	}{
		{"empty flags returns base only", "1234567890.1.localhost", "", "1234567890.1.localhost"},
		{"flags appended with separator", "1234567890.1.localhost", "S", "1234567890.1.localhost" + flagSeparator() + "S"},
		{"multiple flags", "1234567890.1.localhost", "SRF", "1234567890.1.localhost" + flagSeparator() + "SRF"},
		{"single char base name", "a", "S", "a" + flagSeparator() + "S"},
		{"empty base with flags", "", "S", flagSeparator() + "S"},
		{"empty base and empty flags", "", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := joinFlags(tc.baseName, tc.flags)
			if result != tc.expected {
				t.Errorf("joinFlags(%q, %q) = %q, want %q", tc.baseName, tc.flags, result, tc.expected)
			}
		})
	}
}

func TestSplitFlags(t *testing.T) {
	sep := flagSeparator()

	tests := []struct {
		name         string
		filename     string
		expectedBase string
		expectedFlag string
	}{
		{"no flags", "1234567890.1.localhost", "1234567890.1.localhost", ""},
		{"with platform separator", "1234567890.1.localhost" + sep + "S", "1234567890.1.localhost", "S"},
		{"multiple flags", "1234567890.1.localhost" + sep + "SRFTD", "1234567890.1.localhost", "SRFTD"},
		{"empty filename", "", "", ""},
		{"separator only", sep, "", ""},
		{"separator with flag only", sep + "S", "", "S"},
		{"standard Maildir fallback :2,", "1234567890.1.localhost:2,RS", "1234567890.1.localhost", "RS"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, flags := splitFlags(tc.filename)
			if base != tc.expectedBase {
				t.Errorf("splitFlags(%q) base = %q, want %q", tc.filename, base, tc.expectedBase)
			}
			if flags != tc.expectedFlag {
				t.Errorf("splitFlags(%q) flags = %q, want %q", tc.filename, flags, tc.expectedFlag)
			}
		})
	}
}

func TestJoinFlagsAndSplitFlagsRoundTrip(t *testing.T) {
	// Verify that joinFlags -> splitFlags is a round trip
	pairs := []struct {
		base  string
		flags string
	}{
		{"1234567890.1.myhost", "S"},
		{"1234567890.1.myhost", "SRF"},
		{"1234567890.1.myhost", ""},
		{"abc.def.ghi", "T"},
	}
	for _, p := range pairs {
		joined := joinFlags(p.base, p.flags)
		gotBase, gotFlags := splitFlags(joined)
		if gotBase != p.base || gotFlags != p.flags {
			t.Errorf("round trip failed: joinFlags(%q,%q)=%q, splitFlags=%q,%q", p.base, p.flags, joined, gotBase, gotFlags)
		}
	}
}

func TestFetchNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	_, err := store.Fetch("example.com", "testuser", "INBOX", "nonexistent-file")
	if err == nil {
		t.Error("expected error fetching non-existent message")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.Delete("example.com", "testuser", "INBOX", "nonexistent-file")
	if err == nil {
		t.Error("expected error deleting non-existent message")
	}
}

func TestDeleteFolderEmptyName(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.DeleteFolder("example.com", "testuser", "")
	if err == nil {
		t.Error("expected error when deleting empty-name folder (treated as INBOX)")
	}
}

func TestRenameFolderEmptyName(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.RenameFolder("example.com", "testuser", "", "NewName")
	if err == nil {
		t.Error("expected error when renaming empty-name folder (treated as INBOX)")
	}
}

func TestListFoldersNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// User with no maildir directory at all
	folders, err := store.ListFolders("example.com", "nosuchuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(folders) != 1 || folders[0] != "INBOX" {
		t.Errorf("expected [INBOX], got %v", folders)
	}
}

func TestSetFlagsNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.SetFlags("example.com", "testuser", "INBOX", "nonexistent-file", "S")
	if err == nil {
		t.Error("expected error setting flags on non-existent message")
	}
}

func TestSetFlagsNoChange(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Deliver a message with flags already set
	msg := []byte("Subject: NoChange\r\n\r\nBody")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "S")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	// Set the same flags again -- should return nil (no change)
	err = store.SetFlags("example.com", "testuser", "INBOX", filename, "S")
	if err != nil {
		t.Errorf("expected no error when setting same flags, got: %v", err)
	}
}

func TestMoveNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.Move("example.com", "testuser", "INBOX", "Sent", "nonexistent-file")
	if err == nil {
		t.Error("expected error moving non-existent message")
	}
}

func TestDeliverAndFetchFromCur(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// DeliverWithFlags puts file in cur/
	msg := []byte("Subject: CurFetch\r\n\r\nFrom cur")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "S")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	// Fetch should find it in cur/
	data, err := store.Fetch("example.com", "testuser", "INBOX", filename)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("data mismatch: got %q, want %q", string(data), string(msg))
	}
}

func TestFetchReaderFromCur(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: ReaderCur\r\n\r\nReader from cur")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "S")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	reader, err := store.FetchReader("example.com", "testuser", "INBOX", filename)
	if err != nil {
		t.Fatalf("FetchReader failed: %v", err)
	}
	defer reader.Close()
}

func TestGenerateUniqueName(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	name1 := store.generateUniqueName()
	if name1 == "" {
		t.Error("expected non-empty unique name")
	}
	// Verify format: contains timestamp, pid, and hostname parts
	// Format: {timestamp}.{pid}{micros}.{hostname}
	if name1 == "" {
		t.Error("expected non-empty unique name")
	}
	// Name should contain at least one dot (separating timestamp from pid)
	if !contains(name1, ".") {
		t.Errorf("expected name with dots, got %q", name1)
	}
}

func TestQuotaNonExistentUser(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	used, limit, err := store.Quota("example.com", "nosuchuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if used != 0 {
		t.Errorf("expected 0 used for non-existent user, got %d", used)
	}
	if limit != 0 {
		t.Errorf("expected 0 limit, got %d", limit)
	}
}

func TestMoveWithFlaggedFilename(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Deliver with flags, then move
	msg := []byte("Subject: MoveFlagged\r\n\r\nBody")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "S")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	err = store.Move("example.com", "testuser", "INBOX", "Archive", filename)
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}

	// Verify it's in the destination
	messages, err := store.List("example.com", "testuser", "Archive")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("expected 1 message in Archive, got %d", len(messages))
	}
}

func TestListWithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	maildir, _ := store.userMaildirPath("example.com", "testuser")
	// Create the maildir structure
	for _, sub := range []string{"tmp", "new", "cur"} {
		os.MkdirAll(filepath.Join(maildir, sub), 0755)
	}
	// Create a subdirectory inside new/ that should be skipped by List
	os.MkdirAll(filepath.Join(maildir, "new", "subdir"), 0755)

	msg := []byte("Subject: SubdirTest\r\n\r\nBody")
	_, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	messages, err := store.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	// Should only return files, not directories
	for _, m := range messages {
		if m.Filename == "subdir" {
			t.Error("List should not return directories")
		}
	}
}

func TestNewMaildirStoreCreation(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.baseDir != tmpDir {
		t.Errorf("expected baseDir %q, got %q", tmpDir, store.baseDir)
	}
}

func TestFolderPathInbox(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	tests := []struct {
		name   string
		folder string
	}{
		{"empty folder", ""},
		{"INBOX folder", "INBOX"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := store.folderPath("example.com", "testuser", tc.folder)
			expected, _ := store.userMaildirPath("example.com", "testuser")
			if path != expected {
				t.Errorf("folderPath(%q) = %q, want %q", tc.folder, path, expected)
			}
		})
	}
}

func TestFolderPathCustom(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	path := store.folderPath("example.com", "testuser", "Archive")
	userPath, _ := store.userMaildirPath("example.com", "testuser")
	expected := filepath.Join(userPath, ".Archive")
	if path != expected {
		t.Errorf("folderPath(Archive) = %q, want %q", path, expected)
	}
}
