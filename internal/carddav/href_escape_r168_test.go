package carddav

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"testing"
)

// F6481: listed hrefs must be percent-encoded so a contact whose UID holds
// '%', '?', '#' or a space is fetchable at the href the server returned.
func TestListedHrefIsPercentEncodedR168(t *testing.T) {
	server := hrefRoundTripCardDAVServer(t)
	if w := cardDAVHrefRoundTripRequest(t, server, "MKCOL", "/dav/addressbooks/c", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCOL = %d", w.Code)
	}
	uid := "a%b?c d#e"
	vc := "BEGIN:VCARD\nVERSION:3.0\nUID:" + uid + "\nFN:X\nEND:VCARD\n"
	put := (&url.URL{Path: "/dav/addressbooks/c/" + uid + ".vcf"}).EscapedPath()
	if w := cardDAVHrefRoundTripRequest(t, server, "PUT", put, vc); w.Code != http.StatusCreated {
		t.Fatalf("PUT %s = %d %s", put, w.Code, w.Body.String())
	}
	w := cardDAVHrefRoundTripRequest(t, server, "PROPFIND", "/dav/addressbooks/c/", "")
	var ms struct {
		R []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range ms.R {
		if r.Href == "/dav/addressbooks/c/" {
			continue
		}
		found = true
		if r.Href != put {
			t.Errorf("href = %q, want %q", r.Href, put)
		}
	}
	if !found {
		t.Error("no contact href")
	}
}
