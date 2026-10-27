package storage

import "testing"

func TestCountMessages(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/m.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n, err := db.CountMessages("u@ex.com", "INBOX"); err != nil || n != 0 {
		t.Fatalf("missing mailbox: %d, %v", n, err)
	}
	if err := db.CreateMailbox("u@ex.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	for uid := uint32(1); uid <= 3; uid++ {
		if err := db.StoreMessageMetadata("u@ex.com", "INBOX", uid, &MessageMetadata{UID: uid, MessageID: string(rune('a' + uid))}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.CountMessages("u@ex.com", "INBOX"); err != nil || n != 3 {
		t.Fatalf("3 messages: %d, %v", n, err)
	}
	if err := db.DeleteMessage("u@ex.com", "INBOX", 2); err != nil {
		t.Fatal(err)
	}
	if exists, _, _, _ := db.GetMailboxCounts("u@ex.com", "INBOX"); exists != 2 {
		t.Fatalf("fixture: GetMailboxCounts = %d", exists)
	}
	if n, _ := db.CountMessages("u@ex.com", "INBOX"); n != 2 {
		t.Fatalf("after a delete: %d, want what GetMailboxCounts reports (2)", n)
	}
	// Another user's mailbox of the same name is separate.
	if n, _ := db.CountMessages("other@ex.com", "INBOX"); n != 0 {
		t.Fatalf("other user: %d", n)
	}
}
