package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestMailFoldersListsRealMailboxes(t *testing.T) {
	h, database, _ := mailConversionFixture(t, "Subject: s\r\n\r\nbody", nil) // INBOX, 1 unread
	user := "reader@example.com"
	for _, mb := range []string{"Work", "archive", "Junk"} {
		if err := database.CreateMailbox(user, mb); err != nil {
			t.Fatal(err)
		}
	}
	add := func(mb string, uid uint32, flags []string) {
		id, err := h.msgStore.StoreMessage(user, []byte("Subject: x\r\n\r\nbody"))
		if err != nil {
			t.Fatal(err)
		}
		if err := database.StoreMessageMetadata(user, mb, uid, &storage.MessageMetadata{UID: uid, MessageID: id, Flags: flags, InternalDate: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	add("Work", 1, []string{`\Seen`})
	add("Work", 2, nil)
	add("Junk", 1, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/folders", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	rec := httptest.NewRecorder()
	h.handleMailFolders(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	var out struct{ Folders []MailFolder }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	want := []MailFolder{
		{Name: "INBOX", Role: "inbox", Total: 1, Unread: 1},
		{Name: "Junk", Role: "spam", Total: 1, Unread: 1},
		{Name: "archive", Total: 0, Unread: 0},
		{Name: "Work", Total: 2, Unread: 1},
	}
	if len(out.Folders) != len(want) {
		t.Fatalf("folders %+v", out.Folders)
	}
	for i := range want {
		if out.Folders[i] != want[i] {
			t.Errorf("folder %d = %+v, want %+v", i, out.Folders[i], want[i])
		}
	}

	rec = httptest.NewRecorder()
	h.handleMailFolders(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mail/folders", nil).WithContext(req.Context()))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}
