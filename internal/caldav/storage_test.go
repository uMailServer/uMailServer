package caldav

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStorage(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	if storage == nil {
		t.Fatal("NewStorage returned nil")
	}

	if storage.dataDir == "" {
		t.Error("dataDir should not be empty")
	}
}

func TestCreateCalendar(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	tests := []struct {
		name     string
		username string
		calendar *Calendar
		wantErr  bool
	}{
		{
			name:     "create new calendar with ID",
			username: "user1@example.com",
			calendar: &Calendar{
				ID:          "personal",
				Name:        "Personal Calendar",
				Description: "My personal calendar",
				Timezone:    "UTC",
			},
			wantErr: false,
		},
		{
			name:     "create calendar without ID generates UUID",
			username: "user1@example.com",
			calendar: &Calendar{
				Name: "No ID Calendar",
			},
			wantErr: false,
		},
		{
			name:     "create calendar with special chars in username",
			username: "user+test@example.com",
			calendar: &Calendar{
				ID:   "work",
				Name: "Work Calendar",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.CreateCalendar(tt.username, tt.calendar)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateCalendar() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.calendar.ID == "" {
				t.Error("Calendar ID should have been generated")
			}

			if tt.calendar.Created.IsZero() {
				t.Error("Calendar Created time should be set")
			}

			if tt.calendar.Modified.IsZero() {
				t.Error("Calendar Modified time should be set")
			}
		})
	}
}

