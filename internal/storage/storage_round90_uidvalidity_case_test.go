package storage

import (
	"path/filepath"
	"testing"
)

func TestAudit5723Control(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	defer db.Close()
	_ = db.CreateMailbox("u", "A")
	m, _ := db.GetMailbox("u", "A")
	if m == nil || m.UIDValidity == 0 || m.UIDNext != 1 {
		t.Fatalf("INVALID control: %+v", m)
	}
}

// FAILURE: delete+recreate restarts UIDs at 1; RFC 3501 §2.3.1.1 requires a
// strictly larger UIDVALIDITY. It is the wall-clock second, so repeated
// cycles inside one second reuse the value. Repeat to make hitting the same
// second certain (any equal pair confirms).
func TestAudit5723Failure(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	defer db.Close()
	_ = db.CreateMailbox("u", "Tmp")
	prev, _ := db.GetMailbox("u", "Tmp")
	for i := 0; i < 30; i++ {
		_ = db.DeleteMailbox("u", "Tmp")
		_ = db.CreateMailbox("u", "Tmp")
		cur, _ := db.GetMailbox("u", "Tmp")
		if cur.UIDValidity <= prev.UIDValidity {
			t.Logf("EXPECTED: UIDVALIDITY > %d ACTUAL: %d", prev.UIDValidity, cur.UIDValidity)
			t.Fatalf("DEFECT F5723: UIDVALIDITY not increased on recreate (cycle %d)", i)
		}
		prev = cur
	}
}
