package caldav

import (
	"net/http"
	"testing"
)

// F6484: DELETE of a missing event is 404 (RFC 4918 §9.6.1), and extra path
// segments below an event never address the event itself.
func TestDeleteMissingAndExtraSegmentsR168(t *testing.T) {
	server := hrefRoundTripServer(t)
	hrefRoundTripRequest(t, server, "MKCALENDAR", "/dav/calendars/c", "")
	if w := hrefRoundTripRequest(t, server, "DELETE", "/dav/calendars/c/nope", ""); w.Code != http.StatusNotFound {
		t.Errorf("DELETE missing = %d, want 404", w.Code)
	}
	hrefRoundTripRequest(t, server, "PUT", "/dav/calendars/c/e1", roundTripICS)
	if w := hrefRoundTripRequest(t, server, "GET", "/dav/calendars/c/e1/extra", ""); w.Code == http.StatusOK {
		t.Errorf("GET with extra segment = 200, want not found")
	}
	if w := hrefRoundTripRequest(t, server, "DELETE", "/dav/calendars/c/e1/extra", ""); w.Code == http.StatusNoContent {
		t.Errorf("DELETE with extra segment = 204")
	}
	if w := hrefRoundTripRequest(t, server, "GET", "/dav/calendars/c/e1", ""); w.Code != http.StatusOK {
		t.Errorf("event was deleted via extra-segment path: GET = %d", w.Code)
	}
}
