// Package caldav provides CalDAV (RFC 4791) calendar synchronization support
package caldav

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/tracing"
)

// maxRequestBodyBytes caps every CalDAV request body (F5090). Calendar
// objects and DAV XML requests are small; without a cap an authenticated
// client could make the server buffer an arbitrarily large body in memory.
const maxRequestBodyBytes = 10 << 20

// Server represents a CalDAV server
type Server struct {
	logger          *slog.Logger
	authFunc        func(username, password string) (bool, error)
	dataDir         string
	storage         *Storage
	tracingProvider *tracing.Provider
	// writeMu serializes conditional writes so an If-Match/If-None-Match
	// evaluation and the write it guards are atomic (F5088).
	writeMu sync.Mutex
}

// SetTracingProvider attaches an OpenTelemetry tracing provider so each
// CalDAV request emits a caldav.<METHOD> span. Nil disables tracing.
func (s *Server) SetTracingProvider(provider *tracing.Provider) {
	s.tracingProvider = provider
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

// ServeHTTP implements the http.Handler interface, wrapping the actual
// dispatch in a tracing span when a provider is configured.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tracing.HTTPMiddleware(s.tracingProvider, "caldav", http.HandlerFunc(s.handle)).ServeHTTP(w, r)
}

// handle does the auth+dispatch work; ServeHTTP wraps it in a tracing span.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// RFC 6764 §6: the well-known bootstrap URI redirects to the DAV context
	// path. Clients probe it before they have credentials (F5652).
	if strings.TrimRight(r.URL.Path, "/") == "/.well-known/caldav" {
		http.Redirect(w, r, "/dav/", http.StatusMovedPermanently)
		return
	}

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

	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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
	// Only advertise what is implemented (F5656): there is no LOCK (class 2)
	// and no scheduling (calendar-schedule).
	w.Header().Set("DAV", "1, 3, calendar-access")
	w.WriteHeader(http.StatusOK)
}

// readBody reads the request body, answering 413 when it exceeds
// maxRequestBodyBytes (F5090) and 400 on any other read failure.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.sendError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return nil, false
		}
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	return body, true
}

// etagListMatches reports whether a comma-separated If-Match/If-None-Match
// list names etag. Strong comparison (If-Match) never matches a weak entity
// tag; weak comparison (If-None-Match) ignores the W/ prefix (RFC 7232 §2.3.2).
func etagListMatches(list, etag string, weak bool) bool {
	for _, candidate := range strings.Split(list, ",") {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "W/") {
			if !weak {
				continue
			}
			candidate = strings.TrimPrefix(candidate, "W/")
		}
		if candidate == etag {
			return true
		}
	}
	return false
}

// preconditionsHold evaluates If-Match and If-None-Match for a state-changing
// request against the target's current state (RFC 7232 §3.1, §3.2, §6). A
// false result must be answered with 412 and the method not performed (F5088).
func preconditionsHold(r *http.Request, exists bool, etag string) bool {
	if values, present := r.Header["If-Match"]; present {
		list := strings.Join(values, ",")
		if strings.TrimSpace(list) == "*" {
			if !exists {
				return false
			}
		} else if !exists || !etagListMatches(list, etag, false) {
			return false
		}
	}
	if values, present := r.Header["If-None-Match"]; present {
		list := strings.Join(values, ",")
		if strings.TrimSpace(list) == "*" {
			if exists {
				return false
			}
		} else if exists && etagListMatches(list, etag, true) {
			return false
		}
	}
	return true
}

// destinationSegments parses the RFC 4918 §10.3 Destination header, which
// may be an absolute URI or an absolute path, into its decoded path segments
// below /dav/calendars/ (F5092). ok is false when the header is malformed or
// points outside the calendar namespace.
func destinationSegments(destination string) ([]string, bool) {
	u, err := url.Parse(destination)
	if err != nil {
		return nil, false
	}
	const prefix = "/dav/calendars/"
	if !strings.HasPrefix(u.Path, prefix) {
		return nil, false
	}
	return strings.Split(strings.Trim(u.Path, "/"), "/"), true
}

// handlePropfind handles PROPFIND requests
func (s *Server) handlePropfind(w http.ResponseWriter, r *http.Request, username string) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	// Parse PROPFIND request
	var propfind Propfind
	if len(body) > 0 {
		if err := xml.Unmarshal(body, &propfind); err != nil {
			s.logger.Debug("Failed to parse PROPFIND", "error", err, "body", string(body))
			// Continue with empty propfind (allprop)
		}
	}

	// RFC 4918 §9.1: Depth 0 targets only the request-URI; an absent header
	// means infinity. Members are enumerated only when Depth is not 0 (F5472).
	includeMembers := strings.TrimSpace(r.Header.Get("Depth")) != "0"

	// Build response
	multistatus := &Multistatus{}
	path := r.URL.Path
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	// Principal resources (RFC 5397, RFC 4791 §6.2.1): the principal URL the
	// server advertises must itself answer PROPFIND (F5651).
	if len(pathParts) >= 2 && pathParts[0] == "dav" && pathParts[1] == "principals" {
		if !s.propfindPrincipals(multistatus, pathParts, username, includeMembers) {
			s.sendError(w, http.StatusNotFound, "not found")
			return
		}
		applyPropSelection(multistatus, requestedProps(&propfind))
		s.writeMultistatus(w, multistatus)
		return
	}

	// Root: the entry point where current-user-principal is discovered.
	if path == "/" || path == "/dav/" {
		multistatus.Responses = append(multistatus.Responses, s.buildRootResponse(path, username))
		multistatus.Responses = append(multistatus.Responses, s.buildPrincipalResponse(username))
	}

	// Calendar home
	if path == "/" || path == "/dav/" || path == "/dav/calendars/" {
		multistatus.Responses = append(multistatus.Responses, s.buildCalendarHomeResponse(username))

		if includeMembers {
			// Query actual calendars from storage
			calendars, err := s.storage.GetCalendars(username)
			if err != nil {
				s.logger.Error("Failed to query calendars", "error", err)
				s.sendError(w, http.StatusInternalServerError, "failed to query calendars")
				return
			}
			for _, cal := range calendars {
				multistatus.Responses = append(multistatus.Responses, s.buildCalendarResponse(username, cal))
			}
		}
	}

	// Handle specific calendar or event path
	if strings.HasPrefix(path, "/dav/calendars/") {
		found, err := s.handleCalendarPropfind(path, username, multistatus, includeMembers)
		if err != nil {
			s.logger.Error("Failed to query calendar", "error", err)
			s.sendError(w, http.StatusInternalServerError, "failed to query calendar")
			return
		}
		if !found {
			s.sendError(w, http.StatusNotFound, "not found")
			return
		}
	}

	applyPropSelection(multistatus, requestedProps(&propfind))
	s.writeMultistatus(w, multistatus)
}

// writeMultistatus writes a 207 response carrying multistatus.
func (s *Server) writeMultistatus(w http.ResponseWriter, multistatus *Multistatus) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)

	output, _ := xml.MarshalIndent(multistatus, "", "  ")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(output)
}

