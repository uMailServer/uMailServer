package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r134Do(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

const r134Todo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:todo-1\r\nCREATED:20260105T000000Z\r\nCOMPLETED:20260110T120000Z\r\nSTATUS:COMPLETED\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"

func TestGetConditionalAndDepth_R134(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	r134Do(s, "MKCALENDAR", "/dav/calendars/c", "", nil)
	put := r134Do(s, "PUT", "/dav/calendars/c/uuid-1.ics", r134Todo, nil)
	if put.Code != http.StatusCreated {
		t.Fatalf("PUT = %d", put.Code)
	}
	etag := put.Header().Get("ETag")
	if g := r134Do(s, "GET", "/dav/calendars/c/uuid-1.ics", "", map[string]string{"If-None-Match": etag}); g.Code != http.StatusNotModified {
		t.Errorf("If-None-Match GET = %d, want 304", g.Code)
	}
	if g := r134Do(s, "GET", "/dav/calendars/c/uuid-1.ics", "", map[string]string{"If-Match": `"nope"`}); g.Code != http.StatusPreconditionFailed {
		t.Errorf("If-Match mismatch GET = %d, want 412", g.Code)
	}
	if g := r134Do(s, "GET", "/dav/calendars/c/uuid-1.ics", "", map[string]string{"If-Match": etag}); g.Code != http.StatusOK {
		t.Errorf("If-Match GET = %d, want 200", g.Code)
	}
	if g := r134Do(s, "PROPFIND", "/dav/calendars/c/", "", map[string]string{"Depth": "infinity"}); g.Code != http.StatusForbidden {
		t.Errorf("Depth infinity = %d", g.Code)
	}
	if g := r134Do(s, "PROPFIND", "/dav/calendars/c/", "", map[string]string{"Depth": "7"}); g.Code != http.StatusBadRequest {
		t.Errorf("Depth 7 = %d", g.Code)
	}
	pf := r134Do(s, "PROPFIND", "/dav/calendars/c/", "", map[string]string{"Depth": "1"})
	if !strings.Contains(pf.Body.String(), "component=vtodo") || !strings.Contains(pf.Body.String(), "/dav/calendars/c/uuid-1.ics<") {
		t.Errorf("PROPFIND listing: %s", pf.Body.String())
	}
}

func TestVTodoCompletedTimeRange_R134(t *testing.T) {
	cases := []struct {
		name, s, e string
		want       bool
	}{
		{"completed inside", "20260110T000000Z", "20260111T000000Z", true},
		{"completed+created overlap by created", "20260101T000000Z", "20260106T000000Z", true},
		{"after everything", "20260201T000000Z", "20260202T000000Z", false},
	}
	for _, c := range cases {
		got := compFilterMatches(&CompFilter{Name: "VCALENDAR", CompFilters: []*CompFilter{{Name: "VTODO", TimeRange: &TimeRange{Start: c.s, End: c.e}}}}, r134Todo)
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	bare := strings.Replace(strings.Replace(r134Todo, "CREATED:20260105T000000Z\r\n", "", 1), "COMPLETED:20260110T120000Z\r\n", "", 1)
	if !compFilterMatches(&CompFilter{Name: "VCALENDAR", CompFilters: []*CompFilter{{Name: "VTODO", TimeRange: &TimeRange{Start: "20300101T000000Z", End: "20300102T000000Z"}}}}, bare) {
		t.Error("VTODO without temporal properties must match any range")
	}
}

func TestCopyKeepsUIDUnlessConflict_R134(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	r134Do(s, "MKCALENDAR", "/dav/calendars/c", "", nil)
	if w := r134Do(s, "PUT", "/dav/calendars/c/a.ics", r134Todo, nil); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := r134Do(s, "COPY", "/dav/calendars/c/a.ics", "", map[string]string{"Destination": "/dav/calendars/c/b.ics"}); w.Code != http.StatusCreated {
		t.Fatalf("COPY = %d", w.Code)
	}
	g := r134Do(s, "GET", "/dav/calendars/c/b.ics", "", nil)
	if strings.Contains(g.Body.String(), "UID:todo-1") || !strings.Contains(g.Body.String(), "UID:") {
		t.Errorf("same-calendar COPY must get a fresh UID (no-uid-conflict): %s", g.Body.String())
	}
	r134Do(s, "MKCALENDAR", "/dav/calendars/d", "", nil)
	if w := r134Do(s, "COPY", "/dav/calendars/c/a.ics", "", map[string]string{"Destination": "/dav/calendars/d/x.ics"}); w.Code != http.StatusCreated {
		t.Fatalf("COPY = %d", w.Code)
	}
	if g := r134Do(s, "GET", "/dav/calendars/d/x.ics", "", nil); !strings.Contains(g.Body.String(), "UID:todo-1") {
		t.Errorf("cross-calendar COPY must keep UID: %s", g.Body.String())
	}
}
