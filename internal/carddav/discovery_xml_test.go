// Regression tests for F5660-F5662 and F5666: PROPFIND/REPORT responses must be
// namespace-correct WebDAV (RFC 4918 §14), must honour the requested property
// list (RFC 4918 §9.1) and must carry the properties CardDAV clients discover
// with (RFC 6352 §5.2, §6.2.2, §8.3.1). Parsing here is XML-namespace-aware,
// like iOS, DAVx5 and Thunderbird.

package carddav

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const (
	discNSDAV     = "DAV:"
	discNSCardDAV = "urn:ietf:params:xml:ns:carddav"
)

// Namespace-aware model (what iOS, DAVx5 and Thunderbird parse).
type nsMS struct {
	XMLName   xml.Name `xml:"DAV: multistatus"`
	Responses []nsResp `xml:"DAV: response"`
}

type nsResp struct {
	Href      string `xml:"DAV: href"`
	Status    string `xml:"DAV: status"`
	Propstats []nsPS `xml:"DAV: propstat"`
}

type nsPS struct {
	Status string `xml:"DAV: status"`
	Prop   struct {
		Items []discItem `xml:",any"`
	} `xml:"DAV: prop"`
}

// Local-name model: used by the proofs that must not depend on F5660.
type discMS struct {
	Responses []discResp `xml:"response"`
}

type discResp struct {
	Href      string   `xml:"href"`
	Status    string   `xml:"status"`
	Propstats []discPS `xml:"propstat"`
}

// discPS tolerates properties that are direct children of propstat (the
// pre-F5660 output has no DAV:prop wrapper): Direct holds every child, and
// items() picks the property elements out of it.
type discPS struct {
	Status string `xml:"status"`
	Prop   struct {
		Items []discItem `xml:",any"`
	} `xml:"prop"`
	Direct []discItem `xml:",any"`
}

func (p discPS) items() []discItem {
	out := append([]discItem{}, p.Prop.Items...)
	for _, d := range p.Direct {
		if d.XMLName.Local != "prop" && d.XMLName.Local != "status" {
			out = append(out, d)
		}
	}
	return out
}

type discItem struct {
	XMLName  xml.Name
	Text     string     `xml:",chardata"`
	Children []discItem `xml:",any"`
}

func (i discItem) childNames() []xml.Name {
	var out []xml.Name
	for _, c := range i.Children {
		out = append(out, c.XMLName)
	}
	return out
}

func discDo(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func discSetup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	if err := s.storage.CreateAddressbook("alice@x.test", &Addressbook{ID: "ab1", Name: "Main", Description: "My book"}); err != nil {
		t.Fatal(err)
	}
	vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:Alice\r\nEND:VCARD\r\n"
	if w := discDo(s, "PUT", "/dav/addressbooks/ab1/c1.vcf", vcf, nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture PUT = %d", w.Code)
	}
	return s
}

// discParse is the local-name parse (namespaces ignored).
func discParse(t *testing.T, w *httptest.ResponseRecorder) (discMS, bool) {
	t.Helper()
	var ms discMS
	if err := xml.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Errorf("DEFECT: body is not a multistatus: %v\n%s", err, w.Body.String())
		return ms, false
	}
	return ms, true
}

// discProps returns local name -> item for the 200 propstats of href.
func discProps(ms discMS, href string) map[xml.Name]discItem {
	out := map[xml.Name]discItem{}
	for _, r := range ms.Responses {
		if r.Href != href {
			continue
		}
		for _, ps := range r.Propstats {
			if strings.Contains(ps.Status, " 200 ") {
				for _, it := range ps.items() {
					out[xml.Name{Local: it.XMLName.Local}] = it
				}
			}
		}
	}
	return out
}

// nsParse is the namespace-aware parse (F5660).
func nsParse(t *testing.T, w *httptest.ResponseRecorder) (nsMS, bool) {
	t.Helper()
	var ms nsMS
	if err := xml.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Errorf("DEFECT F5660: body is not a DAV: multistatus: %v\n%s", err, w.Body.String())
		return ms, false
	}
	return ms, true
}

