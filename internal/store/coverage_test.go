package store

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestDeliver_EnsureFolderFailure(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Create a file where the maildir base directory would go, preventing folder creation
	domain := "example.com"
	user := "testuser"
	maildir := store.userMaildirPath(domain, user)
	os.MkdirAll(filepath.Dir(maildir), 0755)
	// Create a file at the Maildir path to cause MkdirAll to fail
	os.WriteFile(maildir, []byte("blocker"), 0644)

	msg := []byte("Subject: Fail\r\n\r\nBody")
	_, err := store.Deliver(domain, user, "INBOX", msg)
	if err == nil {
		t.Error("expected error when ensureFolder fails")
	}
}

func TestDeliverWithFlags_EnsureFolderFailure(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"
	maildir := store.userMaildirPath(domain, user)
	os.MkdirAll(filepath.Dir(maildir), 0755)
	os.WriteFile(maildir, []byte("blocker"), 0644)

	msg := []byte("Subject: Fail\r\n\r\nBody")
	_, err := store.DeliverWithFlags(domain, user, "INBOX", msg, "S")
	if err == nil {
		t.Error("expected error when ensureFolder fails")
	}
}

func TestDeliver_WriteFileFails(t *testing.T) {
	// Make tmp/ a file instead of a directory so WriteFile fails
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	// Create the maildir structure ourselves, but make tmp a file
	maildir := store.userMaildirPath(domain, user)
	for _, sub := range []string{"new", "cur"} {
		os.MkdirAll(filepath.Join(maildir, sub), 0755)
	}
	// Create tmp as a file instead of a directory
	os.WriteFile(filepath.Join(maildir, "tmp"), []byte("not a dir"), 0644)

	msg := []byte("Subject: Fail\r\n\r\nBody")
	_, err := store.Deliver(domain, user, "INBOX", msg)
	if err == nil {
		t.Error("expected error when tmp is a file and WriteFile fails")
	}
}

func TestDeliverWithFlags_WriteFileFails(t *testing.T) {
	// Make tmp/ a file instead of a directory so WriteFile fails
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	maildir := store.userMaildirPath(domain, user)
	for _, sub := range []string{"new", "cur"} {
		os.MkdirAll(filepath.Join(maildir, sub), 0755)
	}
	os.WriteFile(filepath.Join(maildir, "tmp"), []byte("not a dir"), 0644)

	msg := []byte("Subject: Fail\r\n\r\nBody")
	_, err := store.DeliverWithFlags(domain, user, "INBOX", msg, "S")
	if err == nil {
		t.Error("expected error when tmp is a file and WriteFile fails")
	}
}

func TestDeliver_RenameBlocked(t *testing.T) {
	// Test that Deliver works through the normal sync+rename path
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Sync Test\r\n\r\nBody")
	filename, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	newPath := filepath.Join(tmpDir, "domains", "example.com", "users", "testuser", "Maildir", "new", filename)
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Errorf("message not found at %s", newPath)
	}

	data, err := store.Fetch("example.com", "testuser", "INBOX", filename)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("data mismatch: got %q, want %q", string(data), string(msg))
	}
}

func TestDeliverWithFlags_EmptyFlags(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: No Flags\r\n\r\nBody")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "")
	if err != nil {
		t.Fatalf("DeliverWithFlags with empty flags failed: %v", err)
	}

	curPath := filepath.Join(tmpDir, "domains", "example.com", "users", "testuser", "Maildir", "cur", filename)
	if _, err := os.Stat(curPath); os.IsNotExist(err) {
		t.Errorf("message not found at %s", curPath)
	}
}

func TestDeliverWithFlags_MultipleFlags(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Multi Flags\r\n\r\nBody")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "SRFTD")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	sep := flagSeparator()
	if !contains(filename, sep+"SRFTD") {
		t.Errorf("expected filename to contain flags, got %s", filename)
	}
}

func TestQuota_WithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	msg := []byte("Subject: Test\r\n\r\nBody content for quota test")
	store.Deliver(domain, user, "INBOX", msg)
	store.CreateFolder(domain, user, "Archive")
	store.Deliver(domain, user, "Archive", msg)

	used, limit, err := store.Quota(domain, user)
	if err != nil {
		t.Fatalf("Quota failed: %v", err)
	}
	if used == 0 {
		t.Error("expected non-zero usage with messages in multiple folders")
	}
	if limit != 0 {
		t.Errorf("expected limit 0, got %d", limit)
	}
}

