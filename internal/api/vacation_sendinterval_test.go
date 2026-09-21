package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleSetVacation_ZeroSendInterval reproduces the zero interval bug:
// handleSetVacation accepts send_interval=0, which bypasses the interval check
// in ShouldSendAutoReply (time.Since(lastSend) < 0 is never true).
// Result: vacation auto-reply fires on EVERY email from each sender.
func TestHandleSetVacation_ZeroSendInterval(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewTestServer(t, tmpDir)

	body := `{"enabled":true,"subject":"Test","message":"Test msg","send_interval":0}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vacation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	s.handleSetVacation(w, req)

	if w.Code == http.StatusOK {
		t.Error("handleSetVacation accepted send_interval=0 — vacation fires on every email")
	} else {
		t.Logf("Correctly rejected zero interval: status=%d body=%s", w.Code, w.Body.String())
	}
}
