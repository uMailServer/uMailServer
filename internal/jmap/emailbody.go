package jmap

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Parsed header forms and body values for Email/get (RFC 8621 §4.1.2-4.1.4).

type headerProp struct {
	name string // as requested
	form string // asRaw, asText, asAddresses, asGroupedAddresses, asMessageIds, asDate, asURLs
	all  bool
}

// parseHeaderProp parses "header:Name[:asForm][:all]".
func parseHeaderProp(p string) (headerProp, bool) {
	parts := strings.Split(p, ":")
	if len(parts) < 2 || parts[0] != "header" || parts[1] == "" {
		return headerProp{}, false
	}
	hp := headerProp{name: parts[1], form: "asRaw"}
	rest := parts[2:]
	if len(rest) > 0 && rest[len(rest)-1] == "all" {
		hp.all = true
		rest = rest[:len(rest)-1]
	}
	if len(rest) > 1 {
		return headerProp{}, false
	}
	if len(rest) == 1 {
		switch rest[0] {
		case "asRaw", "asText", "asAddresses", "asGroupedAddresses", "asMessageIds", "asDate", "asURLs":
			hp.form = rest[0]
		default:
			return headerProp{}, false
		}
	}
	return hp, true
}

// splitMessage returns the raw header block lines (unfolded into
// name + raw value, raw keeping folding) and the body.
type rawHeader struct{ name, raw string }

func splitHeaders(data []byte) ([]rawHeader, []byte) {
	var hs []rawHeader
	rest := data
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		var line []byte
		if i < 0 {
			line, rest = rest, nil
		} else {
			line, rest = rest[:i], rest[i+1:]
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			break
		}
		if (line[0] == ' ' || line[0] == '\t') && len(hs) > 0 {
			hs[len(hs)-1].raw += "\r\n" + string(line)
			continue
		}
		c := bytes.IndexByte(line, ':')
		if c <= 0 {
			continue
		}
		hs = append(hs, rawHeader{name: string(line[:c]), raw: string(line[c+1:])})
	}
	return hs, rest
}

var mimeWordDecoder = new(mime.WordDecoder)

func unfold(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

func decodeText(raw string) string {
	s := unfold(raw)
	if d, err := mimeWordDecoder.DecodeHeader(s); err == nil {
		s = d
	}
	return strings.TrimSpace(s)
}

func parseHeaderForm(form, raw string) interface{} {
	switch form {
	case "asRaw":
		return raw
	case "asText":
		return decodeText(raw)
	case "asAddresses":
		list, err := mail.ParseAddressList(unfold(raw))
		if err != nil {
			return nil
		}
		out := make([]EmailAddress, 0, len(list))
		for _, a := range list {
			out = append(out, EmailAddress{Name: a.Name, Email: a.Address})
		}
		return out
	case "asGroupedAddresses":
		list, err := mail.ParseAddressList(unfold(raw))
		if err != nil {
			return nil
		}
		addrs := make([]EmailAddress, 0, len(list))
		for _, a := range list {
			addrs = append(addrs, EmailAddress{Name: a.Name, Email: a.Address})
		}
		return []map[string]interface{}{{"name": nil, "addresses": addrs}}
	case "asMessageIds":
		ids := bracketed(unfold(raw))
		if len(ids) == 0 {
			return nil
		}
		return ids
	case "asDate":
		t, err := mail.ParseDate(strings.TrimSpace(unfold(raw)))
		if err != nil {
			return nil
		}
		return t.Format(time.RFC3339)
	case "asURLs":
		urls := bracketed(unfold(raw))
		if len(urls) == 0 {
			return nil
		}
		return urls
	}
	return nil
}

// bracketed returns the contents of every <...> in s.
func bracketed(s string) []string {
	out := []string{}
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			return out
		}
		if v := strings.TrimSpace(s[i+1 : i+j]); v != "" {
			out = append(out, v)
		}
		s = s[i+j+1:]
	}
}

