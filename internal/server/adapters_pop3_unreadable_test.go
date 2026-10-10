package server

// Regression tests for F5421: GetMessageData returned the nil Data that
// BboltMailstore leaves when a blob cannot be read, so RETR answered
// "+OK 0 octets" and a following DELE dropped a message the client never got.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pop3UnreadableBlobs returns the message blob files under the MessageStore root.
func pop3UnreadableBlobs(t *testing.T, e *pop3IXEnv) []string {
	t.Helper()
	root := filepath.Join(e.srv.config.Server.DataDir, "mail", "messages")
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("INVALID: walk: %v", err)
	}
	return files
}

func pop3MakeUnreadable(t *testing.T, e *pop3IXEnv, uid uint32) string {
	t.Helper()
	meta, err := e.srv.storageDB.GetMessageMetadata(e.user, "INBOX", uid)
	if err != nil || meta.MessageID == "" {
		t.Fatalf("INVALID: metadata uid %d: %v", uid, err)
	}
	for _, f := range pop3UnreadableBlobs(t, e) {
		if filepath.Base(f) == meta.MessageID {
			if err := os.Chmod(f, 0); err != nil {
				t.Fatalf("INVALID: chmod: %v", err)
			}
			if _, err := os.ReadFile(f); err == nil {
				t.Skip("INVALID: blob still readable after chmod 0 (running as root?)")
			}
			return f
		}
	}
	t.Fatalf("INVALID: blob for uid %d not found", uid)
	return ""
}

// Defect: an unreadable blob must make RETR fail, not succeed with 0 octets.
func TestPOP3AdapterRetrUnreadableBlob(t *testing.T) {
	e := pop3IXSetup(t, "one", "two")
	pop3MakeUnreadable(t, e, 1)
	tr := e.session("RETR 1", "QUIT")
	t.Logf("ACTUAL: transcript=%q", tr)
	if !strings.Contains(tr, "-ERR") {
		t.Fatalf("DEFECT F5421: RETR of an unreadable message reported success")
	}
}

// After a failed RETR, DELE+QUIT of the other message works and the
// unreadable message stays in the maildrop once readable again.
func TestPOP3AdapterUnreadableNotLostAndRecovers(t *testing.T) {
	e := pop3IXSetup(t, "one", "two")
	f := pop3MakeUnreadable(t, e, 1)
	tr := e.session("RETR 1", "TOP 1 0", "RETR 2", "QUIT")
	if strings.Count(tr, "-ERR") != 2 || !strings.Contains(tr, "body of two") {
		t.Fatalf("DEFECT F5421: RETR/TOP 1 must fail, RETR 2 succeed:\n%s", tr)
	}
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatalf("INVALID: chmod: %v", err)
	}
	tr = e.session("STAT", "RETR 1", "QUIT")
	if !strings.Contains(tr, "+OK 2 ") || !strings.Contains(tr, "body of one") {
		t.Fatalf("DEFECT F5421: message one not retrievable after recovery:\n%s", tr)
	}
}

// A genuinely empty stored blob is still served (non-nil empty data).
func TestPOP3AdapterUnreadableEmptyBlobServed(t *testing.T) {
	e := pop3IXSetup(t, "one")
	meta, err := e.srv.storageDB.GetMessageMetadata(e.user, "INBOX", 1)
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	for _, f := range pop3UnreadableBlobs(t, e) {
		if strings.HasSuffix(f, meta.MessageID) {
			if err := os.Truncate(f, 0); err != nil {
				t.Fatalf("INVALID: %v", err)
			}
		}
	}
	tr := e.session("RETR 1", "QUIT")
	if !strings.Contains(tr, "+OK 0 octets") {
		t.Fatalf("DEFECT F5421: empty stored message rejected:\n%s", tr)
	}
}

// Missing blob (deleted from disk): RETR fails.
func TestPOP3AdapterUnreadableMissingBlob(t *testing.T) {
	e := pop3IXSetup(t, "one")
	for _, f := range pop3UnreadableBlobs(t, e) {
		if err := os.Remove(f); err != nil {
			t.Fatalf("INVALID: %v", err)
		}
	}
	tr := e.session("RETR 1", "QUIT")
	if !strings.Contains(tr, "-ERR") {
		t.Fatalf("DEFECT F5421: RETR of a missing blob reported success:\n%s", tr)
	}
}
