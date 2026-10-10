// Regression tests for Round 76 F5570–F5572: addressbook-query filter
// evaluation (RFC 6352 §10.5), CARDDAV:supported-collation (§8.3) and the
// addressbook-multiget REPORT (§8.7).

package carddav

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const q76User = "alice@x.test"

func q76Do(s *Server, user, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth(user, "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func q76Setup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	if w := q76Do(s, q76User, "MKCOL", "/dav/addressbooks/ab1", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture mkcol ab1 %d", w.Code)
	}
	if w := q76Do(s, q76User, "MKCOL", "/dav/addressbooks/ab2", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture mkcol ab2 %d", w.Code)
	}
	cards := map[string]string{
		"/dav/addressbooks/ab1/c1.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:Alice Smith\r\nEMAIL;TYPE=work:alice@example.com\r\nNICKNAME:Ally\r\nEND:VCARD\r\n",
		"/dav/addressbooks/ab1/c2.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c2\r\nFN:Bob Jones\r\nEMAIL;TYPE=home:bob@example.org\r\nitem1.TEL;TYPE=cell:+1555\r\nEND:VCARD\r\n",
		"/dav/addressbooks/ab1/c3.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c3\r\nFN:Çağla Öz\r\nEND:VCARD\r\n",
		"/dav/addressbooks/ab2/d1.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:d1\r\nFN:Other Book\r\nEND:VCARD\r\n",
	}
	for p, c := range cards {
		if w := q76Do(s, q76User, "PUT", p, c, nil); w.Code != http.StatusCreated {
			t.Fatalf("fixture put %s %d", p, w.Code)
		}
	}
	return s
}

var q76HrefRe = regexp.MustCompile(`<href>([^<]*)</href>`)