// headerValue evaluates one header:... property against the parsed headers.
func headerValue(hs []rawHeader, hp headerProp) interface{} {
	var matches []string
	for _, h := range hs {
		if strings.EqualFold(h.name, hp.name) {
			matches = append(matches, h.raw)
		}
	}
	if hp.all {
		out := make([]interface{}, 0, len(matches))
		for _, m := range matches {
			out = append(out, parseHeaderForm(hp.form, m))
		}
		return out
	}
	if len(matches) == 0 {
		return nil
	}
	return parseHeaderForm(hp.form, matches[len(matches)-1])
}

type mimePart struct {
	part         EmailBodyPart
	value        string
	bad          bool // charset/encoding problem
	isAttachment bool // disposition attachment or non-text type
}

// decodeTransfer decodes Content-Transfer-Encoding (base64 or
// quoted-printable) into raw bytes; other encodings pass through unchanged.
// bad reports a decode failure for the caller to flag; the original bytes
// are returned so the caller can apply its own fallback (round 59
// extraction — shared by text and non-text parts).
func decodeTransfer(enc string, body []byte) ([]byte, bool) {
	switch strings.ToLower(enc) {
	case "base64":
		clean := strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' {
				return -1
			}
			return r
		}, string(body))
		b, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			return body, true
		}
		return b, false
	case "quoted-printable":
		b, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil {
			return body, true
		}
		return b, false
	}
	return body, false
}

// partBlobID composes the round-59 per-part blob id "{blobID}.{partId}"
// (real blob ids are 64-hex without dots, so the forms are unambiguous);
// empty when the caller has no parent blob id.
func partBlobID(blobID string, partID int) string {
	if blobID == "" {
		return ""
	}
	return fmt.Sprintf("%s.%d", blobID, partID)
}

// collectMIMEParts walks the MIME tree and returns EVERY leaf part (partIds
// assigned in document order). Text parts carry decoded content; non-text
// parts and Content-Disposition:attachment parts are flagged isAttachment
// and have their transfer encoding decoded (no charset conversion — binary
// content; §4.1.4 defines part size as the octets after decoding, and the
// round-59 per-part blobId download serves exactly those bytes). Each part
// exposes the composite per-part blobId "{blobID}.{partId}" (round 59) so
// clients can fetch part content via the download endpoint; real blob ids
// are 64-hex without dots, so the forms are unambiguous.
func collectMIMEParts(hdrs []rawHeader, body []byte, blobID string) []mimePart {
	var out []mimePart
	n := 0
	var walk func(hdrs []rawHeader, body []byte, depth int)
	walk = func(hdrs []rawHeader, body []byte, depth int) {
		get := func(name string) string {
			for i := len(hdrs) - 1; i >= 0; i-- {
				if strings.EqualFold(hdrs[i].name, name) {
					return strings.TrimSpace(unfold(hdrs[i].raw))
				}
			}
			return ""
		}
		ct := get("Content-Type")
		if ct == "" {
			ct = "text/plain"
		}
		mt, params, err := mime.ParseMediaType(ct)
		if err != nil {
			mt, params = "text/plain", map[string]string{}
		}
		if strings.HasPrefix(mt, "multipart/") && params["boundary"] != "" && depth < 10 {
			mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
			for {
				p, err := mr.NextRawPart()
				if err != nil {
					return
				}
				pb, _ := io.ReadAll(p)
				var ph []rawHeader
				for k, vs := range p.Header {
					for _, v := range vs {
						ph = append(ph, rawHeader{name: k, raw: " " + v})
					}
				}
				walk(ph, pb, depth+1)
			}
		}
		dispType, dispParams := "", map[string]string{}
		if dispRaw := get("Content-Disposition"); dispRaw != "" {
			if dt, dp, derr := mime.ParseMediaType(dispRaw); derr == nil {
				dispType, dispParams = strings.ToLower(dt), dp
			}
		}
		isText := strings.HasPrefix(mt, "text/")
		isAttachment := dispType == "attachment" || !isText
		n++
		tp := mimePart{
			part: EmailBodyPart{
				PartID:      fmt.Sprint(n),
				BlobID:      partBlobID(blobID, n),
				Type:        mt,
				Name:        params["name"],
				Disposition: dispType,
			},
			isAttachment: isAttachment,
		}
		if fn := dispParams["filename"]; fn != "" {
			tp.part.Name = fn
		}
		if !isText {
			// Non-text leaf part (attachment): decode the transfer encoding
			// (no charset conversion — binary content). §4.1.4 defines part
			// size as the octets AFTER content-transfer decoding, and the
			// per-part blobId download (round 59) serves exactly these
			// bytes.
			dec, bad := decodeTransfer(get("Content-Transfer-Encoding"), body)
			tp.bad = bad
			tp.value = string(dec)
			tp.part.Size = int64(len(dec))
			out = append(out, tp)
			return
		}
		tp.part.Charset = params["charset"]
		dec, bad := decodeTransfer(get("Content-Transfer-Encoding"), body)
		tp.bad = bad
		tp.part.Size = int64(len(dec))
		cs := strings.ToLower(params["charset"])
		switch cs {
		case "", "utf-8", "us-ascii":
			if !utf8.Valid(dec) {
				tp.bad = true
				dec = []byte(strings.ToValidUTF8(string(dec), "�"))
			}
		case "iso-8859-1", "latin1":
			r := make([]rune, len(dec))
			for i, c := range dec {
				r[i] = rune(c)
			}
			dec = []byte(string(r))
		default:
			tp.bad = true
			dec = []byte(strings.ToValidUTF8(string(dec), "�"))
		}
		tp.value = string(dec)
		if tp.part.Charset == "" {
			tp.part.Charset = "us-ascii"
		}
		out = append(out, tp)
	}
	walk(hdrs, body, 0)
	return out
}

