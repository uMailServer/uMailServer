package storage

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// F6660: a flag update racing an expunge must not resurrect the row.
func TestR189_UpdateFuncDoesNotResurrectDeletedMessage(t *testing.T) {
	db := setupTestDB(t)
	if err := db.CreateMailbox("u", "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata("u", "INBOX", 7, &MessageMetadata{UID: 7, MessageID: "m"}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMessage("u", "INBOX", 7); err != nil {
		t.Fatal(err)
	}
	called := false
	_ = db.UpdateMessageMetadataFunc("u", "INBOX", 7, func(m *MessageMetadata) error {
		called = true
		m.Flags = append(m.Flags, `\Seen`)
		return nil
	})
	uids, _ := db.GetMessageUIDs("u", "INBOX")
	if len(uids) != 0 {
		t.Fatalf("deleted message resurrected: uids=%v (fn called=%v)", uids, called)
	}
	// and no bucket is invented for a mailbox that does not exist
	_ = db.UpdateMessageMetadataFunc("u", "Ghost", 1, func(m *MessageMetadata) error { return nil })
	_ = db.Bolt().View(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(messagesBucket("u", "Ghost"))) != nil {
			t.Error("Update created a messages bucket for a missing mailbox")
		}
		return nil
	})
}

// F6661: UID counter must not wrap to 0/1 and re-issue UIDs.
func TestR189_GetNextUIDExhaustionDoesNotWrap(t *testing.T) {
	db := setupTestDB(t)
	_ = db.CreateMailbox("u", "INBOX")
	_ = db.Bolt().Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(mailboxKey("u", "INBOX"))).Put([]byte("uidnext"), itob(math.MaxUint32))
	})
	seen := map[uint32]bool{}
	for i := 0; i < 3; i++ {
		uid, err := db.GetNextUID("u", "INBOX")
		if err != nil {
			continue
		}
		if seen[uid] {
			t.Fatalf("duplicate UID %d issued after exhaustion", uid)
		}
		seen[uid] = true
	}
	if _, err := db.GetNextUID("u", "INBOX"); err == nil {
		t.Fatal("expected exhaustion error")
	}
}

// F6662: a mailbox bucket auto-created by GetNextUID must carry a valid UIDVALIDITY.
func TestR189_AutoCreatedMailboxHasUIDValidity(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.GetNextUID("u", "Fresh"); err != nil {
		t.Fatal(err)
	}
	mb, _ := db.GetMailbox("u", "Fresh")
	if mb.UIDValidity == 0 {
		t.Fatal("UIDVALIDITY 0 is invalid (RFC 3501)")
	}
	before := mb.UIDValidity
	if err := db.CreateMailbox("u", "Fresh"); err != nil {
		t.Fatal(err)
	}
	mb, _ = db.GetMailbox("u", "Fresh")
	if mb.UIDValidity != before {
		t.Fatalf("UIDVALIDITY changed %d -> %d on CreateMailbox", before, mb.UIDValidity)
	}
}

// F6663: separator injection in user names must not alias another user's keys.
func TestR189_UserKeySeparatorInjection(t *testing.T) {
	db := setupTestDB(t)
	// user "a" mailbox "b:c"  vs  user "a:b" mailbox "c"
	_ = db.CreateMailbox("a", "b:c")
	_ = db.CreateMailbox("a:b", "c")
	_ = db.StoreMessageMetadata("a", "b:c", 1, &MessageMetadata{UID: 1, MessageID: "blob-of-a"})
	_ = db.StoreMessageMetadata("a:b", "c", 1, &MessageMetadata{UID: 1, MessageID: "blob-of-ab"})
	if m, _ := db.GetMessageMetadata("a", "b:c", 1); m.MessageID != "blob-of-a" {
		t.Fatalf("user a's row overwritten by user a:b: %q", m.MessageID)
	}
	boxes, _ := db.ListMailboxes("a")
	if len(boxes) != 1 || boxes[0] != "b:c" {
		t.Fatalf("ListMailboxes(a) = %v", boxes)
	}
	refs, _ := db.BlobReferenced("a", []string{"blob-of-ab"})
	if refs["blob-of-ab"] {
		t.Fatal("user a counted as referencing user a:b's blob")
	}
	refs, _ = db.BlobReferenced("a", []string{"blob-of-a"})
	if !refs["blob-of-a"] {
		t.Fatal("user a lost own reference")
	}
	_ = db.SetSubscribed("a", "x", true)
	if s, _ := db.ListSubscribed("a:b"); len(s) != 0 {
		t.Fatalf("subscriptions leaked: %v", s)
	}
	// NUL must not alias either
	_ = db.CreateMailbox("n\x00c", "q")
	if boxes, _ := db.ListMailboxes("n:"); len(boxes) != 1 || boxes[0] != "INBOX" {
		t.Fatalf("NUL/colon alias: %v", boxes)
	}
}

