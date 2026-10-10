package imap

import (
	"bufio"
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
)

// bodySectionItem is a parsed FETCH BODY[<section>]<<partial>> or
// BODY.PEEK[...] data item (RFC 3501 §6.4.5). F5067: these items were
// silently answered with nothing.
type bodySectionItem struct {
	peek    bool
	section string // upper-cased section spec as echoed in the response
	partial bool
	origin  int
	count   int // -1 = to the end
}

// parseBodySectionItem recognises BODY[...] / BODY.PEEK[...] items.
func parseBodySectionItem(item string) (bodySectionItem, bool) {
	var it bodySectionItem
	up := strings.ToUpper(item)
	switch {
	case strings.HasPrefix(up, "BODY.PEEK["):
		it.peek = true
		up = up[len("BODY.PEEK["):]
	case strings.HasPrefix(up, "BODY["):
		up = up[len("BODY["):]
	default:
		return it, false
	}
	end := strings.IndexByte(up, ']')
	if end < 0 {
		return it, false
	}
	it.section = strings.TrimSpace(up[:end])
	rest := up[end+1:]
	it.count = -1
	if rest != "" {
		if !strings.HasPrefix(rest, "<") || !strings.HasSuffix(rest, ">") {
			return it, false
		}
		spec := rest[1 : len(rest)-1]
		originStr, countStr, hasCount := strings.Cut(spec, ".")
		origin, err := strconv.ParseUint(originStr, 10, 31)
		if err != nil {
			return it, false
		}
		it.partial, it.origin = true, int(origin)
		if hasCount {
			count, err := strconv.ParseUint(countStr, 10, 31)
			if err != nil {
				return it, false
			}
			it.count = int(count)
		}
	}
	return it, true
}

// format renders the item's response ("BODY[sec]<origin> {n}\r\n<data>").
func (it bodySectionItem) format(data []byte) string {
	b := bodySection(data, it.section)
	name := "BODY[" + it.section + "]"
	if it.partial {
		name += fmt.Sprintf("<%d>", it.origin)
		if it.origin >= len(b) {
			b = nil
		} else {
			b = b[it.origin:]
			if it.count >= 0 && it.count < len(b) {
				b = b[:it.count]
			}
		}
	}
	return fmt.Sprintf("%s {%d}\r\n%s", name, len(b), b)
}

// splitHeaderBody splits a message (or MIME part) after its header block;
// the header keeps its terminating blank line.
func splitHeaderBody(data []byte) (header, body []byte) {
	if bytes.HasPrefix(data, []byte("\r\n")) {
		return data[:2], data[2:]
	}
	if bytes.HasPrefix(data, []byte("\n")) {
		return data[:1], data[1:]
	}
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return data[:i+4], data[i+4:]
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return data[:i+2], data[i+2:]
	}
	return data, nil
}

// bodySection extracts an RFC 3501 section-spec from a message.
func bodySection(data []byte, section string) []byte {
	if section == "" {
		return data
	}
	// Leading part number path ("1", "2.3", "1.HEADER", ...).
	part := data
	isMessage := true // part still has an RFC 822 header (top level)
	for section != "" {
		head, rest, _ := strings.Cut(section, ".")
		n, err := strconv.Atoi(head)
		if err != nil || n <= 0 {
			break
		}
		// F5494: a message/rfc822 part's sub-parts are those of the
		// encapsulated message (RFC 3501 §6.4.5), not of the part itself.
		if !isMessage && isRFC822Part(part) {
			_, part = splitHeaderBody(part)
		}
		part = mimeSubpart(part, n)
		if part == nil {
			return nil
		}
		isMessage = false
		section = rest
	}
	header, body := splitHeaderBody(part)
	// F5494: HEADER, HEADER.FIELDS[.NOT] and TEXT after a part number refer
	// to the message encapsulated by a message/rfc822 part.
	if !isMessage && section != "" && section != "MIME" && isRFC822Part(part) {
		header, body = splitHeaderBody(body)
	}
	switch {
	case section == "":
		if isMessage {
			return part
		}
		return body
	case section == "HEADER" || section == "MIME":
		return header
	case section == "TEXT":
		return body
	case strings.HasPrefix(section, "HEADER.FIELDS.NOT"):
		return filterHeader(header, section[len("HEADER.FIELDS.NOT"):], false)
	case strings.HasPrefix(section, "HEADER.FIELDS"):
		return filterHeader(header, section[len("HEADER.FIELDS"):], true)
	}
	return nil
}

