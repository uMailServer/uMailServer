package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMailListHealthyControl(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "ordinary body", nil)
	rec := httptest.NewRecorder()
	h.handleMailList(rec, mailMutationRequest(http.MethodGet, "/api/v1/mail?folder=inbox"))
	if rec.Code != 200 {
		t.Fatalf("healthy list control: %d", rec.Code)
	}
	t.Log("CONTROL EXPECTED: healthy list=200 ACTUAL: 200")
}
func TestMailListDatabaseFailure(t *testing.T) {
	h, db, _ := mailConversionFixture(t, "ordinary body", nil)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetMailbox("reader@example.com", "INBOX"); err == nil {
		t.Fatal("failure fixture not active")
	}
	rec := httptest.NewRecorder()
	h.handleMailList(rec, mailMutationRequest(http.MethodGet, "/api/v1/mail?folder=inbox"))
	t.Logf("EXPECTED: database failure status=500 ACTUAL: status=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusInternalServerError {
		t.Error("DEFECT F4748: failed database represented as successful empty mailbox")
	}
}
func TestMailListDatabaseBoundaries(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "ordinary body", nil)
	for _, folder := range []string{"Unused", "INBOX"} {
		rec := httptest.NewRecorder()
		h.handleMailList(rec, mailMutationRequest(http.MethodGet, "/api/v1/mail?folder="+folder))
		if rec.Code != 200 {
			t.Errorf("healthy folder %s status=%d", folder, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	NewMailHandler().handleMailList(rec, mailMutationRequest(http.MethodGet, "/api/v1/mail"))
	if rec.Code != 200 {
		t.Errorf("unconfigured legacy handler status=%d", rec.Code)
	}
}
