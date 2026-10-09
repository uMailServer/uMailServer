// Regression tests for F5085: a contact UID containing "../" in GET/COPY/MOVE read another user's contact.
// Promoted from .temp_files/case_F5085_carddav_traversal_read_test.go.

package carddav

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func a5085Do(s *Server, user, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.SetBasicAuth(user, "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5085Setup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	if w := a5085Do(s, "bob@x.test", "MKCOL", "/dav/addressbooks/bobab", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture MKCOL bob %d", w.Code)
	}
	vc := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:secret\r\nFN:Bob Secret Contact\r\nEND:VCARD\r\n"
	if w := a5085Do(s, "bob@x.test", "PUT", "/dav/addressbooks/bobab/secret.vcf", vc, nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture PUT bob %d", w.Code)
	}
	if w := a5085Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/ab", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture MKCOL alice %d", w.Code)
	}
	return s
}

func TestRegressionF5085Control(t *testing.T) {
	s := a5085Setup(t)
	w := a5085Do(s, "bob@x.test", "GET", "/dav/addressbooks/bobab/secret.vcf", "", nil)
	ok := w.Code == http.StatusOK && strings.Contains(w.Body.String(), "Bob Secret Contact")
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5085Failure(t *testing.T) {
	s := a5085Setup(t)
	w := a5085Do(s, "alice@x.test", "GET", "/dav/addressbooks/ab/../../bob_at_x.test/bobab/secret.vcf", "", nil)
	leaked := strings.Contains(w.Body.String(), "Bob Secret Contact")
	if leaked || w.Code < 400 {
		t.Fatal("DEFECT F5085: contact UID path traversal reads another user's contact")
	}
}

func TestRegressionF5085Edges(t *testing.T) {
	s := a5085Setup(t)
	// COPY with a traversing source must not copy bob's contact into alice's book.
	w := a5085Do(s, "alice@x.test", "COPY", "/dav/addressbooks/ab/../../bob_at_x.test/bobab/secret.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab/stolen.vcf"})
	g := a5085Do(s, "alice@x.test", "GET", "/dav/addressbooks/ab/stolen.vcf", "", nil)
	if strings.Contains(g.Body.String(), "Bob Secret Contact") {
		t.Fatalf("DEFECT F5085: COPY exfiltrated bob's contact (copy=%d)", w.Code)
	}
	// MOVE with a traversing source must not copy either.
	a5085Do(s, "alice@x.test", "MOVE", "/dav/addressbooks/ab/../../bob_at_x.test/bobab/secret.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab/stolen2.vcf"})
	g = a5085Do(s, "alice@x.test", "GET", "/dav/addressbooks/ab/stolen2.vcf", "", nil)
	if strings.Contains(g.Body.String(), "Bob Secret Contact") {
		t.Fatal("DEFECT F5085: MOVE exfiltrated bob's contact")
	}
	// Bob's contact must survive intact.
	if g := a5085Do(s, "bob@x.test", "GET", "/dav/addressbooks/bobab/secret.vcf", "", nil); g.Code != http.StatusOK {
		t.Fatalf("bob's contact lost: %d", g.Code)
	}
}
