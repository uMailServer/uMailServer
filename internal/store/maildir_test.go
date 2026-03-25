package store

import (
	"os"
	"path/filepath"
	"testing"
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

		if !contains(filename, ":2,S") {
			t.Errorf("expected filename to contain ':2,S', got %s", filename)
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
		maildir := store.userMaildirPath(domain, user)
		os.RemoveAll(maildir)

		// Deliver some messages
		for i := 0; i < 3; i++ {
			msg := []byte("Subject: Message " + string(rune('0'+i)) + "\r\n\r\nBody")
			_, err := store.Deliver(domain, user, folder, msg)
			if err != nil {
				t.Fatalf("Deliver failed: %v", err)
			}
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

		// Verify message is in Sent folder
		data, err := store.Fetch(domain, user, targetFolder, filename)
		if err != nil {
			t.Fatalf("Fetch from Sent failed: %v", err)
		}
		if string(data) != string(msg) {
			t.Error("moved message data doesn't match")
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
		maildir := store.userMaildirPath(domain, user)
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
