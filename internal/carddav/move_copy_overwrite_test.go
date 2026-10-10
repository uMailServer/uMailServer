// Regression tests for F5483: MOVE/COPY ignored "Overwrite: F" (RFC 4918
// §10.6) and clobbered an existing destination contact.

package carddav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func overwrite5483Do(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func overwrite5483Setup(t *testing.T) *Server {
	s := NewServer(t.TempDir(), nil)
	if w := overwrite5483Do(s, "MKCOL", "/dav/addressbooks/ab1", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	for _, u := range []string{"src", "dst"} {
		vc := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + u + "\r\nFN:" + u + "\r\nEND:VCARD\r\n"
		if w := overwrite5483Do(s, "PUT", "/dav/addressbooks/ab1/"+u+".vcf", vc, nil); w.Code != http.StatusCreated {
			t.Fatalf("fixture put %d", w.Code)
		}
	}
	return s
}

func overwrite5483Body(s *Server, uid string) string {
	d, _ := s.storage.GetContact("alice@x.test", "ab1", uid)
	return d
}

func TestCardDAVOverwriteF5483Control(t *testing.T) {
	s := overwrite5483Setup(t)
	w := overwrite5483Do(s, "COPY", "/dav/addressbooks/ab1/src.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/new.vcf", "Overwrite": "F"})
	ok := w.Code < 300 && strings.Contains(overwrite5483Body(s, "new"), "UID:new")
	t.Logf("CONTROL EXPECTED: COPY Overwrite:F to free target succeeds ACTUAL: code=%d ok=%v", w.Code, ok)
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestCardDAVOverwriteF5483Failure(t *testing.T) {
	s := overwrite5483Setup(t)
	before := overwrite5483Body(s, "dst")
	wc := overwrite5483Do(s, "COPY", "/dav/addressbooks/ab1/src.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/dst.vcf", "Overwrite": "F"})
	afterCopy := overwrite5483Body(s, "dst")
	wm := overwrite5483Do(s, "MOVE", "/dav/addressbooks/ab1/src.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/dst.vcf", "Overwrite": "F"})
	afterMove := overwrite5483Body(s, "dst")
	t.Logf("EXPECTED: COPY Overwrite:F onto existing -> 412, dst unchanged ACTUAL: code=%d unchanged=%v", wc.Code, afterCopy == before)
	t.Logf("EXPECTED: MOVE Overwrite:F onto existing -> 412, dst unchanged ACTUAL: code=%d unchanged=%v src_kept=%v", wm.Code, afterMove == before, overwrite5483Body(s, "src") != "")
	if wc.Code != http.StatusPreconditionFailed || afterCopy != before || wm.Code != http.StatusPreconditionFailed || afterMove != before {
		t.Fatal("DEFECT F5483: Overwrite: F ignored, existing destination clobbered")
	}
}

func TestCardDAVOverwriteF5483Edges(t *testing.T) {
	s := overwrite5483Setup(t)
	// Overwrite: T (and absent) still overwrite.
	if w := overwrite5483Do(s, "COPY", "/dav/addressbooks/ab1/src.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/dst.vcf", "Overwrite": "T"}); w.Code >= 300 || !strings.Contains(overwrite5483Body(s, "dst"), "FN:src") {
		t.Fatalf("overwrite T code %d", w.Code)
	}
	// lowercase f with spaces honored.
	if w := overwrite5483Do(s, "MOVE", "/dav/addressbooks/ab1/src.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/dst.vcf", "Overwrite": " f "}); w.Code != http.StatusPreconditionFailed || overwrite5483Body(s, "src") == "" {
		t.Fatalf("lowercase f code %d", w.Code)
	}
	// Overwrite absent on MOVE overwrites and removes source.
	if w := overwrite5483Do(s, "MOVE", "/dav/addressbooks/ab1/src.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/dst.vcf"}); w.Code >= 300 || overwrite5483Body(s, "src") != "" {
		t.Fatalf("absent overwrite move code %d", w.Code)
	}
}
