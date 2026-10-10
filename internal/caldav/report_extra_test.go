package caldav

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r93xDo(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func r93xICS(uid, sum, start, end string) string {
	return "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nSUMMARY:" + sum + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func r93xSetup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	if w := r93xDo(s, "MKCALENDAR", "/dav/calendars/cal", "", nil); w.Code != 201 {
		t.Fatalf("fixture %d", w.Code)
	}
	for i, u := range []string{"a", "b", "c"} {
		d := fmt.Sprintf("2026010%dT100000Z", i+1)
		e := fmt.Sprintf("2026010%dT110000Z", i+1)
		if w := r93xDo(s, "PUT", "/dav/calendars/cal/"+u, r93xICS(u, "s"+u, d, e), nil); w.Code != 201 {
			t.Fatalf("put %d", w.Code)
		}
	}
	return s
}

const r93xHdr = `<?xml version="1.0"?>`

func TestAuditR93F5750Control(t *testing.T) {
	s := r93xSetup(t)
	w := r93xDo(s, "REPORT", "/dav/calendars/cal/", r93xHdr+`<C:calendar-query xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:D="DAV:"><D:prop><D:getetag/></D:prop><C:filter><C:comp-filter name="VCALENDAR"/></C:filter></C:calendar-query>`, nil)
	fmt.Printf("CONTROL EXPECTED: calendar-query 207 w/ 3 events ACTUAL: %d %d\n", w.Code, strings.Count(w.Body.String(), "<response>"))
	if w.Code != 207 {
		t.Fatal("invalid control")
	}
}

// F5750: rapid rewrites must yield distinct ETags
func TestAuditR93F5750EtagChanges(t *testing.T) {
	s := r93xSetup(t)
	same := 0
	prev := ""
	for i := 0; i < 40; i++ {
		w := r93xDo(s, "PUT", "/dav/calendars/cal/a", r93xICS("a", fmt.Sprintf("v%d", i), "20260101T100000Z", "20260101T110000Z"), nil)
		e := w.Header().Get("ETag")
		if e == prev {
			same++
		}
		prev = e
	}
	fmt.Printf("EXPECTED: 0 equal consecutive etags ACTUAL: %d\n", same)
	if same > 0 {
		t.Fatal("PROBLEM CONFIRMED: ETag unchanged after content change")
	}
}

// F5751: getctag + sync-token on calendar, changing with content
func TestAuditR93F5751Ctag(t *testing.T) {
	s := r93xSetup(t)
	pf := `<D:propfind xmlns:D="DAV:" xmlns:CS="http://calendarserver.org/ns/"><D:prop><CS:getctag/><D:sync-token/></D:prop></D:propfind>`
	w := r93xDo(s, "PROPFIND", "/dav/calendars/cal/", pf, map[string]string{"Depth": "0"})
	b := w.Body.String()
	fmt.Printf("EXPECTED: getctag and sync-token 200 ACTUAL: %s\n", b)
	if !strings.Contains(b, "getctag") || strings.Contains(b, "404") {
		t.Fatal("PROBLEM CONFIRMED: no getctag/sync-token")
	}
}

// F5752: calendar-multiget
func TestAuditR93F5752Multiget(t *testing.T) {
	s := r93xSetup(t)
	body := r93xHdr + `<C:calendar-multiget xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:D="DAV:"><D:prop><D:getetag/><C:calendar-data/></D:prop><D:href>/dav/calendars/cal/a</D:href><D:href>/dav/calendars/cal/zzz</D:href></C:calendar-multiget>`
	w := r93xDo(s, "REPORT", "/dav/calendars/cal/", body, nil)
	b := w.Body.String()
	fmt.Printf("EXPECTED: 207, event a returned, zzz 404, b absent ACTUAL: %d %s\n", w.Code, b)
	if w.Code != 207 || !strings.Contains(b, "cal/a<") || !strings.Contains(b, "404") || strings.Contains(b, "cal/b<") {
		t.Fatal("PROBLEM CONFIRMED: multiget unsupported")
	}
}

// F5753: sync-collection
func TestAuditR93F5753Sync(t *testing.T) {
	s := r93xSetup(t)
	body := r93xHdr + `<D:sync-collection xmlns:D="DAV:"><D:sync-token/><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`
	w := r93xDo(s, "REPORT", "/dav/calendars/cal/", body, nil)
	b := w.Body.String()
	fmt.Printf("EXPECTED: 207 w/ 3 members + sync-token ACTUAL: %d %s\n", w.Code, b)
	if w.Code != 207 || !strings.Contains(b, "sync-token") {
		t.Fatal("PROBLEM CONFIRMED: sync-collection unsupported")
	}
}

// F5754: free-busy-query
func TestAuditR93F5754FreeBusy(t *testing.T) {
	s := r93xSetup(t)
	body := r93xHdr + `<C:free-busy-query xmlns:C="urn:ietf:params:xml:ns:caldav"><C:time-range start="20260101T000000Z" end="20260102T120000Z"/></C:free-busy-query>`
	w := r93xDo(s, "REPORT", "/dav/calendars/cal/", body, nil)
	b := w.Body.String()
	fmt.Printf("EXPECTED: 200 VFREEBUSY w/ 2 FREEBUSY lines ACTUAL: %d %s\n", w.Code, b)
	if w.Code != http.StatusOK || !strings.Contains(b, "FREEBUSY") {
		t.Fatal("PROBLEM CONFIRMED: free-busy-query unsupported")
	}
}

func TestAuditR93CtagChangesAndSync(t *testing.T) {
	s := r93xSetup(t)
	ctag := func() string {
		return s.storage.GetCalendarCTag("alice@x.test", "cal")
	}
	c0 := ctag()
	r93xDo(s, "PUT", "/dav/calendars/cal/a", r93xICS("a", "changed", "20260101T100000Z", "20260101T110000Z"), nil)
	c1 := ctag()
	r93xDo(s, "DELETE", "/dav/calendars/cal/b", "", nil)
	c2 := ctag()
	if c0 == "" || c0 == c1 || c1 == c2 {
		t.Fatalf("ctag must change on PUT and DELETE: %q %q %q", c0, c1, c2)
	}
	req := func(tok string) *httptest.ResponseRecorder {
		return r93xDo(s, "REPORT", "/dav/calendars/cal/", `<D:sync-collection xmlns:D="DAV:"><D:sync-token>`+tok+`</D:sync-token><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`, nil)
	}
	if w := req(syncTokenPrefix + c1); w.Code != 403 {
		t.Fatalf("stale token must be 403, got %d", w.Code)
	}
	w := req(syncTokenPrefix + c2)
	if w.Code != 207 || strings.Contains(w.Body.String(), "<response>") {
		t.Fatalf("current token must give empty 207: %d %s", w.Code, w.Body.String())
	}
}

func TestAuditR93FreeBusyMerge(t *testing.T) {
	s := r93xSetup(t)
	w := r93xDo(s, "REPORT", "/dav/calendars/cal/", `<C:free-busy-query xmlns:C="urn:ietf:params:xml:ns:caldav"><C:time-range start="20260101T103000Z" end="20260102T103000Z"/></C:free-busy-query>`, nil)
	b := w.Body.String()
	if !strings.Contains(b, "FREEBUSY;FBTYPE=BUSY:20260101T103000Z/20260101T110000Z") || !strings.Contains(b, "FREEBUSY;FBTYPE=BUSY:20260102T100000Z/20260102T103000Z") || strings.Contains(b, "20260103") {
		t.Fatalf("unexpected free-busy: %s", b)
	}
}
