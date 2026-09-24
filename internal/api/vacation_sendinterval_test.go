package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleSetVacation_ZeroSendInterval verifies that handleSetVacation rejects
// send_interval=0, which would otherwise bypass the interval check in
// ShouldSendAutoReply and fire vacation replies on every incoming email.
func TestHandleSetVacation_ZeroSendInterval(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewTestServer(t, tmpDir)

	body := `{"enabled":true,"subject":"Test","message":"Test msg","send_interval":0}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vacation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	s.handleSetVacation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleSetVacation rejected zero interval: got status %d, want %d", w.Code, http.StatusBadRequest)
	}
	t.Logf("Correctly rejected send_interval=0: status=%d body=%s", w.Code, w.Body.String())
}
