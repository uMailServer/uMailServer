package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// F6645: byte-identical messages share one blob hash. Deleting one copy by
// its listed id must remove only that (folder, uid) entry.
func TestMailDeleteOneOfIdenticalCopies(t *testing.T) {
	h, db, hash := mailConversionFixture(t, "same bytes", nil)
	mailMutationCopy(t, db, "INBOX", hash, 2) // same-folder duplicate
	mailMutationCopy(t, db, "Sent", hash, 3)  // cross-folder duplicate
	mailMutationCopy(t, db, "INBOX", "unique-blob-id", 4)

	inbox, err := h.getEmailsFromStorage("reader@example.com", "INBOX")
	if err != nil || len(inbox) != 3 {
		t.Fatalf("list: %v %d", err, len(inbox))
	}
	ids := map[string]bool{}
	var firstDup, uniq string
	for _, m := range inbox {
		ids[m.ID] = true
		if m.ID == "unique-blob-id" {
			uniq = m.ID
		} else if firstDup == "" {
			firstDup = m.ID
		}
	}
	if len(ids) != 3 {
		t.Fatalf("EXPECTED 3 distinct ids, ACTUAL %v", ids)
	}
	if uniq == "" {
		t.Fatal("control: unique message must keep its plain id")
	}
	sent, _ := h.getEmailsFromStorage("reader@example.com", "Sent")
	if len(sent) != 1 || sent[0].ID == hash || ids[sent[0].ID] {
		t.Fatalf("Sent copy id %v", sent)
	}

	// GET by the qualified id resolves that copy.
	r := mailMutationRequest(http.MethodGet, "/api/v1/mail/message?id="+url.QueryEscape(firstDup)+"&folder=INBOX")
	rec := httptest.NewRecorder()
	h.handleMailMessage(rec, r)
	if rec.Code != 200 {
		t.Fatalf("GET qualified id: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+url.QueryEscape(firstDup)))
	if rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if n := mailMutationRemaining(t, db, []string{"INBOX", "Sent"}, hash); n != 2 {
		t.Errorf("EXPECTED 2 surviving copies, ACTUAL %d", n)
	}
	if _, err := h.msgStore.ReadMessage("reader@example.com", hash); err != nil {
		t.Errorf("blob removed while copies remain: %v", err)
	}
	// Delete the Sent copy, then the last INBOX copy: blob goes with the last.
	rest, _ := h.getEmailsFromStorage("reader@example.com", "Sent")
	rec = httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+url.QueryEscape(rest[0].ID)))
	left, _ := h.getEmailsFromStorage("reader@example.com", "INBOX")
	var last string
	for _, m := range left {
		if m.ID != uniq {
			last = m.ID
		}
	}
	if last != hash {
		t.Fatalf("sole remaining copy should regain plain id, got %q", last)
	}
	rec = httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+hash))
	if _, err := h.msgStore.ReadMessage("reader@example.com", hash); err == nil {
		t.Error("blob not removed after last copy deleted")
	}
}

func TestQualifiedMailIDRoundTrip(t *testing.T) {
	id := qualifiedMailID("abc", "Nested/Folder", 9)
	h, m, u, ok := parseQualifiedMailID(id)
	if !ok || h != "abc" || m != "Nested/Folder" || u != 9 {
		t.Fatalf("%v %v %v %v", h, m, u, ok)
	}
	for _, bad := range []string{"abc", "abc_x_QQ", "abc_1_", "_1_QQ", "abc_1_!!"} {
		if _, _, _, ok := parseQualifiedMailID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
