// Regression tests for Round 107 F5890-F5899: CardDAV parity with CalDAV
// round 93 (sync-collection, PUT/DELETE status semantics, vCard parsing).

package carddav

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

const r107Card = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:Alice\r\nEND:VCARD\r\n"

func r107Setup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	if w := q76Do(s, q76User, "MKCOL", "/dav/addressbooks/ab", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("mkcol %d", w.Code)
	}
	if w := q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/c1.vcf", r107Card, nil); w.Code != http.StatusCreated {
		t.Fatalf("put %d", w.Code)
	}
	return s
}

const r107Sync = `<?xml version="1.0"?><D:sync-collection xmlns:D="DAV:"><D:sync-token>%s</D:sync-token><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`

var r107TokenRe = regexp.MustCompile(`<sync-token>([^<]*)</sync-token>`)

// F5890: sync-collection (RFC 6578) must return a sync-token, an empty
// change set for an unchanged token and 403 valid-sync-token for a stale one.
func TestR107SyncCollection(t *testing.T) {
	s := r107Setup(t)
	rep := func(tok string) (int, string) {
		w := q76Do(s, q76User, "REPORT", "/dav/addressbooks/ab/", strings.Replace(r107Sync, "%s", tok, 1), nil)
		return w.Code, w.Body.String()
	}
	code, body := rep("")
	m := r107TokenRe.FindStringSubmatch(body)
	if code != 207 || m == nil || !strings.Contains(body, "c1.vcf") {
		t.Fatalf("initial sync %d %s", code, body)
	}
	code, body2 := rep(m[1])
	if code != 207 || strings.Contains(body2, "c1.vcf") {
		t.Fatalf("unchanged sync %d %s", code, body2)
	}
	q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/c2.vcf", strings.ReplaceAll(r107Card, "c1", "c2"), nil)
	if code, _ = rep(m[1]); code != http.StatusForbidden {
		t.Fatalf("stale token = %d, want 403", code)
	}
	if !strings.Contains(supportedReportSet().Raw, "sync-collection") {
		t.Fatal("sync-collection not advertised")
	}
}

// F5891: updating an existing contact answers 204, creating 201.
func TestR107PutUpdateStatus(t *testing.T) {
	s := r107Setup(t)
	if w := q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/c1.vcf", r107Card, nil); w.Code != http.StatusNoContent {
		t.Fatalf("update = %d, want 204", w.Code)
	}
}

// F5892: DELETE of a missing contact answers 404.
func TestR107DeleteMissing(t *testing.T) {
	s := r107Setup(t)
	if w := q76Do(s, q76User, "DELETE", "/dav/addressbooks/ab/nope.vcf", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", w.Code)
	}
}

// F5893: only the .vcf suffix is a resource extension; dotted UIDs survive.
func TestR107DottedUID(t *testing.T) {
	s := r107Setup(t)
	vc := strings.ReplaceAll(r107Card, "c1", "john.doe")
	if w := q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/john.doe", vc, nil); w.Code != http.StatusCreated {
		t.Fatalf("put %d", w.Code)
	}
	if w := q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/john.vcf", strings.ReplaceAll(r107Card, "c1", "john"), nil); w.Code != http.StatusCreated {
		t.Fatalf("put john %d", w.Code)
	}
	mg := `<C:addressbook-multiget xmlns:C="urn:ietf:params:xml:ns:carddav" xmlns:D="DAV:"><D:prop><D:getetag/></D:prop><D:href>/dav/addressbooks/ab/john.doe.vcf</D:href></C:addressbook-multiget>`
	w := q76Do(s, q76User, "REPORT", "/dav/addressbooks/ab/", mg, nil)
	if !strings.Contains(w.Body.String(), "200 OK") {
		t.Fatalf("multiget dotted: %s", w.Body.String())
	}
	if w := q76Do(s, q76User, "GET", "/dav/addressbooks/ab/john.doe.vcf", "", nil); w.Code != 200 {
		t.Fatalf("get dotted %d", w.Code)
	}
}

// F5894: a truncated vCard (no END:VCARD) is rejected.
func TestR107TruncatedVCard(t *testing.T) {
	s := r107Setup(t)
	w := q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/t.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:t\r\nFN:T\r\n", nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "valid-address-data") {
		t.Fatalf("truncated = %d %s", w.Code, w.Body.String())
	}
}

// F5895: quoted-printable values (vCard 2.1/3.0 ENCODING=QUOTED-PRINTABLE,
// incl. soft line breaks) are decoded before text-match.
func TestR107QuotedPrintableMatch(t *testing.T) {
	vc := "BEGIN:VCARD\r\nVERSION:2.1\r\nUID:q\r\nFN;ENCODING=QUOTED-PRINTABLE;CHARSET=UTF-8:=C3=87a=C4=9Fla =\r\n=C3=96z\r\nEND:VCARD\r\n"
	props := parseVCardProps(vc)
	var fn string
	for _, p := range props {
		if p.name == "FN" {
			fn = p.value
		}
	}
	if fn != "Çağla Öz" {
		t.Fatalf("FN = %q", fn)
	}
}

// F5896: a bare-CR folded vCard still unfolds and ETags change on update.
func TestR107ETagChangesOnUpdate(t *testing.T) {
	s := r107Setup(t)
	e1 := q76Do(s, q76User, "GET", "/dav/addressbooks/ab/c1.vcf", "", nil).Header().Get("ETag")
	q76Do(s, q76User, "PUT", "/dav/addressbooks/ab/c1.vcf", strings.Replace(r107Card, "Alice", "Alicia", 1), nil)
	e2 := q76Do(s, q76User, "GET", "/dav/addressbooks/ab/c1.vcf", "", nil).Header().Get("ETag")
	if e1 == "" || e1 == e2 {
		t.Fatalf("etag %q -> %q", e1, e2)
	}
}

// F5897: sync-collection on an unknown address book fails like the other reports.
func TestR107SyncUnknownBook(t *testing.T) {
	s := r107Setup(t)
	w := q76Do(s, q76User, "REPORT", "/dav/addressbooks/zzz/", strings.Replace(r107Sync, "%s", "", 1), nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unknown book = %d", w.Code)
	}
}
