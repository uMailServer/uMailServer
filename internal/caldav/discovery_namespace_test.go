package caldav

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	testNSDAV    = "DAV:"
	testNSCalDAV = "urn:ietf:params:xml:ns:caldav"
)

// xmlNode is a namespace-resolved element tree used to inspect multistatus
// bodies the way a standards-compliant client does.
type xmlNode struct {
	Name     xml.Name
	Text     string
	Children []*xmlNode
}

func parseNSXML(t *testing.T, body []byte) *xmlNode {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(body))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("body is not well-formed XML: %v\n%s", err, body)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: el.Name}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(el)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	if root == nil {
		t.Fatalf("empty XML document")
	}
	return root
}

func (n *xmlNode) child(space, local string) *xmlNode {
	for _, c := range n.Children {
		if c.Name.Space == space && c.Name.Local == local {
			return c
		}
	}
	return nil
}

func (n *xmlNode) children(space, local string) []*xmlNode {
	var out []*xmlNode
	for _, c := range n.Children {
		if c.Name.Space == space && c.Name.Local == local {
			out = append(out, c)
		}
	}
	return out
}

// walk visits n and every descendant.
func (n *xmlNode) walk(fn func(*xmlNode)) {
	fn(n)
	for _, c := range n.Children {
		c.walk(fn)
	}
}

// propOf returns the property element name in the 200 propstat of the
// response whose href equals href (nil when absent).
func propOf(root *xmlNode, href, space, local string) *xmlNode {
	for _, resp := range root.children(testNSDAV, "response") {
		h := resp.child(testNSDAV, "href")
		if h == nil || strings.TrimSpace(h.Text) != href {
			continue
		}
		for _, ps := range resp.children(testNSDAV, "propstat") {
			st := ps.child(testNSDAV, "status")
			if st == nil || !strings.Contains(st.Text, "200") {
				continue
			}
			if prop := ps.child(testNSDAV, "prop"); prop != nil {
				if p := prop.child(space, local); p != nil {
					return p
				}
			}
		}
	}
	return nil
}

func davServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	s.SetAuthFunc(func(u, p string) (bool, error) { return u == "alice" && p == "pw", nil })
	if err := s.storage.CreateCalendar("alice", &Calendar{ID: "c1", Name: "Work", Description: "desc"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func davDo(s *Server, method, path, depth, body string, auth bool) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	if auth {
		r.SetBasicAuth("alice", "pw")
	}
	if depth != "" {
		r.Header.Set("Depth", depth)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// F5650: multistatus must be DAV:-namespaced with a prop wrapper and real
// child elements for structured property values.
func TestCalDAVDiscoveryF5650Control(t *testing.T) {
	w := davDo(davServer(t), "PROPFIND", "/dav/calendars/c1/", "0", "", true)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("code=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<displayname") {
		t.Fatalf("control: displayname text missing")
	}
}

func TestCalDAVDiscoveryF5650Failure(t *testing.T) {
	w := davDo(davServer(t), "PROPFIND", "/dav/calendars/c1/", "0", "", true)
	root := parseNSXML(t, w.Body.Bytes())
	var problems []string
	if root.Name.Space != testNSDAV || root.Name.Local != "multistatus" {
		problems = append(problems, "root="+root.Name.Space+" "+root.Name.Local)
	}
	if propOf(root, "/dav/calendars/c1/", testNSDAV, "displayname") == nil {
		problems = append(problems, "no {DAV:}displayname in {DAV:}response/propstat/prop")
	}
	rt := propOf(root, "/dav/calendars/c1/", testNSDAV, "resourcetype")
	if rt == nil || rt.child(testNSDAV, "collection") == nil || rt.child(testNSCalDAV, "calendar") == nil {
		problems = append(problems, "resourcetype lacks {DAV:}collection/{caldav}calendar child elements")
	}
	cs := propOf(root, "/dav/calendars/c1/", testNSCalDAV, "supported-calendar-component-set")
	if cs == nil || len(cs.children(testNSCalDAV, "comp")) == 0 {
		problems = append(problems, "supported-calendar-component-set lacks comp children")
	}
	root.walk(func(n *xmlNode) {
		if n.Name.Space == "CALDAV:" {
			problems = append(problems, "bogus namespace CALDAV: on "+n.Name.Local)
		}
	})
	if len(problems) > 0 {
		t.Errorf("DEFECT F5650\nEXPECTED: DAV:-namespaced multistatus with prop wrapper and element-valued properties\nACTUAL: %s\nbody:\n%s", strings.Join(problems, "; "), w.Body.String())
	}
}

func TestCalDAVDiscoveryF5650Edges(t *testing.T) {
	s := davServer(t)
	// REPORT bodies share the multistatus encoder.
	put := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e1\r\nDTSTART:20260101T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if w := davDo(s, "PUT", "/dav/calendars/c1/e1", "", put, true); w.Code != http.StatusCreated {
		t.Fatalf("PUT=%d", w.Code)
	}
	q := `<?xml version="1.0"?><C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/><C:calendar-data/></D:prop><C:filter><C:comp-filter name="VCALENDAR"/></C:filter></C:calendar-query>`
	w := davDo(s, "REPORT", "/dav/calendars/c1/", "1", q, true)
	root := parseNSXML(t, w.Body.Bytes())
	if root.Name.Space != testNSDAV || root.Name.Local != "multistatus" {
		t.Fatalf("REPORT root=%v", root.Name)
	}
	cd := propOf(root, "/dav/calendars/c1/e1", testNSCalDAV, "calendar-data")
	if cd == nil || !strings.Contains(cd.Text, "UID:e1") {
		t.Fatalf("calendar-data missing or not in caldav namespace:\n%s", w.Body.String())
	}
	if propOf(root, "/dav/calendars/c1/e1", testNSDAV, "getetag") == nil {
		t.Fatalf("getetag missing")
	}
}

// F5651: the advertised principal URL must answer PROPFIND, and
// current-user-principal / calendar-home-set must be discoverable.
func TestCalDAVDiscoveryF5651Control(t *testing.T) {
	w := davDo(davServer(t), "PROPFIND", "/dav/calendars/", "0", "", true)
	if w.Code != http.StatusMultiStatus || !strings.Contains(w.Body.String(), "/dav/calendars/") {
		t.Fatalf("control: code=%d", w.Code)
	}
}

const discoveryPropBody = `<?xml version="1.0"?><D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:current-user-principal/><C:calendar-home-set/><D:displayname/></D:prop></D:propfind>`

func TestCalDAVDiscoveryF5651Failure(t *testing.T) {
	s := davServer(t)
	var problems []string
	w := davDo(s, "PROPFIND", "/dav/principals/alice/", "0", discoveryPropBody, true)
	root := parseNSXML(t, w.Body.Bytes())
	hs := propOf(root, "/dav/principals/alice/", testNSCalDAV, "calendar-home-set")
	if hs == nil || hs.child(testNSDAV, "href") == nil || strings.TrimSpace(hs.child(testNSDAV, "href").Text) != "/dav/calendars/" {
		problems = append(problems, "PROPFIND /dav/principals/alice/ lacks calendar-home-set/href")
	}
	w2 := davDo(s, "PROPFIND", "/dav/", "0", discoveryPropBody, true)
	root2 := parseNSXML(t, w2.Body.Bytes())
	var cup *xmlNode
	for _, resp := range root2.children(testNSDAV, "response") {
		for _, ps := range resp.children(testNSDAV, "propstat") {
			if prop := ps.child(testNSDAV, "prop"); prop != nil {
				if c := prop.child(testNSDAV, "current-user-principal"); c != nil {
					cup = c
				}
			}
		}
	}
	if cup == nil || cup.child(testNSDAV, "href") == nil || strings.TrimSpace(cup.child(testNSDAV, "href").Text) != "/dav/principals/alice/" {
		problems = append(problems, "PROPFIND /dav/ lacks current-user-principal/href")
	}
	if len(problems) > 0 {
		t.Errorf("DEFECT F5651\nEXPECTED: principal URL answers PROPFIND with calendar-home-set; current-user-principal discoverable\nACTUAL: %s\nprincipal body:\n%s\nroot body:\n%s", strings.Join(problems, "; "), w.Body.String(), w2.Body.String())
	}
}

func TestCalDAVDiscoveryF5651Edges(t *testing.T) {
	s := davServer(t)
	// Another user's principal is not disclosed.
	if w := davDo(s, "PROPFIND", "/dav/principals/bob/", "0", "", true); w.Code != http.StatusNotFound {
		t.Errorf("foreign principal code=%d, want 404", w.Code)
	}
	// Principal without trailing slash and with no body (allprop).
	w := davDo(s, "PROPFIND", "/dav/principals/alice", "0", "", true)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("code=%d", w.Code)
	}
	root := parseNSXML(t, w.Body.Bytes())
	if propOf(root, "/dav/principals/alice/", testNSCalDAV, "calendar-home-set") == nil {
		t.Errorf("allprop principal lacks calendar-home-set:\n%s", w.Body.String())
	}
	// The advertised home set resolves to the user's calendar.
	w = davDo(s, "PROPFIND", "/dav/calendars/", "1", "", true)
	if !strings.Contains(w.Body.String(), "/dav/calendars/c1/") {
		t.Errorf("home set does not list the calendar:\n%s", w.Body.String())
	}
}

// F5652: RFC 6764 §6 bootstrap redirect.
func TestCalDAVDiscoveryF5652Control(t *testing.T) {
	if w := davDo(davServer(t), "PROPFIND", "/dav/", "0", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("control: unauthenticated /dav/ = %d, want 401", w.Code)
	}
}

func TestCalDAVDiscoveryF5652Failure(t *testing.T) {
	s := davServer(t)
	var got []string
	for _, m := range []string{"GET", "PROPFIND"} {
		w := davDo(s, m, "/.well-known/caldav", "", "", false)
		got = append(got, m+"="+strconv.Itoa(w.Code)+" Location="+w.Header().Get("Location"))
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/dav/" {
			t.Errorf("DEFECT F5652\nEXPECTED: %s /.well-known/caldav -> 301 Location /dav/\nACTUAL: %s", m, strings.Join(got, "; "))
		}
	}
}

// F5654: a storage error while listing members must surface.
func TestCalDAVDiscoveryF5654Control(t *testing.T) {
	s := davServer(t)
	if w := davDo(s, "PROPFIND", "/dav/calendars/c1/", "1", "", true); w.Code != http.StatusMultiStatus {
		t.Fatalf("control code=%d", w.Code)
	}
}

func TestCalDAVDiscoveryF5654Failure(t *testing.T) {
	s := davServer(t)
	// An unreadable event file makes ReadFile fail inside GetEvents.
	if err := s.storage.SaveEvent("alice", "c1", &CalendarEvent{UID: "bad"}, "BEGIN:VCALENDAR\nUID:bad\nEND:VCALENDAR"); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(s.storage.calendarDir("alice", "c1"), "bad.ics")
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o600) })
	if _, err := s.storage.GetEvents("alice", "c1"); err == nil {
		t.Fatal("invalid fixture: GetEvents must fail")
	}
	w := davDo(s, "PROPFIND", "/dav/calendars/c1/", "1", "", true)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("DEFECT F5654\nEXPECTED: 500 when members cannot be listed\nACTUAL: code=%d (partial listing presented as complete)\n%s", w.Code, w.Body.String())
	}
}