// truncateUTF8 cuts s to at most max bytes without splitting a character.
func truncateUTF8(s string, max int) (string, bool) {
	if max < 0 || len(s) <= max {
		return s, false
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max], true
}

type bodyFetchOpts struct {
	text, html, all bool
	maxBytes        int // -1 = unlimited
}

// bodyProps computes textBody, htmlBody, attachments, bodyValues, preview
// and hasAttachment for the raw message.
func bodyProps(hs []rawHeader, body []byte, o bodyFetchOpts, blobID string) (textBody, htmlBody, attachments []EmailBodyPart, values map[string]EmailBodyValue, preview string, hasAttachment bool) {
	parts := collectMIMEParts(hs, body, blobID)
	var textOnly []mimePart
	for _, p := range parts {
		if !p.isAttachment && strings.HasPrefix(p.part.Type, "text/") {
			textOnly = append(textOnly, p)
		}
	}
	pick := func(want string) []mimePart {
		var sel, other []mimePart
		for _, p := range textOnly {
			if p.part.Type == want {
				sel = append(sel, p)
			} else {
				other = append(other, p)
			}
		}
		if len(sel) == 0 {
			return other
		}
		return sel
	}
	tb, hb := pick("text/plain"), pick("text/html")
	textBody, htmlBody, attachments = []EmailBodyPart{}, []EmailBodyPart{}, []EmailBodyPart{}
	values = map[string]EmailBodyValue{}
	selected := map[string]bool{}
	add := func(ps []mimePart) {
		for _, p := range ps {
			selected[p.part.PartID] = true
			v, trunc := p.value, false
			if o.maxBytes >= 0 {
				v, trunc = truncateUTF8(v, o.maxBytes)
			}
			values[p.part.PartID] = EmailBodyValue{Value: v, IsTruncated: trunc, IsEncodingProblem: p.bad}
		}
	}
	for _, p := range tb {
		textBody = append(textBody, p.part)
		selected[p.part.PartID] = true
	}
	for _, p := range hb {
		htmlBody = append(htmlBody, p.part)
		selected[p.part.PartID] = true
	}
	if o.text || o.all {
		add(tb)
	}
	if o.html || o.all {
		add(hb)
	}
	if o.all {
		add(textOnly)
	}
	// RFC 8621 §4.1.4: attachments = every part not in textBody/htmlBody
	// (round 51 — non-text and Content-Disposition:attachment parts were
	// previously dropped by collectTextParts).
	for _, p := range parts {
		if !selected[p.part.PartID] {
			attachments = append(attachments, p.part)
		}
	}
	// RFC 8621 §4.1.2 (round 52): hasAttachment SHOULD be true when the
	// attachments list has at least one item that does not have
	// "Content-Disposition: inline"; preview is a plaintext fragment of the
	// body (white space collapsed; the server chooses the part) and MUST
	// NOT exceed 256 characters.
	for _, a := range attachments {
		if a.Disposition != "inline" {
			hasAttachment = true
			break
		}
	}
	if len(tb) > 0 {
		src := tb[0].value
		if tb[0].part.Type == "text/html" {
			// §4.1.2 preview is a plaintext fragment: when the chosen text
			// part is HTML (round 51's text-part fallback), strip tags and
			// decode entities before the collapse+truncate (round 54).
			src = htmlToPreviewText(src)
		}
		preview, _ = truncateUTF8(strings.Join(strings.Fields(src), " "), 256)
	}
	return
}