// F6664: INBOX cannot be deleted (any case).
func TestR189_DeleteMailboxINBOXProtected(t *testing.T) {
	db := setupTestDB(t)
	_ = db.CreateMailbox("u", "INBOX")
	for _, n := range []string{"INBOX", "inbox", "InBox"} {
		if err := db.DeleteMailbox("u", n); err == nil {
			t.Fatalf("DeleteMailbox(%q) succeeded", n)
		}
	}
	if err := db.Bolt().View(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(mailboxKey("u", "INBOX"))) == nil {
			t.Error("INBOX bucket gone")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// F6665: renaming onto a name whose UIDVALIDITY was used before must not go backwards.
func TestR189_RenameUIDValidityAboveDestinationFloor(t *testing.T) {
	db := setupTestDB(t)
	_ = db.CreateMailbox("u", "Old")
	_ = db.CreateMailbox("u", "Dest")
	d, _ := db.GetMailbox("u", "Dest")
	if err := db.DeleteMailbox("u", "Dest"); err != nil {
		t.Fatal(err)
	}
	// Force Old to have a smaller UIDVALIDITY than Dest's retired one.
	_ = db.Bolt().Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(mailboxKey("u", "Old"))).Put([]byte("uidvalidity"), itob(d.UIDValidity-100))
	})
	if err := db.RenameMailbox("u", "Old", "Dest"); err != nil {
		t.Fatal(err)
	}
	mb, _ := db.GetMailbox("u", "Dest")
	if mb.UIDValidity <= d.UIDValidity {
		t.Fatalf("UIDVALIDITY regressed for reused name: %d <= %d", mb.UIDValidity, d.UIDValidity)
	}
}

// F6666: flags are deduplicated case-insensitively; blanks dropped; ModSeq never regresses.
func TestR189_FlagsNormalisedAndModSeqMonotonic(t *testing.T) {
	db := setupTestDB(t)
	_ = db.CreateMailbox("u", "INBOX")
	_ = db.StoreMessageMetadata("u", "INBOX", 1, &MessageMetadata{
		UID: 1, MessageID: "m", Flags: []string{`\Seen`, `\SEEN`, "", `\Flagged`, `\Seen`}, ModSeq: 5,
	})
	m, _ := db.GetMessageMetadata("u", "INBOX", 1)
	if len(m.Flags) != 2 || m.Flags[0] != `\Seen` || m.Flags[1] != `\Flagged` {
		t.Fatalf("flags = %q", m.Flags)
	}
	// stale replace (ModSeq 0) must not lower the stored per-message modseq
	_ = db.UpdateMessageMetadata("u", "INBOX", 1, &MessageMetadata{UID: 1, MessageID: "m", Flags: []string{`\Seen`}})
	m2, _ := db.GetMessageMetadata("u", "INBOX", 1)
	if m2.ModSeq < 5 {
		t.Fatalf("ModSeq regressed to %d", m2.ModSeq)
	}
	// and mailbox HIGHESTMODSEQ covers it
	hm, _ := db.GetHighestModSeq("u", "INBOX")
	if hm < m2.ModSeq {
		t.Fatalf("HIGHESTMODSEQ %d < message modseq %d", hm, m2.ModSeq)
	}
}

// F6667: references list is capped.
func TestR189_ReferencesCapped(t *testing.T) {
	db := setupTestDB(t)
	refs := make([]string, 5000)
	for i := range refs {
		refs[i] = "<r@x>"
	}
	_ = db.StoreMessageMetadata("u", "INBOX", 1, &MessageMetadata{UID: 1, MessageID: "m", References: refs})
	m, _ := db.GetMessageMetadata("u", "INBOX", 1)
	if len(m.References) > MaxStoredReferences {
		t.Fatalf("references not capped: %d", len(m.References))
	}
}

func TestR189_UpdateThreadNilNoPanic(t *testing.T) {
	db := setupTestDB(t)
	if err := db.UpdateThread("u", nil); err == nil {
		t.Fatal("expected error for nil thread")
	}
}

