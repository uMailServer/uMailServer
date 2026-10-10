package caldav

// Regression tests for the CRLF UID defect: extractUIDFromICS used to return
// the trailing \r of CRLF-terminated lines (RFC 5545 §3.1 requires CRLF), so
// handlePut stored RFC-compliant events under a UID ending in \r while clients
// addressed the resource by the request-URL UID — GET/DELETE missed with 404
// and PROPFIND advertised hrefs containing a raw carriage return.
//
// Contract: PUT to /dav/calendars/{calendarID}/{uid} followed by GET/DELETE of
// the same URL must succeed (RFC 4918 §5; RFC 4791 §5.3.2 UID/request-URL
// identity). The LF control documents the previously working path.

import (
	"encoding/base64"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const crlfICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:evt-crlf\r\nDTSTART:20260101T100000Z\r\nSUMMARY:CRLF Event\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

// lfICS: control fixture whose body UID matches the request-URL used for it
// (handlePut prefers the body UID — the URL and the UID: line must agree).
const lfICS = "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//test//EN\nBEGIN:VEVENT\nUID:evt-lf\nDTSTART:20260101T100000Z\nSUMMARY:LF Event\nEND:VEVENT\nEND:VCALENDAR\n"

const crlfICSNoTrailingEOL = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nDTSTART:20260101T100000Z\r\nSUMMARY:Last Line UID\r\nUID:evt-lastline"

func TestExtractUIDFromICS_LineEndings(t *testing.T) {
	tests := []struct {
		name    string
		icsData string
		want    string
	}{
		{
			name:    "CRLF lines strip the carriage return",
			icsData: "BEGIN:VCALENDAR\r\nUID:test-uid\r\nEND:VCALENDAR\r\n",
			want:    "test-uid",
		},
		{
			name:    "LF lines unchanged",
			icsData: "BEGIN:VCALENDAR\nUID:test-uid\nEND:VCALENDAR\n",
			want:    "test-uid",
		},
		{
			name:    "UID as last line without trailing EOL unchanged",
			icsData: "BEGIN:VCALENDAR\r\nUID:evt-lastline",
			want:    "evt-lastline",
		},
		{
			name:    "CRLF UID with special chars",
			icsData: "BEGIN:VCALENDAR\r\nUID:test-uid@example.com\r\nEND:VCALENDAR\r\n",
			want:    "test-uid@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractUIDFromICS(tt.icsData); got != tt.want {
				t.Errorf("extractUIDFromICS() = %q, want %q", got, tt.want)
			}
		})
	}
}

func crlfUIDServer(t *testing.T) *Server {
	t.Helper()
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return username == "alice" && password == "pw", nil
	})
	return server
}

func crlfUIDRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:pw")))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

// TestCRLFUID_EventIsFetchableByRequestURL pins the RFC 5545 CRLF round trip:
// an event PUT with CRLF line endings must be fetchable and deletable at the
// request-URL the client used.
func TestCRLFUID_EventIsFetchableByRequestURL(t *testing.T) {
	server := crlfUIDServer(t)

	if w := crlfUIDRequest(t, server, "MKCALENDAR", "/dav/calendars/work-cal", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR = %d, want %d", w.Code, http.StatusCreated)
	}

	// Control: LF event round-trips.
	if w := crlfUIDRequest(t, server, "PUT", "/dav/calendars/work-cal/evt-lf", lfICS); w.Code != http.StatusCreated {
		t.Fatalf("LF PUT = %d, want %d", w.Code, http.StatusCreated)
	}
	if w := crlfUIDRequest(t, server, "GET", "/dav/calendars/work-cal/evt-lf", ""); w.Code != http.StatusOK {
		t.Fatalf("control LF GET = %d, want %d", w.Code, http.StatusOK)
	}

	// CRLF event: PUT then GET and DELETE by the same request-URL.
	if w := crlfUIDRequest(t, server, "PUT", "/dav/calendars/work-cal/evt-crlf", crlfICS); w.Code != http.StatusCreated {
		t.Fatalf("CRLF PUT = %d, want %d", w.Code, http.StatusCreated)
	}
	w := crlfUIDRequest(t, server, "GET", "/dav/calendars/work-cal/evt-crlf", "")
	if w.Code != http.StatusOK {
		t.Fatalf("CRLF GET = %d, want %d; body %q", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "UID:evt-crlf") {
		t.Errorf("CRLF GET body missing event data: %q", w.Body.String())
	}
	if w := crlfUIDRequest(t, server, "DELETE", "/dav/calendars/work-cal/evt-crlf", ""); w.Code != http.StatusNoContent {
		t.Errorf("CRLF DELETE = %d, want %d", w.Code, http.StatusNoContent)
	}
}

// TestCRLFUID_PropfindHrefsAreCleanURIs verifies PROPFIND never advertises a
// href containing a raw carriage return and lists the CRLF event under its
// request-URL UID.
func TestCRLFUID_PropfindHrefsAreCleanURIs(t *testing.T) {
	server := crlfUIDServer(t)

	if w := crlfUIDRequest(t, server, "MKCALENDAR", "/dav/calendars/work-cal", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR = %d, want %d", w.Code, http.StatusCreated)
	}
	if w := crlfUIDRequest(t, server, "PUT", "/dav/calendars/work-cal/evt-crlf", crlfICS); w.Code != http.StatusCreated {
		t.Fatalf("CRLF PUT = %d, want %d", w.Code, http.StatusCreated)
	}

	w := crlfUIDRequest(t, server, "PROPFIND", "/dav/calendars/work-cal/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	var ms hrefRoundTripMultistatus
	if err := xml.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Fatalf("cannot parse multistatus: %v", err)
	}
	found := false
	for _, r := range ms.Responses {
		if strings.ContainsRune(r.Href, '\r') {
			t.Errorf("PROPFIND href contains a raw carriage return: %q", r.Href)
		}
		if r.Href == "/dav/calendars/work-cal/evt-crlf" {
			found = true
		}
	}
	if !found {
		t.Errorf("PROPFIND did not list the CRLF event at its request-URL path; hrefs=%v", ms.Responses)
	}

	// A truncated body (no END lines) is rejected (F6070), so the unterminated
	// UID line is exercised through a body that closes without a trailing EOL.
	if w := crlfUIDRequest(t, server, "PUT", "/dav/calendars/work-cal/evt-lastline", crlfICSNoTrailingEOL); w.Code != http.StatusForbidden {
		t.Fatalf("truncated PUT = %d, want %d", w.Code, http.StatusForbidden)
	}
	complete := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nDTSTART:20260101T100000Z\r\nUID:evt-lastline\r\nEND:VEVENT\r\nEND:VCALENDAR"
	if w := crlfUIDRequest(t, server, "PUT", "/dav/calendars/work-cal/evt-lastline", complete); w.Code != http.StatusCreated {
		t.Fatalf("last-line PUT = %d, want %d", w.Code, http.StatusCreated)
	}
	if w := crlfUIDRequest(t, server, "GET", "/dav/calendars/work-cal/evt-lastline", ""); w.Code != http.StatusOK {
		t.Errorf("last-line-UID GET = %d, want %d", w.Code, http.StatusOK)
	}
}
