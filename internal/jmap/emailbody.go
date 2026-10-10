package jmap

import (
	"bytes"
	"encoding/base64"
	"fmt"
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

type textPart struct {
	part  EmailBodyPart
	value string
	bad   bool // charset/encoding problem
}

// collectTextParts walks the MIME tree and returns the text/* leaf parts
// (partIds assigned in document order) with decoded content.
func collectTextParts(hdrs []rawHeader, body []byte) []textPart {
	var out []textPart
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
		if !strings.HasPrefix(mt, "text/") {
			return
		}
		if disp := strings.ToLower(get("Content-Disposition")); strings.HasPrefix(disp, "attachment") {
			return
		}
		n++
		tp := textPart{part: EmailBodyPart{PartID: fmt.Sprint(n), Type: mt, Charset: params["charset"]}}
		dec := body
		switch strings.ToLower(get("Content-Transfer-Encoding")) {
		case "base64":
			clean := strings.Map(func(r rune) rune {
				if r == '\r' || r == '\n' || r == ' ' {
					return -1
				}
				return r
			}, string(body))
			b, err := base64.StdEncoding.DecodeString(clean)
			if err != nil {
				tp.bad = true
			} else {
				dec = b
			}
		case "quoted-printable":
			b, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
			if err != nil {
				tp.bad = true
			}
			dec = b
		}
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

// bodyProps computes textBody, htmlBody and bodyValues for the raw message.
func bodyProps(hs []rawHeader, body []byte, o bodyFetchOpts) (textBody, htmlBody []EmailBodyPart, values map[string]EmailBodyValue) {
	parts := collectTextParts(hs, body)
	pick := func(want string) []textPart {
		var sel, other []textPart
		for _, p := range parts {
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
	textBody, htmlBody = []EmailBodyPart{}, []EmailBodyPart{}
	values = map[string]EmailBodyValue{}
	add := func(ps []textPart) {
		for _, p := range ps {
			v, trunc := p.value, false
			if o.maxBytes >= 0 {
				v, trunc = truncateUTF8(v, o.maxBytes)
			}
			values[p.part.PartID] = EmailBodyValue{Value: v, IsTruncated: trunc, IsEncodingProblem: p.bad}
		}
	}
	for _, p := range tb {
		textBody = append(textBody, p.part)
	}
	for _, p := range hb {
		htmlBody = append(htmlBody, p.part)
	}
	if o.text || o.all {
		add(tb)
	}
	if o.html || o.all {
		add(hb)
	}
	if o.all {
		add(parts)
	}
	return
}

// augmentEmailProps adds the header:*, textBody, htmlBody and bodyValues
// properties requested in props to out, reading the message blob once.
func (s *Server) augmentEmailProps(user, blobID string, props []string, args map[string]interface{}, out map[string]interface{}) {
	need := false
	for _, p := range props {
		if strings.HasPrefix(p, "header:") || p == "bodyValues" || p == "textBody" || p == "htmlBody" {
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
	o := bodyFetchOpts{maxBytes: -1}
	o.text, _ = args["fetchTextBodyValues"].(bool)
	o.html, _ = args["fetchHTMLBodyValues"].(bool)
	o.all, _ = args["fetchAllBodyValues"].(bool)
	if m, ok := args["maxBodyValueBytes"].(float64); ok && m >= 0 {
		o.maxBytes = int(m)
	}
	tb, hb, vals := bodyProps(hs, body, o)
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
		}
	}
}