// partHeader parses the MIME header block of an entity.
func partHeader(entity []byte) textproto.MIMEHeader {
	header, _ := splitHeaderBody(entity)
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(header)))
	mh, _ := tp.ReadMIMEHeader()
	return mh
}

// isRFC822Part reports whether a MIME part is of type message/rfc822.
func isRFC822Part(part []byte) bool {
	mediaType, _, err := mime.ParseMediaType(partHeader(part).Get("Content-Type"))
	return err == nil && (mediaType == "message/rfc822" || mediaType == "message/global")
}

// mimeSubpart returns part n (1-based, header+body) of a multipart entity.
// For a non-multipart entity, part 1 is its body (RFC 3501 §6.4.5).
func mimeSubpart(entity []byte, n int) []byte {
	_, body := splitHeaderBody(entity)
	mediaType, params, err := mime.ParseMediaType(partHeader(entity).Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		if n == 1 {
			return append([]byte("\r\n"), body...) // headerless part: body only
		}
		return nil
	}
	parts := splitMultipart(body, params["boundary"])
	if n > len(parts) {
		return nil
	}
	return parts[n-1]
}

// splitMultipart returns the raw body parts (header and body, byte for
// byte) of a multipart body. F5495: parts used to be rebuilt from the
// parsed header map, which reordered and re-cased the MIME header, so
// BODY[n.MIME] (and partial fetches of it) differed between requests.
func splitMultipart(body []byte, boundary string) [][]byte {
	delim := []byte("--" + boundary)
	var parts [][]byte
	start := -1
	for pos := 0; pos < len(body); {
		lineEnd, next := len(body), len(body)
		if i := bytes.IndexByte(body[pos:], '\n'); i >= 0 {
			lineEnd, next = pos+i, pos+i+1
		}
		line := bytes.TrimRight(body[pos:lineEnd], "\r")
		if bytes.HasPrefix(line, delim) {
			rest := line[len(delim):]
			closing := bytes.HasPrefix(rest, []byte("--"))
			if closing {
				rest = rest[2:]
			}
			if len(bytes.TrimRight(rest, " \t")) == 0 {
				if start >= 0 {
					// The line break before a delimiter belongs to it.
					end := pos
					if end > start && body[end-1] == '\n' {
						end--
						if end > start && body[end-1] == '\r' {
							end--
						}
					}
					parts = append(parts, body[start:end])
				}
				if closing {
					return parts
				}
				start = next
			}
		}
		pos = next
	}
	if start >= 0 && start <= len(body) {
		parts = append(parts, body[start:]) // unterminated last part
	}
	return parts
}

// filterHeader keeps (include) or drops the fields named in "(A B ...)".
func filterHeader(header []byte, list string, include bool) []byte {
	list = strings.Trim(strings.TrimSpace(list), "()")
	names := map[string]bool{}
	for _, f := range strings.Fields(list) {
		names[strings.ToUpper(strings.Trim(f, `"`))] = true
	}
	var out bytes.Buffer
	keep := false
	for _, line := range strings.SplitAfter(string(header), "\n") {
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			name, _, _ := strings.Cut(trimmed, ":")
			keep = names[strings.ToUpper(strings.TrimSpace(name))] == include
		}
		if keep {
			out.WriteString(trimmed + "\r\n")
		}
	}
	out.WriteString("\r\n")
	return out.Bytes()
}

