package storage

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func writeRawEntry(db *Database, user string, e ChangeEntry) error {
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(changesBucket(user)))
		if err != nil {
			return err
		}
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return b.Put(seqKey(e.Seq), data)
	})
}

func openTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRecordAndReadChanges(t *testing.T) {
	db := openTestDB(t)

	if err := db.RecordChange("u@x", ChangeTypeMailbox, ChangeKindCreated, "INBOX", ""); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := db.RecordChange("u@x", ChangeTypeEmail, ChangeKindCreated, "<a@x>", "INBOX"); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}

	state, err := db.CurrentChangeState("u@x")
	if err != nil {
		t.Fatalf("CurrentChangeState: %v", err)
	}
	if state == "" || state == "0" {
		t.Fatalf("expected non-zero state, got %q", state)
	}

	entries, hasMore, lastSeq, err := db.GetChangesSince("u@x", ChangeTypeEmail, 0, 100)
	if err != nil {
		t.Fatalf("GetChangesSince: %v", err)
	}
	if hasMore {
		t.Errorf("hasMore should be false")
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 email entry, got %d", len(entries))
	}
	if entries[0].ID != "<a@x>" {
		t.Errorf("got id %q", entries[0].ID)
	}
	if lastSeq == 0 {
		t.Errorf("lastSeq should reflect entry seq")
	}
}

func TestGetChangesSinceFiltersByType(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 5; i++ {
		_ = db.RecordChange("u@x", ChangeTypeMailbox, ChangeKindCreated, "M"+strconv.Itoa(i), "")
		_ = db.RecordChange("u@x", ChangeTypeEmail, ChangeKindCreated, "E"+strconv.Itoa(i), "INBOX")
	}
	got, _, _, err := db.GetChangesSince("u@x", ChangeTypeEmail, 0, 100)
	if err != nil {
		t.Fatalf("GetChangesSince: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 email, got %d", len(got))
	}
	for _, e := range got {
		if e.Type != ChangeTypeEmail {
			t.Errorf("unexpected type %q", e.Type)
		}
	}
}

func TestGetChangesSinceHasMore(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 10; i++ {
		_ = db.RecordChange("u@x", ChangeTypeEmail, ChangeKindCreated, "E"+strconv.Itoa(i), "INBOX")
	}
	got, hasMore, lastSeq, err := db.GetChangesSince("u@x", ChangeTypeEmail, 0, 4)
	if err != nil {
		t.Fatalf("GetChangesSince: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4, got %d", len(got))
	}
	if !hasMore {
		t.Errorf("hasMore should be true with 10 entries and max=4")
	}
	if lastSeq == 0 {
		t.Errorf("lastSeq must advance from 0")
	}

	got2, hasMore2, _, _ := db.GetChangesSince("u@x", ChangeTypeEmail, lastSeq, 100)
	if len(got2) != 6 {
		t.Fatalf("want 6 remaining, got %d", len(got2))
	}
	if hasMore2 {
		t.Errorf("hasMore2 should be false")
	}
}

func TestRecordChangeNopOnEmptyUser(t *testing.T) {
	db := openTestDB(t)
	if err := db.RecordChange("", ChangeTypeMailbox, ChangeKindCreated, "X", ""); err != nil {
		t.Fatalf("RecordChange empty user: %v", err)
	}
	state, _ := db.CurrentChangeState("")
	if state != "0" {
		t.Errorf("empty-user state should be 0, got %q", state)
	}
}

func TestParseChangeState(t *testing.T) {
	cases := map[string]uint64{
		"":     0,
		"0":    0,
		"1":    1,
		"42":   42,
		"junk": 0,
	}
	for in, want := range cases {
		if got := ParseChangeState(in); got != want {
			t.Errorf("ParseChangeState(%q)=%d want %d", in, got, want)
		}
	}
}

func TestPruneChangesByAge(t *testing.T) {
	db := openTestDB(t)

	// Inject an aged entry at seq 1, then a fresh entry at seq 2.
	old := ChangeEntry{Seq: 1, Type: ChangeTypeEmail, Kind: ChangeKindCreated, ID: "old", At: time.Now().Add(-2 * ChangesRetention)}
	if err := writeRawEntry(db, "u@x", old); err != nil {
		t.Fatalf("inject old entry: %v", err)
	}
	fresh := ChangeEntry{Seq: 2, Type: ChangeTypeEmail, Kind: ChangeKindCreated, ID: "fresh", At: time.Now()}
	if err := writeRawEntry(db, "u@x", fresh); err != nil {
		t.Fatalf("inject fresh entry: %v", err)
	}

	if err := db.PruneChanges("u@x"); err != nil {
		t.Fatalf("PruneChanges: %v", err)
	}

	got, _, _, _ := db.GetChangesSince("u@x", ChangeTypeEmail, 0, 100)
	if len(got) != 1 || got[0].ID != "fresh" {
		t.Errorf("after prune want only fresh entry, got %+v", got)
	}
}
