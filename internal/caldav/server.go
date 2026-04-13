// Package caldav provides CalDAV (RFC 4791) calendar synchronization support
package caldav

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Server represents a CalDAV server
type Server struct {
	logger   *slog.Logger
	authFunc func(username, password string) (bool, error)
	dataDir  string
	storage  *Storage
}

// NewServer creates a new CalDAV server
func NewServer(dataDir string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		logger:  logger,
		dataDir: dataDir,
		storage: NewStorage(dataDir),
	}
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(username, password string) (bool, error)) {
	s.authFunc = fn
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authenticate request
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="CalDAV"`)
		s.sendError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if s.authFunc != nil {
		authenticated, err := s.authFunc(username, password)
		if err != nil || !authenticated {
			w.Header().Set("WWW-Authenticate", `Basic realm="CalDAV"`)
			s.sendError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
	}

	// Log request
	s.logger.Debug("CalDAV request",
		"method", r.Method,
		"path", r.URL.Path,
		"user", username,
	)

	// Route based on method
	switch r.Method {
	case "OPTIONS":
		s.handleOptions(w, r)
	case "PROPFIND":
		s.handlePropfind(w, r, username)
	case "REPORT":
		s.handleReport(w, r, username)
	case "PUT":
		s.handlePut(w, r, username)
	case "GET":
		s.handleGet(w, r, username)
	case "DELETE":
		s.handleDelete(w, r, username)
	case "MKCALENDAR":
		s.handleMkCalendar(w, r, username)
	case "MKCOL":
		s.handleMkCol(w, r, username)
	case "PROPPATCH":
		s.handleProppatch(w, r, username)
	case "MOVE":
		s.handleMove(w, r, username)
	case "COPY":
		s.handleCopy(w, r, username)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleOptions handles OPTIONS requests
func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "OPTIONS, GET, PUT, DELETE, PROPFIND, PROPPATCH, REPORT, MKCALENDAR, MKCOL, MOVE, COPY")
	w.Header().Set("DAV", "1, 2, 3, calendar-access, calendar-schedule")
	w.WriteHeader(http.StatusOK)
}

// handlePropfind handles PROPFIND requests
func (s *Server) handlePropfind(w http.ResponseWriter, r *http.Request, username string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	// Parse PROPFIND request
	var propfind Propfind
	if len(body) > 0 {
		if err := xml.Unmarshal(body, &propfind); err != nil {
			s.logger.Debug("Failed to parse PROPFIND", "error", err, "body", string(body))
			// Continue with empty propfind (allprop)
		}
	}

	// Determine depth
	depth := r.Header.Get("Depth")
	if depth == "" {
		depth = "1"
	}

	// Build response
	multistatus := &Multistatus{}

	// Root principal
	if r.URL.Path == "/" || r.URL.Path == "/dav/" {
		multistatus.Responses = append(multistatus.Responses, s.buildPrincipalResponse(username))
	}

	// Calendar home
	if r.URL.Path == "/" || r.URL.Path == "/dav/" || r.URL.Path == "/dav/calendars/" {
		multistatus.Responses = append(multistatus.Responses, s.buildCalendarHomeResponse(username))

		// Query actual calendars from storage
		calendars, err := s.storage.GetCalendars(username)
		if err == nil {
			for _, cal := range calendars {
				multistatus.Responses = append(multistatus.Responses, s.buildCalendarResponse(username, cal))
			}
		}
	}

	// Handle specific calendar or event path
	if strings.HasPrefix(r.URL.Path, "/dav/calendars/") {
		s.handleCalendarPropfind(r.URL.Path, username, multistatus)
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)

	output, _ := xml.MarshalIndent(multistatus, "", "  ")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(output)
}

// handleReport handles REPORT requests
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, username string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	// Parse calendar query
	var query CalendarQuery
	if err := xml.Unmarshal(body, &query); err != nil {
		s.logger.Debug("Failed to parse REPORT", "error", err)
		s.sendError(w, http.StatusBadRequest, "invalid calendar query")
		return
	}

	// Parse path to get calendar ID
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]

	// Build response
	multistatus := &Multistatus{}

	// Query actual events from storage
	events, err := s.storage.GetEvents(username, calendarID)
	if err == nil {
		for _, eventData := range events {
			uid := extractUIDFromICS(eventData)
			if uid != "" {
				multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, uid, eventData))
			}
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)

	output, _ := xml.MarshalIndent(multistatus, "", "  ")
	w.Write([]byte(xml.Header))
	w.Write(output)
}

// handlePut handles PUT requests for creating/updating events
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, username string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	icsData := string(body)

	// Validate iCalendar data
	if !strings.Contains(icsData, "BEGIN:VCALENDAR") {
		s.sendError(w, http.StatusUnsupportedMediaType, "invalid calendar data")
		return
	}

	// Parse path to get calendar ID and event UID
	// Format: /dav/calendars/{username}/{calendarID}/{eventUID}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]
	eventUID := parts[3]

	// Extract UID from ICS if available, otherwise use path
	uid := extractUIDFromICS(icsData)
	if uid == "" {
		uid = eventUID
	}

	// Create event
	event := &CalendarEvent{
		UID:      uid,
		Created:  time.Now(),
		Modified: time.Now(),
	}

	// Store the event
	if err := s.storage.SaveEvent(username, calendarID, event, icsData); err != nil {
		s.logger.Error("Failed to save event", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to save event")
		return
	}

	// Set ETag header
	etag := s.storage.GetETag(username, calendarID, uid)
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusCreated)
}

// handleGet handles GET requests for retrieving events
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, username string) {
	// Parse path
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]
	eventUID := parts[3]

	// Get event
	eventData, err := s.storage.GetEvent(username, calendarID, eventUID)
	if err != nil || eventData == "" {
		s.sendError(w, http.StatusNotFound, "event not found")
		return
	}

	// Set content type and ETag
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	etag := s.storage.GetETag(username, calendarID, eventUID)
	w.Header().Set("ETag", etag)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(eventData))
}

// handleDelete handles DELETE requests
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, username string) {
	// Parse path
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]
	eventUID := parts[3]

	// Delete event
	if err := s.storage.DeleteEvent(username, calendarID, eventUID); err != nil {
		s.logger.Error("Failed to delete event", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to delete event")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMkCalendar handles MKCALENDAR requests
func (s *Server) handleMkCalendar(w http.ResponseWriter, r *http.Request, username string) {
	// Parse path to get calendar ID
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]

	// Create default calendar
	cal := &Calendar{
		ID:          calendarID,
		Name:        "Calendar",
		Description: "Default calendar",
		Timezone:    "UTC",
	}

	if err := s.storage.CreateCalendar(username, cal); err != nil {
		s.logger.Error("Failed to create calendar", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to create calendar")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handleMkCol handles MKCOL requests
func (s *Server) handleMkCol(w http.ResponseWriter, r *http.Request, username string) {
	// For now, treat as MKCALENDAR
	s.handleMkCalendar(w, r, username)
}

// handleProppatch handles PROPPATCH requests
func (s *Server) handleProppatch(w http.ResponseWriter, r *http.Request, username string) {
	// Parse path
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]

	// Get calendar
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		s.sendError(w, http.StatusNotFound, "calendar not found")
		return
	}

	// For now, just return success without parsing PROPPATCH body
	w.WriteHeader(http.StatusOK)
}

// handleMove handles MOVE requests
func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, username string) {
	// Get source path
	sourceParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(sourceParts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid source path")
		return
	}

	sourceCalendarID := sourceParts[2]
	sourceEventUID := sourceParts[3]

	// Get destination from header
	destination := r.Header.Get("Destination")
	if destination == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destParts := strings.Split(strings.Trim(destination, "/"), "/")
	if len(destParts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destCalendarID := destParts[2]
	destEventUID := destParts[3]

	// Get event data
	eventData, err := s.storage.GetEvent(username, sourceCalendarID, sourceEventUID)
	if err != nil || eventData == "" {
		s.sendError(w, http.StatusNotFound, "source event not found")
		return
	}

	// Update UID if different
	if sourceEventUID != destEventUID {
		eventData = strings.Replace(eventData, "UID:"+sourceEventUID, "UID:"+destEventUID, 1)
	}

	// Create event at destination
	event := &CalendarEvent{
		UID:      destEventUID,
		Modified: time.Now(),
	}

	if err := s.storage.SaveEvent(username, destCalendarID, event, eventData); err != nil {
		s.logger.Error("Failed to save event at destination", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to move event")
		return
	}

	// Delete from source
	if err := s.storage.DeleteEvent(username, sourceCalendarID, sourceEventUID); err != nil {
		s.logger.Error("Failed to delete source event", "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCopy handles COPY requests
func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request, username string) {
	// Get source path
	sourceParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(sourceParts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid source path")
		return
	}

	sourceCalendarID := sourceParts[2]
	sourceEventUID := sourceParts[3]

	// Get destination from header
	destination := r.Header.Get("Destination")
	if destination == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destParts := strings.Split(strings.Trim(destination, "/"), "/")
	if len(destParts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destCalendarID := destParts[2]
	destEventUID := destParts[3]

	// Get event data
	eventData, err := s.storage.GetEvent(username, sourceCalendarID, sourceEventUID)
	if err != nil || eventData == "" {
		s.sendError(w, http.StatusNotFound, "source event not found")
		return
	}

	// Update UID if different
	if sourceEventUID != destEventUID {
		eventData = strings.Replace(eventData, "UID:"+sourceEventUID, "UID:"+destEventUID, 1)
	}

	// Create event at destination
	event := &CalendarEvent{
		UID:      destEventUID,
		Modified: time.Now(),
	}

	if err := s.storage.SaveEvent(username, destCalendarID, event, eventData); err != nil {
		s.logger.Error("Failed to save event at destination", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to copy event")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// buildPrincipalResponse builds a response for the principal resource
func (s *Server) buildPrincipalResponse(username string) Response {
	return Response{
		Href: fmt.Sprintf("/dav/principals/%s/", username),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "\n        <collection/>\n        <principal/>\n      "},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: username},
				{XMLName: xml.Name{Space: "CALDAV:", Local: "calendar-home-set"}, Value: fmt.Sprintf("<href>/dav/calendars/%s/</href>", username)},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// buildCalendarHomeResponse builds a response for the calendar home resource
func (s *Server) buildCalendarHomeResponse(username string) Response {
	return Response{
		Href: fmt.Sprintf("/dav/calendars/%s/", username),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "<collection/>"},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: "Calendars"},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// buildCalendarResponse builds a response for a calendar resource
func (s *Server) buildCalendarResponse(username string, cal *Calendar) Response {
	href := fmt.Sprintf("/dav/calendars/%s/%s/", username, cal.ID)
	etag := s.storage.GetCalendarETag(username, cal.ID)

	return Response{
		Href: href,
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "\n        <collection/>\n        <calendar xmlns=\"urn:ietf:params:xml:ns:caldav\"/>\n      "},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: cal.Name},
				{XMLName: xml.Name{Space: "DAV:", Local: "getetag"}, Value: etag},
				{XMLName: xml.Name{Space: "CALDAV:", Local: "calendar-description"}, Value: cal.Description},
				{XMLName: xml.Name{Space: "CALDAV:", Local: "supported-calendar-component-set"}, Value: "<comp name=\"VEVENT\"/><comp name=\"VTODO\"/>"},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// handleCalendarPropfind handles PROPFIND for specific calendar paths
func (s *Server) handleCalendarPropfind(path string, username string, multistatus *Multistatus) {
	// Parse path: /dav/calendars/{username}/{calendarID}/{eventUID?}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Minimum path: /dav/calendars/{username}/{calendarID} = 4 parts
	if len(parts) < 4 {
		return
	}

	calendarID := parts[3]
	if calendarID == "" {
		return
	}

	// Get calendar
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		return
	}

	// If it's just the calendar, return calendar info
	if len(parts) == 4 || (len(parts) == 5 && parts[4] == "") {
		multistatus.Responses = append(multistatus.Responses, s.buildCalendarResponse(username, cal))

		// Also include events
		events, _ := s.storage.GetEvents(username, calendarID)
		for _, eventData := range events {
			uid := extractUIDFromICS(eventData)
			if uid != "" {
				multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, uid, eventData))
			}
		}
		return
	}

	// Specific event
	if len(parts) >= 5 {
		eventUID := parts[4]
		eventData, err := s.storage.GetEvent(username, calendarID, eventUID)
		if err == nil && eventData != "" {
			multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, eventUID, eventData))
		}
	}
}

// buildEventResponse builds a response for a calendar event
func (s *Server) buildEventResponse(username, calendarID, eventUID, eventData string) Response {
	href := fmt.Sprintf("/dav/calendars/%s/%s/%s", username, calendarID, eventUID)
	etag := s.storage.GetETag(username, calendarID, eventUID)

	return Response{
		Href: href,
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: ""},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: eventUID},
				{XMLName: xml.Name{Space: "DAV:", Local: "getetag"}, Value: etag},
				{XMLName: xml.Name{Space: "DAV:", Local: "getcontenttype"}, Value: "text/calendar; component=vevent"},
				{XMLName: xml.Name{Space: "DAV:", Local: "getcontentlength"}, Value: fmt.Sprintf("%d", len(eventData))},
				{XMLName: xml.Name{Space: "CALDAV:", Local: "calendar-data"}, Value: eventData},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// extractUIDFromICS extracts the UID from iCalendar data
func extractUIDFromICS(icsData string) string {
	lines := strings.Split(icsData, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "UID:") {
			return strings.TrimPrefix(line, "UID:")
		}
	}
	return ""
}

// sendError sends an error response
func (s *Server) sendError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(message))
}

// XML structures for WebDAV/CalDAV

// Propfind represents a PROPFIND request
type Propfind struct {
	XMLName xml.Name  `xml:"propfind"`
	AllProp *struct{} `xml:"allprop,omitempty"`
	Prop    *Prop     `xml:"prop,omitempty"`
}

// Prop represents properties
type Prop struct {
	XMLName xml.Name `xml:"prop"`
	Inner   []byte   `xml:",innerxml"`
}

// Multistatus represents a 207 Multi-Status response
type Multistatus struct {
	XMLName   xml.Name   `xml:"multistatus"`
	XMLNSDav  string     `xml:"xmlns:dav,attr,omitempty"`
	XMLNSCal  string     `xml:"xmlns:cal,attr,omitempty"`
	Responses []Response `xml:"response"`
}

// Response represents a response element in multistatus
type Response struct {
	XMLName  xml.Name   `xml:"response"`
	Href     string     `xml:"href"`
	Propstat []Propstat `xml:"propstat"`
}

// Propstat represents property status
type Propstat struct {
	XMLName xml.Name   `xml:"propstat"`
	Prop    []Property `xml:"prop"`
	Status  string     `xml:"status"`
}

// Property represents a single property
type Property struct {
	XMLName xml.Name `xml:""`
	Value   string   `xml:",chardata"`
}

// CalendarQuery represents a calendar-query REPORT
type CalendarQuery struct {
	XMLName    xml.Name    `xml:"calendar-query"`
	CompFilter *CompFilter `xml:"comp-filter,omitempty"`
	Prop       *Prop       `xml:"prop,omitempty"`
}

// CompFilter represents a component filter
type CompFilter struct {
	XMLName xml.Name `xml:"comp-filter"`
	Name    string   `xml:"name,attr"`
}

// CalendarEvent represents a calendar event
type CalendarEvent struct {
	UID         string    `json:"uid"`
	Summary     string    `json:"summary"`
	Description string    `json:"description,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end,omitempty"`
	AllDay      bool      `json:"all_day,omitempty"`
	Location    string    `json:"location,omitempty"`
	Organizer   string    `json:"organizer,omitempty"`
	Attendees   []string  `json:"attendees,omitempty"`
	Recurrence  string    `json:"recurrence,omitempty"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
}

// Calendar represents a calendar collection
type Calendar struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Color       string    `json:"color,omitempty"`
	Timezone    string    `json:"timezone,omitempty"`
	ReadOnly    bool      `json:"read_only,omitempty"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
}
