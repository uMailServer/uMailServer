package api

// Round 87 F5696: /api/v1/search compared ?folder= to the internal mailbox name
// verbatim, so the webmail folder names the rest of the API accepts
// ("inbox", "spam") matched nothing.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/search"
	"github.com/umailserver/umailserver/internal/storage"
)

func f5696Search(t *testing.T, folder string) (int, int) {
	t.Helper()
	srv, _ := webmailSendServer(t)
	const user = "sender@sendtest.invalid"
	for _, mb := range []string{"INBOX", "Junk"} {
		if err := srv.mailDB.CreateMailbox(user, mb); err != nil {
			t.Fatal(err)
		}
	}
	put := func(mb string, uid uint32, body string) {
		id, err := srv.msgStore.StoreMessage(user, []byte("From: a@example.com\r\nSubject: hello\r\n\r\n"+body))
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.mailDB.StoreMessageMetadata(user, mb, uid, &storage.MessageMetadata{UID: uid, MessageID: id, Subject: "hello", From: "a@example.com", InternalDate: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	put("INBOX", 1, "zebrafinch")
	put("Junk", 1, "zebrafinch")
	srv.searchSvc = search.NewService(srv.mailDB, srv.msgStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=zebrafinch&folder="+folder, nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	rec := httptest.NewRecorder()
	srv.handleSearch(rec, req)
	var out struct {
		Emails []search.MessageSearchResult `json:"emails"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, len(out.Emails)
}

func TestAudit5696Control(t *testing.T) {
	if c, n := f5696Search(t, "INBOX"); c != 200 || n != 1 {
		t.Fatalf("internal name: %d %d", c, n)
	}
	if c, n := f5696Search(t, ""); c != 200 || n != 2 {
		t.Fatalf("no folder: %d %d", c, n)
	}
}

func TestAudit5696Failure(t *testing.T) {
	c, n := f5696Search(t, "inbox")
	t.Logf("EXPECTED: 1 result ACTUAL: code=%d results=%d", c, n)
	if c != 200 || n != 1 {
		t.Errorf("DEFECT F5696: folder=inbox found %d results", n)
	}
}

func TestAudit5696Edges(t *testing.T) {
	if _, n := f5696Search(t, "spam"); n != 1 {
		t.Errorf("folder=spam (Junk) results = %d", n)
	}
	if _, n := f5696Search(t, "Junk"); n != 1 {
		t.Errorf("folder=Junk results = %d", n)
	}
	if _, n := f5696Search(t, "Unknown"); n != 0 {
		t.Errorf("unknown folder results = %d", n)
	}
}
