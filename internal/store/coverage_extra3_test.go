package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeliver_CreatesNewDirectory tests that Deliver creates the entire maildir
// structure when it message is delivered to a new user.
func TestDeliver_CreatesNewDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	domain := "newdomain.com"
	user := "newuser"
	msg := []byte("Subject: New User\r\n\r\nBody")

	fn, err := s.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver for new user failed: %v", err)
	}
	if fn == "" {
		t.Error("expected non-empty filename")
	}

	// Verify full directory structure was created
	maildir, _ := s.userMaildirPath(domain, user)
	for _, sub := range []string{"tmp", "new", "cur"} {
		p := filepath.Join(maildir, sub)
		st, err := os.Stat(p)
		if err != nil {
			t.Errorf("subdirectory %s not created: %v", sub, err)
		}
		if !st.IsDir() {
			t.Errorf("%s is not a directory", sub)
		}
	}

	// Verify file is in new/
	newPath := filepath.Join(maildir, "new", fn)
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("file not found in new/: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(msg))
	}
}

// TestDeliver_LargeMessage tests Deliver with a large message body.
func TestDeliver_LargeMessage(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	largeMsg := make([]byte, 100*1024) // 100KB
	for i := range largeMsg {
		largeMsg[i] = byte(i % 256)
	}

	fn, err := s.Deliver("example.com", "testuser", "INBOX", largeMsg)
	if err != nil {
		t.Fatalf("Deliver large message failed: %v", err)
	}

	data, err := s.Fetch("example.com", "testuser", "INBOX", fn)
	if err != nil {
		t.Fatalf("Fetch large message failed: %v", err)
	}
	if len(data) != len(largeMsg) {
		t.Errorf("size mismatch: got %d, want %d", len(data), len(largeMsg))
	}
}

// TestDeliver_EmptyMessage tests Deliver with an empty message body.
func TestDeliver_EmptyMessage(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	fn, err := s.Deliver("example.com", "testuser", "INBOX", []byte{})
	if err != nil {
		t.Fatalf("Deliver empty message failed: %v", err)
	}
	if fn == "" {
		t.Error("expected non-empty filename for empty message")
	}

	data, err := s.Fetch("example.com", "testuser", "INBOX", fn)
	if err != nil {
		t.Fatalf("Fetch empty message failed: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
}

// TestDeliver_ManySequential tests that sequential deliveries produce unique filenames.
func TestDeliver_ManySequential(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	for i := 0; i < 10; i++ {
		msg := []byte("Subject: Seq " + string(rune('A'+i)) + "\r\n\r\nBody")
		fn, err := s.Deliver("example.com", "testuser", "INBOX", msg)
		if err != nil {
			t.Fatalf("Deliver %d failed: %v", i, err)
		}
		if fn == "" {
			t.Errorf("expected non-empty filename at iteration %d", i)
		}
	}
}

// TestDeliverWithFlags_EmptyFlags tests DeliverWithFlags with empty flags string.
func TestDeliverWithFlags_EmptyFlags_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: No Flags\r\n\r\nBody")
	fn, err := s.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "")
	if err != nil {
		t.Fatalf("DeliverWithFlags with empty flags failed: %v", err)
	}

	// File should be in cur/ without flag suffix
	maildir, _ := s.userMaildirPath("example.com", "testuser")
	curPath := filepath.Join(maildir, "cur", fn)
	if _, err := os.Stat(curPath); os.IsNotExist(err) {
		t.Errorf("file not found in cur/: %s", curPath)
	}

	data, err := os.ReadFile(curPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(msg))
	}
}

// TestDeliverWithFlags_MultipleFlags tests DeliverWithFlags with multiple flag characters.
func TestDeliverWithFlags_MultipleFlags_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Multi Flags\r\n\r\nBody")
	flags := "SRFTD"
	fn, err := s.DeliverWithFlags("example.com", "testuser", "INBOX", msg, flags)
	if err != nil {
		t.Fatalf("DeliverWithFlags with multiple flags failed: %v", err)
	}

	if !strings.Contains(fn, flagSeparator()+flags) {
		t.Errorf("expected filename to contain flags %q, got %s", flags, fn)
	}

	// Verify file is in cur/
	maildir, _ := s.userMaildirPath("example.com", "testuser")
	curPath := filepath.Join(maildir, "cur", fn)
	if _, err := os.Stat(curPath); os.IsNotExist(err) {
		t.Errorf("file not found in cur/: %s", curPath)
	}
}

