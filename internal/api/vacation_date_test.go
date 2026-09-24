package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleSetVacation_EndBeforeStart verifies that handleSetVacation rejects
// a vacation config where end_date is chronologically before start_date.
func TestHandleSetVacation_EndBeforeStart(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewTestServer(t, tmpDir)

	// End in 2025, start in 2030 — clearly reversed
	startDate := "2030-01-15T00:00:00Z"
	endDate := "2025-01-10T00:00:00Z"

	body := `{"enabled":true,"subject":"Test","message":"Test msg","start_date":"` + startDate + `","end_date":"` + endDate + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vacation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	s.handleSetVacation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleSetVacation rejected end_date before start_date: got status %d, want %d", w.Code, http.StatusBadRequest)
	}
	t.Logf("Correctly rejected end before start: status=%d body=%s", w.Code, w.Body.String())
}
