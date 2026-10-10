package webhook

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestR110_BlockedRanges(t *testing.T) {
	for _, ip := range []string{"100.100.100.200", "100.64.0.1", "198.18.0.1", "224.0.0.1", "ff02::1", "240.0.0.1"} {
		if !isBlockedIP(net.ParseIP(ip)) {
			t.Errorf("%s not blocked", ip)
		}
	}
	if isBlockedIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 blocked")
	}
}

func TestR110_UniqueHookIDs(t *testing.T) {
	m := NewManager(nil, "")
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		m.HTTPHandler(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"url":"https://example.com","events":["*"]}`)))
		id := strings.Split(w.Body.String(), `"id":"`)[1]
		id = id[:strings.Index(id, `"`)]
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
}

func TestR110_CreateBodyLimit(t *testing.T) {
	m := NewManager(nil, "")
	w := httptest.NewRecorder()
	m.HTTPHandler(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"url":"https://e.com/`+strings.Repeat("a", 1<<20)+`"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code %d", w.Code)
	}
}