func TestGetCalendar(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create a calendar first
	cal := &Calendar{
		ID:          "test-cal",
		Name:        "Test Calendar",
		Description: "Test Description",
	}
	err := storage.CreateCalendar("user1@example.com", cal)
	if err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	tests := []struct {
		name       string
		username   string
		calendarID string
		wantNil    bool
		wantErr    bool
	}{
		{
			name:       "get existing calendar",
			username:   "user1@example.com",
			calendarID: "test-cal",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name:       "get non-existent calendar",
			username:   "user1@example.com",
			calendarID: "nonexistent",
			wantNil:    true,
			wantErr:    false,
		},
		{
			name:       "get from non-existent user",
			username:   "user2@example.com",
			calendarID: "test-cal",
			wantNil:    true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storage.GetCalendar(tt.username, tt.calendarID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCalendar() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantNil && got != nil {
				t.Errorf("GetCalendar() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("GetCalendar() = nil, want non-nil")
			}
		})
	}
}

func TestGetCalendars(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Initially should return empty slice
	cals, err := storage.GetCalendars("user1@example.com")
	if err != nil {
		t.Errorf("GetCalendars() error = %v", err)
	}
	if len(cals) != 0 {
		t.Errorf("GetCalendars() = %d, want 0", len(cals))
	}

	// Create some calendars
	calendars := []*Calendar{
		{ID: "cal1", Name: "Calendar 1"},
		{ID: "cal2", Name: "Calendar 2"},
		{ID: "cal3", Name: "Calendar 3"},
	}

	for _, cal := range calendars {
		if err := storage.CreateCalendar("user1@example.com", cal); err != nil {
			t.Fatalf("Failed to create calendar: %v", err)
		}
	}

	// Now should return all calendars
	cals, err = storage.GetCalendars("user1@example.com")
	if err != nil {
		t.Errorf("GetCalendars() error = %v", err)
	}
	if len(cals) != 3 {
		t.Errorf("GetCalendars() = %d, want 3", len(cals))
	}
}

func TestUpdateCalendar(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create a calendar
	cal := &Calendar{
		ID:          "test-cal",
		Name:        "Original Name",
		Description: "Original Description",
	}
	if err := storage.CreateCalendar("user1@example.com", cal); err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Ensure time difference

	// Update the calendar
	cal.Name = "Updated Name"
	cal.Description = "Updated Description"
	if err := storage.UpdateCalendar("user1@example.com", cal); err != nil {
		t.Errorf("UpdateCalendar() error = %v", err)
	}

	// Verify update
	updated, err := storage.GetCalendar("user1@example.com", "test-cal")
	if err != nil {
		t.Fatalf("Failed to get calendar: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Name = %s, want Updated Name", updated.Name)
	}

	if updated.Description != "Updated Description" {
		t.Errorf("Description = %s, want Updated Description", updated.Description)
	}

	if !updated.Modified.After(updated.Created) {
		t.Error("Modified time should be after Created time")
	}
}

func TestDeleteCalendar(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create a calendar
	cal := &Calendar{ID: "to-delete", Name: "To Delete"}
	if err := storage.CreateCalendar("user1@example.com", cal); err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	// Verify it exists
	_, err := storage.GetCalendar("user1@example.com", "to-delete")
	if err != nil {
		t.Fatalf("Calendar should exist: %v", err)
	}

	// Delete it
	if err := storage.DeleteCalendar("user1@example.com", "to-delete"); err != nil {
		t.Errorf("DeleteCalendar() error = %v", err)
	}

	// Verify it's gone
	_, err = storage.GetCalendar("user1@example.com", "to-delete")
	if err != nil {
		t.Errorf("Calendar should have been deleted, got error: %v", err)
	}
}

func TestSaveEvent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create a calendar first
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	if err := storage.CreateCalendar("user1@example.com", cal); err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	icsData := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:test-event-123
SUMMARY:Test Event
DTSTART:20260403T100000Z
DTEND:20260403T110000Z
END:VEVENT
END:VCALENDAR`

	event := &CalendarEvent{
		UID:     "test-event-123",
		Summary: "Test Event",
	}

	err := storage.SaveEvent("user1@example.com", "test-cal", event, icsData)
	if err != nil {
		t.Errorf("SaveEvent() error = %v", err)
	}

	// Verify event was saved
	savedData, err := storage.GetEvent("user1@example.com", "test-cal", "test-event-123")
	if err != nil {
		t.Errorf("GetEvent() error = %v", err)
	}
	if savedData == "" {
		t.Error("Event should have been saved")
	}
}

func TestGetEvent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	storage.CreateCalendar("user1@example.com", cal)

	icsData := "BEGIN:VCALENDAR\nUID:test-event\nEND:VCALENDAR"
	event := &CalendarEvent{UID: "test-event"}
	storage.SaveEvent("user1@example.com", "test-cal", event, icsData)

	tests := []struct {
		name       string
		username   string
		calendarID string
		eventUID   string
		wantData   bool
		wantErr    bool
	}{
		{
			name:       "get existing event",
			username:   "user1@example.com",
			calendarID: "test-cal",
			eventUID:   "test-event",
			wantData:   true,
			wantErr:    false,
		},
		{
			name:       "get non-existent event",
			username:   "user1@example.com",
			calendarID: "test-cal",
			eventUID:   "nonexistent",
			wantData:   false,
			wantErr:    false,
		},
		{
			name:       "get from non-existent calendar",
			username:   "user1@example.com",
			calendarID: "nonexistent",
			eventUID:   "test-event",
			wantData:   false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storage.GetEvent(tt.username, tt.calendarID, tt.eventUID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantData && got == "" {
				t.Error("GetEvent() returned empty data, want non-empty")
			}
			if !tt.wantData && got != "" {
				t.Errorf("GetEvent() = %v, want empty", got)
			}
		})
	}
}

func TestGetEvents(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create calendar
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	storage.CreateCalendar("user1@example.com", cal)

	// Initially should return empty
	events, err := storage.GetEvents("user1@example.com", "test-cal")
	if err != nil {
		t.Errorf("GetEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("GetEvents() = %d, want 0", len(events))
	}

	// Add some events
	for i := 0; i < 3; i++ {
		icsData := `BEGIN:VCALENDAR
UID:event-` + string(rune('a'+i)) + `
SUMMARY:Event ` + string(rune('A'+i)) + `
END:VCALENDAR`
		event := &CalendarEvent{UID: "event-" + string(rune('a'+i))}
		storage.SaveEvent("user1@example.com", "test-cal", event, icsData)
	}

	// Now should return all events
	events, err = storage.GetEvents("user1@example.com", "test-cal")
	if err != nil {
		t.Errorf("GetEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Errorf("GetEvents() = %d, want 3", len(events))
	}
}

func TestDeleteEvent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	storage.CreateCalendar("user1@example.com", cal)

	icsData := "BEGIN:VCALENDAR\nUID:test-event\nEND:VCALENDAR"
	event := &CalendarEvent{UID: "test-event"}
	storage.SaveEvent("user1@example.com", "test-cal", event, icsData)

	// Delete the event
	if err := storage.DeleteEvent("user1@example.com", "test-cal", "test-event"); err != nil {
		t.Errorf("DeleteEvent() error = %v", err)
	}

	// Verify it's gone
	_, err := storage.GetEvent("user1@example.com", "test-cal", "test-event")
	if err != nil {
		t.Errorf("GetEvent() error = %v", err)
	}

	// Delete non-existent should not error
	if err := storage.DeleteEvent("user1@example.com", "test-cal", "nonexistent"); err != nil {
		t.Errorf("DeleteEvent() non-existent error = %v", err)
	}
}

func TestGetETag(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	storage.CreateCalendar("user1@example.com", cal)

	// Get ETag for non-existent event (should generate UUID)
	etag := storage.GetETag("user1@example.com", "test-cal", "nonexistent")
	if etag == "" {
		t.Error("GetETag() should return non-empty for non-existent")
	}

	// Create event
	icsData := "BEGIN:VCALENDAR\nUID:test-event\nEND:VCALENDAR"
	event := &CalendarEvent{UID: "test-event"}
	storage.SaveEvent("user1@example.com", "test-cal", event, icsData)

	// Get ETag for existing event
	etag = storage.GetETag("user1@example.com", "test-cal", "test-event")
	if etag == "" {
		t.Error("GetETag() should return non-empty")
	}

	// ETag should be based on modification time
	if !isValidETag(etag) {
		t.Errorf("GetETag() = %s, not a valid ETag", etag)
	}
}

func TestGetCalendarETag(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Get ETag for non-existent calendar
	etag := storage.GetCalendarETag("user1@example.com", "nonexistent")
	if etag == "" {
		t.Error("GetCalendarETag() should return non-empty for non-existent")
	}

	// Create calendar
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	storage.CreateCalendar("user1@example.com", cal)

	// Get ETag for existing calendar
	etag = storage.GetCalendarETag("user1@example.com", "test-cal")
	if etag == "" {
		t.Error("GetCalendarETag() should return non-empty")
	}

	if !isValidETag(etag) {
		t.Errorf("GetCalendarETag() = %s, not a valid ETag", etag)
	}
}

func TestUserDirSanitization(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	tests := []struct {
		username string
		want     string
	}{
		{
			username: "user@example.com",
			want:     "user_at_example.com",
		},
		{
			username: "plainuser",
			want:     "plainuser",
		},
		{
			username: "user+tag@example.com",
			want:     "user+tag_at_example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			got := storage.userDir(tt.username)
			wantPath := filepath.Join(storage.dataDir, tt.want)
			if got != wantPath {
				t.Errorf("userDir() = %v, want %v", got, wantPath)
			}
		})
	}
}

func TestCalendarPathFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	username := "user@example.com"
	calendarID := "my-calendar"
	eventUID := "event-123"

	// Test calendarDir
	calDir := storage.calendarDir(username, calendarID)
	wantCalDir := filepath.Join(storage.dataDir, "user_at_example.com", calendarID)
	if calDir != wantCalDir {
		t.Errorf("calendarDir() = %v, want %v", calDir, wantCalDir)
	}

	// Test eventPath
	eventPath := storage.eventPath(username, calendarID, eventUID)
	wantEventPath := filepath.Join(calDir, eventUID+".ics")
	if eventPath != wantEventPath {
		t.Errorf("eventPath() = %v, want %v", eventPath, wantEventPath)
	}

	// Test calendarPath
	calPath := storage.calendarPath(username, calendarID)
	wantCalPath := filepath.Join(calDir, ".calendar.json")
	if calPath != wantCalPath {
		t.Errorf("calendarPath() = %v, want %v", calPath, wantCalPath)
	}
}

func isValidETag(etag string) bool {
	return len(etag) > 2 && etag[0] == '"' && etag[len(etag)-1] == '"'
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create calendar
	cal := &Calendar{ID: "concurrent-cal", Name: "Concurrent Test"}
	if err := storage.CreateCalendar("user@example.com", cal); err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			icsData := `BEGIN:VCALENDAR
UID:event-` + string(rune('a'+idx)) + `
SUMMARY:Event ` + string(rune('A'+idx)) + `
END:VCALENDAR`
			event := &CalendarEvent{UID: "event-" + string(rune('a'+idx))}
			storage.SaveEvent("user@example.com", "concurrent-cal", event, icsData)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all events were saved
	events, err := storage.GetEvents("user@example.com", "concurrent-cal")
	if err != nil {
		t.Errorf("GetEvents() error = %v", err)
	}
	if len(events) != 10 {
		t.Errorf("Got %d events, want 10", len(events))
	}
}

func TestCalendarEventStorage(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	cal := &Calendar{ID: "event-test", Name: "Event Test"}
	storage.CreateCalendar("user@example.com", cal)

	// Test with complex iCalendar data
	icsData := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//Test//EN
BEGIN:VEVENT
UID:complex-event-123
DTSTAMP:20260403T120000Z
DTSTART;TZID=Europe/Istanbul:20260403T140000
DTEND;TZID=Europe/Istanbul:20260403T150000
SUMMARY:Complex Event
DESCRIPTION:This is a multi-line
description with special chars: <>&"
LOCATION:Test Location
ORGANIZER:mailto:organizer@example.com
ATTENDEE:mailto:attendee1@example.com
ATTENDEE:mailto:attendee2@example.com
STATUS:CONFIRMED
PRIORITY:1
END:VEVENT
END:VCALENDAR`

	event := &CalendarEvent{
		UID:         "complex-event-123",
		Summary:     "Complex Event",
		Description: "This is a multi-line description",
		Location:    "Test Location",
		Organizer:   "organizer@example.com",
		Attendees:   []string{"attendee1@example.com", "attendee2@example.com"},
	}

	if err := storage.SaveEvent("user@example.com", "event-test", event, icsData); err != nil {
		t.Errorf("SaveEvent() error = %v", err)
	}

	// Verify data integrity
	retrieved, err := storage.GetEvent("user@example.com", "event-test", "complex-event-123")
	if err != nil {
		t.Errorf("GetEvent() error = %v", err)
	}

	if retrieved != icsData {
		t.Error("Retrieved data doesn't match original")
	}
}

func TestStorageWithRealFilesystem(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create multiple users with multiple calendars
	users := []string{"alice@example.com", "bob@example.com"}
	for _, user := range users {
		for i := 0; i < 3; i++ {
			cal := &Calendar{
				ID:   fmt.Sprintf("cal-%d", i),
				Name: fmt.Sprintf("Calendar %d", i),
			}
			if err := storage.CreateCalendar(user, cal); err != nil {
				t.Errorf("Failed to create calendar for %s: %v", user, err)
			}
		}
	}

	// Verify filesystem structure
	for _, user := range users {
		userPath := storage.userDir(user)
		if _, err := os.Stat(userPath); os.IsNotExist(err) {
			t.Errorf("User directory should exist: %s", userPath)
		}
	}
}
