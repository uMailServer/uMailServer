package carddav

// Regression tests for the PUT UID-identity defect: handlePut stored the
// contact under the vCard BODY UID (no URL comparison at all), so a PUT to
// /dav/addressbooks/{ab}/{uid}.vcf whose vCard UID differed created a
// resource unreachable at its request-URL (GET 404) while the PUT reported
// 201. RFC 6352 §6.3.2 makes the request-URI's UID authoritative and
// requires rejecting a mismatch (403). UID-less vCards now adopt the URL UID
// (previously a random unreachable UUID was generated).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func putUIDAddressbook(t *testing.T) (*Server, func(path, uid string) *httptest.ResponseRecorder) {
	t.Helper()
	server := NewServer(t.TempDir(), nil)

	mk := httptest.NewRequest("MKCOL", "/dav/addressbooks/ab-1", nil)
	mk.SetBasicAuth("alice", "pw")
	mkw := httptest.NewRecorder()
	server.ServeHTTP(mkw, mk)
	if mkw.Code != http.StatusCreated {
		t.Fatalf("CONTROL FAILED (harness): MKCOL = %d, want 201", mkw.Code)
	}

	put := func(path, uid string) *httptest.ResponseRecorder {
		t.Helper()
		body := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Proof\r\nEND:VCARD\r\n"
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		return w
	}

	// Control: matching UIDs — the normal client case — must round-trip.
	if w := put("/dav/addressbooks/ab-1/contact-a.vcf", "contact-a"); w.Code != http.StatusCreated {
		t.Fatalf("CONTROL FAILED (harness): matching-UID PUT = %d, want 201", w.Code)
	}
	get := httptest.NewRequest(http.MethodGet, "/dav/addressbooks/ab-1/contact-a.vcf", nil)
	get.SetBasicAuth("alice", "pw")
	gw := httptest.NewRecorder()
	server.ServeHTTP(gw, get)
	if gw.Code != http.StatusOK {
		t.Fatalf("CONTROL FAILED (harness): matching-UID contact not fetchable: %d", gw.Code)
	}

	return server, put
}

func TestHandlePutRejectsMismatchedUID(t *testing.T) {
	server, put := putUIDAddressbook(t)
	if w := put("/dav/addressbooks/ab-1/contact-2.vcf", "contact-other"); w.Code != http.StatusForbidden {
		t.Fatalf("FAIL: mismatched-UID PUT = %d, want 403 per RFC 6352 §6.3.2 (a mismatch would store the contact at an address the client cannot address)", w.Code)
	}

	// Nothing may be stored at either address after the rejection.
	for _, uid := range []string{"contact-2", "contact-other"} {
		req := httptest.NewRequest(http.MethodGet, "/dav/addressbooks/ab-1/"+uid+".vcf", nil)
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Fatalf("FAIL: rejected PUT still stored a resource at %q", uid)
		}
	}
}

func TestHandlePutUIDLessVCardAdoptsURLUid(t *testing.T) {
	server, _ := putUIDAddressbook(t)

	// UID-less vCard must adopt the URL UID (not an unreachable random UUID).
	body := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:No UID\r\nEND:VCARD\r\n"
	req := httptest.NewRequest(http.MethodPut, "/dav/addressbooks/ab-1/contact-nouid.vcf", strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("FAIL: UID-less vCard PUT = %d, want 201", w.Code)
	}

	get := httptest.NewRequest(http.MethodGet, "/dav/addressbooks/ab-1/contact-nouid.vcf", nil)
	get.SetBasicAuth("alice", "pw")
	gw := httptest.NewRecorder()
	server.ServeHTTP(gw, get)
	if gw.Code != http.StatusOK {
		t.Fatalf("FAIL: UID-less vCard not addressable at its request-URL: %d", gw.Code)
	}
	if !strings.Contains(gw.Body.String(), "UID:contact-nouid") {
		t.Fatal("FAIL: stored vCard body was not made self-describing with the URL UID")
	}
}
