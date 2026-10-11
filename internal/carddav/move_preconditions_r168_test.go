package carddav

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r168Do(t *testing.T, s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:pw")))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

// F6482: MOVE/COPY must honour If-Match/If-None-Match on the source (RFC
// 7232 §3.1); a stale If-Match is 412 and nothing is moved or copied.
func TestMoveCopyHonourSourcePreconditionsR168(t *testing.T) {
	for _, method := range []string{"MOVE", "COPY"} {
		s := hrefRoundTripCardDAVServer(t)
		r168Do(t, s, "MKCOL", "/dav/addressbooks/c", "", nil)
		if w := r168Do(t, s, "PUT", "/dav/addressbooks/c/a.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:a\nFN:A\nEND:VCARD\n", nil); w.Code != http.StatusCreated {
			t.Fatalf("PUT = %d", w.Code)
		}
		w := r168Do(t, s, method, "/dav/addressbooks/c/a.vcf", "", map[string]string{"Destination": "/dav/addressbooks/c/b.vcf", "If-Match": `"stale"`})
		if w.Code != http.StatusPreconditionFailed {
			t.Errorf("%s with stale If-Match = %d, want 412", method, w.Code)
		}
		if g := r168Do(t, s, "GET", "/dav/addressbooks/c/b.vcf", "", nil); g.Code != http.StatusNotFound {
			t.Errorf("%s: destination created despite 412 (GET = %d)", method, g.Code)
		}
	}
}

// F6483: MOVE/COPY must resolve resource names exactly as PUT/GET do (only a
// trailing .vcf is an extension); a dotted UID without extension was truncated.
func TestMoveCopyDottedNameR168(t *testing.T) {
	s := hrefRoundTripCardDAVServer(t)
	r168Do(t, s, "MKCOL", "/dav/addressbooks/c", "", nil)
	if w := r168Do(t, s, "PUT", "/dav/addressbooks/c/john.smith", "BEGIN:VCARD\nVERSION:3.0\nUID:john.smith\nFN:J\nEND:VCARD\n", nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT = %d", w.Code)
	}
	w := r168Do(t, s, "COPY", "/dav/addressbooks/c/john.smith", "", map[string]string{"Destination": "/dav/addressbooks/c/jane.doe"})
	if w.Code != http.StatusCreated {
		t.Fatalf("COPY = %d %s", w.Code, w.Body.String())
	}
	if g := r168Do(t, s, "GET", "/dav/addressbooks/c/jane.doe", "", nil); g.Code != http.StatusOK || !strings.Contains(g.Body.String(), "UID:jane.doe") {
		t.Errorf("GET jane.doe = %d %q", g.Code, g.Body.String())
	}
}
