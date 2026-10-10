package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func r143MailSetup(t *testing.T) (*MailHandler, *storage.Database, *storage.MessageStore) {
	t.Helper()
	mdb, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mdb.Close() })
	ms, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	h := NewMailHandler()
	h.SetStorage(ms, mdb)
	return h, mdb, ms
}

func r143Deliver(t *testing.T, mdb *storage.Database, ms *storage.MessageStore, user, mailbox, subject string, flags []string) string {
	t.Helper()
	id, err := ms.StoreMessage(user, []byte("From: a@x.com\r\nTo: "+user+"\r\nSubject: "+subject+"\r\n\r\nbody "+subject+"\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mdb.CreateMailbox(user, mailbox); err != nil {
		t.Fatal(err)
	}
	uid, _ := mdb.GetNextUID(user, mailbox)
	if err := mdb.StoreMessageMetadata(user, mailbox, uid, &storage.MessageMetadata{
		MessageID: id, UID: uid, Flags: flags, InternalDate: time.Now(), Size: 10, Subject: subject, From: "a@x.com", To: user,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func r143Move(h *MailHandler, user string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/move", bytes.NewReader([]byte(body)))
	if user != "" {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}
	rec := httptest.NewRecorder()
	h.handleMailMove(rec, req)
	return rec
}

func r143Count(mdb *storage.Database, user, mailbox string) int {
	u, _ := mdb.GetMessageUIDs(user, mailbox)
	return len(u)
}

// F6253 (High): the webmail "Restore" from Trash had no move endpoint and
// permanently deleted the message. Move keeps the blob, flags and quota.
func TestR143MailMoveRestoreFromTrash(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	user := "u@example.com"
	id := r143Deliver(t, mdb, ms, user, "Trash", "restore me", []string{"\\Seen", "\\Flagged"})

	rec := r143Move(h, user, `{"id":"`+id+`","from":"trash","to":"inbox"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	if r143Count(mdb, user, "Trash") != 0 || r143Count(mdb, user, "INBOX") != 1 {
		t.Fatalf("trash=%d inbox=%d", r143Count(mdb, user, "Trash"), r143Count(mdb, user, "INBOX"))
	}
	if _, err := ms.ReadMessage(user, id); err != nil {
		t.Fatalf("message body lost by move: %v", err)
	}
	uids, _ := mdb.GetMessageUIDs(user, "INBOX")
	meta, _ := mdb.GetMessageMetadata(user, "INBOX", uids[0])
	if meta.MessageID != id || meta.UID != uids[0] || !hasFlag(meta.Flags, "\\Seen") || !hasFlag(meta.Flags, "\\Flagged") {
		t.Fatalf("metadata not carried over: %+v", meta)
	}
	// the webmail list endpoint sees it in the inbox, not in trash
	got, _ := h.getEmailsFromStorage(user, "INBOX")
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("inbox list: %+v", got)
	}
}

func TestR143MailMoveBatchAndErrors(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	user := "u@example.com"
	a := r143Deliver(t, mdb, ms, user, "Trash", "a", nil)
	b := r143Deliver(t, mdb, ms, user, "Trash", "b", nil)
	other := r143Deliver(t, mdb, ms, "other@example.com", "Trash", "theirs", nil)

	for name, tc := range map[string]struct {
		user, body string
		want       int
	}{
		"unauth":            {"", `{"id":"` + a + `","from":"trash","to":"inbox"}`, http.StatusUnauthorized},
		"bad json":          {user, `{`, http.StatusBadRequest},
		"no id":             {user, `{"from":"trash","to":"inbox"}`, http.StatusBadRequest},
		"no folders":        {user, `{"id":"` + a + `"}`, http.StatusBadRequest},
		"same folder":       {user, `{"id":"` + a + `","from":"trash","to":"trash"}`, http.StatusBadRequest},
		"ctl in id":         {user, `{"id":"a\nb","from":"trash","to":"inbox"}`, http.StatusBadRequest},
		"unknown msg":       {user, `{"id":"nope","from":"trash","to":"inbox"}`, http.StatusNotFound},
		"unknown dest":      {user, `{"id":"` + a + `","from":"trash","to":"NoSuchFolder"}`, http.StatusNotFound},
		"not mine":          {user, `{"id":"` + other + `","from":"trash","to":"inbox"}`, http.StatusNotFound},
		"batch partial 404": {user, `{"ids":["` + a + `","nope"],"from":"trash","to":"inbox"}`, http.StatusNotFound},
	} {
		if rec := r143Move(h, tc.user, tc.body); rec.Code != tc.want {
			t.Errorf("%s: got %d want %d (%s)", name, rec.Code, tc.want, rec.Body.String())
		}
	}
	if r143Count(mdb, user, "Trash") != 2 || r143Count(mdb, user, "INBOX") != 0 {
		t.Fatalf("a failed request moved something: trash=%d inbox=%d", r143Count(mdb, user, "Trash"), r143Count(mdb, user, "INBOX"))
	}
	if rec := r143Move(h, user, `{"ids":["`+a+`","`+b+`","`+a+`"],"from":"trash","to":"inbox"}`); rec.Code != http.StatusOK {
		t.Fatalf("batch: %d %s", rec.Code, rec.Body.String())
	} else {
		var out struct {
			Moved []string `json:"moved"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Moved) != 2 {
			t.Errorf("moved=%v", out.Moved)
		}
	}
	if r143Count(mdb, user, "Trash") != 0 || r143Count(mdb, user, "INBOX") != 2 {
		t.Fatalf("batch result trash=%d inbox=%d", r143Count(mdb, user, "Trash"), r143Count(mdb, user, "INBOX"))
	}
	// the other user's message is untouched
	if r143Count(mdb, "other@example.com", "Trash") != 1 {
		t.Fatal("other user's mailbox modified")
	}
	// moving it back: destination conflict when the id already exists there
	r143Deliver(t, mdb, ms, user, "Trash", "a", nil) // same content => same id as a
	if rec := r143Move(h, user, `{"id":"`+a+`","from":"trash","to":"inbox"}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate in destination: %d %s", rec.Code, rec.Body.String())
	}
}

func TestR143MailMoveMethod(t *testing.T) {
	h, _, _ := r143MailSetup(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/move", nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", "u@example.com"))
	rec := httptest.NewRecorder()
	h.handleMailMove(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
}
