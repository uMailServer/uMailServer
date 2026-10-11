package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// F6680: REST delete must release the blob bytes exactly once, and only when
// the file is really removed (identical copies share one blob).
func TestMailDeleteReleasesQuotaOnce(t *testing.T) {
	h, db, hash := mailConversionFixture(t, "quota bytes", nil)
	var total int64
	h.SetQuotaAdjustFunc(func(_ string, d int64) error { total += d; return nil })
	size, err := h.msgStore.MessageSize("reader@example.com", hash)
	if err != nil || size <= 0 {
		t.Fatalf("size %d %v", size, err)
	}
	mailMutationCopy(t, db, "Sent", hash, 2)

	// Control: deleting one of two copies keeps the blob and releases nothing.
	inbox, _ := h.getEmailsFromStorage("reader@example.com", "INBOX")
	if len(inbox) != 1 {
		t.Fatalf("list %d", len(inbox))
	}
	rec := httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+url.QueryEscape(inbox[0].ID)))
	if rec.Code != 200 || total != 0 {
		t.Fatalf("EXPECTED no release while a copy remains, ACTUAL code=%d total=%d", rec.Code, total)
	}

	sent, _ := h.getEmailsFromStorage("reader@example.com", "Sent")
	rec = httptest.NewRecorder()
	h.handleMailDelete(rec, mailMutationRequest(http.MethodDelete, "/api/v1/mail/delete?id="+url.QueryEscape(sent[0].ID)))
	if rec.Code != 200 || total != -size {
		t.Fatalf("EXPECTED release of %d, ACTUAL %d (code %d)", size, -total, rec.Code)
	}
}
