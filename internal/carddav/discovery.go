package carddav

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// XML namespaces used in CardDAV responses.
const (
	nsDAV       = "DAV:"
	nsCardDAV   = "urn:ietf:params:xml:ns:carddav"
	nsCalServer = "http://calendarserver.org/ns/"
)

// wellKnownCardDAV is the RFC 6764 §6 bootstrap path; it redirects to the
// DAV context root so clients that only know the domain can discover the
// service (F5664).
const wellKnownCardDAV = "/.well-known/carddav"

// davRoot is the context path the well-known redirect points at.
const davRoot = "/dav/"

// collationSet lists the collations the addressbook-query filter supports
// (see TextMatch.validate); advertised as CARDDAV:supported-collation-set.
var collationSet = []string{"i;ascii-casemap", "i;octet", "i;unicode-casemap"}

// principalHref is the principal URL of a user.
func principalHref(username string) string {
	return fmt.Sprintf("/dav/principals/%s/", username)
}

// escapedText returns s escaped for use inside a raw XML fragment.
func escapedText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// hrefElement builds a DAV:href element for use as raw property content.
func hrefElement(href string) string {
	return `<href xmlns="DAV:">` + escapedText(href) + `</href>`
}

// textProp builds a property with character-data content.
func textProp(space, local, value string) Property {
	return Property{XMLName: xml.Name{Space: space, Local: local}, Value: value}
}

// rawProp builds a property whose content is a fragment of nested elements.
// Every element in raw must carry its own namespace declaration, because the
// fragment is written verbatim.
func rawProp(space, local, raw string) Property {
	return Property{XMLName: xml.Name{Space: space, Local: local}, Raw: raw}
}

// currentUserPrincipal builds DAV:current-user-principal (RFC 5397).
func currentUserPrincipal(username string) Property {
	return rawProp(nsDAV, "current-user-principal", hrefElement(principalHref(username)))
}

// supportedReportSet builds DAV:supported-report-set for an address book
// collection (RFC 3253 §3.1.5, RFC 6352 §3).
func supportedReportSet() Property {
	report := func(name string) string {
		return `<supported-report><report><` + name + ` xmlns="` + nsCardDAV + `"/></report></supported-report>`
	}
	return rawProp(nsDAV, "supported-report-set", report("addressbook-query")+report("addressbook-multiget"))
}

// supportedCollationSet builds CARDDAV:supported-collation-set (RFC 6352 §8.3.1).
func supportedCollationSet() Property {
	var b strings.Builder
	for _, c := range collationSet {
		b.WriteString(`<supported-collation xmlns="` + nsCardDAV + `">` + escapedText(c) + `</supported-collation>`)
	}
	return rawProp(nsCardDAV, "supported-collation-set", b.String())
}

