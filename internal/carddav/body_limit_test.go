// Regression tests for F5091: request bodies were read without any size limit.
// Promoted from .temp_files/case_F5091_carddav_body_limit_test.go.

package carddav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func a5091Do(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5091VCF(pad int) string {
	return "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:big\r\nNOTE:" + strings.Repeat("A", pad) + "\r\nEND:VCARD\r\n"
}

func a5091Setup(t *testing.T) *Server {
	s := NewServer(t.TempDir(), nil)
	if w := a5091Do(s, "MKCOL", "/dav/addressbooks/ab", ""); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	return s
}

func TestRegressionF5091Control(t *testing.T) {
	s := a5091Setup(t)
	w := a5091Do(s, "PUT", "/dav/addressbooks/ab/big.vcf", a5091VCF(1<<20))
	if w.Code != http.StatusCreated {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5091Failure(t *testing.T) {
	s := a5091Setup(t)
	w := a5091Do(s, "PUT", "/dav/addressbooks/ab/big.vcf", a5091VCF(11<<20))
	data, _ := s.storage.GetContact("alice@x.test", "ab", "big")
	if w.Code != http.StatusRequestEntityTooLarge || data != "" {
		t.Fatal("DEFECT F5091: CardDAV request bodies are read without a size limit")
	}
}

func TestRegressionF5091Edges(t *testing.T) {
	s := a5091Setup(t)
	big := "<?xml version=\"1.0\"?><mkcol xmlns=\"DAV:\"><set><prop><displayname>" + strings.Repeat("A", 11<<20) + "</displayname></prop></set></mkcol>"
	if w := a5091Do(s, "MKCOL", "/dav/addressbooks/ab2", big); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized MKCOL = %d, want 413", w.Code)
	}
	if ab, _ := s.storage.GetAddressbook("alice@x.test", "ab2"); ab != nil {
		t.Fatal("oversized MKCOL still created the address book")
	}
	if w := a5091Do(s, "PROPPATCH", "/dav/addressbooks/ab", big); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PROPPATCH = %d, want 413", w.Code)
	}
	if w := a5091Do(s, "PUT", "/dav/addressbooks/ab/big.vcf", a5091VCF(4<<20)); w.Code != http.StatusCreated {
		t.Fatalf("4 MiB PUT = %d, want 201", w.Code)
	}
}
