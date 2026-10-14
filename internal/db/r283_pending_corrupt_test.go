package db

import (
	"errors"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func r283Put(t *testing.T, d *DB, key, val string) {
	t.Helper()
	if err := d.bolt.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(BucketQueue)).Put([]byte(key), []byte(val))
	}); err != nil {
		t.Fatal(err)
	}
}

// F7570: a corrupt row must be reported but must not hide the valid pending
// entries stored before or after it.
func TestGetPendingQueueKeepsValidEntriesAroundCorruptRows(t *testing.T) {
	d := helperDB(t)
	due := time.Now().Add(-time.Hour)
	for _, id := range []string{"1-ok", "3-ok"} {
		if err := d.Enqueue(&QueueEntry{ID: id, From: "a@x.test", To: []string{"b@y.test"}, Status: "pending", NextRetry: due}); err != nil {
			t.Fatal(err)
		}
	}
	r283Put(t, d, "0-notjson", "not json")                  // fails the cheap sniff
	r283Put(t, d, "2-badjson", `{"status":"pending","id":`) // passes the sniff, fails Unmarshal
	entries, err := d.GetPendingQueue(time.Now())
	if err == nil {
		t.Fatal("corruption must still be surfaced as an error")
	}
	if len(entries) != 2 || entries[0].ID != "1-ok" || entries[1].ID != "3-ok" {
		t.Fatalf("entries = %v, want the 2 valid pending ones", entries)
	}
	if !errors.Is(err, errInvalidQueueEntry) {
		t.Logf("first corrupt row was %v", err) // the first bad row decides the wrapped cause
	}
}

func TestGetPendingQueueOnlyCorruptRows(t *testing.T) {
	d := helperDB(t)
	r283Put(t, d, "a", "garbage")
	entries, err := d.GetPendingQueue(time.Now())
	if err == nil || len(entries) != 0 {
		t.Fatalf("entries=%v err=%v, want no entries and an error", entries, err)
	}
	if !errors.Is(err, errInvalidQueueEntry) {
		t.Fatalf("err = %v, want wrapped errInvalidQueueEntry", err)
	}
}
