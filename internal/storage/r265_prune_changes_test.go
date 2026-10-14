package storage

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func r265SeedChanges(t *testing.T, db *Database, user string, old, fresh int) {
	t.Helper()
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(changesBucket(user)))
		if err != nil {
			return err
		}
		for i := 0; i < old+fresh; i++ {
			seq, _ := b.NextSequence()
			at := time.Now().UTC()
			if i < old {
				at = at.Add(-ChangesRetention - time.Hour)
			}
			d, _ := json.Marshal(ChangeEntry{Seq: seq, Type: ChangeTypeEmail, Kind: ChangeKindCreated, ID: "x", At: at})
			if err := b.Put(seqKey(seq), d); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func r265CountChanges(t *testing.T, db *Database, user string) int {
	t.Helper()
	n := 0
	_ = db.bolt.View(func(tx *bbolt.Tx) error {
		n = tx.Bucket([]byte(changesBucket(user))).Stats().KeyN
		return nil
	})
	return n
}

// F7390: PruneChanges used the pre-delete committed key count for the cap
// pass, trimming live entries when expiry already brought the journal under
// the cap.
func TestPruneChangesDoesNotOvertrimAfterExpiry(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	live := ChangesMaxEntriesPerUser - 5
	r265SeedChanges(t, db, "u", 10, live)
	if err := db.PruneChanges("u"); err != nil {
		t.Fatal(err)
	}
	if got := r265CountChanges(t, db, "u"); got != live {
		t.Fatalf("live entries = %d, want %d", got, live)
	}

	// Genuine excess is still trimmed to the cap, oldest first.
	r265SeedChanges(t, db, "v", 10, ChangesMaxEntriesPerUser+7)
	if err := db.PruneChanges("v"); err != nil {
		t.Fatal(err)
	}
	if got := r265CountChanges(t, db, "v"); got != ChangesMaxEntriesPerUser {
		t.Fatalf("over-cap entries = %d, want %d", got, ChangesMaxEntriesPerUser)
	}
	// Control: nothing to prune.
	r265SeedChanges(t, db, "w", 0, 100)
	_ = db.PruneChanges("w")
	if got := r265CountChanges(t, db, "w"); got != 100 {
		t.Fatalf("control = %d", got)
	}
}
