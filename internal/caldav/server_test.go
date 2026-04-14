package caldav

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	logger := slog.Default()
	server := NewServer(t.TempDir(), logger)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	if server.storage == nil {
		t.Error("Server storage should not be nil")
	}

	if server.logger == nil {
		t.Error("Server logger should not be nil")
	}
}

func TestNewServerWithNilLogger(t *testing.T) {
	server := NewServer(t.TempDir(), nil)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	if server.logger == nil {
		t.Error("Server logger should use default when nil")
	}
}

func TestSetAuthFunc(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	authFunc := func(username, password string) (bool, error) {
		return username == "test" && password == "pass", nil
	}

	server.SetAuthFunc(authFunc)

	if server.authFunc == nil {
		t.Error("AuthFunc should be set")
	}
}

func TestServeHTTP_AuthRequired(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	req := httptest.NewRequest("OPTIONS", "/", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	authHeader := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(authHeader, "Basic") {
		t.Errorf("WWW-Authenticate = %s, should contain Basic", authHeader)
	}
}

func TestServeHTTP_InvalidAuth(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return false, nil
	})

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wrong:creds")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestServeHTTP_ValidAuth(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleOptions(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "PROPFIND") {
		t.Errorf("Allow header = %s, should contain PROPFIND", allow)
	}

	dav := w.Header().Get("DAV")
	if !strings.Contains(dav, "calendar-access") {
		t.Errorf("DAV header = %s, should contain calendar-access", dav)
	}
}

func TestHandlePropfind_Root(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/xml") {
		t.Errorf("Content-Type = %s, want application/xml", contentType)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "multistatus") {
		t.Error("Response should contain multistatus")
	}
}

func TestHandlePropfind_WithCalendars(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create a calendar
	cal := &Calendar{
		ID:   "test-cal",
		Name: "Test Calendar",
	}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "Test Calendar") {
		t.Error("Response should contain calendar name")
	}
}

