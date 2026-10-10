// Regression test for F5484: MOVE onto itself (RFC 4918 §9.9.4) must be
// rejected with 403 instead of saving then deleting the same contact file.

package carddav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func moveSelf5484Do(s *Server, method, path, dest string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	req.SetBasicAuth("alice@x.test", "pw")
	if dest != "" {
		req.Header.Set("Destination", dest)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func moveSelf5484Setup(t *testing.T) *Server {
	s := NewServer(t.TempDir(), nil)
	if w := moveSelf5484Do(s, "MKCOL", "/dav/addressbooks/ab1", ""); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	req := httptest.NewRequest("PUT", "/dav/addressbooks/ab1/c1.vcf", strings.NewReader("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:Keep\r\nEND:VCARD\r\n"))
	req.SetBasicAuth("alice@x.test", "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("fixture put %d", w.Code)
	}
	return s
}

func moveSelf5484Has(s *Server, uid string) bool {
	d, _ := s.storage.GetContact("alice@x.test", "ab1", uid)
	return d != ""
}

func TestCardDAVMoveSelfF5484Control(t *testing.T) {
	s := moveSelf5484Setup(t)
	w := moveSelf5484Do(s, "MOVE", "/dav/addressbooks/ab1/c1.vcf", "/dav/addressbooks/ab1/c2.vcf")
	ok := w.Code < 300 && moveSelf5484Has(s, "c2") && !moveSelf5484Has(s, "c1")
	t.Logf("CONTROL EXPECTED: MOVE c1->c2 renames ACTUAL: code=%d ok=%v", w.Code, ok)
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestCardDAVMoveSelfF5484Failure(t *testing.T) {
	s := moveSelf5484Setup(t)
	w := moveSelf5484Do(s, "MOVE", "/dav/addressbooks/ab1/c1.vcf", "/dav/addressbooks/ab1/c1.vcf")
	kept := moveSelf5484Has(s, "c1")
	t.Logf("EXPECTED: MOVE onto itself -> 403, contact kept (RFC 4918 §9.9.4) ACTUAL: code=%d kept=%v", w.Code, kept)
	if !kept {
		t.Fatal("DEFECT F5484: MOVE onto itself deletes the contact")
	}
	if w.Code != http.StatusForbidden {
		t.Fatal("DEFECT F5484: MOVE onto itself not rejected")
	}
}

func TestCardDAVMoveSelfF5484Edges(t *testing.T) {
	s := moveSelf5484Setup(t)
	// Same resource spelled differently (absolute URI, no extension) is still the same resource.
	for _, d := range []string{"http://h.example/dav/addressbooks/ab1/c1.vcf", "/dav/addressbooks/ab1/c1"} {
		if w := moveSelf5484Do(s, "MOVE", "/dav/addressbooks/ab1/c1.vcf", d); w.Code != http.StatusForbidden || !moveSelf5484Has(s, "c1") {
			t.Fatalf("%s code=%d kept=%v", d, w.Code, moveSelf5484Has(s, "c1"))
		}
	}
	// COPY onto itself leaves the contact intact.
	moveSelf5484Do(s, "COPY", "/dav/addressbooks/ab1/c1.vcf", "/dav/addressbooks/ab1/c1.vcf")
	if !moveSelf5484Has(s, "c1") {
		t.Fatal("copy self lost contact")
	}
}