// propfindPrincipals answers PROPFIND below /dav/principals/. It reports
// false when the target is not the authenticated user's own principal, so
// other users' principals are not disclosed.
func (s *Server) propfindPrincipals(multistatus *Multistatus, parts []string, username string, includeMembers bool) bool {
	switch {
	case len(parts) == 2:
		multistatus.Responses = append(multistatus.Responses, Response{
			Href: "/dav/principals/",
			Propstat: []Propstat{{
				Prop: []Property{
					{XMLName: xml.Name{Space: nsDAV, Local: "resourcetype"}, Inner: "<collection/>"},
					{XMLName: xml.Name{Space: nsDAV, Local: "displayname"}, Value: "Principals"},
				},
				Status: statusLine(http.StatusOK),
			}},
		})
		if includeMembers {
			multistatus.Responses = append(multistatus.Responses, s.buildPrincipalResponse(username))
		}
		return true
	case len(parts) == 3 && parts[2] == username:
		multistatus.Responses = append(multistatus.Responses, s.buildPrincipalResponse(username))
		return true
	default:
		return false
	}
}

// requestedProps returns the property names a PROPFIND body asks for, or nil
// when every property is wanted (no body, allprop, or an empty prop list).
func requestedProps(pf *Propfind) []xml.Name {
	if pf.AllProp != nil || pf.Prop == nil {
		return nil
	}
	var names []xml.Name
	for _, p := range pf.Prop.Names {
		names = append(names, p.XMLName)
	}
	if pf.Prop.CalendarData != nil {
		names = append(names, xml.Name{Space: nsCalDAV, Local: "calendar-data"})
	}
	return names
}

// applyPropSelection restricts every response to the requested properties and
// reports requested-but-unknown ones in a 404 propstat (RFC 4918 §9.1). A nil
// want leaves the full property set in place (allprop).
func applyPropSelection(multistatus *Multistatus, want []xml.Name) {
	if len(want) == 0 {
		return
	}
	for i := range multistatus.Responses {
		resp := &multistatus.Responses[i]
		var have []Property
		for _, ps := range resp.Propstat {
			have = append(have, ps.Prop...)
		}
		var found, missing []Property
		seen := make(map[xml.Name]bool, len(want))
		for _, w := range want {
			if seen[w] {
				continue
			}
			seen[w] = true
			matched := false
			for _, h := range have {
				if h.XMLName.Local == w.Local && (w.Space == "" || h.XMLName.Space == w.Space) {
					found = append(found, h)
					matched = true
					break
				}
			}
			if !matched {
				missing = append(missing, Property{XMLName: w})
			}
		}
		resp.Propstat = nil
		if len(found) > 0 {
			resp.Propstat = append(resp.Propstat, Propstat{Prop: found, Status: statusLine(http.StatusOK)})
		}
		if len(missing) > 0 {
			resp.Propstat = append(resp.Propstat, Propstat{Prop: missing, Status: statusLine(http.StatusNotFound)})
		}
	}
}

// statusLine renders an HTTP status for a DAV:status element.
func statusLine(code int) string {
	return "HTTP/1.1 " + strconv.Itoa(code) + " " + http.StatusText(code)
}

// handleReport handles REPORT requests
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, username string) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	// Dispatch on the REPORT root element (RFC 3253 §3.6): multiget,
	// free-busy and sync-collection are distinct reports (F5752-F5754).
	switch reportRoot(body) {
	case "calendar-multiget":
		s.reportMultiget(w, r, username, body)
		return
	case "free-busy-query":
		s.reportFreeBusy(w, r, username, body)
		return
	case "sync-collection":
		s.reportSyncCollection(w, r, username, body)
		return
	}

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

	// Apply the calendar-query filter (RFC 4791 §9.9). The filter element is
	// the RFC shape; a direct comp-filter child is tolerated as a legacy
	// form. Shapes the server cannot honor (property filters, unparseable or
	// misplaced time ranges, a non-VCALENDAR root) are rejected explicitly
	// with the CALDAV:supported-filter precondition (RFC 4791 §3.11) instead
	// of silently ignoring the filter.
	var compFilter *CompFilter
	switch {
	case query.Filter != nil:
		compFilter = query.Filter.CompFilter
	case query.CompFilter != nil:
		compFilter = query.CompFilter
	}

	if compFilter != nil && (compFilter.Name != "VCALENDAR" || !supportedCompFilter(compFilter)) {
		s.sendUnsupportedFilter(w)
		return
	}

	// CALDAV:expand / CALDAV:limit (RFC 4791 §9.6.4/§9.6.5): recurrence
	// expansion and a result cap for the returned component data.
	var expandStart, expandEnd time.Time
	expandSet := false
	limitN := 0
	if query.Prop != nil && query.Prop.CalendarData != nil {
		calData := query.Prop.CalendarData
		if calData.Expand != nil {
			s0, ok1 := parseICSTime(calData.Expand.Start)
			e0, ok2 := parseICSTime(calData.Expand.End)
			if !ok1 || !ok2 {
				s.sendError(w, http.StatusBadRequest, "invalid expand window")
				return
			}
			expandStart, expandEnd, expandSet = s0, e0, true
		}
		if calData.Limit != nil && calData.Limit.NResults > 0 {
			limitN = calData.Limit.NResults
		}
	}

	// Build response
	multistatus := &Multistatus{}

	// Query actual events from storage
	events, err := s.storage.GetEvents(username, calendarID)
	if err == nil {
		for _, eventData := range events {
			if !compFilterMatches(compFilter, eventData) {
				continue
			}
			uid := extractUIDFromICS(eventData)
			if uid == "" {
				continue
			}
			if expandSet {
				var instances []time.Time
				var duration time.Duration
				blocks := extractComponentBlocks(eventData, "VEVENT")
				if len(blocks) > 0 {
					start, end, ok := componentTimeRange(blocks[0])
					if ok {
						duration = end.Sub(start)
						if r := parseRRULEBlock(blocks[0]); r != nil {
							instances = rruleInstances(start, end, r, expandStart, expandEnd)
						} else if !start.Before(expandStart) && start.Before(expandEnd) {
							instances = []time.Time{start}
						}
					}
				}
				if limitN > 0 && len(instances) > limitN {
					instances = instances[:limitN]
				}
				if len(instances) == 0 {
					continue
				}
				multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, uid, buildExpandedICS(eventData, instances, duration)))
				continue
			}
			multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, uid, eventData))
		}
	}

	if err != nil {
		s.logger.Error("Failed to query calendar events", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to query calendar events")
		return
	}

	s.writeMultistatus(w, multistatus)
}

// extractComponentBlocks returns the bodies of all BEGIN:<name>...END:<name>
// blocks in the iCalendar data.
func extractComponentBlocks(icsData, name string) []string {
	var blocks []string
	inBlock := false
	var current []string
	for _, line := range strings.Split(icsData, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "BEGIN:"+name) {
			inBlock = true
			current = nil
			continue
		}
		if inBlock && strings.HasPrefix(line, "END:"+name) {
			blocks = append(blocks, strings.Join(current, "\n"))
			inBlock = false
			continue
		}
		if inBlock {
			current = append(current, line)
		}
	}
	return blocks
}

