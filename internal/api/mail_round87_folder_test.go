package api

// Round 87 F5690: the webmail client lists a folder with GET /api/v1/mail/<folder>
// (inbox, sent, drafts, trash, spam) but handleMailList only read ?folder=, so every
// folder route returned the INBOX. The trash page's "empty trash" then permanently
// deleted every listed (INBOX) message.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func f5690Fixture(t *testing.T) (*MailHandler, string, string) {
	t.Helper()
	h, database, inboxID := mailConversionFixture(t, "From: a@example.com\r\n\r\ninbox body", nil)
	if err := database.CreateMailbox("reader@example.com", "Trash"); err != nil {
		t.Fatal(err)
	}
	trashID, err := h.msgStore.StoreMessage("reader@example.com", []byte("From: b@example.com\r\n\r\ntrash body"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.StoreMessageMetadata("reader@example.com", "Trash", 1, &storage.MessageMetadata{UID: 1, MessageID: trashID, Subject: "trashed"}); err != nil {
		t.Fatal(err)
	}
	return h, inboxID, trashID
}

func f5690IDs(t *testing.T, h *MailHandler, url string) (int, []string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleMailList(rec, mailMutationRequest(http.MethodGet, url))
	var out struct {
		Emails []Mail `json:"emails"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{}
	for _, m := range out.Emails {
		ids = append(ids, m.ID)
	}
	return rec.Code, ids
}

func TestAudit5690Control(t *testing.T) {
	h, inboxID, trashID := f5690Fixture(t)
	if code, ids := f5690IDs(t, h, "/api/v1/mail/inbox"); code != 200 || len(ids) != 1 || ids[0] != inboxID {
		t.Fatalf("inbox route: %d %v", code, ids)
	}
	if code, ids := f5690IDs(t, h, "/api/v1/mail?folder=trash"); code != 200 || len(ids) != 1 || ids[0] != trashID {
		t.Fatalf("query folder control: %d %v", code, ids)
	}
}

func TestAudit5690Failure(t *testing.T) {
	h, inboxID, trashID := f5690Fixture(t)
	code, ids := f5690IDs(t, h, "/api/v1/mail/trash")
	t.Logf("EXPECTED: [%s] ACTUAL: code=%d %v", trashID, code, ids)
	if code != 200 || len(ids) != 1 || ids[0] != trashID {
		t.Errorf("DEFECT F5690: /api/v1/mail/trash did not list the Trash folder (inbox=%s)", inboxID)
	}
}

func TestAudit5690Edges(t *testing.T) {
	h, inboxID, trashID := f5690Fixture(t)
	// Explicit ?folder= wins over the path; unknown path segment falls back to INBOX.
	if _, ids := f5690IDs(t, h, "/api/v1/mail/inbox?folder=trash"); len(ids) != 1 || ids[0] != trashID {
		t.Errorf("explicit folder query must win: %v", ids)
	}
	if _, ids := f5690IDs(t, h, "/api/v1/mail"); len(ids) != 1 || ids[0] != inboxID {
		t.Errorf("bare path must default to INBOX: %v", ids)
	}
	if _, ids := f5690IDs(t, h, "/api/v1/mail/TRASH"); len(ids) != 1 || ids[0] != trashID {
		t.Errorf("path segment is case-insensitive: %v", ids)
	}
	// Spam route maps to Junk, which does not exist yet: empty list, not INBOX.
	if code, ids := f5690IDs(t, h, "/api/v1/mail/spam"); code != 200 || len(ids) != 0 {
		t.Errorf("spam route: %d %v", code, ids)
	}
}
