package api

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"

	"github.com/umailserver/umailserver/internal/mimeutil"
)

// Limits for decoding a body for display.
const (
	maxDisplayMIMEDepth = 8
	maxDisplayBodyBytes = 10 << 20
)

// decodeDisplayBody returns the text the webmail shows for a MIME message:
// the decoded text/plain part, or the text/html part when there is no plain
// one (the frontend sanitizes HTML). ok is false for a message that declares
// no MIME structure or is a single non-text part; the caller then shows the raw body
// (F7775: transfer encodings and multipart wrappers were shown verbatim).
func decodeDisplayBody(raw string) (string, bool) {
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return "", false
	}
	ctype := msg.Header.Get("Content-Type")
	cte := msg.Header.Get("Content-Transfer-Encoding")
	if ctype == "" && cte == "" {
		return "", false
	}
	var plain, html string
	var foundPlain, foundHTML bool
	var walk func(ctype, cte, disposition string, body io.Reader, depth int)
	walk = func(ctype, cte, disposition string, body io.Reader, depth int) {
		if depth > maxDisplayMIMEDepth || foundPlain {
			return
		}
		mt, params, err := mime.ParseMediaType(ctype)
		if err != nil || ctype == "" {
			mt, params = "text/plain", map[string]string{}
		}
		if strings.HasPrefix(mt, "multipart/") {
			if params["boundary"] == "" {
				return
			}
			mr := multipart.NewReader(body, params["boundary"])
			for {
				// NextRawPart: the transfer encoding is decoded below.
				p, err := mr.NextRawPart()
				if err != nil {
					return
				}
				walk(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p.Header.Get("Content-Disposition"), p, depth+1)
				if foundPlain {
					return
				}
			}
		}
		if d, _, _ := mime.ParseMediaType(disposition); d == "attachment" {
			return
		}
		if mt != "text/plain" && mt != "text/html" {
			return
		}
		if mt == "text/html" && foundHTML {
			return
		}
		text := mimeutil.DecodeCharset(decodeTransferEncoding(cte, body), params["charset"])
		if mt == "text/plain" {
			plain, foundPlain = text, true
		} else {
			html, foundHTML = text, true
		}
	}
	walk(ctype, cte, "", msg.Body, 0)
	switch {
	case foundPlain:
		return strings.TrimSpace(plain), true
	case foundHTML:
		return strings.TrimSpace(html), true
	}
	// A multipart message without a text part (only attachments) has no
	// body to show; its raw MIME structure is not one.
	if mt, _, err := mime.ParseMediaType(ctype); err == nil && strings.HasPrefix(mt, "multipart/") {
		return "", true
	}
	return "", false
}

// decodeTransferEncoding reads body, undoing a base64 or quoted-printable
// transfer encoding; other encodings are read as-is.
func decodeTransferEncoding(cte string, body io.Reader) []byte {
	body = io.LimitReader(body, maxDisplayBodyBytes)
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		data, _ := io.ReadAll(body)
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, data)
		if out, err := base64.StdEncoding.DecodeString(string(clean)); err == nil {
			return out
		}
		if out, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(string(clean), "=")); err == nil {
			return out
		}
		return data
	case "quoted-printable":
		out, _ := io.ReadAll(quotedprintable.NewReader(body))
		return out
	}
	out, _ := io.ReadAll(body)
	return out
}