// parseBodyFetchOpts reads the §4.2 body-fetch arguments shared by the
// projected (map) and full-form (struct) response paths.
func parseBodyFetchOpts(args map[string]interface{}) bodyFetchOpts {
	o := bodyFetchOpts{maxBytes: -1}
	o.text, _ = args["fetchTextBodyValues"].(bool)
	o.html, _ = args["fetchHTMLBodyValues"].(bool)
	o.all, _ = args["fetchAllBodyValues"].(bool)
	if m, ok := args["maxBodyValueBytes"].(float64); ok && m >= 0 {
		o.maxBytes = int(m)
	}
	return o
}

// augmentEmailBodyStruct fills textBody/htmlBody/bodyValues on a full-form
// Email/get response (properties omitted ⇒ RFC 8621 §4.2 "all properties").
// Round 50: the full form previously returned the bare storageToJMAPEmail
// struct with no body properties.
func (s *Server) augmentEmailBodyStruct(user, blobID string, args map[string]interface{}, email *Email) {
	if s.msgStore == nil {
		return
	}
	data, err := s.msgStore.ReadMessage(user, blobID)
	if err != nil {
		return
	}
	hs, body := splitHeaders(data)
	tb, hb, atts, vals, preview, hasAtt := bodyProps(hs, body, parseBodyFetchOpts(args), blobID)
	email.TextBody = tb
	email.HTMLBody = hb
	email.Attachments = atts
	email.BodyValues = vals
	email.Preview = preview
	email.HasAttachment = hasAtt
}

// augmentEmailProps adds the header:*, textBody, htmlBody and bodyValues
// properties requested in props to out, reading the message blob once.
func (s *Server) augmentEmailProps(user, blobID string, props []string, args map[string]interface{}, out map[string]interface{}) {
	need := false
	for _, p := range props {
		if strings.HasPrefix(p, "header:") || p == "bodyValues" || p == "textBody" || p == "htmlBody" || p == "attachments" || p == "preview" || p == "hasAttachment" {
			need = true
		}
	}
	if !need || s.msgStore == nil {
		return
	}
	data, err := s.msgStore.ReadMessage(user, blobID)
	if err != nil {
		return
	}
	hs, body := splitHeaders(data)
	o := parseBodyFetchOpts(args)
	tb, hb, atts, vals, preview, hasAtt := bodyProps(hs, body, o, blobID)
	for _, p := range props {
		switch {
		case strings.HasPrefix(p, "header:"):
			if hp, ok := parseHeaderProp(p); ok {
				out[p] = headerValue(hs, hp)
			}
		case p == "textBody":
			out[p] = tb
		case p == "htmlBody":
			out[p] = hb
		case p == "bodyValues":
			out[p] = vals
		case p == "attachments":
			out[p] = atts
		case p == "preview":
			out[p] = preview
		case p == "hasAttachment":
			out[p] = hasAtt
		}
	}
}

