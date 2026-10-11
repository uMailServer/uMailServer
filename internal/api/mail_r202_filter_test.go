package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// F6761: ?flag=flagged / ?unread=1 filter server-side before pagination.
func TestMailListServerSideFilters(t *testing.T) {
	h, db, _ := mailConversionFixture(t, "x", nil)
	for i := uint32(2); i <= 7; i++ {
		var fl []string
		if i%3 == 0 {
			fl = append(fl, "\\Flagged")
		}
		if i%2 == 0 {
			fl = append(fl, "\\Seen")
		}
		if err := db.StoreMessageMetadata("reader@example.com", "INBOX", i, &storage.MessageMetadata{UID: i, MessageID: "m" + string(rune('0'+i)), Flags: fl}); err != nil {
			t.Fatal(err)
		}
	}
	type resp struct {
		Emails       []Mail `json:"emails"`
		Total        int    `json:"total"`
		UnreadTotal  int    `json:"unreadTotal"`
		StarredTotal int    `json:"starredTotal"`
	}
	get := func(qs string) (int, resp) {
		rec := httptest.NewRecorder()
		h.handleMailList(rec, mailMutationRequest(http.MethodGet, "/api/v1/mail/inbox?"+qs))
		var r resp
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		return rec.Code, r
	}
	// uid1 unread; 2 seen; 3 flagged unread; 4 seen; 5 unread; 6 flagged seen; 7 unread
	if _, r := get(""); r.Total != 7 || r.UnreadTotal != 4 || r.StarredTotal != 2 {
		t.Fatalf("unfiltered %+v", r)
	}
	_, r := get("flag=flagged&limit=1")
	if r.Total != 2 || len(r.Emails) != 1 || !r.Emails[0].Starred || r.UnreadTotal != 4 {
		t.Fatalf("flagged page1 %+v", r)
	}
	_, r2 := get("flag=flagged&limit=1&offset=1")
	if len(r2.Emails) != 1 || r2.Emails[0].ID == r.Emails[0].ID {
		t.Fatalf("flagged page2 %+v", r2)
	}
	_, r = get("unread=1&limit=3&offset=3")
	if r.Total != 4 || len(r.Emails) != 1 || r.Emails[0].Read {
		t.Fatalf("unread %+v", r)
	}
	_, r = get("unread=1&flag=flagged")
	if r.Total != 1 {
		t.Fatalf("both %+v", r)
	}
	if code, _ := get("flag=bogus"); code != 400 {
		t.Fatalf("bogus flag = %d", code)
	}
	if code, _ := get("unread=maybe"); code != 400 {
		t.Fatalf("bogus unread = %d", code)
	}
}
