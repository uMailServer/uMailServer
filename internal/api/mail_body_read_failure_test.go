package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func mailBodyReadRequest(id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/mail/message?id="+id+"&folder=inbox", nil)
	return r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
}
func TestMailBodyReadHealthyControl(t *testing.T) {
	h, db, id := mailConversionFixture(t, "Subject: ordinary\r\n\r\nordinary body", nil)
	rec := httptest.NewRecorder()
	h.handleMailGet(rec, mailBodyReadRequest(id))
	var mail Mail
	if err := json.Unmarshal(rec.Body.Bytes(), &mail); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || mail.Body != "ordinary body" {
		t.Fatalf("healthy detail control: %d %v", rec.Code, mail)
	}
	meta, err := db.GetMessageMetadata("reader@example.com", "INBOX", 1)
	if err != nil || !storage.HasFlag(meta.Flags, "\\Seen") {
		t.Fatalf("healthy mark-read control: %v %v", meta, err)
	}
	t.Log("CONTROL EXPECTED: readable body returned and Seen persisted ACTUAL: correct")
}
func TestMailBodyReadFailure(t *testing.T) {
	h, db, id := mailConversionFixture(t, "Subject: ordinary\r\n\r\nordinary body", nil)
	if err := h.msgStore.DeleteMessage("reader@example.com", id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.msgStore.ReadMessage("reader@example.com", id); err == nil {
		t.Fatal("failure fixture: body still readable")
	}
	rec := httptest.NewRecorder()
	h.handleMailGet(rec, mailBodyReadRequest(id))
	meta, err := db.GetMessageMetadata("reader@example.com", "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("EXPECTED: status=500 Seen=false ACTUAL: status=%d Seen=%v body=%s", rec.Code, storage.HasFlag(meta.Flags, "\\Seen"), rec.Body.String())
	if rec.Code != http.StatusInternalServerError {
		t.Error("DEFECT F4750: missing body returned as valid empty message")
	}
	if storage.HasFlag(meta.Flags, "\\Seen") {
		t.Error("DEFECT F4750: failed body read marks message Seen")
	}
}
func TestMailBodyReadBoundaries(t *testing.T) {
	h, _, id := mailConversionFixture(t, "Subject: ordinary\r\n\r\n", nil)
	rec := httptest.NewRecorder()
	h.handleMailGet(rec, mailBodyReadRequest(id))
	var mail Mail
	if err := json.Unmarshal(rec.Body.Bytes(), &mail); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || mail.Body != "" {
		t.Fatalf("genuine empty body status=%d body=%q", rec.Code, mail.Body)
	}
	rec = httptest.NewRecorder()
	h.handleMailGet(rec, mailBodyReadRequest("absent-id"))
	if rec.Code != 404 {
		t.Fatalf("absent metadata status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NewMailHandler().handleMailGet(rec, mailBodyReadRequest("absent-id"))
	if rec.Code != 404 {
		t.Fatalf("legacy nil-storage status=%d", rec.Code)
	}
}