// parseICSTime parses an iCalendar DATE-TIME (optionally UTC-suffixed) or
// DATE value into a time.Time.
// extractPropertyValue returns the value of the first property named name in
// a component body, with RFC 5545 folded continuation lines (leading space or
// tab) unfolded into the value.
func extractPropertyValue(block, name string) (string, bool) {
	want := strings.ToUpper(name)
	value := ""
	found := false
	for _, raw := range strings.Split(block, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if found {
				value += strings.TrimPrefix(strings.TrimPrefix(line, " "), "\t")
			}
			continue
		}
		if found {
			break
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		namePart := line[:colon]
		if semi := strings.Index(namePart, ";"); semi >= 0 {
			namePart = namePart[:semi]
		}
		if strings.ToUpper(namePart) != want {
			continue
		}
		value = line[colon+1:]
		found = true
	}
	return strings.TrimSpace(value), found
}

// propFilterMatches applies one RFC 4791 §9.9.2 prop-filter to a component
// body: property presence (or absence with is-not-defined), then the §9.9.4
// text-match substring test with negation and collation (i;octet is
// case-sensitive; the casemap collations and no collation are
// case-insensitive). Unknown collations fail closed; supportedCompFilter
// answers them with a 403 upstream.
func propFilterMatches(pf *PropFilter, block string) bool {
	value, found := extractPropertyValue(block, pf.Name)
	if pf.IsNotDefined != nil {
		return !found
	}
	if !found {
		return false
	}
	if pf.TextMatch == nil {
		return true
	}
	var matched bool
	switch pf.TextMatch.Collation {
	case "i;octet":
		matched = strings.Contains(value, pf.TextMatch.Value)
	case "", "i;ascii-casemap", "i;unicode-casemap":
		matched = strings.Contains(strings.ToLower(value), strings.ToLower(pf.TextMatch.Value))
	default:
		return false
	}
	if pf.TextMatch.NegateCondition == "yes" {
		return !matched
	}
	return matched
}

func parseICSTime(value string) (time.Time, bool) {
	value = strings.TrimSuffix(value, "\r")
	if t, err := time.Parse("20060102T150405Z", value); err == nil {
		return t, true
	}
	if t, err := time.Parse("20060102T150405", value); err == nil {
		return t, true
	}
	if t, err := time.Parse("20060102", value); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// componentTimeRange returns the interval covered by a component body from
// its DTSTART/DTEND properties; ok is false when DTSTART is absent or
// unparseable. Without DTEND, DATE values span one day and DATE-TIME
// values are zero-length intervals.
func componentTimeRange(body string) (start, end time.Time, ok bool) {
	startIsDate := false
	var duration time.Duration
	hasDuration := false
	nested := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		// DURATION inside a nested component (VALARM repeat interval) does not
		// describe this component's span (F5471).
		switch {
		case strings.HasPrefix(line, "BEGIN:"):
			nested++
		case strings.HasPrefix(line, "END:"):
			nested--
		case nested == 0 && strings.HasPrefix(line, "DURATION"):
			if idx := strings.Index(line, ":"); idx >= 0 {
				if d, parsed := parseICSDuration(line[idx+1:]); parsed {
					duration, hasDuration = d, true
				}
			}
		}
		if start.IsZero() && strings.HasPrefix(line, "DTSTART") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				if t, parsed := parseICSTime(line[idx+1:]); parsed {
					start = t
					startIsDate = len(line[idx+1:]) == 8
				}
			}
			continue
		}
		if strings.HasPrefix(line, "DTEND") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				if t, parsed := parseICSTime(line[idx+1:]); parsed {
					end = t
				}
			}
		}
	}
	if start.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	if end.IsZero() {
		end = start
		if hasDuration {
			// RFC 4791 §9.9: DTSTART+DURATION spans [DTSTART, DTSTART+DURATION).
			end = start.Add(duration)
		} else if startIsDate {
			end = start.AddDate(0, 0, 1)
		}
	}
	return start, end, true
}

// parseICSDuration parses an RFC 5545 §3.3.6 dur-value ("PT2H", "P1DT30M",
// "P2W"). Negative durations are rejected: a component's DURATION is
// positive (RFC 5545 §3.8.2.5).
func parseICSDuration(value string) (time.Duration, bool) {
	v := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(value)), "+")
	if !strings.HasPrefix(v, "P") || len(v) < 3 {
		return 0, false
	}
	v = v[1:]
	var total time.Duration
	inTime := false
	num := ""
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			num += string(c)
			continue
		case c == 'T' && !inTime && num == "":
			inTime = true
			continue
		}
		if num == "" {
			return 0, false
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return 0, false
		}
		num = ""
		var unit time.Duration
		switch {
		case c == 'W' && !inTime:
			unit = 7 * 24 * time.Hour
		case c == 'D' && !inTime:
			unit = 24 * time.Hour
		case c == 'H' && inTime:
			unit = time.Hour
		case c == 'M' && inTime:
			unit = time.Minute
		case c == 'S' && inTime:
			unit = time.Second
		default:
			return 0, false
		}
		total += time.Duration(n) * unit
	}
	if num != "" || total <= 0 {
		return 0, false
	}
	return total, true
}

