package webhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The IPv4-compatible IPv6 range ::/96 (deprecated by RFC 4291) embeds an
// IPv4 address in its low 32 bits and routes as ordinary global unicast on
// common stacks — the same embed-and-route-to-internal-IPv4 class as the
// blocked NAT64 and 6to4 forms. A webhook target given as a literal
// http://[::10.0.0.1]/ must be rejected at registration, and isBlockedIP
// (which also guards resolved DNS answers and every redirect target) must
// refuse such addresses.
func TestIsBlockedIPRefusesIPv4CompatibleV6(t *testing.T) {
	for _, a := range []string{"::127.0.0.1", "::10.0.0.5", "::169.254.169.254", "::a00:1"} {
		if !isBlockedIP(netParse(a)) {
			t.Errorf("%s allowed (want blocked: deprecated IPv4-compatible form embeds a non-public IPv4 address)", a)
		}
	}
	// The mapped (::ffff:0:0/96) forms keep working.
	for _, a := range []string{"::ffff:127.0.0.1", "::ffff:10.0.0.5"} {
		if !isBlockedIP(netParse(a)) {
			t.Errorf("%s allowed (mapped form must stay blocked)", a)
		}
	}
	// Public space stays reachable.
	for _, a := range []string{"1.1.1.1", "2606:4700:4700::1111", "::ffff:1.1.1.1"} {
		if isBlockedIP(netParse(a)) {
			t.Errorf("%s blocked (public addresses must stay reachable)", a)
		}
	}
}

func TestRegistrationRejectsIPv4CompatibleV6Literal(t *testing.T) {
	m := NewManager(nil, "")
	for _, u := range []string{"http://[::127.0.0.1]/hook", "http://[::10.0.0.1]/hook", "http://[::a9fe:a9fe]/hook"} {
		rr := httptest.NewRecorder()
		m.HTTPHandler(rr, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"url":"`+u+`","events":["*"]}`)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d want 400", u, rr.Code)
		}
	}
}