// buildRootResponse describes the context root ("/" or "/dav/"): a collection
// that names the user's principal, which is where clients start discovery
// (RFC 6764 §6, RFC 5397).
func (s *Server) buildRootResponse(href, username string) Response {
	return Response{
		Href: href,
		Propstat: []Propstat{{
			Prop: []Property{
				rawProp(nsDAV, "resourcetype", `<collection xmlns="DAV:"/>`),
				currentUserPrincipal(username),
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// contactFileInfo returns the stored size and modification time of a contact.
func (s *Server) contactFileInfo(username, addressbookID, uid string) (os.FileInfo, bool) {
	info, err := os.Stat(s.storage.contactPath(username, addressbookID, uid))
	if err != nil {
		return nil, false
	}
	return info, true
}

// contactMetaProps builds getcontentlength and getlastmodified for a stored
// contact (RFC 4918 §15.4, §15.7); they are omitted when the file is gone.
func (s *Server) contactMetaProps(username, addressbookID, uid string, content string) []Property {
	props := []Property{textProp(nsDAV, "getcontentlength", strconv.Itoa(len(content)))}
	if info, ok := s.contactFileInfo(username, addressbookID, uid); ok {
		props = append(props, textProp(nsDAV, "getlastmodified", info.ModTime().UTC().Format(http.TimeFormat)))
	}
	return props
}

// propRequest is the property selection of a PROPFIND or REPORT body.
type propRequest struct {
	names    []xml.Name
	nameOnly bool
}

// selectProps applies a property selection to a response (RFC 4918 §9.1):
// requested properties that exist go in a 200 propstat, the others in a 404
// propstat, and propname returns names only. A nil request, or an empty
// DAV:prop, keeps the full property set.
func selectProps(resp Response, req *propRequest) Response {
	if req == nil || len(resp.Propstat) == 0 {
		return resp
	}
	if req.nameOnly {
		for i := range resp.Propstat {
			props := make([]Property, len(resp.Propstat[i].Prop))
			for j, p := range resp.Propstat[i].Prop {
				props[j] = Property{XMLName: p.XMLName}
			}
			resp.Propstat[i].Prop = props
		}
		return resp
	}
	if len(req.names) == 0 {
		return resp
	}
	var have []Property
	status := resp.Propstat[0].Status
	for _, ps := range resp.Propstat {
		have = append(have, ps.Prop...)
	}
	var found, missing []Property
	for _, want := range req.names {
		match := false
		for _, p := range have {
			// A request name without a namespace matches by local name, as
			// the pre-namespace responses did.
			if p.XMLName.Local == want.Local && (want.Space == "" || p.XMLName.Space == want.Space) {
				found = append(found, p)
				match = true
				break
			}
		}
		if !match {
			missing = append(missing, Property{XMLName: want})
		}
	}
	resp.Propstat = nil
	if len(found) > 0 {
		resp.Propstat = append(resp.Propstat, Propstat{Prop: found, Status: status})
	}
	if len(missing) > 0 {
		resp.Propstat = append(resp.Propstat, Propstat{Prop: missing, Status: "HTTP/1.1 404 Not Found"})
	}
	return resp
}

// writeMultistatus filters every response by req and writes the 207 body.
func (s *Server) writeMultistatus(w http.ResponseWriter, ms *Multistatus, req *propRequest) {
	for i := range ms.Responses {
		ms.Responses[i] = selectProps(ms.Responses[i], req)
	}
	ms.Xmlns = nsDAV
	output, err := xml.MarshalIndent(ms, "", "  ")
	if err != nil {
		s.logger.Error("Failed to marshal multistatus", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to encode response")
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(output)
}

// requestFromProp turns a parsed DAV:prop into a propRequest (nil when the
// body had no DAV:prop).
func requestFromProp(p *Prop) *propRequest {
	if p == nil {
		return nil
	}
	return &propRequest{names: p.Names}
}

// UnmarshalXML records the child element names (namespace-resolved) of a
// DAV:prop, which is the property selection of a PROPFIND/REPORT.
func (p *Prop) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	p.XMLName = start.Name
	depth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				p.Names = append(p.Names, t.Name)
			}
			depth++
		case xml.EndElement:
			if depth == 0 {
				return nil
			}
			depth--
		}
	}
}

// MarshalXML writes a propstat as DAV:propstat holding a DAV:prop wrapper
// (RFC 4918 §14.22) around its properties; the previous encoding emitted the
// properties directly under propstat.
func (ps Propstat) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	start := xml.StartElement{Name: xml.Name{Local: "propstat"}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	propStart := xml.StartElement{Name: xml.Name{Local: "prop"}}
	if err := e.EncodeToken(propStart); err != nil {
		return err
	}
	for _, p := range ps.Prop {
		if err := e.Encode(p); err != nil {
			return err
		}
	}
	if err := e.EncodeToken(propStart.End()); err != nil {
		return err
	}
	if err := e.EncodeElement(ps.Status, xml.StartElement{Name: xml.Name{Local: "status"}}); err != nil {
		return err
	}
	return e.EncodeToken(start.End())
}
