package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r299Send(t *testing.T, h *MailHandler, subject string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"to":["bob@example.com"],"subject":"` + subject + `","body":"hello"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
	w := httptest.NewRecorder()
	h.handleMailSend(w, r)
	return w
}

func r299Sent(t *testing.T, h *MailHandler) int {
	t.Helper()
	list, err := h.getEmailsFromStorage("reader@example.com", "Sent")
	if err != nil {
		return 0
	}
	return len(list)
}

// F7740: after the message was delivered, a failure saving the sender's Sent
// copy is not reported as a failed send (the user would send it again).
func TestSendDeliveredButSentCopyFailsStaysSuccessful(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	delivered := 0
	h.SetDeliverFunc(func(string, []string, []byte) error { delivered++; return nil })
	h.SetQuotaAdjustFunc(func(string, int64) error { return errors.New("quota exceeded") })
	w := r299Send(t, h, "one")
	if w.Code != http.StatusOK || delivered != 1 {
		t.Fatalf("status %d (%s), delivered %d; want 200 and 1", w.Code, strings.TrimSpace(w.Body.String()), delivered)
	}
	if n := r299Sent(t, h); n != 0 {
		t.Errorf("Sent copy listed despite the quota failure: %d", n)
	}
}

// Control: with room in the quota the Sent copy is saved and its id returned.
func TestSendStoresSentCopyWhenItCan(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	h.SetDeliverFunc(func(string, []string, []byte) error { return nil })
	w := r299Send(t, h, "two")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"`) || strings.Contains(w.Body.String(), `"id":""`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if n := r299Sent(t, h); n != 1 {
		t.Errorf("Sent copies = %d, want 1", n)
	}
}

// A failed delivery is still an error and nothing is filed as sent.
func TestSendDeliveryFailureStaysAnError(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	h.SetDeliverFunc(func(string, []string, []byte) error { return errors.New("no such user") })
	if w := r299Send(t, h, "three"); w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", w.Code)
	}
	if n := r299Sent(t, h); n != 0 {
		t.Errorf("a failed send left %d Sent copies", n)
	}
}

// With no delivery or queue wired (legacy store-only mode) the Sent copy IS the
// result, so its failure is still reported.
func TestSendWithoutDeliveryReportsSentCopyFailure(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	h.SetQuotaAdjustFunc(func(string, int64) error { return errors.New("quota exceeded") })
	if w := r299Send(t, h, "four"); w.Code != http.StatusInsufficientStorage {
		t.Fatalf("status %d, want 507", w.Code)
	}
}
