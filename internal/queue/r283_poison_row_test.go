package queue

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"go.etcd.io/bbolt"
)

// F7570: one corrupt queue row must not stop the sweeper from dispatching the
// valid pending entries.
func r283Sweep(t *testing.T, corrupt bool) []string {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/q.db")
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	defer database.Close()
	if corrupt {
		if err := database.BoltDB().Update(func(tx *bbolt.Tx) error {
			return tx.Bucket([]byte(db.BucketQueue)).Put([]byte("0-corrupt"), []byte("not json"))
		}); err != nil {
			t.Fatalf("INVALID: %v", err)
		}
	}
	for _, id := range []string{"1-ok", "2-ok"} {
		if err := database.Enqueue(&db.QueueEntry{ID: id, From: "a@x.test", To: []string{"b@y.test"},
			Status: "pending", NextRetry: time.Now().Add(-time.Hour)}); err != nil {
			t.Fatalf("INVALID: %v", err)
		}
	}
	m := NewManager(database, nil, dir, nil)
	m.deliveryChan = make(chan *db.QueueEntry, 10)
	m.sweepPendingEntries()
	var got []string
	for len(m.deliveryChan) > 0 {
		got = append(got, (<-m.deliveryChan).ID)
	}
	return got
}

func TestSweepDispatchesValidEntriesDespiteCorruptRow(t *testing.T) {
	// control: without a corrupt row both pending entries are dispatched
	if got := r283Sweep(t, false); len(got) != 2 {
		t.Fatalf("CONTROL FAILED: dispatched %v", got)
	}
	got := r283Sweep(t, true)
	if len(got) != 2 {
		t.Fail()
	}
}
