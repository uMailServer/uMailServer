package search

import (
	"bytes"
	"encoding/base64"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxMIMEDepth = 8

var htmlSkipRE = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)\s*>`)

// extractMIMEText returns normalized searchable body text.
func extractMIMEText(data []byte) string {
	var parts []string
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		// Not parseable as a message: fall back to the legacy split.
		return legacyExtract(data)
	}
	walkEntity(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body, 0, &parts)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func legacyExtract(data []byte) string {
	content := string(data)
	i := strings.Index(content, "\r\n\r\n")
	if i == -1 {
		i = strings.Index(content, "\n\n")
	}
	if i != -1 {
		content = content[i:]
	}
	return strings.Join(strings.Fields(stripHTML(content)), " ")
}

func walkEntity(ctype, cte string, body io.Reader, depth int, out *[]string) {
	if depth > maxMIMEDepth {
		return
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil || mt == "" {
		mt, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mt, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(body, boundary)
		for {
			p, err := mr.NextPart()
			if err != nil {
				return
			}
			if strings.EqualFold(strings.SplitN(p.Header.Get("Content-Disposition"), ";", 2)[0], "attachment") {
				continue
			}
			// NextPart already decodes quoted-printable; do not double decode.
			walkEntity(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p, depth+1, out)
		}
	}
	if mt != "text/plain" && mt != "text/html" {
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(body, 8<<20))
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		clean := strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, string(raw))
		if dec, err := base64.StdEncoding.DecodeString(clean); err == nil {
			raw = dec
		} else if dec, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "=")); err == nil {
			raw = dec
		}
	case "quoted-printable":
		if dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw))); err == nil || len(dec) > 0 {
			raw = dec
		}
	}
	text := decodeCharset(raw, params["charset"])
	if mt == "text/html" {
		text = htmlSkipRE.ReplaceAllString(text, " ")
		text = stripHTML(text)
		text = html.UnescapeString(text)
	}
	*out = append(*out, text)
}

func decodeCharset(b []byte, charset string) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "iso8859-1", "windows-1252", "cp1252":
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), " ")
}

// decodeHeaderValue decodes RFC 2047 encoded-words for indexing.
func decodeHeaderValue(v string) string {
	dec := mime.WordDecoder{CharsetReader: func(cs string, in io.Reader) (io.Reader, error) {
		bs, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		return strings.NewReader(decodeCharset(bs, cs)), nil
	}}
	if out, err := dec.DecodeHeader(v); err == nil {
		return out
	}
	return v
}
