package autoconfig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F6551: Autodiscover must answer only for a plain address with a
// hostname-shaped domain, and use the bare address (not a display-name
// form) as login name.
func TestR177AutodiscoverAddressHandling(t *testing.T) {
	h := NewHandler(nil)
	get := func(email string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/autodiscover/autodiscover.xml", nil)
		q := req.URL.Query()
		q.Set("email", email)
		req.URL.RawQuery = q.Encode()
		w := httptest.NewRecorder()
		h.HandleAutodiscover(w, req)
		return w
	}
	for _, bad := range []string{"a@[1.2.3.4]", "a@localhost", "a@-x.com", "a@exa_mple.com"} {
		if w := get(bad); w.Code != http.StatusBadRequest {
			t.Errorf("%q: got %d want 400 body=%s", bad, w.Code, w.Body.String())
		}
	}
	w := get("Eve <bob@example.com>")
	if w.Code == http.StatusOK {
		if strings.Contains(w.Body.String(), "Eve") {
			t.Errorf("display-name leaked into login/user: %s", w.Body.String())
		}
	}
}

func TestR177AutoconfigRejectsBadHostLabels(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/x?emailaddress=a@exa_mple.com", nil)
	req.Host = "autoconfig.example.com"
	w := httptest.NewRecorder()
	h.HandleAutoconfig(w, req)
	if strings.Contains(w.Body.String(), "exa_mple") {
		t.Errorf("invalid domain reflected: %s", w.Body.String())
	}
}