// parseTimeRange parses the RFC 4791 §9.9.3 start/end attributes.
func parseTimeRange(tr *TimeRange) (time.Time, time.Time, bool) {
	if tr == nil {
		return time.Time{}, time.Time{}, false
	}
	start, okStart := parseICSTime(tr.Start)
	end, okEnd := parseICSTime(tr.End)
	if !okStart || !okEnd {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// compFilterMatches reports whether the iCalendar data satisfies the
// component filter: the named component must be present, any time-range must
// intersect its interval, and every nested comp-filter must match within it.
// A nil filter matches everything.
func compFilterMatches(cf *CompFilter, icsData string) bool {
	if cf == nil {
		return true
	}

	blocks := extractComponentBlocks(icsData, cf.Name)
	if len(blocks) == 0 {
		return false
	}

	for _, pf := range cf.PropFilters {
		matched := false
		for _, body := range blocks {
			if propFilterMatches(&pf, body) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if cf.TimeRange != nil {
		rStart, rEnd, ok := parseTimeRange(cf.TimeRange)
		if !ok {
			return false
		}
		matched := false
		for _, body := range blocks {
			start, end, ok := componentTimeRange(body)
			if !ok {
				continue
			}
			if r := parseRRULEBlock(body); r != nil {
				// RFC 4791 §9.9.1: a recurring component matches when ANY
				// generated instance intersects the range; the base DTSTART
				// alone is not decisive. Unsupported RRULE parts degrade to
				// base-only matching (the documented subset).
				if len(rruleInstances(start, end, r, rStart, rEnd)) > 0 {
					matched = true
					break
				}
				continue
			}
			// RFC 4791 §9.9.1: intervals intersect when the component start
			// is before the range end and the component end is after the
			// range start; a zero-length component matches points inside.
			if end.Equal(start) {
				// RFC 4791 §9.9.1: a zero-length component matches when its
				// point lies inside the range (rStart <= start < rEnd).
				pointInRange := start.Before(rEnd) && !start.Before(rStart)
				if !pointInRange {
					continue
				}
				matched = true
				break
			} else if start.Before(rEnd) && end.After(rStart) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	for _, nested := range cf.CompFilters {
		matched := false
		for _, body := range blocks {
			wrapped := "BEGIN:" + cf.Name + "\n" + body + "\nEND:" + cf.Name
			if compFilterMatches(nested, wrapped) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// supportedCompFilter reports whether the filter tree only uses implemented
// features: component presence matching, time-range on temporal components,
// property filters with text-match/is-not-defined, and nested comp-filters.
// Param-filters, mutually exclusive is-not-defined and text-match, unknown
// collations, and unparseable time ranges are unsupported (RFC 4791 §3.11).
func supportedCompFilter(cf *CompFilter) bool {
	if cf == nil {
		return true
	}
	if cf.Name == "" {
		return false
	}
	for _, pf := range cf.PropFilters {
		if pf.IsNotDefined != nil && pf.TextMatch != nil {
			return false
		}
		if len(pf.ParamFilters) > 0 {
			return false
		}
		if pf.TextMatch != nil {
			switch pf.TextMatch.Collation {
			case "", "i;ascii-casemap", "i;unicode-casemap", "i;octet":
			default:
				return false
			}
		}
	}
	if cf.TimeRange != nil {
		if _, _, ok := parseTimeRange(cf.TimeRange); !ok {
			return false
		}
		switch cf.Name {
		case "VEVENT", "VTODO", "VJOURNAL":
		default:
			return false
		}
	}
	for _, nested := range cf.CompFilters {
		if !supportedCompFilter(nested) {
			return false
		}
	}
	return true
}

// sendUnsupportedFilter answers a REPORT whose filter the server cannot
// honor with the CALDAV:supported-filter precondition (RFC 4791 §3.11).
func (s *Server) sendUnsupportedFilter(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write([]byte(`<d:error xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<c:supported-filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"/><c:comp-filter name="VTODO"/></c:supported-filter>` +
		`</d:error>`))
}

// handlePut handles PUT requests for creating/updating events
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, username string) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

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

	// Verify the calendar belongs to this user
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		s.sendError(w, http.StatusForbidden, "calendar not found")
		return
	}

	// RFC 4791 §5.3.2: the request-URI names the resource, so its UID is
	// authoritative. A body UID that differs would store the event at an
	// address the client cannot address back, so reject the mismatch.
	if bodyUID := extractUIDFromICS(icsData); bodyUID != "" && bodyUID != eventUID {
		s.sendError(w, http.StatusForbidden, "UID in request URL does not match UID in calendar data")
		return
	}
	uid := eventUID

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	existing, err := s.storage.GetEvent(username, calendarID, uid)
	if err != nil {
		s.logger.Error("Failed to read event", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to read event")
		return
	}
	currentETag := s.storage.GetETag(username, calendarID, uid)
	if existing != "" && currentETag == "" {
		s.sendError(w, http.StatusInternalServerError, "failed to read event state")
		return
	}
	if !preconditionsHold(r, existing != "", currentETag) {
		s.sendError(w, http.StatusPreconditionFailed, "precondition failed")
		return
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
	if etag := s.storage.GetETag(username, calendarID, uid); etag != "" {
		w.Header().Set("ETag", etag)
	}
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

	// Verify the calendar belongs to this user
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		s.sendError(w, http.StatusForbidden, "calendar not found")
		return
	}

	// Get event
	eventData, err := s.storage.GetEvent(username, calendarID, eventUID)
	if err != nil || eventData == "" {
		s.sendError(w, http.StatusNotFound, "event not found")
		return
	}

	// Set content type and ETag
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	if etag := s.storage.GetETag(username, calendarID, eventUID); etag != "" {
		w.Header().Set("ETag", etag)
	}

	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- Content-Type is explicitly text/calendar, not executable HTML
	_, _ = w.Write([]byte(eventData))
}

// handleDelete handles DELETE requests
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, username string) {
	// Parse path
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 3 {
		s.deleteCalendar(w, r, username, parts[2])
		return
	}
	if len(parts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]
	eventUID := parts[3]

	// Verify the calendar belongs to this user
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		s.sendError(w, http.StatusForbidden, "calendar not found")
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	existing, err := s.storage.GetEvent(username, calendarID, eventUID)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid event")
		return
	}
	currentETag := s.storage.GetETag(username, calendarID, eventUID)
	if existing != "" && currentETag == "" {
		s.sendError(w, http.StatusInternalServerError, "failed to read event state")
		return
	}
	if !preconditionsHold(r, existing != "", currentETag) {
		s.sendError(w, http.StatusPreconditionFailed, "precondition failed")
		return
	}

	// Delete event
	if err := s.storage.DeleteEvent(username, calendarID, eventUID); err != nil {
		s.logger.Error("Failed to delete event", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to delete event")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// deleteCalendar deletes a calendar collection and its events (RFC 4918
// §9.6.1; F5585). If-Match/If-None-Match are evaluated against the
// calendar's ETag under writeMu.
func (s *Server) deleteCalendar(w http.ResponseWriter, r *http.Request, username, calendarID string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil {
		if errors.Is(err, errInvalidID) {
			s.sendError(w, http.StatusBadRequest, "invalid calendar ID")
			return
		}
		s.logger.Error("Failed to read calendar", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to read calendar")
		return
	}
	if cal == nil {
		s.sendError(w, http.StatusNotFound, "calendar not found")
		return
	}
	calETag := s.storage.GetCalendarETag(username, calendarID)
	if calETag == "" {
		s.sendError(w, http.StatusInternalServerError, "failed to read calendar state")
		return
	}
	if !preconditionsHold(r, true, calETag) {
		s.sendError(w, http.StatusPreconditionFailed, "precondition failed")
		return
	}
	if err := s.storage.DeleteCalendar(username, calendarID); err != nil {
		s.logger.Error("Failed to delete calendar", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to delete calendar")
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

	// RFC 4791 §5.3.1.2 (DAV:resource-must-be-null), RFC 4918 §9.3.1: an
	// existing calendar is never re-created, which would reset its metadata
	// (F5584). The check and the create are atomic under writeMu.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	existing, err := s.storage.GetCalendar(username, calendarID)
	if err != nil {
		if errors.Is(err, errInvalidID) {
			s.sendError(w, http.StatusBadRequest, "invalid calendar ID")
			return
		}
		s.logger.Error("Failed to read calendar", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to read calendar")
		return
	}
	if existing != nil {
		w.Header().Set("Allow", "OPTIONS, GET, PUT, DELETE, PROPFIND, PROPPATCH, REPORT, MOVE, COPY")
		s.sendError(w, http.StatusMethodNotAllowed, "calendar already exists")
		return
	}

	// Create default calendar
	cal := &Calendar{
		ID:          calendarID,
		Name:        "Calendar",
		Description: "Default calendar",
		Timezone:    "UTC",
	}

	if err := s.storage.CreateCalendar(username, cal); err != nil {
		if errors.Is(err, errInvalidID) {
			s.sendError(w, http.StatusBadRequest, "invalid calendar ID")
			return
		}
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

// proppatchItem is one property change of a PROPPATCH request.
type proppatchItem struct {
	name   xml.Name
	value  string
	remove bool
}

// parsePropertyUpdate reads the RFC 4918 §14.19 propertyupdate body into its
// ordered set/remove items.
func parsePropertyUpdate(body []byte) ([]proppatchItem, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var items []proppatchItem
	depth := 0
	remove := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			depth++
			switch depth {
			case 1:
				if el.Name.Space != nsDAV || el.Name.Local != "propertyupdate" {
					return nil, errors.New("root element is not DAV:propertyupdate")
				}
			case 2:
				remove = el.Name.Local == "remove"
			case 4:
				var v struct {
					Text string `xml:",chardata"`
				}
				if err := dec.DecodeElement(&v, &el); err != nil {
					return nil, err
				}
				depth-- // DecodeElement consumed the end tag
				items = append(items, proppatchItem{name: el.Name, value: strings.TrimSpace(v.Text), remove: remove})
			}
		case xml.EndElement:
			depth--
		}
	}
	if len(items) == 0 {
		return nil, errors.New("propertyupdate has no properties")
	}
	return items, nil
}

// proppatchSetters are the dead properties a calendar collection stores.
var proppatchSetters = map[xml.Name]func(*Calendar, string){
	{Space: nsDAV, Local: "displayname"}:             func(c *Calendar, v string) { c.Name = v },
	{Space: nsCalDAV, Local: "calendar-description"}: func(c *Calendar, v string) { c.Description = v },
	{Space: nsCalDAV, Local: "calendar-timezone"}:    func(c *Calendar, v string) { c.Timezone = v },
	{Space: nsAppleICal, Local: "calendar-color"}:    func(c *Calendar, v string) { c.Color = v },
}

// handleProppatch handles PROPPATCH requests (RFC 4918 §9.2). The changes are
// applied atomically: when any property cannot be set nothing is applied, the
// offending properties report 403 and the others 424 (F5653).
func (s *Server) handleProppatch(w http.ResponseWriter, r *http.Request, username string) {
	// Parse path
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return
	}

	calendarID := parts[2]
	// A PROPPATCH addressed at an event must not change its calendar.
	onEvent := len(parts) > 3 && parts[3] != ""

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Verify calendar belongs to this user (ownership check)
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		s.sendError(w, http.StatusForbidden, "calendar not found")
		return
	}

	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	items, err := parsePropertyUpdate(body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid propertyupdate")
		return
	}

	failed := onEvent
	for _, it := range items {
		if _, ok := proppatchSetters[it.name]; !ok {
			failed = true
		}
	}

	byStatus := map[int][]Property{}
	var order []int
	add := func(code int, name xml.Name) {
		if _, seen := byStatus[code]; !seen {
			order = append(order, code)
		}
		byStatus[code] = append(byStatus[code], Property{XMLName: name})
	}
	if failed {
		for _, it := range items {
			if _, ok := proppatchSetters[it.name]; ok && !onEvent {
				add(http.StatusFailedDependency, it.name)
			} else {
				add(http.StatusForbidden, it.name)
			}
		}
	} else {
		for _, it := range items {
			value := it.value
			if it.remove {
				value = ""
			}
			proppatchSetters[it.name](cal, value)
			add(http.StatusOK, it.name)
		}
		if err := s.storage.UpdateCalendar(username, cal); err != nil {
			s.logger.Error("Failed to update calendar", "error", err)
			s.sendError(w, http.StatusInternalServerError, "failed to update calendar")
			return
		}
	}

	resp := Response{Href: (&url.URL{Path: r.URL.Path}).EscapedPath()}
	for _, code := range order {
		resp.Propstat = append(resp.Propstat, Propstat{Prop: byStatus[code], Status: statusLine(code)})
	}
	s.writeMultistatus(w, &Multistatus{Responses: []Response{resp}})
}

// handleMove handles MOVE requests
func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, username string) {
	s.transferEvent(w, r, username, true)
}

// handleCopy handles COPY requests
func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request, username string) {
	s.transferEvent(w, r, username, false)
}

// transferEvent implements MOVE (move=true) and COPY of one event. The
// source preconditions (F5581), the Overwrite check (F5580), and the write
// run under writeMu like PUT and DELETE (F5582). The status is 201 when the
// destination was created and 204 when it was replaced (F5587).
func (s *Server) transferEvent(w http.ResponseWriter, r *http.Request, username string, move bool) {
	// Get source path
	sourceParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(sourceParts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid source path")
		return
	}

	sourceCalendarID := sourceParts[2]
	sourceEventUID := sourceParts[3]

	// Verify source calendar belongs to this user
	sourceCal, err := s.storage.GetCalendar(username, sourceCalendarID)
	if err != nil || sourceCal == nil {
		s.sendError(w, http.StatusForbidden, "calendar not found")
		return
	}

	// Get destination from header
	destination := r.Header.Get("Destination")
	if destination == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destParts, destOK := destinationSegments(destination)
	if !destOK {
		s.sendError(w, http.StatusForbidden, "destination outside calendar namespace")
		return
	}
	if len(destParts) < 4 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destCalendarID := destParts[2]
	destEventUID := destParts[3]

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Get event data
	eventData, err := s.storage.GetEvent(username, sourceCalendarID, sourceEventUID)
	if err != nil || eventData == "" {
		s.sendError(w, http.StatusNotFound, "source event not found")
		return
	}

	// Verify destination calendar exists before writing (RFC 4791 §7.5)
	destCal, err := s.storage.GetCalendar(username, destCalendarID)
	if err != nil || destCal == nil {
		s.sendError(w, http.StatusPreconditionFailed, "destination calendar does not exist")
		return
	}

	// RFC 7232 §3.1: If-Match/If-None-Match apply to the request-URI, the source.
	sourceETag := s.storage.GetETag(username, sourceCalendarID, sourceEventUID)
	if sourceETag == "" {
		s.sendError(w, http.StatusInternalServerError, "failed to read source state")
		return
	}
	if !preconditionsHold(r, true, sourceETag) {
		s.sendError(w, http.StatusPreconditionFailed, "precondition failed")
		return
	}

	if sourceCalendarID == destCalendarID && sourceEventUID == destEventUID {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	existing, err := s.storage.GetEvent(username, destCalendarID, destEventUID)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid destination event")
		return
	}
	// RFC 4918 §10.6: with "Overwrite: F" an existing destination is kept.
	if existing != "" && strings.EqualFold(strings.TrimSpace(r.Header.Get("Overwrite")), "F") {
		s.sendError(w, http.StatusPreconditionFailed, "destination exists and Overwrite is F")
		return
	}

	// Update UID if different
	if sourceEventUID != destEventUID {
		eventData = rewriteUID(eventData, extractUIDFromICS(eventData), destEventUID)
	}

	// Create event at destination
	event := &CalendarEvent{
		UID:      destEventUID,
		Modified: time.Now(),
	}

	if err := s.storage.SaveEvent(username, destCalendarID, event, eventData); err != nil {
		s.logger.Error("Failed to save event at destination", "error", err)
		if move {
			s.sendError(w, http.StatusInternalServerError, "failed to move event")
		} else {
			s.sendError(w, http.StatusInternalServerError, "failed to copy event")
		}
		return
	}

	if move {
		// Delete from source — failure here means the event exists at both source and destination
		if err := s.storage.DeleteEvent(username, sourceCalendarID, sourceEventUID); err != nil {
			s.logger.Error("Failed to delete source event after move", "error", err)
			s.sendError(w, http.StatusInternalServerError, "failed to delete source event")
			return
		}
	}

	if existing == "" {
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rewriteUID replaces the value of every UID property whose unfolded value is
// oldUID with newUID (F5583). It works on RFC 5545 §3.1 logical lines, so
// folded, parameterised and lower-case UID lines are handled, every
// component of a recurrence set is renamed, and other properties (X-ALT-UID)
// or a nested UID with a different value are left alone. Line endings follow
// the original line.
func rewriteUID(icsData, oldUID, newUID string) string {
	if oldUID == "" || oldUID == newUID {
		return icsData
	}
	physical := strings.SplitAfter(icsData, "\n")
	var out strings.Builder
	for i := 0; i < len(physical); {
		j := i + 1
		for j < len(physical) && physical[j] != "" && (physical[j][0] == ' ' || physical[j][0] == '\t') {
			j++
		}
		var logical strings.Builder
		for k := i; k < j; k++ {
			line := strings.TrimSuffix(strings.TrimSuffix(physical[k], "\n"), "\r")
			if k > i {
				line = line[1:]
			}
			logical.WriteString(line)
		}
		unfolded := logical.String()
		if colon := strings.Index(unfolded, ":"); colon >= 0 {
			name := unfolded[:colon]
			if semi := strings.Index(name, ";"); semi >= 0 {
				name = name[:semi]
			}
			if strings.EqualFold(name, "UID") && strings.TrimSpace(unfolded[colon+1:]) == oldUID {
				ending := physical[i][len(strings.TrimRight(physical[i], "\r\n")):]
				out.WriteString(unfolded[:colon+1] + newUID + ending)
				i = j
				continue
			}
		}
		for k := i; k < j; k++ {
			out.WriteString(physical[k])
		}
		i = j
	}
	return out.String()
}

// davHref renders a DAV:href element for an href path.
func davHref(path string) string {
	var b bytes.Buffer
	b.WriteString(`<href xmlns="DAV:">`)
	_ = xml.EscapeText(&b, []byte(path))
	b.WriteString("</href>")
	return b.String()
}

// principalHref returns the principal URL of username.
func principalHref(username string) string {
	return (&url.URL{Path: "/dav/principals/" + username + "/"}).EscapedPath()
}

// buildRootResponse builds the response for the DAV root, where clients look
// up current-user-principal (RFC 5397).
func (s *Server) buildRootResponse(path, username string) Response {
	return Response{
		Href: path,
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: nsDAV, Local: "resourcetype"}, Inner: "<collection/>"},
				{XMLName: xml.Name{Space: nsDAV, Local: "displayname"}, Value: "DAV"},
				{XMLName: xml.Name{Space: nsDAV, Local: "current-user-principal"}, Inner: davHref(principalHref(username))},
			},
			Status: statusLine(http.StatusOK),
		}},
	}
}

// buildPrincipalResponse builds a response for the principal resource
func (s *Server) buildPrincipalResponse(username string) Response {
	return Response{
		Href: principalHref(username),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: nsDAV, Local: "resourcetype"}, Inner: "<collection/><principal/>"},
				{XMLName: xml.Name{Space: nsDAV, Local: "displayname"}, Value: username},
				{XMLName: xml.Name{Space: nsDAV, Local: "current-user-principal"}, Inner: davHref(principalHref(username))},
				{XMLName: xml.Name{Space: nsDAV, Local: "principal-URL"}, Inner: davHref(principalHref(username))},
				{XMLName: xml.Name{Space: nsCalDAV, Local: "calendar-home-set"}, Inner: davHref("/dav/calendars/")},
			},
			Status: statusLine(http.StatusOK),
		}},
	}
}

