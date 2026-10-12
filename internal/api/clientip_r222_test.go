package api

import (
	"net/http/httptest"
	"testing"
)

func TestGetClientIPRightmostUntrusted_F6991(t *testing.T) {
	tp := []string{"10.0.0.1", "10.0.0.0/24"}
	cases := []struct{ remote, xff, want string }{
		{"10.0.0.1:5", "6.6.6.6, 203.0.113.9", "203.0.113.9"},
		{"10.0.0.1:5", "6.6.6.6, 203.0.113.9, 10.0.0.2", "203.0.113.9"},
		{"[::1]:5", "6.6.6.6", "::1"},
		{"192.0.2.1:5", "6.6.6.6", "192.0.2.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", c.xff)
		if got := getClientIP(r, tp); got != c.want {
			t.Errorf("remote=%s xff=%q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}
