package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fuzzDB(f *testing.F) *Database {
	dir, err := os.MkdirTemp("", "storagefuzz")
	if err != nil {
		f.Fatal(err)
	}
	db, err := OpenDatabase(filepath.Join(dir, "t.db"))
	if err != nil {
		f.Fatal(err)
	}
	db.bolt.NoSync = true
	f.Cleanup(func() { db.Close(); os.RemoveAll(dir) })
	return db
}

// ACL entries of one mailbox must never be visible to, deletable through, or
// migrated by operations on a different mailbox whose name is a prefix
// ("Work" vs "Work:Old"; mailbox names may contain colons).
func FuzzACLMailboxIsolation(f *testing.F) {
	db := fuzzDB(f)
	n := 0
	f.Add("Work", "Work:Old", "bob@example.com")
	f.Add("a", "a:b:c", "g")
	f.Add("", ":", "x")
	f.Add("INBOX", "INBOX/Sub", "carol@example.com")
	f.Fuzz(func(t *testing.T, mbA, mbB, grantee string) {
		if grantee == "" || strings.Contains(grantee, ":") || mbA == mbB {
			return
		}
		n++
		owner := "owner" + string(rune('a'+n%26)) + "@example.com"
		_ = db.DeleteACL(owner, mbA, "")
		_ = db.DeleteACL(owner, mbB, "")
		if err := db.SetACL(owner, mbB, grantee, ACLRead|ACLLookup, owner); err != nil {
			return
		}
		// Listing / reading mbA must not see mbB's grant.
		entries, err := db.ListACL(owner, mbA)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("ListACL(%q) leaked entries of %q: %+v", mbA, mbB, entries)
		}
		// Deleting every ACL of mbA must not touch mbB's.
		if err := db.DeleteACL(owner, mbA, ""); err != nil {
			t.Fatal(err)
		}
		if r, _ := db.GetACL(owner, mbB, grantee); r == 0 {
			t.Fatalf("DeleteACL(%q, all) removed the grant of %q", mbA, mbB)
		}
		// Renaming mbA must not migrate mbB's grant.
		if err := db.RenameMailbox(owner, mbA, mbA+"-renamed"); err != nil {
			return
		}
		if r, _ := db.GetACL(owner, mbB, grantee); r == 0 {
			t.Fatalf("RenameMailbox(%q) migrated the grant of %q", mbA, mbB)
		}
		_ = db.DeleteACL(owner, mbB, "")
	})
}

func FuzzMailboxOps(f *testing.F) {
	db := fuzzDB(f)
	f.Add("u@example.com", "INBOX", "Archive")
	f.Add("", "", "")
	f.Add("u:v", "a:b", "a")
	f.Add("u@example.com", strings.Repeat("x", 40000), "y")
	f.Fuzz(func(t *testing.T, user, a, b string) {
		start := time.Now()
		if err := db.CreateMailbox(user, a); err == nil {
			ms, _ := db.ListMailboxes(user)
			found := false
			for _, m := range ms {
				if m == a {
					found = true
				}
			}
			if !found && user != "" && !strings.Contains(user, ":") {
				t.Fatalf("created mailbox %q not listed for %q: %v", a, user, ms)
			}
			db.GetMailbox(user, a)
			db.GetNextUID(user, a)
			db.StoreMessageMetadata(user, a, 1, &MessageMetadata{UID: 1, MessageID: "m", Subject: b})
			db.GetMessageUIDs(user, a)
			db.SetSubscribed(user, a, true)
			db.GetMailboxCounts(user, a)
		}
		db.RenameMailbox(user, a, b)
		db.DeleteMailbox(user, a)
		db.DeleteMailbox(user, b)
		db.RecordChange(user, ChangeTypeEmail, ChangeKindCreated, a, b)
		db.GetChangesSince(user, ChangeTypeEmail, 0, 10)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("slow: %v", d)
		}
	})
}

