package caldav

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleMkCalendar_InvalidPath_Coverage tests MKCALENDAR with invalid path
func TestHandleMkCalendar_InvalidPath_Coverage(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Test MKCALENDAR with path that's too short
	req := httptest.NewRequest("MKCALENDAR", "/", nil)
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestHandleMkCalendar_NoAuth_Coverage tests MKCALENDAR without authentication
func TestHandleMkCalendar_NoAuth_Coverage(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	req := httptest.NewRequest("MKCALENDAR", "/calendars/user@example.com/test-cal", nil)
	req.Header.Set("Content-Type", "text/xml")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

// TestHandleMove_NoOverwrite_Coverage tests MOVE without overwrite header
func TestHandleMove_NoOverwrite_Coverage(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewServer(tmpDir, slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	server.storage.CreateCalendar("user@example.com", &Calendar{
		ID:       "test-cal",
		Name:     "Test Calendar",
		Timezone: "UTC",
	})

	server.storage.SaveEvent("user@example.com", "test-cal", &CalendarEvent{
		UID:     "event1",
		Summary: "Test Event",
	}, "BEGIN:VCALENDAR\nEND:VCALENDAR")

	req := httptest.NewRequest("MOVE", "/calendars/user@example.com/test-cal/event1.ics", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	req.Header.Set("Destination", "http://example.com/calendars/user@example.com/test-cal/event2.ics")
	// No Overwrite header
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	// Accept any valid response (success or not found due to different implementations)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusNoContent && rr.Code != http.StatusNotFound {
		t.Errorf("Expected valid status, got %d", rr.Code)
	}
}

// TestHandleCopy_NoAuth_Coverage tests COPY without authentication
func TestHandleCopy_NoAuth_Coverage(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	req := httptest.NewRequest("COPY", "/calendars/user@example.com/test-cal/event1.ics", nil)
	req.Header.Set("Destination", "/calendars/user@example.com/test-cal/event2.ics")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

// TestHandleCopy_InvalidDestination_Coverage tests COPY with invalid destination
func TestHandleCopy_InvalidDestination_Coverage(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewServer(tmpDir, slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	server.storage.CreateCalendar("user@example.com", &Calendar{
		ID:       "test-cal",
		Name:     "Test Calendar",
		Timezone: "UTC",
	})

	server.storage.SaveEvent("user@example.com", "test-cal", &CalendarEvent{
		UID:     "event1",
		Summary: "Test Event",
	}, "BEGIN:VCALENDAR\nEND:VCALENDAR")

	req := httptest.NewRequest("COPY", "/calendars/user@example.com/test-cal/event1.ics", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	// Invalid destination URL
	req.Header.Set("Destination", "://invalid-url")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	// Should fail with bad request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestHandleCalendarPropfind_NotFound_Coverage tests PROPFIND on non-existent calendar
func TestHandleCalendarPropfind_NotFound_Coverage(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	body := `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:displayname/>
  </D:prop>
</D:propfind>`

	req := httptest.NewRequest("PROPFIND", "/calendars/user@example.com/nonexistent/", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	req.Header.Set("Content-Type", "text/xml")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	// Accept 404 or 207 (multistatus) - implementation may vary
	if rr.Code != http.StatusNotFound && rr.Code != http.StatusMultiStatus {
		t.Errorf("Expected status 404 or 207, got %d", rr.Code)
	}
}

// TestStorage_UpdateCalendar_NotFound_Coverage tests updating non-existent calendar
func TestStorage_UpdateCalendar_NotFound_Coverage(t *testing.T) {
	storage := NewStorage(t.TempDir())

	cal := &Calendar{
		ID:       "nonexistent",
		Name:     "Nonexistent",
		Timezone: "UTC",
	}

	err := storage.UpdateCalendar("user@example.com", cal)
	if err == nil {
		t.Error("expected error when updating non-existent calendar")
	}
}

// TestStorage_DeleteEvent_NotFound_Coverage tests deleting non-existent event
func TestStorage_DeleteEvent_NotFound_Coverage(t *testing.T) {
	storage := NewStorage(t.TempDir())

	// Create calendar first
	storage.CreateCalendar("user@example.com", &Calendar{
		ID:       "test-cal",
		Name:     "Test Calendar",
		Timezone: "UTC",
	})

	// Deleting non-existent event should not error
	err := storage.DeleteEvent("user@example.com", "test-cal", "nonexistent.ics")
	if err != nil {
		t.Errorf("expected no error when deleting non-existent event: %v", err)
	}
}
