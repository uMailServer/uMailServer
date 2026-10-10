package caldav

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// syncTokenPrefix marks sync-tokens issued by this server (RFC 6578 §3.1:
// tokens are URIs). The remainder is the calendar collection tag.
const syncTokenPrefix = "data:,"

// reportRoot returns the local name of the REPORT body's root element, or ""
// when the body is not well-formed XML.
func reportRoot(body []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

// reportCalendarID extracts the calendar collection ID from a REPORT URL.
func (s *Server) reportCalendarID(w http.ResponseWriter, r *http.Request, username string) (string, bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		s.sendError(w, http.StatusBadRequest, "invalid path")
		return "", false
	}
	id := parts[2]
	cal, err := s.storage.GetCalendar(username, id)
	if err != nil || cal == nil {
		s.sendError(w, http.StatusNotFound, "calendar not found")
		return "", false
	}
	return id, true
}

type multigetQuery struct {
	XMLName xml.Name `xml:"calendar-multiget"`
	Prop    *Prop    `xml:"prop"`
	Hrefs   []string `xml:"href"`
}

// reportMultiget implements CALDAV:calendar-multiget (RFC 4791 §7.9): only
// the listed resources are returned, unknown ones as 404 responses.
func (s *Server) reportMultiget(w http.ResponseWriter, r *http.Request, username string, body []byte) {
	var q multigetQuery
	if err := xml.Unmarshal(body, &q); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid calendar-multiget")
		return
	}
	calendarID, ok := s.reportCalendarID(w, r, username)
	if !ok {
		return
	}
	ms := &Multistatus{}
	for _, h := range q.Hrefs {
		h = strings.TrimSpace(h)
		uid, found := multigetUID(h, calendarID)
		if found {
			if data, err := s.storage.GetEvent(username, calendarID, uid); err == nil && data != "" {
				ms.Responses = append(ms.Responses, s.buildEventResponse(username, calendarID, uid, data))
				continue
			}
		}
		ms.Responses = append(ms.Responses, Response{Href: h, Status: statusLine(http.StatusNotFound)})
	}
	applyPropSelection(ms, requestedProps(&Propfind{Prop: q.Prop}))
	s.writeMultistatus(w, ms)
}

// multigetUID maps a request href to an event UID inside calendarID.
func multigetUID(href, calendarID string) (string, bool) {
	p := href
	if u, err := url.Parse(href); err == nil {
		p = u.Path
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 4 || parts[0] != "dav" || parts[1] != "calendars" || parts[2] != calendarID || parts[3] == "" {
		return "", false
	}
	return parts[3], true
}

type syncQuery struct {
	XMLName   xml.Name `xml:"sync-collection"`
	SyncToken string   `xml:"sync-token"`
	Prop      *Prop    `xml:"prop"`
}

// reportSyncCollection implements DAV:sync-collection (RFC 6578). An empty
// token yields the full membership and a new token. The server keeps no
// deletion history, so a presented token equal to the current one yields an
// empty change set and any other token is refused with DAV:valid-sync-token
// (RFC 6578 §3.2), which makes the client re-sync from scratch.
func (s *Server) reportSyncCollection(w http.ResponseWriter, r *http.Request, username string, body []byte) {
	var q syncQuery
	if err := xml.Unmarshal(body, &q); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid sync-collection")
		return
	}
	calendarID, ok := s.reportCalendarID(w, r, username)
	if !ok {
		return
	}
	ctag := s.storage.GetCalendarCTag(username, calendarID)
	if ctag == "" {
		s.sendError(w, http.StatusInternalServerError, "failed to read calendar state")
		return
	}
	token := strings.TrimSpace(q.SyncToken)
	ms := &Multistatus{SyncToken: syncTokenPrefix + ctag}
	switch {
	case token == "":
		events, err := s.storage.GetEvents(username, calendarID)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to query calendar events")
			return
		}
		for _, data := range events {
			if uid := extractUIDFromICS(data); uid != "" {
				ms.Responses = append(ms.Responses, s.buildEventResponse(username, calendarID, uid, data))
			}
		}
		sort.Slice(ms.Responses, func(i, j int) bool { return ms.Responses[i].Href < ms.Responses[j].Href })
		applyPropSelection(ms, requestedProps(&Propfind{Prop: q.Prop}))
	case token == syncTokenPrefix+ctag:
		// nothing changed
	default:
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><error xmlns="DAV:"><valid-sync-token/></error>`))
		return
	}
	s.writeMultistatus(w, ms)
}

type freeBusyQuery struct {
	XMLName   xml.Name   `xml:"free-busy-query"`
	TimeRange *TimeRange `xml:"time-range"`
}

// reportFreeBusy implements CALDAV:free-busy-query (RFC 4791 §7.10): a
// VFREEBUSY of the busy periods that intersect the requested range.
func (s *Server) reportFreeBusy(w http.ResponseWriter, r *http.Request, username string, body []byte) {
	var q freeBusyQuery
	if err := xml.Unmarshal(body, &q); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid free-busy-query")
		return
	}
	ws, we, ok := parseTimeRange(q.TimeRange)
	if !ok {
		s.sendError(w, http.StatusBadRequest, "invalid time-range")
		return
	}
	calendarID, ok := s.reportCalendarID(w, r, username)
	if !ok {
		return
	}
	events, err := s.storage.GetEvents(username, calendarID)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to query calendar events")
		return
	}
	type period struct{ s, e time.Time }
	var busy []period
	for _, data := range events {
		for _, blk := range extractComponentBlocks(data, "VEVENT") {
			if v, _ := extractPropertyValue(blk, "TRANSP"); strings.EqualFold(v, "TRANSPARENT") {
				continue
			}
			if v, _ := extractPropertyValue(blk, "STATUS"); strings.EqualFold(v, "CANCELLED") {
				continue
			}
			start, end, ok := componentTimeRange(blk)
			if !ok {
				continue
			}
			if rr := parseRRULEBlock(blk); rr != nil {
				d := end.Sub(start)
				for _, inst := range rruleInstances(start, end, rr, ws, we) {
					busy = append(busy, period{inst, inst.Add(d)})
				}
			} else if end.After(ws) && start.Before(we) {
				busy = append(busy, period{start, end})
			}
		}
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].s.Before(busy[j].s) })
	const f = "20060102T150405Z"
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//uMailServer//CalDAV//EN\r\nBEGIN:VFREEBUSY\r\n")
	fmt.Fprintf(&sb, "DTSTART:%s\r\nDTEND:%s\r\n", ws.UTC().Format(f), we.UTC().Format(f))
	var merged []period
	for _, p := range busy {
		if p.s.Before(ws) {
			p.s = ws
		}
		if p.e.After(we) {
			p.e = we
		}
		if n := len(merged); n > 0 && !p.s.After(merged[n-1].e) {
			if p.e.After(merged[n-1].e) {
				merged[n-1].e = p.e
			}
			continue
		}
		merged = append(merged, p)
	}
	for _, p := range merged {
		fmt.Fprintf(&sb, "FREEBUSY;FBTYPE=BUSY:%s/%s\r\n", p.s.UTC().Format(f), p.e.UTC().Format(f))
	}
	sb.WriteString("END:VFREEBUSY\r\nEND:VCALENDAR\r\n")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}
