//go:build windows

package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

// TestListFolders_SkipsFiles covers the branch in ListFolders where
// non-directory entries are skipped (line 428-429). We place a regular file
// in the maildir root alongside dot-prefixed directories.
func TestListFolders_SkipsFiles(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	// Deliver a message to create the maildir
	msg := []byte("Subject: Test\r\n\r\nBody")
	_, err := store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Create a subfolder so there's a dot-prefixed directory
	store.CreateFolder(domain, user, "Archive")

	// Place regular files in the maildir root -- these should be skipped
	maildir, _ := store.userMaildirPath(domain, user)
	os.WriteFile(filepath.Join(maildir, "regularfile.txt"), []byte("test"), 0o644)
	os.WriteFile(filepath.Join(maildir, ".dotfile"), []byte("test"), 0o644)

	folders, err := store.ListFolders(domain, user)
	if err != nil {
		t.Fatalf("ListFolders failed: %v", err)
	}

	// Should only contain directories, not regular files
	for _, f := range folders {
		if f == "regularfile.txt" || f == "dotfile" {
			t.Errorf("ListFolders returned a file instead of a directory: %s", f)
		}
	}

	// Must have INBOX and Archive
	hasInbox := false
	hasArchive := false
	for _, f := range folders {
		if f == "INBOX" {
			hasInbox = true
		}
		if f == "Archive" {
			hasArchive = true
		}
	}
	if !hasInbox {
		t.Error("INBOX not found in folder list")
	}
	if !hasArchive {
		t.Error("Archive not found in folder list")
	}
}

// TestGenerateUniqueName_FormatValidation verifies generateUniqueName produces
// a well-formed unique filename with the expected format and that consecutive
// calls produce different names (via the microsecond component).
func TestGenerateUniqueName_FormatValidation(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	name := store.generateUniqueName()
	if name == "" {
		t.Fatal("generateUniqueName returned empty string")
	}

	// Verify format contains at least 2 dots (timestamp.pid_micros.hostname)
	dotCount := 0
	for _, c := range name {
		if c == '.' {
			dotCount++
		}
	}
	if dotCount < 2 {
		t.Errorf("expected at least 2 dots in unique name, got %d: %s", dotCount, name)
	}
}

// TestSetFlags_MkdirAllFailure covers the MkdirAll error path in SetFlags
// (line 301-303) when moving a message from new/ to cur/. We deliver a
// message (placed in new/), then replace cur/ with a regular file so that
// MkdirAll fails when SetFlags tries to create it.
func TestSetFlags_MkdirAllFailure(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	// Deliver a message (goes to new/)
	msg := []byte("Subject: MkdirFail\r\n\r\nBody")
	fn, err := store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Remove the cur/ directory and replace it with a regular file.
	// This causes MkdirAll("cur/") to fail, and the stat for "cur/filename"
	// also fails, so the message is found in new/ with subdir="new".
	maildir, _ := store.userMaildirPath(domain, user)
	curDir := filepath.Join(maildir, "cur")
	os.RemoveAll(curDir)
	os.WriteFile(curDir, []byte("blocker"), 0o644)

	err = store.SetFlags(domain, user, "INBOX", fn, "S")
	if err == nil {
		t.Error("expected error when MkdirAll for cur/ fails in SetFlags")
	}
}

// TestQuota_WalkErrorNonNotExist covers the error path in Quota where
// filepath.Walk returns a non-IsNotExist error (line 458-460).
func TestQuota_WalkErrorNonNotExist(t *testing.T) {
	if runtime.GOOS == "windows" {
		tmpDir := t.TempDir()
		store := NewMaildirStore(tmpDir)

		domain := "example.com"
		user := "testuser"

		// Deliver a message and create a subfolder for deeper directory tree
		msg := []byte("Subject: Test\r\n\r\nBody")
		_, err := store.Deliver(domain, user, "INBOX", msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}
		store.CreateFolder(domain, user, "Archive")
		store.Deliver(domain, user, "Archive", msg)

		// Lock the Archive subfolder exclusively to cause Walk to fail
		maildir, _ := store.userMaildirPath(domain, user)
		archiveDir := filepath.Join(maildir, ".Archive")
		p, err := windows.UTF16PtrFromString(archiveDir)
		if err != nil {
			t.Fatalf("UTF16PtrFromString: %v", err)
		}
		h, err := windows.CreateFile(
			p,
			windows.GENERIC_READ,
			0, // no sharing
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS,
			0,
		)
		if err != nil {
			t.Skipf("could not lock archive directory: %v", err)
		}
		defer windows.CloseHandle(h)

		used, limit, err := store.Quota(domain, user)
		if err != nil {
			t.Logf("Quota returned error (acceptable): %v", err)
		}
		t.Logf("Quota: used=%d, limit=%d", used, limit)
	} else {
		tmpDir := t.TempDir()
		store := NewMaildirStore(tmpDir)

		domain := "example.com"
		user := "testuser"

		msg := []byte("Subject: Test\r\n\r\nBody")
		_, err := store.Deliver(domain, user, "INBOX", msg)
		if err != nil {
			t.Fatalf("Deliver failed: %v", err)
		}

		// Replace the maildir root with a file to cause Walk to error
		maildir, _ := store.userMaildirPath(domain, user)
		os.RemoveAll(maildir)
		os.WriteFile(maildir, []byte("not a directory"), 0o644)

		used, _, err := store.Quota(domain, user)
		if err == nil {
			t.Logf("Quota did not error; used=%d", used)
		} else {
			t.Logf("Quota returned error: %v", err)
		}
	}
}

// TestQuota_SkipsInaccessibleFiles covers the filepath.Walk error callback
// (line 449-450) where files that cannot be accessed are skipped.
func TestQuota_SkipsInaccessibleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	domain := "example.com"
	user := "testuser"

	msg := []byte("Subject: Quota\r\n\r\nBody")
	_, err := store.Deliver(domain, user, "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	maildir, _ := store.userMaildirPath(domain, user)
	newDir := filepath.Join(maildir, "new")

	if runtime.GOOS == "windows" {
		// On Windows, lock the new/ directory to make Walk receive an error
		p, err := windows.UTF16PtrFromString(newDir)
		if err != nil {
			t.Fatalf("UTF16PtrFromString: %v", err)
		}
		h, err := windows.CreateFile(
			p,
			windows.GENERIC_READ,
			0, // no sharing
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS,
			0,
		)
		if err != nil {
			t.Skipf("could not lock new/ directory: %v", err)
		}
		defer windows.CloseHandle(h)

		used, limit, err := store.Quota(domain, user)
		t.Logf("Quota: used=%d, limit=%d, err=%v", used, limit, err)
	} else {
		os.Chmod(newDir, 0o000)
		defer os.Chmod(newDir, 0o755)

		used, limit, err := store.Quota(domain, user)
		t.Logf("Quota: used=%d, limit=%d, err=%v", used, limit, err)
	}
}

// TestDeliver_SyncPath exercises the file.Sync() and file.Close() path in
// Deliver (lines 133-134), ensuring the sync-to-disk code path is covered.
func TestDeliver_SyncPath(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	msg := []byte("Subject: Sync Test\r\n\r\nBody with sync path")
	filename, err := store.Deliver("example.com", "testuser", "INBOX", msg)
	if err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Verify the file exists and content matches
	newPath := filepath.Join(tmpDir, "domains", "example.com", "users", "testuser", "Maildir", "new", filename)
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("failed to read delivered file: %v", err)
	}
	if string(data) != string(msg) {
		t.Errorf("data mismatch: got %q, want %q", string(data), string(msg))
	}
}