// F5655: ETag must be stable; an unreadable resource yields no ETag rather
// than a fresh random one that can never match.
func TestCalDAVDiscoveryF5655Control(t *testing.T) {
	st := NewStorage(t.TempDir())
	_ = st.CreateCalendar("alice", &Calendar{ID: "c1", Name: "n"})
	_ = st.SaveEvent("alice", "c1", &CalendarEvent{UID: "e"}, "BEGIN:VCALENDAR\nUID:e\nEND:VCALENDAR")
	if a, b := st.GetETag("alice", "c1", "e"), st.GetETag("alice", "c1", "e"); a != b || a == "" {
		t.Fatalf("control: existing ETag unstable %q %q", a, b)
	}
}

func TestCalDAVDiscoveryF5655Failure(t *testing.T) {
	st := NewStorage(t.TempDir())
	_ = st.CreateCalendar("alice", &Calendar{ID: "c1", Name: "n"})
	a, b := st.GetETag("alice", "c1", "gone"), st.GetETag("alice", "c1", "gone")
	c, d := st.GetCalendarETag("alice", "nocal"), st.GetCalendarETag("alice", "nocal")
	if a != b || c != d {
		t.Errorf("DEFECT F5655\nEXPECTED: repeated ETag calls on an unreadable resource agree (stable/empty)\nACTUAL: event %q vs %q; calendar %q vs %q", a, b, c, d)
	}
}