// applyServerSetProps fills the RFC 8621 §4.1.2 server-set properties
// (preview, hasAttachment) on a response Email object from the raw message
// bytes. The created responses (Email/set create, Email/import) otherwise
// carry storageToJMAPEmail's zero values — omitted on the wire via omitempty
// — while Email/get of the same message reports them (rounds 50–52
// machinery); RFC 8620 §5.3 returns the created object, so the server-set
// properties must agree. bodyFetchOpts{} leaves bodyValues unset: the
// created-response contract carries no body values.
func applyServerSetProps(email *Email, data []byte, blobID string) {
	if len(data) == 0 {
		return
	}
	hs, body := splitHeaders(data)
	_, _, _, _, preview, hasAtt := bodyProps(hs, body, bodyFetchOpts{}, blobID)
	email.Preview = preview
	email.HasAttachment = hasAtt
}

// htmlToPreviewText strips tags and decodes HTML entities from an HTML
// fragment so a text/html part can serve as the §4.1.2 preview source
// (round 54). Deterministic index-based scanner: text between tags is kept,
// `<`…`>` runs are dropped (an unterminated trailing tag drops the
// remainder), and the result is entity-decoded. Round 56: <script> and
// <style> block CONTENTS are dropped too — an open tag skips to the
// matching close tag (searched case-insensitively, like HTML parsers that
// end script blocks at the first "</script"), and a missing close tag
// drops the remainder, consistent with the unterminated-tag rule.
func htmlToPreviewText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			b.WriteString(s[i:])
			break
		}
		lt += i
		b.WriteString(s[i:lt])
		gt := strings.IndexByte(s[lt:], '>')
		if gt < 0 {
			break
		}
		end := lt + gt + 1
		name, block := htmlBlockOpen(strings.ToLower(s[lt:end]))
		if !block {
			i = end
			continue
		}
		// Round 56: drop the block's contents up to the matching close
		// tag; the close tag itself is then handled as a normal (dropped)
		// tag by the main loop.
		closed := false
		off := end
		for !closed {
			c := strings.IndexByte(s[off:], '<')
			if c < 0 {
				return b.String()
			}
			c += off
			g2 := strings.IndexByte(s[c:], '>')
			if g2 < 0 {
				return b.String()
			}
			e2 := c + g2 + 1
			ct := strings.ToLower(s[c:e2])
			if strings.HasPrefix(ct, "</"+name) && len(ct) > len(name)+2 {
				switch ct[len(name)+2] {
				case '>', ' ', '\t', '\n', '\r', '/':
					i = c
					closed = true
					continue
				}
			}
			off = e2
		}
	}
	return html.UnescapeString(b.String())
}

// htmlBlockOpen reports whether the lowercased tag opens a <script> or
// <style> block whose contents the preview must drop (round 56), returning
// the block name. A word boundary is required: "<scripty>" is a plain
// unknown tag, not a script block.
func htmlBlockOpen(tag string) (string, bool) {
	for _, name := range []string{"script", "style"} {
		open := "<" + name
		if !strings.HasPrefix(tag, open) {
			continue
		}
		rest := tag[len(open):]
		if rest != "" {
			switch rest[0] {
			case '>', ' ', '\t', '\n', '\r', '/':
				return name, true
			}
		}
	}
	return "", false
}