// buildCalendarHomeResponse builds a response for the calendar home resource
func (s *Server) buildCalendarHomeResponse(username string) Response {
	return Response{
		Href: "/dav/calendars/",
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: nsDAV, Local: "resourcetype"}, Inner: "<collection/>"},
				{XMLName: xml.Name{Space: nsDAV, Local: "displayname"}, Value: "Calendars"},
				{XMLName: xml.Name{Space: nsDAV, Local: "current-user-principal"}, Inner: davHref(principalHref(username))},
			},
			Status: statusLine(http.StatusOK),
		}},
	}
}

// buildCalendarResponse builds a response for a calendar resource
func (s *Server) buildCalendarResponse(username string, cal *Calendar) Response {
	// Hrefs follow the server's request convention "/dav/calendars/{calendarID}/"
	// (the authenticated username scopes storage and is not part of the URL), so
	// clients operating on these hrefs (RFC 4918 §8.3) reach the item handlers.
	href := fmt.Sprintf("/dav/calendars/%s/", cal.ID)
	props := []Property{
		{XMLName: xml.Name{Space: nsDAV, Local: "resourcetype"}, Inner: `<collection/><calendar xmlns="` + nsCalDAV + `"/>`},
		{XMLName: xml.Name{Space: nsDAV, Local: "displayname"}, Value: cal.Name},
	}
	if etag := s.storage.GetCalendarETag(username, cal.ID); etag != "" {
		props = append(props, Property{XMLName: xml.Name{Space: nsDAV, Local: "getetag"}, Value: etag})
	}
	if ctag := s.storage.GetCalendarCTag(username, cal.ID); ctag != "" {
		props = append(props,
			Property{XMLName: xml.Name{Space: nsCalendarServer, Local: "getctag"}, Value: ctag},
			Property{XMLName: xml.Name{Space: nsDAV, Local: "sync-token"}, Value: syncTokenPrefix + ctag},
		)
	}
	props = append(props,
		Property{XMLName: xml.Name{Space: nsCalDAV, Local: "calendar-description"}, Value: cal.Description},
		Property{XMLName: xml.Name{Space: nsCalDAV, Local: "supported-calendar-component-set"}, Inner: `<comp name="VEVENT"/><comp name="VTODO"/>`},
		Property{XMLName: xml.Name{Space: nsDAV, Local: "supported-report-set"}, Inner: `<supported-report><report><calendar-query xmlns="` + nsCalDAV + `"/></report></supported-report><supported-report><report><calendar-multiget xmlns="` + nsCalDAV + `"/></report></supported-report><supported-report><report><free-busy-query xmlns="` + nsCalDAV + `"/></report></supported-report><supported-report><report><sync-collection/></report></supported-report>`},
	)
	if cal.Color != "" {
		props = append(props, Property{XMLName: xml.Name{Space: nsAppleICal, Local: "calendar-color"}, Value: cal.Color})
	}
	// calendar-timezone carries iCalendar text (RFC 4791 §5.2.2); only a
	// stored VCALENDAR is returned, not a bare zone identifier.
	if strings.HasPrefix(cal.Timezone, "BEGIN:VCALENDAR") {
		props = append(props, Property{XMLName: xml.Name{Space: nsCalDAV, Local: "calendar-timezone"}, Value: cal.Timezone})
	}

	return Response{
		Href:     href,
		Propstat: []Propstat{{Prop: props, Status: statusLine(http.StatusOK)}},
	}
}