func q76Hrefs(body string) []string {
	var out []string
	for _, m := range q76HrefRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

func q76Query(filter string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop><D:getetag/><C:address-data/></D:prop>
  ` + filter + `
</C:addressbook-query>`
}

func q76Report(s *Server, body string) (int, []string, string) {
	w := q76Do(s, q76User, "REPORT", "/dav/addressbooks/ab1/", body, map[string]string{"Depth": "1"})
	return w.Code, q76Hrefs(w.Body.String()), w.Body.String()
}

func q76Want(names ...string) string {
	var out []string
	for _, n := range names {
		out = append(out, "/dav/addressbooks/ab1/"+n+".vcf")
	}
	sort.Strings(out)
	return fmt.Sprint(out)
}

// ---- F5570: addressbook-query filter ignored ----

func TestCardDAVQueryF5570Control(t *testing.T) {
	s := q76Setup(t)
	code, hrefs, _ := q76Report(s, q76Query(""))
	t.Logf("CONTROL EXPECTED: no filter -> 207 %s ACTUAL: %d %v", q76Want("c1", "c2", "c3"), code, hrefs)
	if code != http.StatusMultiStatus || fmt.Sprint(hrefs) != q76Want("c1", "c2", "c3") {
		t.Fatal("invalid control")
	}
}

func TestCardDAVQueryF5570Failure(t *testing.T) {
	s := q76Setup(t)
	code, hrefs, _ := q76Report(s, q76Query(`<C:filter><C:prop-filter name="FN"><C:text-match match-type="contains">alice</C:text-match></C:prop-filter></C:filter>`))
	t.Logf("EXPECTED: FN contains alice -> 207 %s ACTUAL: %d %v", q76Want("c1"), code, hrefs)
	if code != http.StatusMultiStatus || fmt.Sprint(hrefs) != q76Want("c1") {
		t.Fatal("DEFECT F5570: addressbook-query ignores the filter")
	}
}

func TestCardDAVQueryF5570Edges(t *testing.T) {
	s := q76Setup(t)
	cases := []struct {
		name, filter string
		want         []string
	}{
		{"equals", `<C:filter><C:prop-filter name="NICKNAME"><C:text-match match-type="equals">ally</C:text-match></C:prop-filter></C:filter>`, []string{"c1"}},
		{"equals-partial", `<C:filter><C:prop-filter name="NICKNAME"><C:text-match match-type="equals">all</C:text-match></C:prop-filter></C:filter>`, nil},
		{"starts-with", `<C:filter><C:prop-filter name="FN"><C:text-match match-type="starts-with">BOB</C:text-match></C:prop-filter></C:filter>`, []string{"c2"}},
		{"ends-with", `<C:filter><C:prop-filter name="EMAIL"><C:text-match match-type="ends-with">.org</C:text-match></C:prop-filter></C:filter>`, []string{"c2"}},
		{"negate", `<C:filter><C:prop-filter name="FN"><C:text-match negate-condition="yes">alice</C:text-match></C:prop-filter></C:filter>`, []string{"c2", "c3"}},
		{"is-not-defined", `<C:filter><C:prop-filter name="EMAIL"><C:is-not-defined/></C:prop-filter></C:filter>`, []string{"c3"}},
		{"defined-empty", `<C:filter><C:prop-filter name="email"/></C:filter>`, []string{"c1", "c2"}},
		{"param-filter", `<C:filter><C:prop-filter name="EMAIL"><C:param-filter name="TYPE"><C:text-match match-type="equals">home</C:text-match></C:param-filter></C:prop-filter></C:filter>`, []string{"c2"}},
		{"group-any", `<C:filter><C:prop-filter name="TEL"/></C:filter>`, []string{"c2"}},
		{"group-exact-miss", `<C:filter><C:prop-filter name="item2.TEL"/></C:filter>`, nil},
		{"anyof", `<C:filter test="anyof"><C:prop-filter name="FN"><C:text-match>alice</C:text-match></C:prop-filter><C:prop-filter name="FN"><C:text-match>bob</C:text-match></C:prop-filter></C:filter>`, []string{"c1", "c2"}},
		{"allof", `<C:filter test="allof"><C:prop-filter name="FN"><C:text-match>alice</C:text-match></C:prop-filter><C:prop-filter name="FN"><C:text-match>bob</C:text-match></C:prop-filter></C:filter>`, nil},
		{"prop-allof", `<C:filter><C:prop-filter name="FN" test="allof"><C:text-match>bob</C:text-match><C:text-match>jones</C:text-match></C:prop-filter></C:filter>`, []string{"c2"}},
		{"unicode-casemap", `<C:filter><C:prop-filter name="FN"><C:text-match collation="i;unicode-casemap">ÇAĞLA</C:text-match></C:prop-filter></C:filter>`, []string{"c3"}},
		{"default-collation", `<C:filter><C:prop-filter name="FN"><C:text-match>öz</C:text-match></C:prop-filter></C:filter>`, []string{"c3"}},
		{"ascii-casemap", `<C:filter><C:prop-filter name="FN"><C:text-match collation="i;ascii-casemap">ÇAĞLA</C:text-match></C:prop-filter></C:filter>`, nil},
		{"octet", `<C:filter><C:prop-filter name="FN"><C:text-match collation="i;octet">alice</C:text-match></C:prop-filter></C:filter>`, nil},
		{"empty-filter", `<C:filter/>`, []string{"c1", "c2", "c3"}},
	}
	for _, tc := range cases {
		code, hrefs, _ := q76Report(s, q76Query(tc.filter))
		want := q76Want(tc.want...)
		t.Logf("%s EXPECTED: 207 %s ACTUAL: %d %v", tc.name, want, code, hrefs)
		if code != http.StatusMultiStatus || fmt.Sprint(hrefs) != want {
			t.Errorf("DEFECT F5570 edge %s", tc.name)
		}
	}
}

// ---- F5571: unsupported collation accepted ----

func TestCardDAVQueryF5571Control(t *testing.T) {
	s := q76Setup(t)
	code, _, _ := q76Report(s, q76Query(`<C:filter><C:prop-filter name="FN"><C:text-match collation="i;ascii-casemap">alice</C:text-match></C:prop-filter></C:filter>`))
	t.Logf("CONTROL EXPECTED: supported collation -> 207 ACTUAL: %d", code)
	if code != http.StatusMultiStatus {
		t.Fatal("invalid control")
	}
}

func TestCardDAVQueryF5571Failure(t *testing.T) {
	s := q76Setup(t)
	code, _, body := q76Report(s, q76Query(`<C:filter><C:prop-filter name="FN"><C:text-match collation="i;bogus">alice</C:text-match></C:prop-filter></C:filter>`))
	t.Logf("EXPECTED: unsupported collation -> 403 + supported-collation (RFC 6352 §8.3) ACTUAL: %d supported-collation=%v", code, strings.Contains(body, "supported-collation"))
	if code != http.StatusForbidden || !strings.Contains(body, "supported-collation") {
		t.Fatal("DEFECT F5571: unsupported collation not rejected")
	}
}

func TestCardDAVQueryF5571Edges(t *testing.T) {
	s := q76Setup(t)
	code, _, body := q76Report(s, q76Query(`<C:filter><C:prop-filter name="EMAIL"><C:param-filter name="TYPE"><C:text-match collation="i;x-nope">home</C:text-match></C:param-filter></C:prop-filter></C:filter>`))
	t.Logf("param-filter EXPECTED: 403 supported-collation ACTUAL: %d %v", code, strings.Contains(body, "supported-collation"))
	if code != http.StatusForbidden || !strings.Contains(body, "supported-collation") {
		t.Error("DEFECT F5571 edge param-filter")
	}
	code, hrefs, _ := q76Report(s, q76Query(`<C:filter><C:prop-filter name="FN"><C:text-match collation="default">ALICE</C:text-match></C:prop-filter></C:filter>`))
	t.Logf("default EXPECTED: 207 %s ACTUAL: %d %v", q76Want("c1"), code, hrefs)
	if code != http.StatusMultiStatus || fmt.Sprint(hrefs) != q76Want("c1") {
		t.Error("DEFECT F5571 edge default")
	}
	code, _, _ = q76Report(s, q76Query(`<C:filter><C:prop-filter name="FN"><C:text-match match-type="regex">a</C:text-match></C:prop-filter></C:filter>`))
	t.Logf("bad match-type EXPECTED: 400 ACTUAL: %d", code)
	if code != http.StatusBadRequest {
		t.Error("DEFECT F5571 edge match-type")
	}
}

// ---- F5572: addressbook-multiget unsupported ----

func q76Multiget(hrefs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<C:addressbook-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop><D:getetag/><C:address-data/></D:prop>
`)
	for _, h := range hrefs {
		b.WriteString("  <D:href>" + h + "</D:href>\n")
	}
	b.WriteString("</C:addressbook-multiget>")
	return b.String()
}