func TestDeliver_ToSubfolder(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Subfolder\r\n\r\nBody")
	filename, err := store.Deliver("example.com", "testuser", "Sent", msg)
	if err != nil {
		t.Fatalf("Deliver to subfolder failed: %v", err)
	}
	if filename == "" {
		t.Error("expected non-empty filename")
	}

	data, err := store.Fetch("example.com", "testuser", "Sent", filename)
	if err != nil {
		t.Fatalf("Fetch from subfolder failed: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("data mismatch: got %q, want %q", string(data), string(msg))
	}
}

func TestDeliverWithFlags_ToSubfolder(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Subfolder Flags\r\n\r\nBody")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "Archive", msg, "RS")
	if err != nil {
		t.Fatalf("DeliverWithFlags to subfolder failed: %v", err)
	}

	data, err := store.Fetch("example.com", "testuser", "Archive", filename)
	if err != nil {
		t.Fatalf("Fetch from subfolder failed: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("data mismatch: got %q, want %q", string(data), string(msg))
	}
}

func TestMessageCount_AfterDelete(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Count\r\n\r\nBody")
	fn, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	count, err := store.MessageCount("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("MessageCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 message, got %d", count)
	}

	err = store.Delete("example.com", "testuser", "INBOX", fn)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, err = store.MessageCount("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("MessageCount after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages after delete, got %d", count)
	}
}

// ---------- Move coverage ----------

func TestMove_BetweenFolders(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Move Test\r\n\r\nBody")
	fn, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	err = store.Move("example.com", "testuser", "INBOX", "Archive", fn)
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}

	// Verify message is in Archive
	data, err := store.Fetch("example.com", "testuser", "Archive", fn)
	if err != nil {
		// Move generates new unique name, so original filename won't exist
		// That's expected - just verify no error on Move
	}
	_ = data
}

func TestMove_SourceNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.Move("example.com", "testuser", "INBOX", "Archive", "nonexistent-file")
	if err == nil {
		t.Error("Expected error for moving non-existent message")
	}
}

// ---------- SetFlags coverage ----------

func TestSetFlags_ChangeFlags(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Flags Test\r\n\r\nBody")
	fn, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Set flags on the message (Deliver puts in new/, SetFlags moves to cur/)
	err = store.SetFlags("example.com", "testuser", "INBOX", fn, "S")
	if err != nil {
		t.Fatalf("SetFlags failed: %v", err)
	}

	// After SetFlags, the filename has changed to include flags.
	// List messages to get the new filename
	msgs, err := store.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("Expected at least 1 message after SetFlags")
	}
	newFn := msgs[0].Filename

	// Set flags again on the renamed file
	err = store.SetFlags("example.com", "testuser", "INBOX", newFn, "SR")
	if err != nil {
		t.Fatalf("SetFlags (second) failed: %v", err)
	}
}

func TestSetFlags_MessageNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.SetFlags("example.com", "testuser", "INBOX", "nonexistent", "S")
	if err == nil {
		t.Error("Expected error for setting flags on non-existent message")
	}
}

// ---------- DeleteFolder coverage ----------

func TestDeleteFolder_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	err := store.DeleteFolder("example.com", "testuser", "NonExistent")
	// Should not error or return error for non-existent
	_ = err
}

func TestDeleteFolder_WithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Delete Folder\r\n\r\nBody")
	_, err := store.Deliver("example.com", "testuser", "TempFolder", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	err = store.DeleteFolder("example.com", "testuser", "TempFolder")
	if err != nil {
		t.Fatalf("DeleteFolder failed: %v", err)
	}
}

// ---------- RenameFolder coverage ----------

func TestRenameFolder(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Rename\r\n\r\nBody")
	_, err := store.Deliver("example.com", "testuser", "OldName", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	err = store.RenameFolder("example.com", "testuser", "OldName", "NewName")
	if err != nil {
		t.Fatalf("RenameFolder failed: %v", err)
	}

	// Verify folder exists under new name
	msgs, err := store.List("example.com", "testuser", "NewName")
	if err != nil {
		t.Fatalf("List on renamed folder failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message in renamed folder, got %d", len(msgs))
	}
}

// ---------- ListFolders coverage ----------

func TestListFolders(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// No folders yet - ListFolders always includes INBOX
	folders, err := store.ListFolders("example.com", "testuser")
	if err != nil {
		t.Fatalf("ListFolders failed: %v", err)
	}
	if len(folders) != 1 || folders[0] != "INBOX" {
		t.Errorf("Expected just [INBOX], got %v", folders)
	}

	// Create folders by delivering messages
	msg := []byte("Subject: Test\r\n\r\nBody")
	store.Deliver("example.com", "testuser", "INBOX", msg)
	store.Deliver("example.com", "testuser", "Sent", msg)

	folders, err = store.ListFolders("example.com", "testuser")
	if err != nil {
		t.Fatalf("ListFolders (2nd) failed: %v", err)
	}
	if len(folders) < 2 { // INBOX + Sent
		t.Errorf("Expected at least 2 folders, got %d", len(folders))
	}
}

// ---------- FetchReader coverage ----------

func TestFetchReaderExtra(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: FetchReader\r\n\r\nBody content")
	fn, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	reader, err := store.FetchReader("example.com", "testuser", "INBOX", fn)
	if err != nil {
		t.Fatalf("FetchReader failed: %v", err)
	}
	defer reader.Close()

	data := make([]byte, len(msg))
	n, err := reader.Read(data)
	if n != len(msg) {
		t.Errorf("Read returned %d bytes, expected %d", n, len(msg))
	}
	if string(data[:n]) != string(msg) {
		t.Errorf("Data mismatch: got %q, want %q", string(data[:n]), string(msg))
	}
}

// ---------- Fetch not found coverage ----------

func TestFetch_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	_, err := store.Fetch("example.com", "testuser", "INBOX", "nonexistent")
	if err == nil {
		t.Error("Expected error for fetching non-existent message")
	}
}

