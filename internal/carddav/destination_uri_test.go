// Regression tests for F5093: an absolute-URI Destination header (RFC 4918 §10.3) was not parsed, so MOVE/COPY always failed.
// Promoted from .temp_files/case_F5093_carddav_destination_uri_test.go.

package carddav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func a5093Do(s *Server, method, path, body, dest string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	if dest != "" {
		req.Header.Set("Destination", dest)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5093Setup(t *testing.T) *Server {
	s := NewServer(t.TempDir(), nil)
	for _, c := range []string{"ab1", "ab2"} {
		if w := a5093Do(s, "MKCOL", "/dav/addressbooks/"+c, "", ""); w.Code != http.StatusCreated {
			t.Fatalf("fixture %d", w.Code)
		}
	}
	vc := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:Move Me\r\nEND:VCARD\r\n"
	if w := a5093Do(s, "PUT", "/dav/addressbooks/ab1/c1.vcf", vc, ""); w.Code != http.StatusCreated {
		t.Fatalf("fixture put %d", w.Code)
	}
	return s
}

func a5093Has(s *Server, ab, uid string) bool {
	d, _ := s.storage.GetContact("alice@x.test", ab, uid)
	return d != ""
}

func TestRegressionF5093Control(t *testing.T) {
	s := a5093Setup(t)
	w := a5093Do(s, "MOVE", "/dav/addressbooks/ab1/c1.vcf", "", "/dav/addressbooks/ab2/c1.vcf")
	ok := w.Code < 300 && a5093Has(s, "ab2", "c1") && !a5093Has(s, "ab1", "c1")
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5093Failure(t *testing.T) {
	s := a5093Setup(t)
	w := a5093Do(s, "MOVE", "/dav/addressbooks/ab1/c1.vcf", "", "http://example.com/dav/addressbooks/ab2/c1.vcf")
	moved := a5093Has(s, "ab2", "c1") && !a5093Has(s, "ab1", "c1")
	if w.Code >= 300 || !moved {
		t.Fatal("DEFECT F5093: CardDAV MOVE/COPY rejects an absolute-URI Destination")
	}
}

func TestRegressionF5093Edges(t *testing.T) {
	s := a5093Setup(t)
	if w := a5093Do(s, "COPY", "/dav/addressbooks/ab1/c1.vcf", "", "http://example.com/dav/addressbooks/ab2/c%20copy.vcf"); w.Code >= 300 {
		t.Fatalf("COPY absolute = %d", w.Code)
	}
	if !a5093Has(s, "ab2", "c copy") {
		t.Fatal("COPY did not store the decoded destination name")
	}
	if w := a5093Do(s, "COPY", "/dav/addressbooks/ab1/c1.vcf", "", "/dav/calendars/ab2/x.vcf"); w.Code < 400 {
		t.Fatalf("out-of-namespace Destination accepted: %d", w.Code)
	}
}
