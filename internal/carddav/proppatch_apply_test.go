// Regression tests for F5665: PROPPATCH must apply what it reports, refuse
// what it cannot apply, and answer with a 207 Multi-Status (RFC 4918 §9.2).

package carddav

import (
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

const proppatchHead = `<D:propertyupdate xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">`

func proppatchBook(t *testing.T, s *Server) *Addressbook {
	t.Helper()
	ab, err := s.storage.GetAddressbook("alice@x.test", "ab1")
	if err != nil || ab == nil {
		t.Fatalf("book: %v %v", ab, err)
	}
	return ab
}

func proppatchStatuses(t *testing.T, body string) map[string]string {
	t.Helper()
	var ms discMS
	if err := xml.Unmarshal([]byte(body), &ms); err != nil {
		t.Fatalf("DEFECT F5665: PROPPATCH body is not a multistatus: %v\n%s", err, body)
	}
	out := map[string]string{}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			for _, it := range ps.items() {
				out[it.XMLName.Local] = ps.Status
			}
		}
	}
	return out
}

func TestCardDAVProppatchF5665Control(t *testing.T) {
	s := discSetup(t)
	// Unaffected path: an unknown address book is 404 and a PROPPATCH without
	// a book ID is 400.
	body := proppatchHead + `<D:set><D:prop><D:displayname>X</D:displayname></D:prop></D:set></D:propertyupdate>`
	if w := discDo(s, "PROPPATCH", "/dav/addressbooks/nope/", body, nil); w.Code != http.StatusNotFound {
		t.Fatalf("control: unknown book = %d, want 404", w.Code)
	}
	if w := discDo(s, "PROPPATCH", "/dav/addressbooks/", body, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("control: no book = %d, want 400", w.Code)
	}
}

func TestCardDAVProppatchF5665Success(t *testing.T) {
	s := discSetup(t)
	defer func() {
		if ab := proppatchBook(t, s); ab.Name != "Renamed" || ab.Description != "Desc2" {
			t.Errorf("DEFECT F5665: acknowledged PROPPATCH did not apply: name=%q desc=%q", ab.Name, ab.Description)
		}
	}()
	w := discDo(s, "PROPPATCH", "/dav/addressbooks/ab1/", proppatchHead+`<D:set><D:prop><D:displayname>Renamed</D:displayname><C:addressbook-description>Desc2</C:addressbook-description></D:prop></D:set></D:propertyupdate>`, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("DEFECT F5665: PROPPATCH = %d, want 207\n%s", w.Code, w.Body.String())
	}
	st := proppatchStatuses(t, w.Body.String())
	if !strings.Contains(st["displayname"], " 200 ") || !strings.Contains(st["addressbook-description"], " 200 ") {
		t.Errorf("DEFECT F5665: statuses %v", st)
	}
}

func TestCardDAVProppatchF5665Failure(t *testing.T) {
	s := discSetup(t)
	// Malformed body: must be rejected, not acknowledged with 200.
	if w := discDo(s, "PROPPATCH", "/dav/addressbooks/ab1/", "garbage", nil); w.Code != http.StatusBadRequest {
		t.Errorf("DEFECT F5665: garbage body\nEXPECTED: 400\nACTUAL: %d", w.Code)
	}
	// A protected property and an unsupported dead property: the request is
	// atomic, so nothing is applied and the statuses say why.
	w := discDo(s, "PROPPATCH", "/dav/addressbooks/ab1/", proppatchHead+`<D:set><D:prop><D:displayname>Hijack</D:displayname><D:getetag>"x"</D:getetag><D:foo>bar</D:foo></D:prop></D:set></D:propertyupdate>`, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("DEFECT F5665: PROPPATCH = %d, want 207", w.Code)
	}
	st := proppatchStatuses(t, w.Body.String())
	if !strings.Contains(st["getetag"], " 403 ") || !strings.Contains(st["foo"], " 403 ") || !strings.Contains(st["displayname"], " 424 ") {
		t.Errorf("DEFECT F5665: statuses\nEXPECTED: getetag 403, foo 403, displayname 424\nACTUAL: %v", st)
	}
	if ab := proppatchBook(t, s); ab.Name != "Main" {
		t.Errorf("DEFECT F5665: failed PROPPATCH still renamed the book to %q", ab.Name)
	}
}

func TestCardDAVProppatchF5665Edges(t *testing.T) {
	s := discSetup(t)
	// PROPPATCH on a contact URL must not write to the address book.
	w := discDo(s, "PROPPATCH", "/dav/addressbooks/ab1/c1.vcf", proppatchHead+`<D:set><D:prop><D:displayname>FromContact</D:displayname></D:prop></D:set></D:propertyupdate>`, nil)
	if w.Code == http.StatusOK || w.Code == http.StatusMultiStatus && proppatchBook(t, s).Name != "Main" {
		t.Errorf("DEFECT F5665: contact PROPPATCH = %d and book name %q", w.Code, proppatchBook(t, s).Name)
	}
	if ab := proppatchBook(t, s); ab.Name != "Main" {
		t.Errorf("DEFECT F5665: contact PROPPATCH renamed the book to %q", ab.Name)
	}
	// remove: description cleared, an unknown property is a no-op success.
	w = discDo(s, "PROPPATCH", "/dav/addressbooks/ab1/", proppatchHead+`<D:remove><D:prop><C:addressbook-description/><D:foo/></D:prop></D:remove></D:propertyupdate>`, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("DEFECT F5665: remove = %d", w.Code)
	}
	if ab := proppatchBook(t, s); ab.Description != "" {
		t.Errorf("DEFECT F5665: description not removed: %q", ab.Description)
	}
	// Document order: a later set wins over an earlier one.
	discDo(s, "PROPPATCH", "/dav/addressbooks/ab1/", proppatchHead+`<D:set><D:prop><D:displayname>A</D:displayname></D:prop></D:set><D:set><D:prop><D:displayname>B</D:displayname></D:prop></D:set></D:propertyupdate>`, nil)
	if ab := proppatchBook(t, s); ab.Name != "B" {
		t.Errorf("DEFECT F5665: last set did not win: %q", ab.Name)
	}
}