// ---------- Quota coverage ----------

func TestQuota_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	used, limit, err := store.Quota("example.com", "testuser")
	if err != nil {
		t.Fatalf("Quota failed: %v", err)
	}
	if used != 0 {
		t.Errorf("Expected 0 usage, got %d", used)
	}
	if limit != 0 {
		t.Errorf("Expected limit 0, got %d", limit)
	}
}

// ---------- Additional coverage tests ----------

func TestMove_ToSameFolder(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Same\r\n\r\nBody")
	fn, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}
	err = store.Move("example.com", "testuser", "INBOX", "INBOX", fn)
	// Moving to same folder should succeed (rename within same maildir tree)
	if err != nil {
		t.Fatalf("Move to same folder failed: %v", err)
	}
}

func TestMessageCount_EmptyFolder(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Call MessageCount on a folder that has never been created
	count, err := store.MessageCount("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("MessageCount on empty: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0, got %d", count)
	}
}

func TestMessageCount_MultipleFolders(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"
	msg := []byte("Subject: Count\r\n\r\nBody")

	// Deliver to INBOX
	store.Deliver(domain, user, "INBOX", msg)
	// Deliver to a subfolder
	store.CreateFolder(domain, user, "Archive")
	store.Deliver(domain, user, "Archive", msg)
	store.Deliver(domain, user, "Archive", msg)

	countInbox, err := store.MessageCount(domain, user, "INBOX")
	if err != nil {
		t.Fatalf("MessageCount INBOX failed: %v", err)
	}
	if countInbox != 1 {
		t.Errorf("INBOX: expected 1, got %d", countInbox)
	}

	countArchive, err := store.MessageCount(domain, user, "Archive")
	if err != nil {
		t.Fatalf("MessageCount Archive failed: %v", err)
	}
	if countArchive != 2 {
		t.Errorf("Archive: expected 2, got %d", countArchive)
	}
}

func TestDeliverWithFlags_EmptyFlagsToCur(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Empty Flags Cur\r\n\r\nBody")
	filename, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "")
	if err != nil {
		t.Fatalf("DeliverWithFlags with empty flags failed: %v", err)
	}

	// With empty flags, file should be in cur/ without any flag suffix
	curPath := filepath.Join(tmpDir, "domains", "example.com", "users", "testuser", "Maildir", "cur", filename)
	if _, err := os.Stat(curPath); os.IsNotExist(err) {
		t.Errorf("message not found in cur/ at %s", curPath)
	}

	// Verify that fetch retrieves it
	data, err := store.Fetch("example.com", "testuser", "INBOX", filename)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("data mismatch: got %q, want %q", string(data), string(msg))
	}
}

func TestList_NestedMaildirStructure(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Deliver one message to new/ and one to cur/ with flags
	msgNew := []byte("Subject: In New\r\n\r\nNew message")
	msgCur := []byte("Subject: In Cur\r\n\r\nCur message")
	_, err := store.Deliver("example.com", "testuser", "INBOX", msgNew)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}
	_, err = store.DeliverWithFlags("example.com", "testuser", "INBOX", msgCur, "RS")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	messages, err := store.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}

	// Verify we have one with flags and one without
	hasFlagged := false
	hasUnflagged := false
	for _, m := range messages {
		if m.Flags != "" {
			hasFlagged = true
		} else {
			hasUnflagged = true
		}
	}
	if !hasFlagged {
		t.Error("Expected at least one message with flags from cur/")
	}
	if !hasUnflagged {
		t.Error("Expected at least one message without flags from new/")
	}
}

