package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleSetVacation_EndBeforeStart reproduces the date-inversion bug:
// handleSetVacation accepts and stores a vacation config where end_date is
// chronologically before start_date. No validation rejects this.
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

	if w.Code == http.StatusOK {
		t.Error("handleSetVacation accepted end_date before start_date — date inversion should be rejected")
	} else {
		t.Logf("Correctly rejected: status=%d body=%s", w.Code, w.Body.String())
	}
}