func TestR189_NormalizeSubjectBracketPrefix(t *testing.T) {
	cases := map[string]string{
		"Re[2]: Hello":                   "Hello",
		"Re[ticket 4] ok really]: topic": "Re[ticket 4] ok really]: topic",
		"RE: Fwd: ŞİRKET toplantısı":     "ŞİRKET toplantısı",
		"ſtraße":                         "ſtraße",
		"Re: Re: Re[3]: x":               "x",
	}
	for in, want := range cases {
		if got := NormalizeSubject(in); got != want {
			t.Errorf("NormalizeSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

// F6668: ReadMessage refuses oversize files.
func TestR189_ReadMessageSizeCap(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	id, err := s.StoreMessage("u", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.basePath, "u", id[:2], id[2:4], id)
	if err := os.Truncate(p, maxMessageSize+1); err != nil {
		t.Skip("cannot make sparse file")
	}
	if _, err := s.ReadMessage("u", id); err == nil {
		t.Fatal("expected size cap error")
	}
}

func TestR189_UserDirNamesUnusual(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	for _, u := range []string{"a+tag@example.com", "üser@exämple.com", "a..b@x", "..hidden", "a b@x"} {
		id, err := s.StoreMessage(u, []byte("x-"+u))
		if u == "..hidden" || err == nil {
			if err != nil {
				t.Fatalf("%q: %v", u, err)
			}
			p := filepath.Join(s.basePath, u, id[:2], id[2:4], id)
			rel, _ := filepath.Rel(s.basePath, p)
			if filepath.IsAbs(rel) || len(rel) > 2 && rel[:2] == ".." && (len(rel) == 2 || rel[2] == '/') {
				t.Fatalf("%q escapes: %s", u, rel)
			}
		}
	}
	for _, u := range []string{"..", ".", "a/b", "a\\b", "", "a\x00b"} {
		if _, err := s.StoreMessage(u, []byte("x")); err == nil {
			t.Errorf("user %q accepted", u)
		}
	}
}

func gcSetup(t *testing.T) (*MessageStore, *Database) {
	t.Helper()
	s, err := NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db := setupTestDB(t)
	s.SetBlobReferenceChecker(db.BlobReferenced)
	return s, db
}

func ageBlob(t *testing.T, s *MessageStore, user, id string, age time.Duration) {
	t.Helper()
	p := filepath.Join(s.basePath, user, id[:2], id[2:4], id)
	old := time.Now().Add(-age)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestR189_GCOrphanBlobs(t *testing.T) {
	s, db := gcSetup(t)
	const u = "u@x"
	ref, _ := s.StoreMessage(u, []byte("referenced"))
	orphan, _ := s.StoreMessage(u, []byte("orphan"))
	pending, _ := s.StoreMessage(u, []byte("pending upload"))
	young, _ := s.StoreMessage(u, []byte("young orphan"))
	other, _ := s.StoreMessage("v@x", []byte("other user orphan"))
	_ = db.StoreMessageMetadata(u, "INBOX", 1, &MessageMetadata{UID: 1, MessageID: ref})
	_ = db.Bolt().Update(func(tx *bbolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte(pendingUploadsBucketName))
		return b.Put([]byte(u+"\x00"+pending), []byte("{}"))
	})
	for _, id := range []string{ref, orphan, pending} {
		ageBlob(t, s, u, id, 2*orphanGCGrace)
	}
	ageBlob(t, s, "v@x", other, 2*orphanGCGrace)
	// stale temp file in the orphan's directory
	tmp := filepath.Join(s.basePath, u, orphan[:2], orphan[2:4], ".tmp-dead")
	_ = os.WriteFile(tmp, []byte("x"), 0o600)
	old := time.Now().Add(-3 * time.Hour)
	_ = os.Chtimes(tmp, old, old)

	n, err := s.GCOrphanBlobs(u)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	if s.MessageExists(u, orphan) {
		t.Error("orphan survived")
	}
	for name, id := range map[string]string{"referenced": ref, "pending": pending, "young": young} {
		if !s.MessageExists(u, id) {
			t.Errorf("%s blob was removed", name)
		}
	}
	if !s.MessageExists("v@x", other) {
		t.Error("other user's blob removed")
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Error("stale temp survived")
	}
}

func TestR189_GCOrphanBlobsRequiresChecker(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	id, _ := s.StoreMessage("u", []byte("x"))
	ageBlob(t, s, "u", id, 2*orphanGCGrace)
	if _, err := s.GCOrphanBlobs("u"); err == nil {
		t.Fatal("GC without reference checker must refuse")
	}
	if !s.MessageExists("u", id) {
		t.Fatal("blob removed without a checker")
	}
	_ = errors.New
}

// Permanent regression: concurrent UID allocation is gap-free and unique.
func TestR189_ConcurrentUIDAllocationUnique(t *testing.T) {
	db := setupTestDB(t)
	_ = db.CreateMailbox("u", "INBOX")
	const n = 64
	var wg sync.WaitGroup
	start := make(chan struct{})
	got := make([]uint32, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i], _ = db.GetNextUID("u", "INBOX")
		}(i)
	}
	close(start)
	wg.Wait()
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	for i, v := range got {
		if v != uint32(i+1) {
			t.Fatalf("uids not 1..n unique: %v", got)
		}
	}
}
