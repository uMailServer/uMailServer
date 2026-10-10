package storage

import (
	"path/filepath"
	"testing"
)

func audit5722DB(t *testing.T) *Database {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAudit5722Control(t *testing.T) {
	db := audit5722DB(t)
	_ = db.CreateMailbox("u", "Sent")
	a, _ := db.GetNextUID("u", "Sent")
	_ = db.CreateMailbox("u", "Sent")
	b, _ := db.GetNextUID("u", "Sent")
	if a != 1 || b != 2 {
		t.Fatalf("INVALID control: %d %d", a, b)
	}
}

// FAILURE: GetNextUID auto-creates the bucket without uidvalidity; a later
// CreateMailbox sees "no uidvalidity" and resets uidnext to 1, re-issuing UIDs.
func TestAudit5722Failure(t *testing.T) {
	db := audit5722DB(t)
	first, _ := db.GetNextUID("u", "Sent")
	if err := db.CreateMailbox("u", "Sent"); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	second, _ := db.GetNextUID("u", "Sent")
	t.Logf("EXPECTED: second UID %d ACTUAL: %d", first+1, second)
	if second != first+1 {
		t.Fatalf("DEFECT F5722: UID %d re-issued after CreateMailbox (first=%d)", second, first)
	}
}