func FuzzMessageStorePaths(f *testing.F) {
	root, err := os.MkdirTemp("", "msfuzz")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { os.RemoveAll(root) })
	base := filepath.Join(root, "base")
	ms, err := NewMessageStore(base)
	if err != nil {
		f.Fatal(err)
	}
	secret := []byte("TOP-SECRET")
	victimID, err := ms.StoreMessage("victim", []byte("victim mail"))
	if err != nil {
		f.Fatal(err)
	}
	secretPath := filepath.Join(base, "secret.txt")
	_ = os.WriteFile(secretPath, secret, 0o600)

	f.Add("alice", "abcdef")
	f.Add("..", "x")
	f.Add("alice", "ab/../../secret.txt")
	f.Add("alice", "se/cr/../secret.txt")
	f.Add("a\x00b", "abcd")
	f.Add("alice", "/etc/passwd")
	f.Add("../victim", victimID)
	f.Add("alice", "ab//")
	f.Add("alice", "..")
	f.Fuzz(func(t *testing.T, user, id string) {
		data, err := ms.ReadMessage(user, id)
		if err == nil && bytes.Contains(data, secret) {
			t.Fatalf("ReadMessage(%q,%q) escaped the user directory", user, id)
		}
		if err == nil && user != "victim" && bytes.Contains(data, []byte("victim mail")) {
			t.Fatalf("ReadMessage(%q,%q) read another user's mail", user, id)
		}
		ms.MessageExists(user, id)
		if user != "victim" {
			_ = ms.DeleteMessage(user, id)
		}
		if _, err := os.Stat(secretPath); err != nil {
			t.Fatalf("DeleteMessage(%q,%q) removed files outside the user dir: %v", user, id, err)
		}
		if !ms.MessageExists("victim", victimID) {
			t.Fatalf("DeleteMessage(%q,%q) removed another user's message", user, id)
		}
		if _, err := ms.StoreMessage(user, []byte(id)); err == nil {
			if got, err := ms.ReadMessage(user, sha(id)); err != nil || string(got) != id {
				t.Fatalf("store/read round trip failed for user %q: %v", user, err)
			}
			_ = ms.DeleteMessage(user, sha(id))
		}
	})
}

func FuzzACLRightsAndState(f *testing.F) {
	f.Add("lrswitek")
	f.Add("-e")
	f.Add("")
	f.Add("18446744073709551615")
	f.Add("+1")
	f.Fuzz(func(t *testing.T, s string) {
		r, neg, err := ParseACLRights(s)
		if err == nil && !neg {
			if r2, _, err2 := ParseACLRights(r.String()); err2 != nil || r2 != r {
				t.Fatalf("rights round trip %q -> %q -> %v (%v)", s, r.String(), r2, err2)
			}
		}
		ParseChangeState(s)
	})
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// F6121: mailbox "Work" shares the ACL key prefix of mailbox "Work:Old".
func TestACL_ColonPrefixedMailboxIsolation(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const owner = "o@example.com"
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.CreateMailbox(owner, "Work"))
	must(db.CreateMailbox(owner, "Work:Old"))
	must(db.SetACL(owner, "Work", "a@example.com", ACLRead, owner))
	must(db.SetACL(owner, "Work:Old", "b@example.com", ACLRead, owner))

	got, err := db.ListACL(owner, "Work")
	must(err)
	if len(got) != 1 || got[0].Grantee != "a@example.com" {
		t.Fatalf("ListACL(Work) = %+v, want only a@example.com", got)
	}
	must(db.RenameMailbox(owner, "Work", "Job"))
	if r, _ := db.GetACL(owner, "Job:Old", "b@example.com"); r != 0 {
		t.Fatal("RenameMailbox(Work) migrated the ACL of Work:Old to Job:Old")
	}
	if r, _ := db.GetACL(owner, "Job", "a@example.com"); r == 0 {
		t.Fatal("RenameMailbox lost the ACL of the renamed mailbox")
	}
	must(db.DeleteACL(owner, "Job", ""))
	if r, _ := db.GetACL(owner, "Work:Old", "b@example.com"); r == 0 {
		t.Fatal("DeleteACL(Job, all) removed the ACL of Work:Old")
	}
}