// handleCalendarPropfind handles PROPFIND for specific calendar paths. It
// returns false when the addressed calendar or event does not exist, which
// the caller answers with 404 instead of an empty 207 (F5586). A storage
// error while listing members is returned instead of a partial listing
// (F5654).
func (s *Server) handleCalendarPropfind(path string, username string, multistatus *Multistatus, includeMembers bool) (bool, error) {
	// Parse path: /dav/calendars/{calendarID}/{eventUID?}
	// Request convention matches the item handlers: the authenticated username
	// scopes storage and is not part of the URL.
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Minimum path: /dav/calendars/{calendarID} = 3 parts
	if len(parts) < 3 {
		return true, nil
	}

	calendarID := parts[2]
	if calendarID == "" {
		return true, nil
	}

	// Get calendar
	cal, err := s.storage.GetCalendar(username, calendarID)
	if err != nil || cal == nil {
		return false, nil
	}

	// If it's just the calendar, return calendar info
	if len(parts) == 3 || (len(parts) == 4 && parts[3] == "") {
		multistatus.Responses = append(multistatus.Responses, s.buildCalendarResponse(username, cal))
		if !includeMembers {
			return true, nil
		}

		// Also include events
		events, err := s.storage.GetEvents(username, calendarID)
		if err != nil {
			return true, err
		}
		for _, eventData := range events {
			uid := extractUIDFromICS(eventData)
			if uid != "" {
				multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, uid, eventData))
			}
		}
		return true, nil
	}

	// Specific event
	eventUID := parts[3]
	if eventUID == "" {
		return true, nil
	}
	eventData, err := s.storage.GetEvent(username, calendarID, eventUID)
	if err != nil || eventData == "" {
		return false, nil
	}
	multistatus.Responses = append(multistatus.Responses, s.buildEventResponse(username, calendarID, eventUID, eventData))
	return true, nil
}