func TestCalDAVDiscoveryF5655Edges(t *testing.T) {
	s := davServer(t)
	put := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e1\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	w := davDo(s, "PUT", "/dav/calendars/c1/e1", "", put, true)
	if w.Code != http.StatusCreated || w.Header().Get("ETag") == "" {
		t.Fatalf("PUT code=%d etag=%q", w.Code, w.Header().Get("ETag"))
	}
	if g := davDo(s, "GET", "/dav/calendars/c1/e1", "", "", true); g.Header().Get("ETag") != w.Header().Get("ETag") {
		t.Errorf("GET ETag %q != PUT ETag %q", g.Header().Get("ETag"), w.Header().Get("ETag"))
	}
}

// F5656: only advertise what is implemented.
func TestCalDAVDiscoveryF5656Control(t *testing.T) {
	if w := davDo(davServer(t), "LOCK", "/dav/calendars/c1/e1", "", "", true); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("control: LOCK=%d, want 405", w.Code)
	}
}

func TestCalDAVDiscoveryF5656Failure(t *testing.T) {
	w := davDo(davServer(t), "OPTIONS", "/dav/", "", "", true)
	dav := w.Header().Get("DAV")
	var bad []string
	for _, tok := range strings.Split(dav, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "2" || tok == "calendar-schedule" {
			bad = append(bad, tok)
		}
	}
	if len(bad) > 0 || !strings.Contains(dav, "calendar-access") {
		t.Errorf("DEFECT F5656\nEXPECTED: DAV header without unimplemented class 2 (LOCK) / calendar-schedule, with calendar-access\nACTUAL: DAV=%q unimplemented=%v", dav, bad)
	}
}

// F5657: requested-property selection, 404 propstat, supported-report-set.
func TestCalDAVDiscoveryF5657Control(t *testing.T) {
	w := davDo(davServer(t), "PROPFIND", "/dav/calendars/c1/", "0", "", true)
	if !strings.Contains(w.Body.String(), "Work") {
		t.Fatalf("control: displayname value missing")
	}
}

const discoverySelectBody = `<?xml version="1.0"?><D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:X="urn:x"><D:prop><D:displayname/><X:nonesuch/><D:supported-report-set/></D:prop></D:propfind>`

func TestCalDAVDiscoveryF5657Failure(t *testing.T) {
	w := davDo(davServer(t), "PROPFIND", "/dav/calendars/c1/", "0", discoverySelectBody, true)
	root := parseNSXML(t, w.Body.Bytes())
	var problems []string
	found404 := false
	for _, resp := range root.children(testNSDAV, "response") {
		for _, ps := range resp.children(testNSDAV, "propstat") {
			st := ps.child(testNSDAV, "status")
			if st != nil && strings.Contains(st.Text, "404") {
				if prop := ps.child(testNSDAV, "prop"); prop != nil && prop.child("urn:x", "nonesuch") != nil {
					found404 = true
				}
			}
		}
	}
	if !found404 {
		problems = append(problems, "unknown requested property not reported in a 404 propstat")
	}
	if propOf(root, "/dav/calendars/c1/", testNSDAV, "getetag") != nil {
		problems = append(problems, "unrequested getetag returned for a prop request")
	}
	srs := propOf(root, "/dav/calendars/c1/", testNSDAV, "supported-report-set")
	if srs == nil {
		problems = append(problems, "supported-report-set absent")
	}
	if len(problems) > 0 {
		t.Errorf("DEFECT F5657\nEXPECTED: only requested props in 200, unknown in 404 propstat, supported-report-set present\nACTUAL: %s\n%s", strings.Join(problems, "; "), w.Body.String())
	}
}

func TestCalDAVDiscoveryF5657Edges(t *testing.T) {
	s := davServer(t)
	w := davDo(s, "PROPFIND", "/dav/calendars/c1/", "0", discoverySelectBody, true)
	root := parseNSXML(t, w.Body.Bytes())
	srs := propOf(root, "/dav/calendars/c1/", testNSDAV, "supported-report-set")
	if srs == nil {
		t.Fatalf("no supported-report-set:\n%s", w.Body.String())
	}
	var hasQuery bool
	srs.walk(func(n *xmlNode) {
		if n.Name.Space == testNSCalDAV && n.Name.Local == "calendar-query" {
			hasQuery = true
		}
	})
	if !hasQuery {
		t.Errorf("supported-report-set lacks calendar-query")
	}
	// allprop (no body) keeps returning the full property set.
	w = davDo(s, "PROPFIND", "/dav/calendars/c1/", "0", "", true)
	if propOf(parseNSXML(t, w.Body.Bytes()), "/dav/calendars/c1/", testNSDAV, "getetag") == nil {
		t.Errorf("allprop lost getetag")
	}
}