func TestHandleMkCalendar(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Note: Implementation uses parts[2] as calendarID
	// Path format: /dav/calendars/{calendarID}
	req := httptest.NewRequest("MKCALENDAR", "/dav/calendars/new-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}

	// Verify calendar was created
	cal, err := server.storage.GetCalendar("user@example.com", "new-cal")
	if err != nil || cal == nil {
		t.Error("Calendar should have been created")
	}
}

func TestHandlePut(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar first
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:test-event-123
SUMMARY:Test Event
DTSTART:20260403T100000Z
DTEND:20260403T110000Z
END:VEVENT
END:VCALENDAR`

	// Note: Implementation uses parts[2] as calendarID, parts[3] as eventUID
	// Path format: /dav/calendars/{calendarID}/{eventUID}
	req := httptest.NewRequest("PUT", "/dav/calendars/test-cal/test-event-123", strings.NewReader(icsData))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}

	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Error("ETag header should be set")
	}

	// Verify event was saved
	event, _ := server.storage.GetEvent("user@example.com", "test-cal", "test-event-123")
	if event == "" {
		t.Error("Event should have been saved")
	}
}

func TestHandlePut_InvalidData(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar first
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	invalidData := "This is not iCalendar data"

	req := httptest.NewRequest("PUT", "/dav/calendars/test-cal/event", strings.NewReader(invalidData))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleGet(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event-123
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event-123"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	req := httptest.NewRequest("GET", "/dav/calendars/test-cal/test-event-123", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/calendar") {
		t.Errorf("Content-Type = %s, want text/calendar", contentType)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "Test Event") {
		t.Error("Response should contain event data")
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("GET", "/dav/calendars/test-cal/nonexistent", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDelete(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event-123
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event-123"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	req := httptest.NewRequest("DELETE", "/dav/calendars/test-cal/test-event-123", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify event was deleted
	eventData, _ := server.storage.GetEvent("user@example.com", "test-cal", "test-event-123")
	if eventData != "" {
		t.Error("Event should have been deleted")
	}
}

func TestHandleReport(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and events
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	for i := 0; i < 3; i++ {
		icsData := `BEGIN:VCALENDAR
UID:event-` + string(rune('a'+i)) + `
SUMMARY:Event ` + string(rune('A'+i)) + `
END:VCALENDAR`
		event := &CalendarEvent{UID: "event-" + string(rune('a'+i))}
		server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)
	}

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<calendar-query xmlns="urn:ietf:params:xml:ns:caldav">
<prop><calendar-data/></prop>
<filter><comp-filter name="VCALENDAR"/></filter>
</calendar-query>`

	req := httptest.NewRequest("REPORT", "/dav/calendars/user@example.com/test-cal", strings.NewReader(reportBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "multistatus") {
		t.Error("Response should contain multistatus")
	}
}

func TestHandleReport_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<calendar-query xmlns="urn:ietf:params:xml:ns:caldav">
<prop><calendar-data/></prop>
</calendar-query>`

	req := httptest.NewRequest("REPORT", "/dav/invalid", strings.NewReader(reportBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleMkCol(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("MKCOL", "/dav/calendars/user@example.com/mkcol-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestHandleProppatch(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	req := httptest.NewRequest("PROPPATCH", "/dav/calendars/test-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleProppatch_NotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("PROPPATCH", "/dav/calendars/user@example.com/nonexistent", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleMove(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event-123
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event-123"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	req := httptest.NewRequest("MOVE", "/dav/calendars/test-cal/test-event-123", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/test-cal/moved-event")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify original is gone
	original, _ := server.storage.GetEvent("user@example.com", "test-cal", "test-event-123")
	if original != "" {
		t.Error("Original event should be gone")
	}

	// Verify new exists
	moved, _ := server.storage.GetEvent("user@example.com", "test-cal", "moved-event")
	if moved == "" {
		t.Error("Moved event should exist")
	}
}

func TestHandleMove_MissingDestination(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("MOVE", "/dav/calendars/test-cal/event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCopy(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event-123
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event-123"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	req := httptest.NewRequest("COPY", "/dav/calendars/test-cal/test-event-123", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/test-cal/copied-event")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify original still exists
	original, _ := server.storage.GetEvent("user@example.com", "test-cal", "test-event-123")
	if original == "" {
		t.Error("Original event should still exist")
	}

	// Verify copy exists
	copied, _ := server.storage.GetEvent("user@example.com", "test-cal", "copied-event")
	if copied == "" {
		t.Error("Copied event should exist")
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestExtractUIDFromICS(t *testing.T) {
	tests := []struct {
		name    string
		icsData string
		want    string
	}{
		{
			name: "UID present",
			icsData: `BEGIN:VCALENDAR
UID:test-uid-123
SUMMARY:Test
END:VCALENDAR`,
			want: "test-uid-123",
		},
		{
			name: "UID absent",
			icsData: `BEGIN:VCALENDAR
SUMMARY:Test
END:VCALENDAR`,
			want: "",
		},
		{
			name:    "Empty data",
			icsData: "",
			want:    "",
		},
		{
			name: "UID with special chars",
			icsData: `BEGIN:VCALENDAR
UID:test-uid-123@example.com
SUMMARY:Test
END:VCALENDAR`,
			want: "test-uid-123@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUIDFromICS(tt.icsData)
			if got != tt.want {
				t.Errorf("extractUIDFromICS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPrincipalResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	resp := server.buildPrincipalResponse("user@example.com")

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	found := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" {
			found = true
			if prop.Value != "user@example.com" {
				t.Errorf("displayname = %s, want user@example.com", prop.Value)
			}
		}
	}
	if !found {
		t.Error("displayname property not found")
	}
}

func TestBuildCalendarHomeResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	resp := server.buildCalendarHomeResponse("user@example.com")

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	found := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" {
			found = true
			if prop.Value != "Calendars" {
				t.Errorf("displayname = %s, want Calendars", prop.Value)
			}
		}
	}
	if !found {
		t.Error("displayname property not found")
	}
}

func TestBuildCalendarResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	cal := &Calendar{
		ID:          "test-cal",
		Name:        "Test Calendar",
		Description: "A test calendar",
	}

	resp := server.buildCalendarResponse("user@example.com", cal)

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	hasName := false
	hasDesc := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" && prop.Value == "Test Calendar" {
			hasName = true
		}
		if prop.XMLName.Local == "calendar-description" && prop.Value == "A test calendar" {
			hasDesc = true
		}
	}

	if !hasName {
		t.Error("displayname property not found or incorrect")
	}
	if !hasDesc {
		t.Error("calendar-description property not found or incorrect")
	}
}

func TestBuildEventResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	icsData := `BEGIN:VCALENDAR
UID:test-event
SUMMARY:Test Event
END:VCALENDAR`

	resp := server.buildEventResponse("user@example.com", "test-cal", "test-event", icsData)

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	hasCalendarData := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "calendar-data" {
			hasCalendarData = true
			if prop.Value != icsData {
				t.Error("calendar-data should match icsData")
			}
		}
	}

	if !hasCalendarData {
		t.Error("calendar-data property not found")
	}
}

func TestXMLStructures(t *testing.T) {
	// Test Propfind marshaling/unmarshaling
	propfind := Propfind{}
	data, err := xml.Marshal(propfind)
	if err != nil {
		t.Errorf("Failed to marshal Propfind: %v", err)
	}
	if len(data) == 0 {
		t.Error("Marshaled Propfind should not be empty")
	}

	// Test Multistatus marshaling
	multistatus := &Multistatus{
		Responses: []Response{
			{
				Href: "/test",
				Propstat: []Propstat{{
					Status: "HTTP/1.1 200 OK",
					Prop: []Property{
						{XMLName: xml.Name{Local: "displayname"}, Value: "Test"},
					},
				}},
			},
		},
	}

	data, err = xml.MarshalIndent(multistatus, "", "  ")
	if err != nil {
		t.Errorf("Failed to marshal Multistatus: %v", err)
	}

	var unmarshaled Multistatus
	if err := xml.Unmarshal(data, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal Multistatus: %v", err)
	}

	if len(unmarshaled.Responses) != 1 {
		t.Errorf("Expected 1 response, got %d", len(unmarshaled.Responses))
	}
}

func TestHandlePropfind_WithBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	propfindBody := `<?xml version="1.0" encoding="utf-8"?>
<propfind xmlns="DAV:">
<prop>
<displayname/>
<resourcetype/>
</prop>
</propfind>`

	req := httptest.NewRequest("PROPFIND", "/dav/", bytes.NewReader([]byte(propfindBody)))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestHandlePut_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	icsData := `BEGIN:VCALENDAR
UID:test
END:VCALENDAR`

	// Path with only 3 parts (missing event UID)
	req := httptest.NewRequest("PUT", "/dav/calendars/only-cal", strings.NewReader(icsData))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGet_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 3 parts (missing event UID)
	req := httptest.NewRequest("GET", "/dav/calendars/only-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDelete_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 3 parts (missing event UID)
	req := httptest.NewRequest("DELETE", "/dav/calendars/only-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleMkCalendar_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 2 parts (missing calendar ID)
	req := httptest.NewRequest("MKCALENDAR", "/dav/calendars/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleProppatch_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 2 parts (missing calendar ID)
	req := httptest.NewRequest("PROPPATCH", "/dav/calendars/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleMove_InvalidSourcePath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 3 parts (missing event UID)
	req := httptest.NewRequest("MOVE", "/dav/calendars/only-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/other-cal/event")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleMove_InvalidDestinationPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	req := httptest.NewRequest("MOVE", "/dav/calendars/test-cal/event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	// Destination with only 3 parts (missing event UID)
	req.Header.Set("Destination", "/dav/calendars/other-cal")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCopy_InvalidSourcePath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 3 parts (missing event UID)
	req := httptest.NewRequest("COPY", "/dav/calendars/only-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/other-cal/event")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCopy_SourceNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("COPY", "/dav/calendars/test-cal/nonexistent", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/test-cal/copy")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSendError(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	w := httptest.NewRecorder()

	server.sendError(w, http.StatusInternalServerError, "test error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "test error" {
		t.Errorf("Body = %s, want test error", string(body))
	}
}

func TestCalendarEventStruct(t *testing.T) {
	event := CalendarEvent{
		UID:         "test-uid",
		Summary:     "Test Summary",
		Description: "Test Description",
		Location:    "Test Location",
		Organizer:   "organizer@example.com",
		Attendees:   []string{"attendee1@example.com", "attendee2@example.com"},
		Recurrence:  "FREQ=DAILY",
		AllDay:      true,
	}

	if event.UID != "test-uid" {
		t.Error("UID mismatch")
	}
	if event.Summary != "Test Summary" {
		t.Error("Summary mismatch")
	}
	if !event.AllDay {
		t.Error("AllDay should be true")
	}
	if len(event.Attendees) != 2 {
		t.Errorf("Attendees count = %d, want 2", len(event.Attendees))
	}
}

func TestCalendarStruct(t *testing.T) {
	cal := Calendar{
		ID:          "test-id",
		Name:        "Test Calendar",
		Description: "Test Description",
		Color:       "#FF0000",
		Timezone:    "Europe/Istanbul",
		ReadOnly:    true,
	}

	if cal.ID != "test-id" {
		t.Error("ID mismatch")
	}
	if cal.Name != "Test Calendar" {
		t.Error("Name mismatch")
	}
	if cal.Color != "#FF0000" {
		t.Error("Color mismatch")
	}
	if !cal.ReadOnly {
		t.Error("ReadOnly should be true")
	}
}

func TestHandleCalendarPropfind_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	multistatus := &Multistatus{}
	server.handleCalendarPropfind("/invalid", "user@example.com", multistatus)

	// Should not panic and not add any responses
	if len(multistatus.Responses) != 0 {
		t.Error("Should not add responses for invalid path")
	}
}

func TestHandleCalendarPropfind_NonExistentCalendar(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	multistatus := &Multistatus{}
	server.handleCalendarPropfind("/dav/calendars/user@example.com/nonexistent", "user@example.com", multistatus)

	// Should not panic and not add responses
	if len(multistatus.Responses) != 0 {
		t.Error("Should not add responses for non-existent calendar")
	}
}

func TestHandleCalendarPropfind_EmptyCalendarID(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	multistatus := &Multistatus{}
	// Test with empty calendar ID (parts[3] == "")
	server.handleCalendarPropfind("/dav/calendars/user@example.com/", "user@example.com", multistatus)

	if len(multistatus.Responses) != 0 {
		t.Error("Should not add responses for empty calendar ID")
	}
}

func TestHandleCalendarPropfind_WithEvent(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create a calendar first
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	err := server.storage.CreateCalendar("user@example.com", cal)
	if err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	// Save an event
	eventData := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:test-event-1
SUMMARY:Test Event
END:VEVENT
END:VCALENDAR`
	event := &CalendarEvent{
		UID:     "test-event-1",
		Summary: "Test Event",
	}
	err = server.storage.SaveEvent("user@example.com", "test-cal", event, eventData)
	if err != nil {
		t.Fatalf("Failed to save event: %v", err)
	}

	multistatus := &Multistatus{}
	// Test with event path
	server.handleCalendarPropfind("/dav/calendars/user@example.com/test-cal/test-event-1", "user@example.com", multistatus)

	// Should have one response for the event
	if len(multistatus.Responses) != 1 {
		t.Errorf("Expected 1 response, got %d", len(multistatus.Responses))
	}
}

func TestHandleCalendarPropfind_NonExistentEvent(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create a calendar
	cal := &Calendar{ID: "test-cal", Name: "Test Calendar"}
	err := server.storage.CreateCalendar("user@example.com", cal)
	if err != nil {
		t.Fatalf("Failed to create calendar: %v", err)
	}

	multistatus := &Multistatus{}
	// Test with non-existent event path
	server.handleCalendarPropfind("/dav/calendars/user@example.com/test-cal/nonexistent-event", "user@example.com", multistatus)

	// Should have no responses (event not found)
	if len(multistatus.Responses) != 0 {
		t.Errorf("Expected 0 responses for non-existent event, got %d", len(multistatus.Responses))
	}
}

func TestHandleReport_InvalidQuery(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	invalidBody := "This is not valid XML"

	req := httptest.NewRequest("REPORT", "/dav/calendars/user@example.com/test-cal", strings.NewReader(invalidBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePut_InvalidCalendarData(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar first
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	// Send PUT with invalid calendar data (missing BEGIN:VCALENDAR)
	invalidICS := "SUMMARY:Test Event\nDTSTART:20240101T120000Z"
	req := httptest.NewRequest("PUT", "/dav/calendars/user@example.com/test-cal/event1.ics", strings.NewReader(invalidICS))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleDelete_NotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Try to delete non-existent calendar - should return not found or succeed (idempotent)
	req := httptest.NewRequest("DELETE", "/dav/calendars/user@example.com/nonexistent/event1.ics", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// DELETE on non-existent resource typically returns 404 or 204 (No Content)
	if w.Code != http.StatusNotFound && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusNotFound, http.StatusNoContent)
	}
}

func TestHandleMkCalendar_AlreadyExists(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar first
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	// Try to create same calendar again - storage overwrites existing
	body := `<?xml version="1.0" encoding="UTF-8"?>
<CALDAV:mkcalendar xmlns:CALDAV="urn:ietf:params:xml:ns:caldav">
  <D:set>
    <D:prop>
      <D:displayname>Test Calendar</D:displayname>
    </D:prop>
  </D:set>
</CALDAV:mkcalendar>`

	req := httptest.NewRequest("MKCALENDAR", "/dav/calendars/user@example.com/test-cal", strings.NewReader(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// CreateCalendar overwrites existing, so this returns 201 Created
	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestHandlePropfind_InvalidBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Send invalid XML body - should still work (falls back to allprop)
	body := "invalid xml <"

	req := httptest.NewRequest("PROPFIND", "/dav/calendars/user@example.com", strings.NewReader(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestHandleReport_EmptyCalendar(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar but no events
	cal := &Calendar{ID: "empty-cal", Name: "Empty Calendar"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<calendar-query xmlns="urn:ietf:params:xml:ns:caldav">
<prop><calendar-data/></prop>
<filter><comp-filter name="VCALENDAR"/></filter>
</calendar-query>`

	req := httptest.NewRequest("REPORT", "/dav/calendars/user@example.com/empty-cal", strings.NewReader(reportBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "multistatus") {
		t.Error("Response should contain multistatus")
	}
}

func TestHandleReport_EmptyBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	// Send empty body
	req := httptest.NewRequest("REPORT", "/dav/calendars/user@example.com/test-cal", strings.NewReader(""))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Empty body should cause bad request
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePropfind_Depth0(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	req := httptest.NewRequest("PROPFIND", "/dav/calendars/user@example.com/test-cal", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "0")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestHandlePropfind_DepthInfinity(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create calendar with event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	req := httptest.NewRequest("PROPFIND", "/dav/calendars/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "infinity")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestHandlePut_NoCalendar(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Try to PUT without creating calendar first - storage creates event anyway
	icsData := `BEGIN:VCALENDAR
UID:test-event
SUMMARY:Test Event
END:VCALENDAR`

	// Path with non-existent calendar
	req := httptest.NewRequest("PUT", "/dav/calendars/nonexistent-cal/test-event", strings.NewReader(icsData))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Storage creates event regardless - returns 201
	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestHandleDelete_CalendarNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Try to delete event from non-existent calendar - returns 204 (idempotent)
	req := httptest.NewRequest("DELETE", "/dav/calendars/nonexistent-cal/event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleMkCalendar_InvalidPathTooShort(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path with only 2 parts
	req := httptest.NewRequest("MKCALENDAR", "/dav/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCopy_CalendarNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Try to copy from non-existent calendar
	req := httptest.NewRequest("COPY", "/dav/calendars/nonexistent-cal/event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/other-cal/copy")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusNotFound, http.StatusBadRequest)
	}
}

func TestHandleMove_CalendarNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Try to move from non-existent calendar
	req := httptest.NewRequest("MOVE", "/dav/calendars/nonexistent-cal/event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/other-cal/move")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusNotFound, http.StatusBadRequest)
	}
}

func TestHandleMove_DestinationCalendarNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create source calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	// Try to move to non-existent destination calendar - returns 204
	req := httptest.NewRequest("MOVE", "/dav/calendars/test-cal/test-event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/nonexistent-cal/move")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Storage creates at destination regardless
	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleCopy_DestinationCalendarNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create source calendar and event
	cal := &Calendar{ID: "test-cal", Name: "Test"}
	_ = server.storage.CreateCalendar("user@example.com", cal)

	icsData := `BEGIN:VCALENDAR
UID:test-event
SUMMARY:Test Event
END:VCALENDAR`
	event := &CalendarEvent{UID: "test-event"}
	server.storage.SaveEvent("user@example.com", "test-cal", event, icsData)

	// Try to copy to non-existent destination calendar - returns 204
	req := httptest.NewRequest("COPY", "/dav/calendars/test-cal/test-event", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/calendars/nonexistent-cal/copy")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Storage creates at destination regardless
	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
}