// maxRRULEInstances bounds recurrence expansion so a malformed or
// pathological RRULE cannot loop unbounded.
const maxRRULEInstances = 5000

// rruleSpec is the supported RRULE subset: FREQ=DAILY|WEEKLY|MONTHLY|YEARLY,
// INTERVAL, COUNT, UNTIL. Other parts (BYDAY, BYMONTH, BYSETPOS, ...) are
// unsupported: the event degrades to base-only matching and unexpanded
// responses (documented behavior).
type rruleSpec struct {
	freq     string
	interval int
	count    int
	until    time.Time
}

// parseRRULEValue parses an RRULE value ("FREQ=DAILY;COUNT=10"). It returns
// nil when the rule is absent or uses unsupported parts.
func parseRRULEValue(value string) *rruleSpec {
	var r rruleSpec
	seenFreq := false
	for _, part := range strings.Split(strings.TrimSpace(value), ";") {
		k, v, found := strings.Cut(part, "=")
		if !found {
			return nil
		}
		switch strings.ToUpper(strings.TrimSpace(k)) {
		case "FREQ":
			freq := strings.ToUpper(strings.TrimSpace(v))
			if freq != "DAILY" && freq != "WEEKLY" && freq != "MONTHLY" && freq != "YEARLY" {
				return nil
			}
			r.freq = freq
			seenFreq = true
		case "INTERVAL":
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 {
				return nil
			}
			r.interval = n
		case "COUNT":
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 {
				return nil
			}
			r.count = n
		case "UNTIL":
			t, ok := parseICSTime(strings.TrimSpace(v))
			if !ok {
				return nil
			}
			r.until = t
		default:
			return nil
		}
	}
	if !seenFreq {
		return nil
	}
	if r.interval == 0 {
		r.interval = 1
	}
	return &r
}

// parseRRULEBlock extracts the RRULE property line from a component block
// and parses it; nil when the block has no supported RRULE.
func parseRRULEBlock(body string) *rruleSpec {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(strings.ToUpper(line), "RRULE:") {
			return parseRRULEValue(line[len("RRULE:"):])
		}
	}
	return nil
}

// icsTimeFormat mirrors the shape of an existing iCalendar time value so
// generated instances look like the source data.
func icsTimeFormat(value string) string {
	v := strings.TrimSpace(value)
	switch {
	case len(v) == 8:
		return "20060102"
	case strings.HasSuffix(v, "Z"):
		return "20060102T150405Z"
	default:
		return "20060102T150405"
	}
}

// rruleInstances generates the recurrence instances of a component whose
// base interval is [baseStart, baseEnd) and returns those intersecting
// [windowStart, windowEnd). Zero-length instances match points inside the
// window (RFC 4791 §9.9.1).
func rruleInstances(baseStart, baseEnd time.Time, r *rruleSpec, windowStart, windowEnd time.Time) []time.Time {
	dur := baseEnd.Sub(baseStart)
	var out []time.Time
	count := 0
	for i := 0; i < maxRRULEInstances; i++ {
		var inst time.Time
		switch r.freq {
		case "DAILY":
			inst = baseStart.Add(time.Duration(i*r.interval) * 24 * time.Hour)
		case "WEEKLY":
			inst = baseStart.Add(time.Duration(i*r.interval) * 7 * 24 * time.Hour)
		case "MONTHLY":
			inst = baseStart.AddDate(0, i*r.interval, 0)
			// Nonexistent dates are omitted and do not consume COUNT.
			if inst.Day() != baseStart.Day() {
				continue
			}
		case "YEARLY":
			inst = baseStart.AddDate(i*r.interval, 0, 0)
			if inst.Month() != baseStart.Month() || inst.Day() != baseStart.Day() {
				continue
			}
		}
		if !r.until.IsZero() && inst.After(r.until) {
			break
		}
		if r.count > 0 && count >= r.count {
			break
		}
		if !inst.Before(windowEnd) {
			break
		}
		count++
		instEnd := inst.Add(dur)
		intersects := instEnd.After(windowStart) && inst.Before(windowEnd)
		if dur == 0 {
			intersects = !inst.Before(windowStart) && inst.Before(windowEnd)
		}
		if intersects {
			out = append(out, inst)
		}
	}
	return out
}

