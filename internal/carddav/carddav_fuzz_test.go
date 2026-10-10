package carddav

import (
	"encoding/xml"
	"testing"
	"time"
)

func FuzzVCardParse(f *testing.F) {
	for _, s := range []string{
		"BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\nFN;CHARSET=utf-8;ENCODING=QUOTED-PRINTABLE:=C3=A9=\n =41\nEND:VCARD",
		"BEGIN:VCARD\n FN:folded\n\t more\nEND:VCARD", "TEL;TYPE=\"a:b\":1", ":", ";:", "A;;;:", "=", "FN;ENCODING=QP:=ZZ=", "UID:x\nUID:y",
	} {
		f.Add(s, "uid-1", "fn", "x")
	}
	f.Fuzz(func(t *testing.T, data, uid, name, val string) {
		start := time.Now()
		props := parseVCardProps(data)
		_ = unfoldVCard(data)
		_ = rewriteVCardUID(data, uid)
		_ = hasVCardEnd(data)
		_ = trimVCardExt(name)
		_ = decodeQP(val, nil)
		q := &QueryFilter{PropFilters: []PropFilter{{Name: name, TextMatches: []TextMatch{{Value: val, MatchType: "contains"}}}}}
		_ = q.matches(props)
		for _, mt := range []string{"equals", "contains", "starts-with", "ends-with"} {
			tm := TextMatch{Value: val, MatchType: mt}
			_ = tm.validate()
			_ = tm.matches(data)
		}
		if time.Since(start) > time.Second {
			t.Fatal("slow")
		}
	})
}

func FuzzCardDAVXML(f *testing.F) {
	for _, s := range []string{
		`<propertyupdate xmlns="DAV:"><set><prop><displayname>x</displayname></prop></set></propertyupdate>`,
		`<c:addressbook-query xmlns:c="urn:ietf:params:xml:ns:carddav"><c:filter test="anyof"><c:prop-filter name="FN"><c:text-match>a</c:text-match></c:prop-filter></c:filter></c:addressbook-query>`,
		`<sync-collection xmlns="DAV:"><sync-token>data:,1</sync-token></sync-collection>`,
		`<addressbook-multiget xmlns="urn:ietf:params:xml:ns:carddav"><href>/a</href></addressbook-multiget>`,
		`<propfind xmlns="DAV:"><prop><a/></prop></propfind>`, `<a><b>`, ``,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		start := time.Now()
		_, _ = parsePropertyUpdate(body)
		_ = reportRootName(body)
		var q AddressbookQuery
		if xml.Unmarshal(body, &q) == nil && q.Filter != nil {
			if q.Filter.validate() == nil {
				_ = q.Filter.matches(parseVCardProps("BEGIN:VCARD\nFN:abc\nEND:VCARD"))
			}
		}
		var mg AddressbookMultiget
		_ = xml.Unmarshal(body, &mg)
		var sq syncQuery
		_ = xml.Unmarshal(body, &sq)
		var pf struct {
			XMLName xml.Name `xml:"propfind"`
			Prop    *Prop    `xml:"prop"`
		}
		_ = xml.Unmarshal(body, &pf)
		if time.Since(start) > time.Second {
			t.Fatal("slow")
		}
	})
}
