// Regression tests for F5092: an absolute-URI Destination header (RFC 4918 §10.3) was split as a raw path, so MOVE/COPY failed or targeted the wrong calendar.
// Promoted from .temp_files/case_F5092_caldav_destination_uri_test.go.

package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func a5092Do(s *Server, method, path, body, dest string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	if dest != "" {
		req.Header.Set("Destination", dest)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5092Setup(t *testing.T) *Server {
	s := NewServer(t.TempDir(), nil)
	for _, c := range []string{"cal1", "cal2"} {
		if w := a5092Do(s, "MKCALENDAR", "/dav/calendars/"+c, "", ""); w.Code != http.StatusCreated {
			t.Fatalf("fixture %d", w.Code)
		}
	}
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:ev1\r\nSUMMARY:moveme\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if w := a5092Do(s, "PUT", "/dav/calendars/cal1/ev1", ics, ""); w.Code != http.StatusCreated {
		t.Fatalf("fixture put %d", w.Code)
	}
	return s
}

func a5092Has(s *Server, cal, uid string) bool {
	d, _ := s.storage.GetEvent("alice@x.test", cal, uid)
	return d != ""
}

func TestRegressionF5092Control(t *testing.T) {
	s := a5092Setup(t)
	w := a5092Do(s, "MOVE", "/dav/calendars/cal1/ev1", "", "/dav/calendars/cal2/ev1")
	ok := w.Code < 300 && a5092Has(s, "cal2", "ev1") && !a5092Has(s, "cal1", "ev1")
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5092Failure(t *testing.T) {
	s := a5092Setup(t)
	w := a5092Do(s, "MOVE", "/dav/calendars/cal1/ev1", "", "http://example.com/dav/calendars/cal2/ev1")
	moved := a5092Has(s, "cal2", "ev1") && !a5092Has(s, "cal1", "ev1")
	if w.Code >= 300 || !moved {
		t.Fatal("DEFECT F5092: CalDAV MOVE/COPY misparses an absolute-URI Destination")
	}
}

func TestRegressionF5092Edges(t *testing.T) {
	s := a5092Setup(t)
	// COPY with an absolute URI and a percent-encoded member name.
	if w := a5092Do(s, "COPY", "/dav/calendars/cal1/ev1", "", "http://example.com/dav/calendars/cal2/ev%20copy"); w.Code >= 300 {
		t.Fatalf("COPY absolute = %d", w.Code)
	}
	if !a5092Has(s, "cal2", "ev copy") {
		t.Fatal("COPY did not store the decoded destination name")
	}
	// Destination outside the calendar namespace is rejected, not reinterpreted.
	if w := a5092Do(s, "COPY", "/dav/calendars/cal1/ev1", "", "/other/tree/cal2/stray"); w.Code < 400 {
		t.Fatalf("out-of-namespace Destination accepted: %d", w.Code)
	}
	if a5092Has(s, "cal2", "stray") {
		t.Fatal("out-of-namespace Destination wrote into cal2")
	}
}
