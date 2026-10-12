package audit

import (
	"net/http/httptest"
	"testing"
)

func TestExtractIPRightmostUntrusted_F6990(t *testing.T) {
	SetTrustedProxies([]string{"10.0.0.0/8"})
	defer SetTrustedProxies(nil)
	cases := []struct{ remote, xff, xri, want string }{
		{"10.0.0.1:1", "6.6.6.6, 203.0.113.9", "", "203.0.113.9"},
		{"10.0.0.1:1", "6.6.6.6, 203.0.113.9, 10.0.0.5", "", "203.0.113.9"},
		{"10.0.0.1:1", "10.0.0.7, 10.0.0.5", "", "10.0.0.1"},
		{"10.0.0.1:1", "6.6.6.6, garbage", "", "10.0.0.1"},
		{"10.0.0.1:1", "", "198.51.100.2", "198.51.100.2"},
		{"192.0.2.1:1", "6.6.6.6", "", "192.0.2.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.xri != "" {
			r.Header.Set("X-Real-IP", c.xri)
		}
		if got := ExtractIP(r); got != c.want {
			t.Errorf("remote=%s xff=%q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}
