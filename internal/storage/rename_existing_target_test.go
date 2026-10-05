package storage

import (
	"fmt"
	"testing"
)

func seedRenameTargetRegression(t *testing.T, d *Database, box, id string) {
	t.Helper()
	if e := d.CreateMailbox("fixture", box); e != nil {
		t.Fatal(e)
	}
	if e := d.StoreMessageMetadata("fixture", box, 1, &MessageMetadata{MessageID: id, UID: 1}); e != nil {
		t.Fatal(e)
	}
}
func TestRenameMailboxUnusedTargetPreservesMessage(t *testing.T) {
	d := setupTestDB(t)
	seedRenameTargetRegression(t, d, "source", "source-message")
	e := d.RenameMailbox("fixture", "source", "new")
	v, re := d.GetMessageMetadata("fixture", "new", 1)
	ok := e == nil && re == nil && v.MessageID == "source-message"
	fmt.Printf("CONTROL EXPECTED: rename into unused target preserves message ACTUAL: %v\n", ok)
	if !ok {
		t.Fatal("invalid control")
	}
}
func TestRenameMailboxExistingTargetPreservesBothMailboxes(t *testing.T) {
	d := setupTestDB(t)
	seedRenameTargetRegression(t, d, "source", "source-message")
	seedRenameTargetRegression(t, d, "target", "target-message")
	e := d.RenameMailbox("fixture", "source", "target")
	v, re := d.GetMessageMetadata("fixture", "target", 1)
	if re != nil {
		t.Fatal(re)
	}
	fmt.Printf("EXPECTED: reject existing target, target-message preserved ACTUAL: error=%v target=%s\n", e, v.MessageID)
	if e == nil || v.MessageID != "target-message" {
		t.Fatal("DEFECT F4819: rename overwrites destination metadata")
	}
	v, re = d.GetMessageMetadata("fixture", "source", 1)
	if re != nil || v.MessageID != "source-message" {
		t.Fatal("source lost after rejection")
	}
}
func TestRenameMailboxEmptyTargetRejectsRepeatedAttempts(t *testing.T) {
	d := setupTestDB(t)
	seedRenameTargetRegression(t, d, "source", "source-message")
	if e := d.RenameMailbox("fixture", "source", "source"); e != nil {
		t.Fatal(e)
	}
	if e := d.CreateMailbox("fixture", "empty"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e := d.RenameMailbox("fixture", "source", "empty"); e == nil {
			t.Fatal("existing empty target must also reject")
		}
	}
	v, e := d.GetMessageMetadata("fixture", "source", 1)
	if e != nil || v.MessageID != "source-message" {
		t.Fatal(v, e)
	}
}
