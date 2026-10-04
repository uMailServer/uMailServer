package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func mailMutationRequest(method, url string) *http.Request {
	r := httptest.NewRequest(method, url, nil)
	return r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
}
func mailMutationCopy(t *testing.T, db *storage.Database, folder, id string, uid uint32) {
	t.Helper()
	if err := db.CreateMailbox("reader@example.com", folder); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata("reader@example.com", folder, uid, &storage.MessageMetadata{UID: uid, MessageID: id}); err != nil {
		t.Fatal(err)
	}
}
func mailMutationRemaining(t *testing.T, db *storage.Database, folders []string, id string) int {
	t.Helper()
	n := 0
	for _, folder := range folders {
		uids, err := db.GetMessageUIDs("reader@example.com", folder)
		if err != nil {
			t.Fatal(err)
		}
		for _, uid := range uids {
			m, err := db.GetMessageMetadata("reader@example.com", folder, uid)
			if err != nil {
				t.Fatal(err)
			}
			if m.MessageID == id {
				n++
			}
		}
	}
	return n
}
func TestMailDeleteStandardFolderControl(t *testing.T) {
	h, db, id := mailConversionFixture(t, "ordinary body", nil)
	rec := httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+id))
	if rec.Code != 200 || mailMutationRemaining(t, db, []string{"INBOX"}, id) != 0 {
		t.Fatal("control: single standard-folder delete failed")
	}
	t.Log("CONTROL EXPECTED: standard-folder metadata removed ACTUAL: removed")
}
func TestMailDeleteAllReferences(t *testing.T) {
	h, db, id := mailConversionFixture(t, "ordinary body", nil)
	mailMutationCopy(t, db, "Projects", id, 2)
	mailMutationCopy(t, db, "Sent", id, 3)
	mailMutationCopy(t, db, "INBOX", id, 4)
	mailMutationCopy(t, db, "Projects", "unrelated-content", 5)
	rec := httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+id))
	if rec.Code != 200 {
		t.Fatalf("fixture request failed: %d", rec.Code)
	}
	n := mailMutationRemaining(t, db, []string{"INBOX", "Sent", "Projects"}, id)
	t.Logf("EXPECTED: deleted content references=0 ACTUAL: references=%d", n)
	if n != 0 {
		t.Errorf("DEFECT F4747: deleted body still has %d metadata references", n)
	}
	if _, err := db.GetMessageMetadata("reader@example.com", "Projects", 5); err != nil {
		t.Fatal("unrelated metadata changed", err)
	}
	if _, err := h.msgStore.ReadMessage("reader@example.com", id); err == nil {
		t.Fatal("fixture: target body was not deleted")
	}
}
func TestMailDeleteReferenceBoundaries(t *testing.T) {
	for _, folder := range []string{"Projects", "Nested/Folder", "Trash"} {
		h, db, id := mailConversionFixture(t, "ordinary body", nil)
		mailMutationCopy(t, db, folder, id, 2)
		h.deleteMessageMetadata("reader@example.com", "absent-id")
		if mailMutationRemaining(t, db, []string{"INBOX", folder}, id) != 2 {
			t.Fatal("absent ID removed existing mail")
		}
		h.deleteMessageMetadata("reader@example.com", id)
		h.deleteMessageMetadata("reader@example.com", id)
		if n := mailMutationRemaining(t, db, []string{"INBOX", folder}, id); n != 0 {
			t.Errorf("DEFECT F4747: repeated deletion in %s leaves %d", folder, n)
		}
	}
}
