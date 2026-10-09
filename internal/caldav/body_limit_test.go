// Regression tests for F5090: request bodies were read without any size limit.
// Promoted from .temp_files/case_F5090_caldav_body_limit_test.go.

package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func a5090Do(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5090ICS(pad int) string {
	return "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:big\r\nDESCRIPTION:" + strings.Repeat("A", pad) + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func a5090Setup(t *testing.T) *Server {
	s := NewServer(t.TempDir(), nil)
	if w := a5090Do(s, "MKCALENDAR", "/dav/calendars/cal", ""); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	return s
}

func TestRegressionF5090Control(t *testing.T) {
	s := a5090Setup(t)
	w := a5090Do(s, "PUT", "/dav/calendars/cal/big", a5090ICS(1<<20))
	if w.Code != http.StatusCreated {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5090Failure(t *testing.T) {
	s := a5090Setup(t)
	w := a5090Do(s, "PUT", "/dav/calendars/cal/big", a5090ICS(11<<20))
	data, _ := s.storage.GetEvent("alice@x.test", "cal", "big")
	if w.Code != http.StatusRequestEntityTooLarge || data != "" {
		t.Fatal("DEFECT F5090: CalDAV request bodies are read without a size limit")
	}
}

func TestRegressionF5090Edges(t *testing.T) {
	s := a5090Setup(t)
	big := "<?xml version=\"1.0\"?><propfind xmlns=\"DAV:\"><prop>" + strings.Repeat("<x/>", 3<<20) + "</prop></propfind>"
	if w := a5090Do(s, "PROPFIND", "/dav/calendars/cal/", big); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PROPFIND = %d, want 413", w.Code)
	}
	if w := a5090Do(s, "REPORT", "/dav/calendars/cal/", big); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized REPORT = %d, want 413", w.Code)
	}
	if w := a5090Do(s, "PUT", "/dav/calendars/cal/big", a5090ICS(4<<20)); w.Code != http.StatusCreated {
		t.Fatalf("4 MiB PUT = %d, want 201", w.Code)
	}
}