// nsProps returns the fully qualified name -> item map of the 200 propstats.
func nsProps(ms nsMS, href string) map[xml.Name]discItem {
	out := map[xml.Name]discItem{}
	for _, r := range ms.Responses {
		if r.Href != href {
			continue
		}
		for _, ps := range r.Propstats {
			if strings.Contains(ps.Status, " 200 ") {
				for _, it := range ps.Prop.Items {
					out[it.XMLName] = it
				}
			}
		}
	}
	return out
}

func TestCardDAVDiscoveryF5660Control(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", "", map[string]string{"Depth": "0"})
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND = %d", w.Code)
	}
	// Control: the href and the book name are present as text in any parse.
	if !strings.Contains(w.Body.String(), "/dav/addressbooks/ab1/") || !strings.Contains(w.Body.String(), "Main") {
		t.Fatalf("control body lacks href/name:\n%s", w.Body.String())
	}
}

func TestCardDAVDiscoveryF5660Failure(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", "", map[string]string{"Depth": "0"})
	ms, ok := nsParse(t, w)
	if !ok {
		return
	}
	p := nsProps(ms, "/dav/addressbooks/ab1/")
	rt := p[xml.Name{Space: discNSDAV, Local: "resourcetype"}]
	got := rt.childNames()
	want := []xml.Name{{Space: discNSDAV, Local: "collection"}, {Space: discNSCardDAV, Local: "addressbook"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("DEFECT F5660: resourcetype children\nEXPECTED: %v\nACTUAL: %v (text %q)", want, got, strings.TrimSpace(rt.Text))
	}
	if _, ok := p[xml.Name{Space: discNSCardDAV, Local: "addressbook-description"}]; !ok {
		t.Errorf("DEFECT F5660: addressbook-description not in %s namespace; got %v", discNSCardDAV, keys(p))
	}
	if it, ok := p[xml.Name{Space: discNSDAV, Local: "displayname"}]; !ok || it.Text != "Main" {
		t.Errorf("DEFECT F5660: DAV:displayname = %q (found=%v)", it.Text, ok)
	}
}

func TestCardDAVDiscoveryF5660Edges(t *testing.T) {
	s := discSetup(t)
	// Principal and home resources: nested resourcetype children and a nested href.
	w := discDo(s, "PROPFIND", "/dav/", "", map[string]string{"Depth": "0"})
	ms, ok := nsParse(t, w)
	if !ok {
		return
	}
	pr := nsProps(ms, "/dav/principals/alice@x.test/")
	if got := pr[xml.Name{Space: discNSDAV, Local: "resourcetype"}].childNames(); len(got) != 2 || got[1] != (xml.Name{Space: discNSDAV, Local: "principal"}) {
		t.Errorf("DEFECT F5660: principal resourcetype = %v", got)
	}
	hs := pr[xml.Name{Space: discNSCardDAV, Local: "addressbook-home-set"}]
	if got := hs.childNames(); len(got) != 1 || got[0] != (xml.Name{Space: discNSDAV, Local: "href"}) || strings.TrimSpace(hs.Children[0].Text) != "/dav/addressbooks/" {
		t.Errorf("DEFECT F5660: addressbook-home-set = %+v", hs)
	}
	home := nsProps(ms, "/dav/addressbooks/")
	if got := home[xml.Name{Space: discNSDAV, Local: "resourcetype"}].childNames(); len(got) != 1 || got[0] != (xml.Name{Space: discNSDAV, Local: "collection"}) {
		t.Errorf("DEFECT F5660: home resourcetype = %v", got)
	}
	// REPORT responses use the same namespaces; address-data is CardDAV-namespaced.
	rw := discDo(s, "REPORT", "/dav/addressbooks/ab1/", `<C:addressbook-query xmlns:C="urn:ietf:params:xml:ns:carddav" xmlns:D="DAV:"><D:prop><D:getetag/><C:address-data/></D:prop></C:addressbook-query>`, nil)
	rms, ok := nsParse(t, rw)
	if !ok {
		return
	}
	rp := nsProps(rms, "/dav/addressbooks/ab1/c1.vcf")
	if it, ok := rp[xml.Name{Space: discNSCardDAV, Local: "address-data"}]; !ok || !strings.Contains(it.Text, "UID:c1") {
		t.Errorf("DEFECT F5660: address-data missing/incorrect: %v", keys(rp))
	}
}

func keys(m map[xml.Name]discItem) []xml.Name {
	var out []xml.Name
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestCardDAVDiscoveryF5661Control(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "OPTIONS", "/dav/addressbooks/ab1/", "", nil)
	if !strings.Contains(w.Header().Get("DAV"), "addressbook") {
		t.Fatalf("OPTIONS DAV header = %q", w.Header().Get("DAV"))
	}
}

func TestCardDAVDiscoveryF5661Failure(t *testing.T) {
	s := discSetup(t)
	body := `<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:supported-report-set/><C:supported-collation-set/></D:prop></D:propfind>`
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", body, map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	p := discProps(ms, "/dav/addressbooks/ab1/")
	srs := p[xml.Name{Local: "supported-report-set"}]
	var reports []xml.Name
	for _, sr := range srs.Children {
		for _, rep := range sr.Children {
			reports = append(reports, rep.childNames()...)
		}
	}
	if len(reports) != 3 || reports[0] != (xml.Name{Space: discNSCardDAV, Local: "addressbook-query"}) || reports[1] != (xml.Name{Space: discNSCardDAV, Local: "addressbook-multiget"}) || reports[2] != (xml.Name{Space: "DAV:", Local: "sync-collection"}) {
		t.Errorf("DEFECT F5661: supported-report-set\nEXPECTED: addressbook-query, addressbook-multiget, sync-collection\nACTUAL: %v", reports)
	}
	var colls []string
	for _, c := range p[xml.Name{Local: "supported-collation-set"}].Children {
		if c.XMLName == (xml.Name{Space: discNSCardDAV, Local: "supported-collation"}) {
			colls = append(colls, c.Text)
		}
	}
	if strings.Join(colls, ",") != "i;ascii-casemap,i;octet,i;unicode-casemap" {
		t.Errorf("DEFECT F5661: supported-collation-set\nEXPECTED: i;ascii-casemap,i;octet,i;unicode-casemap\nACTUAL: %v", colls)
	}
}

func TestCardDAVDiscoveryF5662Control(t *testing.T) {
	s := discSetup(t)
	// No body (allprop): the fixed default set stays available.
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", "", map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	if len(discProps(ms, "/dav/addressbooks/ab1/")) < 4 {
		t.Errorf("allprop returned too few properties")
	}
}

