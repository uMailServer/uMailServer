package autoconfig

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostedLookup404NoReflectionF6590(t *testing.T) {
	h := NewHandler(NewHostedProvider(nil, func(d string) bool { return d == "ok.example" }))
	r := httptest.NewRequest("GET", "/x?emailaddress=a@Evil.Example", nil)
	w := httptest.NewRecorder()
	h.HandleAutoconfig(w, r)
	if w.Code != 404 || strings.Contains(strings.ToLower(w.Body.String()), "evil") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/x?email=a@OK.example", nil)
	w = httptest.NewRecorder()
	h.HandleAutodiscover(w, r)
	if w.Code != 200 {
		t.Fatalf("hosted autodiscover %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/x?email=a@evil.example", nil)
	w = httptest.NewRecorder()
	h.HandleAutodiscover(w, r)
	if w.Code != 404 {
		t.Fatalf("unknown autodiscover %d", w.Code)
	}
}
