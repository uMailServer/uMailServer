package caldav

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHandleMove_MissingDestinationCalendar verifies that MOVE returns 412
// when the destination calendar does not exist (RFC 4791 §7.5).
func TestHandleMove_MissingDestinationCalendar(t *testing.T) {
	storage := NewStorage(t.TempDir())
	server := &Server{storage: storage, logger: slog.Default()}

	event := &CalendarEvent{UID: "event-1", Modified: time.Now()}
	icsData := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:event-1\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

	// Create the source calendar so GetEvent finds the event
	if err := storage.CreateCalendar("alice", &Calendar{ID: "src-cal", Name: "Source"}); err != nil {
		t.Fatalf("failed to create source calendar: %v", err)
	}
	if err := storage.SaveEvent("alice", "src-cal", event, icsData); err != nil {
		t.Fatalf("failed to setup source: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("MOVE", "/dav/calendars/alice/src-cal/event-1", nil)
	// Note: URL path is /dav/calendars/{user}/{calendar}/{event} — 4 segments from "alice" onward.
	// handleMove splits strings.Trim("/dav/calendars/alice/src-cal/event-1", "/")
	// → [dav calendars alice src-cal event-1]; sourceCalendarID=alice, sourceEventUID=src-cal (wrong!)
	// Fixed: path is /dav/calendars/{calendar}/{event} (user comes from handleMove's username arg).
	r = httptest.NewRequest("MOVE", "/dav/calendars/src-cal/event-1", nil)
	r.Header.Set("Destination", "http://localhost/dav/calendars/missing-cal/event-1")

	server.handleMove(w, r, "alice")

	// RFC 4791 §7.5: 412 Precondition Failed when destination calendar doesn't exist
	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("MOVE to missing destination calendar: got status %d, want %d (412 Precondition Failed)",
			w.Code, http.StatusPreconditionFailed)
	}
}