func TestCardDAVDiscoveryF5662Failure(t *testing.T) {
	s := discSetup(t)
	body := `<D:propfind xmlns:D="DAV:"><D:prop><D:displayname/><D:no-such-prop/></D:prop></D:propfind>`
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", body, map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	if len(ms.Responses) != 1 {
		t.Fatalf("DEFECT F5662: responses = %d", len(ms.Responses))
	}
	got200, got404 := []string{}, []string{}
	for _, ps := range ms.Responses[0].Propstats {
		for _, it := range ps.items() {
			switch {
			case strings.Contains(ps.Status, " 200 "):
				got200 = append(got200, it.XMLName.Local)
			case strings.Contains(ps.Status, " 404 "):
				got404 = append(got404, it.XMLName.Local)
			}
		}
	}
	if strings.Join(got200, ",") != "displayname" || strings.Join(got404, ",") != "no-such-prop" {
		t.Errorf("DEFECT F5662: requested displayname + no-such-prop\nEXPECTED: 200=[displayname] 404=[no-such-prop]\nACTUAL: 200=%v 404=%v", got200, got404)
	}
}

func TestCardDAVDiscoveryF5662Edges(t *testing.T) {
	s := discSetup(t)
	// propname lists names without values.
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/", `<D:propfind xmlns:D="DAV:"><D:propname/></D:propfind>`, map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	p := discProps(ms, "/dav/addressbooks/ab1/")
	if it, ok := p[xml.Name{Local: "displayname"}]; !ok || it.Text != "" {
		t.Errorf("DEFECT F5662: propname displayname = %+v found=%v", it, ok)
	}
	// REPORT honours the requested list: only getetag, no address-data.
	rw := discDo(s, "REPORT", "/dav/addressbooks/ab1/", `<C:addressbook-query xmlns:C="urn:ietf:params:xml:ns:carddav" xmlns:D="DAV:"><D:prop><D:getetag/></D:prop></C:addressbook-query>`, nil)
	rms, ok := discParse(t, rw)
	if !ok {
		return
	}
	rp := discProps(rms, "/dav/addressbooks/ab1/c1.vcf")
	if _, has := rp[xml.Name{Local: "address-data"}]; has || len(rp) != 1 {
		t.Errorf("DEFECT F5662: REPORT prop=getetag returned %v", keys(rp))
	}
	// An unknown property in a REPORT is a 404 propstat.
	mw := discDo(s, "REPORT", "/dav/addressbooks/ab1/", `<C:addressbook-multiget xmlns:C="urn:ietf:params:xml:ns:carddav" xmlns:D="DAV:"><D:prop><D:getetag/><D:bogus/></D:prop><D:href>/dav/addressbooks/ab1/c1.vcf</D:href></C:addressbook-multiget>`, nil)
	mms, ok := discParse(t, mw)
	if !ok {
		return
	}
	saw404 := false
	for _, r := range mms.Responses {
		for _, ps := range r.Propstats {
			if strings.Contains(ps.Status, " 404 ") && len(ps.items()) == 1 && ps.items()[0].XMLName.Local == "bogus" {
				saw404 = true
			}
		}
	}
	if !saw404 {
		t.Errorf("DEFECT F5662: multiget bogus prop not in a 404 propstat:\n%s", mw.Body.String())
	}
}