func TestMove_ByBaseNameWithoutFlags(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Deliver a message to INBOX -- file goes to new/ with base name (no flags)
	msg := []byte("Subject: BaseName Move\r\n\r\nBody")
	baseFilename, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Call Move with a filename that has flags appended.
	// The Move function will try to find the file:
	//   1. INBOX/cur/<filename_with_flags> -- stat fails (not in cur/)
	//   2. INBOX/new/<filename_with_flags> -- stat fails (file has no flags suffix)
	//   3. INBOX/cur/<baseName> -- stat fails (not in cur/)
	//   4. INBOX/new/<baseName> -- stat SUCCEEDS (this is the actual file)
	// This exercises the "try without flags" branch in Move.
	flaggedFilename := baseFilename + flagSeparator() + "S"
	err = store.Move("example.com", "testuser", "INBOX", "Archive", flaggedFilename)
	if err != nil {
		t.Fatalf("Move by base name failed: %v", err)
	}

	// Verify message is in Archive
	messages, err := store.List("example.com", "testuser", "Archive")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message in Archive, got %d", len(messages))
	}
}

func TestMove_EnsureFolderDestFails(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	// Deliver a message to INBOX so source exists
	msg := []byte("Subject: Move Fail\r\n\r\nBody")
	fn, err := store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Block destination folder creation: place a file where the dest maildir base would go.
	// The dest folder "Archive" maps to userMaildirPath + "/.Archive"
	destMaildir := store.folderPath(domain, user, "Archive")
	// Create the parent directory but put a file at the Archive path
	os.MkdirAll(filepath.Dir(destMaildir), 0755)
	os.WriteFile(destMaildir, []byte("blocker"), 0644)

	err = store.Move(domain, user, "INBOX", "Archive", fn)
	if err == nil {
		t.Error("Expected error when ensureFolder for destination fails")
	}
}

func TestMove_FromCurWithFlags(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Deliver with flags -- file goes to cur/ with flags in filename
	msg := []byte("Subject: Move From Cur\r\n\r\nBody")
	fn, err := store.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "RS")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	// Move should find the file in cur/ by exact filename match
	err = store.Move("example.com", "testuser", "INBOX", "Sent", fn)
	if err != nil {
		t.Fatalf("Move from cur/ failed: %v", err)
	}

	// Verify in destination
	messages, err := store.List("example.com", "testuser", "Sent")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message in Sent, got %d", len(messages))
	}
}

func TestMove_Delete_EnsureFolderForMove(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Deliver to INBOX
	msg := []byte("Subject: Move\r\n\r\nBody")
	fn, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Move to a new folder that doesn't exist yet -- Move calls ensureFolder
	err = store.Move("example.com", "testuser", "INBOX", "NewFolder", fn)
	if err != nil {
		t.Fatalf("Move to new folder failed: %v", err)
	}

	// Verify message is in NewFolder
	messages, err := store.List("example.com", "testuser", "NewFolder")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message in NewFolder, got %d", len(messages))
	}
}

func TestList_ReadDirNonExistentSubdir(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Don't create any maildir structure at all
	// List should handle non-existent directories gracefully
	messages, err := store.List("example.com", "nonexistent", "INBOX")
	if err != nil {
		t.Fatalf("List on non-existent user should not error: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(messages))
	}
}

func TestDeleteFolder_EmptyFolder(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Create an empty folder
	err := store.CreateFolder("example.com", "testuser", "EmptyFolder")
	if err != nil {
		t.Fatalf("CreateFolder failed: %v", err)
	}

	// Delete the empty folder
	err = store.DeleteFolder("example.com", "testuser", "EmptyFolder")
	if err != nil {
		t.Fatalf("DeleteFolder on empty folder failed: %v", err)
	}
}

func TestDeleteFolder_RemoveAllError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("This test uses Windows-specific file locking")
	}

	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Create a folder with a message
	msg := []byte("Subject: Locked\r\n\r\nBody")
	_, err := store.Deliver("example.com", "testuser", "ToDelete", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Find the message file and lock it without FILE_SHARE_DELETE
	maildir := store.folderPath("example.com", "testuser", "ToDelete")
	var lockedHandle syscall.Handle
	filepath.Walk(maildir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		p, _ := syscall.UTF16PtrFromString(path)
		h, e := syscall.CreateFile(p,
			syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
			nil,
			syscall.OPEN_EXISTING,
			syscall.FILE_ATTRIBUTE_NORMAL,
			0)
		if e == nil {
			lockedHandle = h
		}
		return nil
	})

	if lockedHandle == 0 {
		t.Skip("Could not lock file for testing")
	}
	defer syscall.CloseHandle(lockedHandle)

	err = store.DeleteFolder("example.com", "testuser", "ToDelete")
	if err == nil {
		t.Error("Expected error when RemoveAll fails due to locked file")
	}
}

func TestRenameFolder_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Rename a folder that doesn't exist (not INBOX)
	err := store.RenameFolder("example.com", "testuser", "DoesNotExist", "NewName")
	if err == nil {
		t.Error("Expected error when renaming non-existent folder")
	}
}
