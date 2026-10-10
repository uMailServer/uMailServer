package imap

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// F6175: SEARCH matched keys only against the raw header / body bytes, so a
// non-ASCII key never matched an RFC 2047 encoded header or a base64 /
// quoted-printable / legacy-charset body, and folded headers were cut at the
// first line when the metadata was extracted.

// unfoldHeaders joins folded header lines (RFC 5322 §2.2.3).
func unfoldHeaders(h string) string {
	if !strings.ContainsAny(h, "\r\n") {
		return h
	}
	h = strings.ReplaceAll(h, "\r\n", "\n")
	var b strings.Builder
	for i := 0; i < len(h); i++ {
		if h[i] == '\n' && i+1 < len(h) && (h[i+1] == ' ' || h[i+1] == '\t') {
			continue
		}
		b.WriteByte(h[i])
	}
	return b.String()
}

// headerContains reports whether a header value contains key, comparing
// case-insensitively against both the raw and the RFC 2047-decoded value.
func headerContains(value, key string) bool {
	lk := strings.ToLower(key)
	if strings.Contains(strings.ToLower(value), lk) {
		return true
	}
	if strings.Contains(value, "=?") {
		return strings.Contains(strings.ToLower(decodeEncodedWords(value)), lk)
	}
	return false
}

var latin5Diff = map[byte]rune{0xD0: 'Ğ', 0xDD: 'İ', 0xDE: 'Ş', 0xF0: 'ğ', 0xFD: 'ı', 0xFE: 'ş'}

// charsetToUTF8 converts text in a few common single-byte charsets.
func charsetToUTF8(charset string, data []byte) (string, bool) {
	switch strings.ToLower(charset) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return string(data), utf8.Valid(data)
	case "iso-8859-1", "latin1", "windows-1252", "iso-8859-9", "windows-1254", "latin5":
		turkish := strings.HasSuffix(strings.ToLower(charset), "9") || strings.HasSuffix(strings.ToLower(charset), "1254") || strings.EqualFold(charset, "latin5")
		var b strings.Builder
		for _, c := range data {
			if r, ok := latin5Diff[c]; ok && turkish {
				b.WriteRune(r)
			} else {
				b.WriteRune(rune(c))
			}
		}
		return b.String(), true
	}
	return "", false
}

func headerWordDecoder() *mime.WordDecoder {
	return &mime.WordDecoder{CharsetReader: func(charset string, in io.Reader) (io.Reader, error) {
		data, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		s, ok := charsetToUTF8(charset, data)
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		return strings.NewReader(s), nil
	}}
}

// decodeEncodedWords decodes RFC 2047 words, returning v when it cannot.
func decodeEncodedWords(v string) string {
	if d, err := headerWordDecoder().DecodeHeader(v); err == nil {
		return d
	}
	return v
}

// decodedHeaderText returns the unfolded, RFC 2047-decoded header block.
func decodedHeaderText(msg []byte) string {
	h := string(msg)
	if i := strings.Index(h, "\r\n\r\n"); i >= 0 {
		h = h[:i]
	} else if i := strings.Index(h, "\n\n"); i >= 0 {
		h = h[:i]
	}
	return decodeEncodedWords(unfoldHeaders(h))
}

// decodedBodyText returns the text of every text/* part, transfer- and
// charset-decoded to UTF-8 (undecodable parts are skipped).
func decodedBodyText(msg []byte) string {
	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		return ""
	}
	var out strings.Builder
	collectText(&out, m.Header.Get("Content-Type"), m.Header.Get("Content-Transfer-Encoding"), m.Body, 0)
	return out.String()
}

func collectText(out *strings.Builder, ctype, cte string, body io.Reader, depth int) {
	if depth > 10 || out.Len() > 16<<20 {
		return
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		mt, params = "text/plain", nil
	}
	if strings.HasPrefix(mt, "multipart/") && params["boundary"] != "" {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			collectText(out, p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p, depth+1)
		}
	}
	if !strings.HasPrefix(mt, "text/") && ctype != "" {
		return
	}
	var r io.Reader = body
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &stripWS{r: body})
	case "quoted-printable":
		r = quotedprintable.NewReader(body)
	}
	data, _ := io.ReadAll(io.LimitReader(r, 16<<20))
	if s, ok := charsetToUTF8(params["charset"], data); ok {
		out.WriteString(s)
		out.WriteByte('\n')
	}
}

// stripWS drops CR/LF/space so base64 with line breaks decodes.
type stripWS struct{ r io.Reader }

func (s *stripWS) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	k := 0
	for i := 0; i < n; i++ {
		if c := p[i]; c != '\r' && c != '\n' && c != ' ' && c != '\t' {
			p[k] = c
			k++
		}
	}
	return k, err
}