func TestCardDAVDiscoveryF5666Control(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/c1.vcf", "", map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	p := discProps(ms, "/dav/addressbooks/ab1/c1.vcf")
	if p[xml.Name{Local: "getetag"}].Text == "" {
		t.Errorf("control: getetag missing: %v", keys(p))
	}
}

func TestCardDAVDiscoveryF5666Failure(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/c1.vcf", "", map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	p := discProps(ms, "/dav/addressbooks/ab1/c1.vcf")
	get := w2(s, "GET", "/dav/addressbooks/ab1/c1.vcf")
	if n := p[xml.Name{Local: "getcontentlength"}].Text; n != itoa(get.Body.Len()) {
		t.Errorf("DEFECT F5666: getcontentlength\nEXPECTED: %d\nACTUAL: %q", get.Body.Len(), n)
	}
	lm := p[xml.Name{Local: "getlastmodified"}].Text
	if _, err := http.ParseTime(lm); err != nil {
		t.Errorf("DEFECT F5666: getlastmodified\nEXPECTED: an RFC 7231 HTTP-date\nACTUAL: %q", lm)
	}
	if et := p[xml.Name{Local: "getetag"}].Text; et != get.Header().Get("ETag") {
		t.Errorf("DEFECT F5666: PROPFIND getetag %q != GET ETag %q", et, get.Header().Get("ETag"))
	}
	if rt, ok := p[xml.Name{Local: "resourcetype"}]; !ok || len(rt.Children) != 0 {
		t.Errorf("DEFECT F5666: contact resourcetype must be present and empty: %+v found=%v", rt, ok)
	}
}

func TestCardDAVDiscoveryF5666Edges(t *testing.T) {
	s := discSetup(t)
	// Multi-byte content: length is in bytes, not characters.
	vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u1\r\nFN:Çağla Öz\r\nEND:VCARD\r\n"
	if w := discDo(s, "PUT", "/dav/addressbooks/ab1/u1.vcf", vcf, nil); w.Code != http.StatusCreated {
		t.Fatalf("PUT = %d", w.Code)
	}
	w := discDo(s, "PROPFIND", "/dav/addressbooks/ab1/u1.vcf", `<D:propfind xmlns:D="DAV:"><D:prop><D:getcontentlength/></D:prop></D:propfind>`, map[string]string{"Depth": "0"})
	ms, ok := discParse(t, w)
	if !ok {
		return
	}
	if n := discProps(ms, "/dav/addressbooks/ab1/u1.vcf")[xml.Name{Local: "getcontentlength"}].Text; n != itoa(len(vcf)) {
		t.Errorf("DEFECT F5666: multibyte getcontentlength\nEXPECTED: %d\nACTUAL: %q", len(vcf), n)
	}
}

func w2(s *Server, method, path string) *httptest.ResponseRecorder {
	return discDo(s, method, path, "", nil)
}

func itoa(n int) string { return strconv.Itoa(n) }
