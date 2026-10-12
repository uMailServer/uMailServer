package storage

import "testing"

// F7251: storing a row at a UID at or beyond UIDNEXT must advance UIDNEXT so
// GetNextUID can never re-issue a UID that already names a message.
func TestR248StoreMetadataAdvancesUIDNext(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/m.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateMailbox("u@x.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata("u@x.com", "INBOX", 5, &MessageMetadata{MessageID: "abcd1234", UID: 5}); err != nil {
		t.Fatal(err)
	}
	uid, err := db.GetNextUID("u@x.com", "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if uid <= 5 {
		t.Fatalf("GetNextUID re-issued occupied UID range: got %d", uid)
	}
}