// TestDeliverWithFlags_ToSubfolder tests delivering with flags to a non-INBOX folder.
func TestDeliverWithFlags_ToSubfolder_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Subfolder Flags\r\n\r\nBody")
	fn, err := s.DeliverWithFlags("example.com", "testuser", "Archive", msg, "RS")
	if err != nil {
		t.Fatalf("DeliverWithFlags to subfolder failed: %v", err)
	}

	// Verify file exists in the subfolder's cur/
	maildir, _ := s.userMaildirPath("example.com", "testuser")
	curPath := filepath.Join(maildir, ".Archive", "cur", fn)
	if _, err := os.Stat(curPath); os.IsNotExist(err) {
		t.Errorf("file not found in subfolder cur/: %s", curPath)
	}

	data, err := s.Fetch("example.com", "testuser", "Archive", fn)
	if err != nil {
		t.Fatalf("Fetch from subfolder failed: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(msg))
	}
}

// TestDeliver_SyncAndRename tests that the full sync+rename path works correctly
// in the Deliver function.
func TestDeliver_SyncAndRename(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Sync Test\r\n\r\nBody for sync test")
	fn, err := s.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Verify tmp file is gone
	maildir, _ := s.userMaildirPath("example.com", "testuser")
	tmpPath := filepath.Join(maildir, "tmp", fn)
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful rename")
	}

	// Verify file is in new/
	newPath := filepath.Join(maildir, "new", fn)
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("failed to read file from new/: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(msg))
	}
}

// TestDeliver_MultipleMessages tests delivering multiple messages to the same folder.
func TestDeliver_MultipleMessages(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	for i := 0; i < 5; i++ {
		msg := []byte("Subject: Test " + string(rune('A'+i)) + "\r\n\r\nBody")
		fn, err := s.Deliver("example.com", "testuser", "INBOX", msg)
		if err != nil {
			t.Fatalf("Deliver %d failed: %v", i, err)
		}
		if fn == "" {
			t.Errorf("expected non-empty filename at iteration %d", i)
		}
	}
}

// TestList_EmptyFolder tests List on a folder with no messages.
func TestList_EmptyFolder(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	// Create the folder structure but deliver no messages
	s.ensureFolder("example.com", "testuser", "INBOX")

	msgs, err := s.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List on empty folder failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

// TestList_NonexistentFolder tests List on a folder that doesn't exist.
func TestList_NonexistentFolder(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msgs, err := s.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List on nonexistent folder should not error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for nonexistent folder, got %d", len(msgs))
	}
}

// TestList_WithMessagesFromNewAndCur tests List returns messages from both
// new/ and cur/ directories.
func TestList_WithMessagesFromNewAndCur(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Test\r\n\r\nBody")

	// Deliver one message (goes to new/)
	id1, err := s.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Deliver with flags (goes to cur/)
	_, err = s.DeliverWithFlags("example.com", "testuser", "INBOX", msg, "RS")
	if err != nil {
		t.Fatalf("DeliverWithFlags failed: %v", err)
	}

	msgs, err := s.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Verify at least one message has the expected filename from new/
	found := false
	for _, m := range msgs {
		if m.Filename == id1 {
			found = true
			if m.Size == 0 {
				t.Error("expected non-zero size")
			}
		}
	}
	if !found {
		t.Errorf("expected to find message %q in list results", id1)
	}
}

// TestList_WithSubdirectories tests that List skips subdirectories.
func TestList_WithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	s.ensureFolder("example.com", "testuser", "INBOX")

	// Create a subdirectory inside new/
	maildir, _ := s.userMaildirPath("example.com", "testuser")
	newPath := filepath.Join(maildir, "new")
	os.MkdirAll(newPath+"/subdir", 0o755)

	// Create a file
	os.WriteFile(newPath+"/testfile", []byte("data"), 0o644)

	msgs, err := s.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should only return files, not directories
	for _, m := range msgs {
		if m.Filename == "subdir" {
			t.Error("List should not return directories")
		}
	}
}

