package pop3

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// F6362: POP3 DELE of one of two identical messages keeps the other's body.
func TestBboltStore_DeleteIdenticalMessageKeepsOther(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ms, err := storage.NewMessageStore(dir + "/m")
	if err != nil {
		t.Fatal(err)
	}
	user := "u@example.com"
	body := []byte("Subject: same\r\n\r\nx\r\n")
	id, _ := ms.StoreMessage(user, body)
	for uid := uint32(1); uid <= 2; uid++ {
		if err := db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{UID: uid, MessageID: id, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
	}
	st := NewBboltStore(db, ms)
	if err := st.DeleteMessage(user, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := ms.ReadMessage(user, id); err != nil || string(got) != string(body) {
		t.Fatalf("remaining message lost body: %v", err)
	}
	if err := st.DeleteMessage(user, 1); err != nil {
		t.Fatal(err)
	}
	if ms.MessageExists(user, id) {
		t.Fatal("blob leaked after last reference")
	}
}