// buildExpandedICS renders the recurrence set as one VEVENT per instance
// (RECURRENCE-ID set) inside a VCALENDAR wrapper.
func buildExpandedICS(eventData string, instances []time.Time, duration time.Duration) string {
	blocks := extractComponentBlocks(eventData, "VEVENT")
	if len(blocks) == 0 {
		return eventData
	}
	block := blocks[0]
	dtstartVal, _ := extractPropertyValue(block, "DTSTART")
	layout := icsTimeFormat(strings.TrimSpace(dtstartVal))
	var out []string
	for _, line := range strings.Split(eventData, "\n") {
		trimmed := strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(trimmed, "BEGIN:VEVENT") {
			break
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	for _, inst := range instances {
		out = append(out, "BEGIN:VEVENT")
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSuffix(line, "\r")
			upper := strings.ToUpper(trimmed)
			switch {
			case strings.HasPrefix(upper, "DTSTART"):
				out = append(out, "DTSTART:"+inst.UTC().Format(layout))
				out = append(out, "RECURRENCE-ID:"+inst.UTC().Format(layout))
			case strings.HasPrefix(upper, "DTEND"):
				out = append(out, "DTEND:"+inst.Add(duration).UTC().Format(layout))
			case strings.HasPrefix(upper, "RRULE"):
				// expanded instances carry RECURRENCE-ID, not RRULE
			default:
				out = append(out, trimmed)
			}
		}
		out = append(out, "END:VEVENT")
	}
	out = append(out, "END:VCALENDAR")
	return strings.Join(out, "\r\n") + "\r\n"
}

// buildEventResponse builds a response for a calendar event
func (s *Server) buildEventResponse(username, calendarID, eventUID, eventData string) Response {
	// Request convention: "/dav/calendars/{calendarID}/{eventUID}" (no username segment).
	href := fmt.Sprintf("/dav/calendars/%s/%s", calendarID, eventUID)
	props := []Property{
		{XMLName: xml.Name{Space: nsDAV, Local: "resourcetype"}},
		{XMLName: xml.Name{Space: nsDAV, Local: "displayname"}, Value: eventUID},
	}
	if etag := s.storage.GetETag(username, calendarID, eventUID); etag != "" {
		props = append(props, Property{XMLName: xml.Name{Space: nsDAV, Local: "getetag"}, Value: etag})
	}
	props = append(props,
		Property{XMLName: xml.Name{Space: nsDAV, Local: "getcontenttype"}, Value: "text/calendar; component=vevent"},
		Property{XMLName: xml.Name{Space: nsDAV, Local: "getcontentlength"}, Value: fmt.Sprintf("%d", len(eventData))},
		Property{XMLName: xml.Name{Space: nsCalDAV, Local: "calendar-data"}, Value: eventData},
	)

	return Response{
		Href:     href,
		Propstat: []Propstat{{Prop: props, Status: statusLine(http.StatusOK)}},
	}
}

// extractUIDFromICS extracts the UID from iCalendar data
func extractUIDFromICS(icsData string) string {
	// RFC 5545 §3.1: CRLF line endings are stripped and folded continuation
	// lines unfolded, so a long UID matches the request-URL identifier
	// (RFC 4791 §5.3.2; F5470).
	uid, _ := extractPropertyValue(icsData, "UID")
	return uid
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

// Namespaces used in WebDAV/CalDAV XML.
const (
	nsDAV       = "DAV:"
	nsCalDAV    = "urn:ietf:params:xml:ns:caldav"
	nsAppleICal = "http://apple.com/ns/ical/"

	nsCalendarServer = "http://calendarserver.org/ns/"
)

// propName is one requested property element, namespace resolved.
type propName struct {
	XMLName xml.Name
}

// Prop represents properties
type Prop struct {
	XMLName      xml.Name      `xml:"prop"`
	Inner        []byte        `xml:",innerxml"`
	CalendarData *CalendarData `xml:"calendar-data"`
	Names        []propName    `xml:",any"`
}

// Multistatus represents a 207 Multi-Status response. The root and its
// unprefixed children are in the DAV: namespace (RFC 4918 §14.16).
type Multistatus struct {
	XMLName   xml.Name   `xml:"DAV: multistatus"`
	Responses []Response `xml:"response"`
	SyncToken string     `xml:"sync-token,omitempty"`
}

// Response represents a response element in multistatus
type Response struct {
	XMLName  xml.Name   `xml:"response"`
	Href     string     `xml:"href"`
	Propstat []Propstat `xml:"propstat"`
	Status   string     `xml:"status,omitempty"`
}

// Propstat represents property status
type Propstat struct {
	XMLName xml.Name   `xml:"propstat"`
	Prop    []Property `xml:"prop"`
	Status  string     `xml:"status"`
}

// propstatWire is the encoded shape of Propstat: the properties sit inside a
// single prop element (RFC 4918 §14.18).
type propstatWire struct {
	Prop struct {
		Items []Property `xml:",any"`
	} `xml:"prop"`
	Status string `xml:"status"`
}

// MarshalXML wraps the properties in one prop element.
func (p Propstat) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	var wire propstatWire
	wire.Prop.Items = p.Prop
	wire.Status = p.Status
	return e.EncodeElement(wire, start)
}

// Property represents a single property. Value is escaped text; Inner is raw
// XML for structured values (resourcetype, calendar-home-set, ...).
type Property struct {
	XMLName xml.Name `xml:""`
	Value   string   `xml:",chardata"`
	Inner   string   `xml:",innerxml"`
}

// CalendarQuery represents a calendar-query REPORT
type CalendarQuery struct {
	XMLName    xml.Name    `xml:"calendar-query"`
	Filter     *FilterWrap `xml:"filter,omitempty"`
	CompFilter *CompFilter `xml:"comp-filter,omitempty"` // tolerated direct form
	Prop       *Prop       `xml:"prop,omitempty"`
}

// CalendarData represents the RFC 4791 §9.6 calendar-data element of a
// calendar-query REPORT: the requested recurrence expansion and result cap
// for the returned component data.
type CalendarData struct {
	Expand *Expand `xml:"expand,omitempty"`
	Limit  *Limit  `xml:"limit,omitempty"`
}

// Expand represents the RFC 4791 §9.6.5 expand element: the recurrence set
// is expanded and only instances overlapping [start, end) are returned.
type Expand struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
}

// Limit represents the RFC 4791 §9.6.4 limit element: the maximum number of
// recurrence instances returned in the expanded data.
type Limit struct {
	NResults int `xml:"nresults"`
}

// FilterWrap wraps the RFC 4791 §9.9 filter element.
type FilterWrap struct {
	XMLName    xml.Name    `xml:"filter"`
	CompFilter *CompFilter `xml:"comp-filter"`
}

// CompFilter represents a component filter (RFC 4791 §9.9.1)
type CompFilter struct {
	XMLName     xml.Name      `xml:"comp-filter"`
	Name        string        `xml:"name,attr"`
	TimeRange   *TimeRange    `xml:"time-range,omitempty"`
	CompFilters []*CompFilter `xml:"comp-filter,omitempty"`
	PropFilters []PropFilter  `xml:"prop-filter,omitempty"`
}

// TimeRange represents the RFC 4791 §9.9.3 time-range element.
type TimeRange struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
}

// TextMatch represents the RFC 4791 §9.9.4 text-match element: a substring
// test over a property value with an optional collation and negation.
type TextMatch struct {
	Collation       string `xml:"collation,attr"`
	NegateCondition string `xml:"negate-condition,attr"`
	Value           string `xml:",chardata"`
}

// ParamFilter represents the RFC 4791 §9.9.3 param-filter element; parsing
// exists so its presence can be rejected via supported-filter instead of
// silently ignored.
type ParamFilter struct {
	XMLName xml.Name `xml:"param-filter"`
	Name    string   `xml:"name,attr"`
}

// PropFilter represents the RFC 4791 §9.9.2 prop-filter element: a test over
// a component property's presence (or, with is-not-defined, its absence) and
// — via text-match — its value. Nested param-filters are parsed so their
// presence is rejected via supported-filter instead of silently ignored.
type PropFilter struct {
	XMLName      xml.Name      `xml:"prop-filter"`
	Name         string        `xml:"name,attr"`
	TextMatch    *TextMatch    `xml:"text-match"`
	IsNotDefined *struct{}     `xml:"is-not-defined"`
	ParamFilters []ParamFilter `xml:"param-filter"`
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