// q76Status returns the status text of the response whose href is href.
func q76Status(body, href string) string {
	for _, part := range strings.Split(body, "<response>")[1:] {
		if strings.Contains(part, "<href>"+href+"</href>") {
			if i := strings.Index(part, "<status>"); i >= 0 {
				return part[i+len("<status>") : i+strings.Index(part[i:], "</status>")]
			}
		}
	}
	return ""
}

func TestCardDAVQueryF5572Control(t *testing.T) {
	s := q76Setup(t)
	code, hrefs, _ := q76Report(s, q76Query(""))
	t.Logf("CONTROL EXPECTED: addressbook-query 207 lists 3 ACTUAL: %d %d", code, len(hrefs))
	if code != http.StatusMultiStatus || len(hrefs) != 3 {
		t.Fatal("invalid control")
	}
}

func TestCardDAVQueryF5572Failure(t *testing.T) {
	s := q76Setup(t)
	w := q76Do(s, q76User, "REPORT", "/dav/addressbooks/ab1/", q76Multiget("/dav/addressbooks/ab1/c1.vcf", "/dav/addressbooks/ab1/c3.vcf"), map[string]string{"Depth": "0"})
	hrefs := q76Hrefs(w.Body.String())
	data := strings.Contains(w.Body.String(), "Alice Smith") && !strings.Contains(w.Body.String(), "Bob Jones")
	t.Logf("EXPECTED: multiget -> 207 %s with address-data ACTUAL: %d %v data=%v", q76Want("c1", "c3"), w.Code, hrefs, data)
	if w.Code != http.StatusMultiStatus || fmt.Sprint(hrefs) != q76Want("c1", "c3") || !data {
		t.Fatal("DEFECT F5572: addressbook-multiget not supported")
	}
}

func TestCardDAVQueryF5572Edges(t *testing.T) {
	s := q76Setup(t)
	missing := "/dav/addressbooks/ab1/nope.vcf"
	other := "/dav/addressbooks/ab2/d1.vcf"
	trav := "/dav/addressbooks/ab1/..%2F..%2Fx.vcf"
	abs := "https://mail.x.test/dav/addressbooks/ab1/c2.vcf"
	w := q76Do(s, q76User, "REPORT", "/dav/addressbooks/ab1", q76Multiget(missing, other, trav, abs), map[string]string{"Depth": "0"})
	body := w.Body.String()
	t.Logf("EXPECTED: 207; missing 404; other-book 404/403; traversal 404/403; absolute c2 200 ACTUAL: %d missing=%q other=%q trav=%q abs=%q otherData=%v", w.Code,
		q76Status(body, missing), q76Status(body, other), q76Status(body, trav), q76Status(body, abs), strings.Contains(body, "Other Book"))
	if w.Code != http.StatusMultiStatus || !strings.Contains(q76Status(body, missing), "404") ||
		strings.Contains(q76Status(body, other), "200") || q76Status(body, other) == "" ||
		strings.Contains(q76Status(body, trav), "200") || q76Status(body, trav) == "" ||
		!strings.Contains(q76Status(body, abs), "200") || !strings.Contains(body, "Bob Jones") || strings.Contains(body, "Other Book") {
		t.Error("DEFECT F5572 edge statuses")
	}
	// Another user naming alice's href only reaches its own (empty) storage.
	_ = q76Do(s, "bob@x.test", "MKCOL", "/dav/addressbooks/ab1", "", nil)
	w = q76Do(s, "bob@x.test", "REPORT", "/dav/addressbooks/ab1/", q76Multiget("/dav/addressbooks/ab1/c1.vcf"), map[string]string{"Depth": "0"})
	t.Logf("cross-user EXPECTED: 207, no alice data ACTUAL: %d leak=%v", w.Code, strings.Contains(w.Body.String(), "Alice Smith"))
	if w.Code != http.StatusMultiStatus || strings.Contains(w.Body.String(), "Alice Smith") {
		t.Error("DEFECT F5572 edge cross-user")
	}
	// No href at all is malformed (RFC 6352 §10.7 DAV:href+).
	w = q76Do(s, q76User, "REPORT", "/dav/addressbooks/ab1/", q76Multiget(), map[string]string{"Depth": "0"})
	t.Logf("no-href EXPECTED: 400 ACTUAL: %d", w.Code)
	if w.Code != http.StatusBadRequest {
		t.Error("DEFECT F5572 edge no href")
	}
}
