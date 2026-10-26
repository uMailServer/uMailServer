package jmap

// Regression test: an Email/set that files an Email into Junk, or takes it
// out of Junk other than into Trash, reports it to the spam classifier.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestEmailSetMailboxChangeReportsJunk(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	data := []byte("Subject: offer\r\n\r\nbody\r\n")
	blobID, err := msgStore.StoreMessage(user, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, &storage.MessageMetadata{
		MessageID: blobID, InternalDate: time.Now(), Size: int64(len(data)), UID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for _, mb := range []string{"INBOX", "Junk", "Archive", "Trash"} {
		if err := db.CreateMailbox(user, mb); err != nil {
			t.Fatal(err)
		}
	}

	type call struct {
		id   string
		spam bool
	}
	var calls []call
	server.SetJunkReportFunc(func(u, id string, spam bool) {
		if u != user {
			t.Errorf("reported user %q, want %q", u, user)
		}
		calls = append(calls, call{id, spam})
	})

	moveTo := func(mailbox string) {
		t.Helper()
		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"mailboxIds": map[string]interface{}{getMailboxIDFromName(mailbox): true},
					},
				},
			},
			ID: "c1",
		})
		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("move to %s not applied: %v", mailbox, resp.Args)
		}
	}
	moveTo("Junk")    // spam
	moveTo("Archive") // ham
	moveTo("Junk")    // spam
	moveTo("Trash")   // out of Junk into Trash: no report

	want := []call{{blobID, true}, {blobID, false}, {blobID, true}}
	if len(calls) != len(want) {
		t.Fatalf("reports = %+v, want %+v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("report %d = %+v, want %+v", i, calls[i], want[i])
		}
	}
}
