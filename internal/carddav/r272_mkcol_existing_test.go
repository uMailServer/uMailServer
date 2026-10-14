package carddav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r272Do(s *Server, user, method, path, body, dest string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth(user, "pw")
	if dest != "" {
		req.Header.Set("Destination", dest)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

const (
	r272VC = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:X\r\nEND:VCARD\r\n"
	r272Mk = `<?xml version="1.0"?><mkcol xmlns="DAV:"><set><prop><displayname>Mine</displayname></prop></set></mkcol>`
)

// F7460: MKCOL on an existing address book is 405 and must not reset it.
func TestR272MkcolExistingAddressbook(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	if w := r272Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/ab1", r272Mk, ""); w.Code != http.StatusCreated {
		t.Fatalf("create=%d", w.Code)
	}
	if w := r272Do(s, "alice@x.test", "PUT", "/dav/addressbooks/ab1/c1.vcf", r272VC, ""); w.Code != http.StatusCreated {
		t.Fatalf("put=%d", w.Code)
	}
	before, _ := s.storage.GetAddressbook("alice@x.test", "ab1")
	w := r272Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/ab1", "", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("MKCOL existing=%d want 405", w.Code)
	}
	after, _ := s.storage.GetAddressbook("alice@x.test", "ab1")
	if after.Name != "Mine" || !after.Modified.Equal(before.Modified) || !after.Created.Equal(before.Created) {
		t.Fatalf("metadata reset: %+v -> %+v", before, after)
	}
	if d, _ := s.storage.GetContact("alice@x.test", "ab1", "c1"); d == "" {
		t.Fatal("contact lost")
	}
	// Edge: another user may use the same address book id; invalid ids stay 400.
	if w := r272Do(s, "bob@x.test", "MKCOL", "/dav/addressbooks/ab1", "", ""); w.Code != http.StatusCreated {
		t.Fatalf("bob MKCOL=%d want 201", w.Code)
	}
	if w := r272Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/a..b", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id=%d want 400", w.Code)
	}
}

// F7461: MOVE/COPY to a destination that names no valid contact is 400 and has no side effects.
func TestR272MoveCopyInvalidDestinationName(t *testing.T) {
	for _, method := range []string{"COPY", "MOVE"} {
		for _, d := range []string{"/dav/addressbooks/ab1/", "/dav/addressbooks/ab1/.vcf", "/dav/addressbooks/ab1/a..b", "/dav/addressbooks/ab1/a%2Fb.vcf", "http://h.test/dav/addressbooks/ab1/"} {
			s := NewServer(t.TempDir(), nil)
			r272Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/ab1", "", "")
			r272Do(s, "alice@x.test", "PUT", "/dav/addressbooks/ab1/c1.vcf", r272VC, "")
			w := r272Do(s, "alice@x.test", method, "/dav/addressbooks/ab1/c1.vcf", "", d)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s -> %q = %d want 400", method, d, w.Code)
			}
			all, _ := s.storage.GetContacts("alice@x.test", "ab1")
			if len(all) != 1 || !strings.Contains(all[0], "UID:c1") {
				t.Errorf("%s -> %q changed the address book: %q", method, d, all)
			}
		}
	}
	// Control: a valid destination still works.
	s := NewServer(t.TempDir(), nil)
	r272Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/ab1", "", "")
	r272Do(s, "alice@x.test", "PUT", "/dav/addressbooks/ab1/c1.vcf", r272VC, "")
	if w := r272Do(s, "alice@x.test", "COPY", "/dav/addressbooks/ab1/c1.vcf", "", "/dav/addressbooks/ab1/c2.vcf"); w.Code != http.StatusCreated {
		t.Fatalf("valid COPY=%d", w.Code)
	}
}