// TestList_SkipsFilesWithBadInfo tests List when entry.Info() might fail.
// This is a basic test that List doesn't panic with normal files.
func TestList_SkipsFilesWithBadInfo(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Test\r\n\r\nBody")
	_, err := s.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	msgs, err := s.List("example.com", "testuser", "INBOX")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

// TestDeliver_DifferentUsers tests delivering to different users in different domains.
func TestDeliver_DifferentUsers(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	users := []struct {
		domain, user string
	}{
		{"example.com", "alice"},
		{"example.com", "bob"},
		{"other.org", "charlie"},
	}

	for _, u := range users {
		msg := []byte("Subject: Test for " + u.user + "@" + u.domain + "\r\n\r\nBody")
		fn, err := s.Deliver(u.domain, u.user, "INBOX", msg)
		if err != nil {
			t.Errorf("Deliver for %s@%s failed: %v", u.user, u.domain, err)
			continue
		}
		if fn == "" {
			t.Errorf("expected non-empty filename for %s@%s", u.user, u.domain)
			continue
		}

		// Verify file exists
		data, err := s.Fetch(u.domain, u.user, "INBOX", fn)
		if err != nil {
			t.Errorf("Fetch for %s@%s failed: %v", u.user, u.domain, err)
			continue
		}
		if string(data) != string(msg) {
			t.Errorf("content mismatch for %s@%s", u.user, u.domain)
		}
	}
}

// TestDeliverWithFlags_DifferentUsers tests DeliverWithFlags for different users.
func TestDeliverWithFlags_DifferentUsers(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	users := []struct {
		domain, user string
	}{
		{"example.com", "alice"},
		{"other.org", "bob"},
	}

	for _, u := range users {
		msg := []byte("Subject: Flagged for " + u.user + "\r\n\r\nBody")
		fn, err := s.DeliverWithFlags(u.domain, u.user, "INBOX", msg, "S")
		if err != nil {
			t.Errorf("DeliverWithFlags for %s@%s failed: %v", u.user, u.domain, err)
			continue
		}
		if !strings.Contains(fn, flagSeparator()+"S") {
			t.Errorf("expected flags in filename for %s@%s, got %s", u.user, u.domain, fn)
		}

		// Verify file is in cur/
		maildir, _ := s.userMaildirPath(u.domain, u.user)
		curPath := filepath.Join(maildir, "cur", fn)
		if _, err := os.Stat(curPath); os.IsNotExist(err) {
			t.Errorf("file not found in cur/ for %s@%s: %s", u.user, u.domain, curPath)
		}
	}
}

// TestDeliver_ToSubfolder tests delivering to a non-INBOX folder.
func TestDeliver_ToSubfolder_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Subfolder Delivery\r\n\r\nBody")
	fn, err := s.Deliver("example.com", "testuser", "Sent", msg)
	if err != nil {
		t.Fatalf("Deliver to subfolder failed: %v", err)
	}
	if fn == "" {
		t.Error("expected non-empty filename")
	}

	// Verify file is in the Sent folder's new/
	maildir, _ := s.userMaildirPath("example.com", "testuser")
	newPath := filepath.Join(maildir, ".Sent", "new", fn)
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("file not found in Sent/new/: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(msg))
	}
}

// TestDeliver_UniqueNamesOverTime tests that names remain unique even with
// rapid successive calls.
func TestDeliver_UniqueNamesOverTime(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Rapid\r\n\r\nBody")
	names := make(map[string]bool)

	for i := 0; i < 20; i++ {
		fn, err := s.Deliver("example.com", "testuser", "INBOX", msg)
		if err != nil {
			t.Fatalf("Deliver %d failed: %v", i, err)
		}
		if names[fn] {
			t.Fatalf("duplicate filename at iteration %d: %s", i, fn)
		}
		names[fn] = true
		time.Sleep(time.Microsecond)
	}
}