// splitFetchItems tokenises a FETCH item list, keeping bracketed section
// specs such as BODY[HEADER.FIELDS (FROM TO)] in one item.
func splitFetchItems(s string) []string {
	var items []string
	var cur strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		}
		if depth == 0 && (r == ' ' || r == '\t') {
			if cur.Len() > 0 {
				items = append(items, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		items = append(items, cur.String())
	}
	return items
}

// maxBodyStructureDepth bounds recursion into nested multiparts and
// message/rfc822 parts; deeper entities are described as opaque leaves.
const maxBodyStructureDepth = 32

// bodyStructure renders the RFC 3501 §7.4.2 BODY (ext=false) or
// BODYSTRUCTURE (ext=true) of a message or MIME part. F5496: both items were
// hard-coded to a single TEXT/PLAIN part, hiding every attachment and
// nested message from clients that render from the structure.
func bodyStructure(entity []byte, ext bool) string {
	return bodyStructureDepth(entity, ext, 0)
}

func bodyStructureDepth(entity []byte, ext bool, depth int) string {
	mh := partHeader(entity)
	_, body := splitHeaderBody(entity)
	mediaType, params, err := mime.ParseMediaType(mh.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType, params = "text/plain", map[string]string{"charset": "us-ascii"}
	}
	typ, sub, _ := strings.Cut(mediaType, "/")
	var b strings.Builder
	b.WriteByte('(')
	if typ == "multipart" && params["boundary"] != "" && depth < maxBodyStructureDepth {
		parts := splitMultipart(body, params["boundary"])
		if len(parts) == 0 {
			parts = [][]byte{[]byte("\r\n")} // RFC 3501 requires at least one part
		}
		for _, p := range parts {
			b.WriteString(bodyStructureDepth(p, ext, depth+1))
		}
		b.WriteString(" " + imapNString(strings.ToUpper(sub)))
		if ext {
			b.WriteString(" " + imapParamList(params) + " " + imapDisposition(mh) + " " +
				imapNString(mh.Get("Content-Language")) + " " + imapNString(mh.Get("Content-Location")))
		}
		b.WriteByte(')')
		return b.String()
	}
	enc := strings.ToUpper(strings.TrimSpace(mh.Get("Content-Transfer-Encoding")))
	if enc == "" {
		enc = "7BIT"
	}
	fmt.Fprintf(&b, "%s %s %s %s %s %s %d",
		imapNString(strings.ToUpper(typ)), imapNString(strings.ToUpper(sub)), imapParamList(params),
		imapNString(mh.Get("Content-Id")), imapNString(mh.Get("Content-Description")),
		imapNString(enc), len(body))
	lines := bytes.Count(body, []byte("\n"))
	switch {
	case (mediaType == "message/rfc822" || mediaType == "message/global") && depth < maxBodyStructureDepth:
		fmt.Fprintf(&b, " %s %s %d", imapEnvelope(partHeader(body)), bodyStructureDepth(body, ext, depth+1), lines)
	case typ == "text":
		fmt.Fprintf(&b, " %d", lines)
	}
	if ext {
		b.WriteString(" " + imapNString(mh.Get("Content-Md5")) + " " + imapDisposition(mh) + " " +
			imapNString(mh.Get("Content-Language")) + " " + imapNString(mh.Get("Content-Location")))
	}
	b.WriteByte(')')
	return b.String()
}

// imapNString renders s as an IMAP nstring: NIL when empty, a quoted string
// when it is plain 7-bit text, a literal otherwise.
func imapNString(s string) string {
	if s == "" {
		return "NIL"
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 || c == '\r' || c == '\n' || c == 0 {
			return fmt.Sprintf("{%d}\r\n%s", len(s), s)
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// imapParamList renders a body parameter list (sorted for stable output).
func imapParamList(params map[string]string) string {
	if len(params) == 0 {
		return "NIL"
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		parts = append(parts, imapNString(strings.ToUpper(k)), imapNString(params[k]))
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// imapDisposition renders the body-fld-dsp of a part.
func imapDisposition(mh textproto.MIMEHeader) string {
	v := mh.Get("Content-Disposition")
	if v == "" {
		return "NIL"
	}
	disp, params, err := mime.ParseMediaType(v)
	if err != nil {
		return "NIL"
	}
	return "(" + imapNString(strings.ToUpper(disp)) + " " + imapParamList(params) + ")"
}

// imapEnvelope renders the ENVELOPE of an encapsulated message header.
func imapEnvelope(mh textproto.MIMEHeader) string {
	from := imapAddressList(mh.Get("From"))
	sender, replyTo := imapAddressList(mh.Get("Sender")), imapAddressList(mh.Get("Reply-To"))
	if sender == "NIL" {
		sender = from
	}
	if replyTo == "NIL" {
		replyTo = from
	}
	return "(" + strings.Join([]string{
		imapNString(mh.Get("Date")), imapNString(mh.Get("Subject")), from, sender, replyTo,
		imapAddressList(mh.Get("To")), imapAddressList(mh.Get("Cc")), imapAddressList(mh.Get("Bcc")),
		imapNString(mh.Get("In-Reply-To")), imapNString(mh.Get("Message-Id")),
	}, " ") + ")"
}

// imapAddressList renders an address header as an IMAP address list.
func imapAddressList(v string) string {
	if v == "" {
		return "NIL"
	}
	addrs, err := mail.ParseAddressList(v)
	if err != nil || len(addrs) == 0 {
		return "NIL"
	}
	var b strings.Builder
	b.WriteByte('(')
	for _, a := range addrs {
		local, domain := splitAddress(a.Address)
		fmt.Fprintf(&b, "(%s NIL %s %s)", imapNString(a.Name), imapNString(local), imapNString(domain))
	}
	b.WriteByte(')')
	return b.String()
}
