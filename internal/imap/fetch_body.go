package imap

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
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
		part = mimeSubpart(part, n)
		if part == nil {
			return nil
		}
		isMessage = false
		section = rest
	}
	header, body := splitHeaderBody(part)
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

// mimeSubpart returns part n (1-based, header+body) of a multipart entity.
// For a non-multipart entity, part 1 is its body (RFC 3501 §6.4.5).
func mimeSubpart(entity []byte, n int) []byte {
	header, body := splitHeaderBody(entity)
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(header)))
	mh, _ := tp.ReadMIMEHeader()
	mediaType, params, err := mime.ParseMediaType(mh.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		if n == 1 {
			return append([]byte("\r\n"), body...) // headerless part: body only
		}
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for i := 1; ; i++ {
		p, err := mr.NextRawPart()
		if err != nil {
			return nil
		}
		if i != n {
			continue
		}
		var buf bytes.Buffer
		for k, vs := range p.Header {
			for _, v := range vs {
				fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
			}
		}
		buf.WriteString("\r\n")
		if _, err := io.Copy(&buf, p); err != nil {
			return nil
		}
		return buf.Bytes()
	}
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
