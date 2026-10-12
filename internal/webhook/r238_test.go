package webhook

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF7152_RegistrationRejectsBlockedLiteral(t *testing.T) {
	m := NewManager(nil, "")
	for _, u := range []string{"http://127.0.0.1/x", "http://localhost/x", "http://169.254.169.254/", "http://[64:ff9b::7f00:1]/", "http://10.0.0.1/"} {
		rr := httptest.NewRecorder()
		m.HTTPHandler(rr, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"url":"`+u+`","events":["*"]}`)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d want 400", u, rr.Code)
		}
	}
	if !isBlockedIP(netParse("64:ff9b::a00:1")) {
		t.Error("NAT64 must be blocked")
	}
}

func netParse(s string) net.IP { return net.ParseIP(s) }
