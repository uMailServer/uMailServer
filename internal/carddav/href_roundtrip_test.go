package carddav

// Regression tests for the CardDAV href round-trip defect: the PROPFIND href
// builders used to advertise "/dav/addressbooks/{username}/{addressbookID}/..."
// while every item and report handler parses the request convention
// "/dav/addressbooks/{addressbookID}[/{contactUID}.vcf]" (the authenticated
// username scopes storage and is not part of the URL). RFC 6352 §7.2.1
// discovery plus RFC 4918 §5.3/§8.3 require clients to operate on the hrefs
// the server returns, so a contact listed by PROPFIND could not be fetched or
// deleted via its own href (the item handlers parsed the username segment as
// the addressbook ID and returned 403).
//
// Fixtures use LF line endings; extractUIDFromVCard TrimSpaces lines, so CRLF
// vCards are handled (unlike the CalDAV defect fixed separately).

import (
	"encoding/base64"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cardDAVHrefRoundTripVCard = "BEGIN:VCARD\nVERSION:3.0\nUID:alice\nFN:Alice Proof\nEND:VCARD\n"

func hrefRoundTripCardDAVServer(t *testing.T) *Server {
	t.Helper()
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return username == "alice" && password == "pw", nil
	})
	return server
}

func cardDAVHrefRoundTripRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:pw")))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

// TestCardDAVHrefRoundTrip_ListedContactIsUsable drives the standard client
// flow: create addressbook, PUT a contact, PROPFIND the home, then GET and
// DELETE the contact at the exact href the server returned (RFC 4918 §8.3).
func TestCardDAVHrefRoundTrip_ListedContactIsUsable(t *testing.T) {
	server := hrefRoundTripCardDAVServer(t)

	if w := cardDAVHrefRoundTripRequest(t, server, "MKCOL", "/dav/addressbooks/contacts", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCOL = %d, want %d", w.Code, http.StatusCreated)
	}

	// Control: the no-username item convention round-trips.
	if w := cardDAVHrefRoundTripRequest(t, server, "PUT", "/dav/addressbooks/contacts/alice.vcf", cardDAVHrefRoundTripVCard); w.Code != http.StatusCreated {
		t.Fatalf("PUT = %d, want %d", w.Code, http.StatusCreated)
	}
	if w := cardDAVHrefRoundTripRequest(t, server, "GET", "/dav/addressbooks/contacts/alice.vcf", ""); w.Code != http.StatusOK {
		t.Fatalf("control GET = %d, want %d", w.Code, http.StatusOK)
	}

	// Discovery: the advertised addressbook href must use the request
	// convention.
	w := cardDAVHrefRoundTripRequest(t, server, "PROPFIND", "/dav/addressbooks/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	var ms Multistatus
	if err := xml.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Fatalf("cannot parse multistatus: %v\nbody:\n%s", err, w.Body.String())
	}
	var abHref, contactHref string
	for _, r := range ms.Responses {
		if strings.ContainsRune(r.Href, '\r') {
			t.Errorf("PROPFIND href contains a carriage return: %q", r.Href)
		}
		if r.Href == "/dav/addressbooks/contacts/" {
			abHref = r.Href
		}
		if strings.HasSuffix(r.Href, "/alice.vcf") {
			contactHref = r.Href
		}
	}
	if abHref == "" {
		t.Errorf("PROPFIND did not advertise the addressbook at /dav/addressbooks/contacts/; hrefs=%v", ms.Responses)
	}
	if contactHref != "/dav/addressbooks/contacts/alice.vcf" {
		t.Errorf("advertised contact href = %q, want /dav/addressbooks/contacts/alice.vcf (request convention)", contactHref)
	}

	// The listed contact must be fetchable and deletable at its advertised href.
	if w := cardDAVHrefRoundTripRequest(t, server, "GET", contactHref, ""); w.Code != http.StatusOK {
		t.Errorf("GET advertised href %q = %d, want %d", contactHref, w.Code, http.StatusOK)
	} else if !strings.Contains(w.Body.String(), "UID:alice") {
		t.Errorf("GET advertised href %q returned wrong body: %q", contactHref, w.Body.String())
	}
	if w := cardDAVHrefRoundTripRequest(t, server, "DELETE", contactHref, ""); w.Code != http.StatusNoContent {
		t.Errorf("DELETE advertised href %q = %d, want %d", contactHref, w.Code, http.StatusNoContent)
	}
}

// TestCardDAVHrefRoundTrip_PrincipalHomeSetUsesConvention verifies the
// addressbook-home-set URL advertised by the principal response lists the
// user's addressbooks when PROPFINDed (RFC 6352 §7.2.1 discovery).
func TestCardDAVHrefRoundTrip_PrincipalHomeSetUsesConvention(t *testing.T) {
	server := hrefRoundTripCardDAVServer(t)

	if w := cardDAVHrefRoundTripRequest(t, server, "MKCOL", "/dav/addressbooks/contacts", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCOL = %d, want %d", w.Code, http.StatusCreated)
	}

	w := cardDAVHrefRoundTripRequest(t, server, "PROPFIND", "/dav/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /dav/ = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	body := w.Body.String()
	marker := strings.Index(body, "addressbook-home-set")
	if marker == -1 {
		t.Fatalf("principal response has no addressbook-home-set:\n%s", body)
	}
	urlStart := strings.Index(body[marker:], "/dav/addressbooks/")
	if urlStart == -1 {
		t.Fatalf("addressbook-home-set contains no /dav/addressbooks/ URL:\n%s", body)
	}
	rest := body[marker+urlStart:]
	homeSet := rest
	if end := strings.IndexAny(rest, "&<"); end != -1 {
		homeSet = rest[:end]
	}

	w = cardDAVHrefRoundTripRequest(t, server, "PROPFIND", homeSet, "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND advertised home-set %q = %d, want %d", homeSet, w.Code, http.StatusMultiStatus)
	}
	if !strings.Contains(w.Body.String(), "/dav/addressbooks/contacts/") {
		t.Errorf("PROPFIND of advertised home-set %q did not advertise the addressbook at its request-convention path; body:\n%s", homeSet, w.Body.String())
	}
}

// TestCardDAVHrefRoundTrip_ReportUsesConvention verifies REPORT lists contacts
// under the request-convention hrefs as well (buildContactResponse is shared).
func TestCardDAVHrefRoundTrip_ReportUsesConvention(t *testing.T) {
	server := hrefRoundTripCardDAVServer(t)

	if w := cardDAVHrefRoundTripRequest(t, server, "MKCOL", "/dav/addressbooks/contacts", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCOL = %d, want %d", w.Code, http.StatusCreated)
	}
	if w := cardDAVHrefRoundTripRequest(t, server, "PUT", "/dav/addressbooks/contacts/alice.vcf", cardDAVHrefRoundTripVCard); w.Code != http.StatusCreated {
		t.Fatalf("PUT = %d, want %d", w.Code, http.StatusCreated)
	}

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<C:addressbook-query xmlns:C="urn:ietf:params:xml:ns:carddav"/>`
	w := cardDAVHrefRoundTripRequest(t, server, "REPORT", "/dav/addressbooks/contacts", reportBody)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("REPORT = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	if !strings.Contains(w.Body.String(), "/dav/addressbooks/contacts/alice.vcf") {
		t.Errorf("REPORT did not list the contact at its request-convention href; body:\n%s", w.Body.String())
	}
}
