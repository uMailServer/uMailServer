package api

import (
	"bufio"
	"bytes"
	"mime"
	"net/textproto"
	"strings"
)

// rawHeader parses the header block of a raw RFC 5322 message.
func rawHeader(raw []byte) textproto.MIMEHeader {
	h, _ := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw))).ReadMIMEHeader()
	return h
}

// rawHasAttachment cheaply reports whether a message carries attachments,
// derived from the top-level Content-Type: multipart/mixed (and friends other
// than alternative/related/signed-only bodies) or an attachment disposition.
func rawHasAttachment(raw []byte) bool {
	h := rawHeader(raw)
	if h == nil {
		return false
	}
	if mt, _, err := mime.ParseMediaType(h.Get("Content-Type")); err == nil {
		switch mt {
		case "multipart/mixed", "multipart/parallel", "multipart/report":
			return true
		}
	}
	if d, _, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil && d == "attachment" {
		return true
	}
	return false
}

// rawMessageIDHeader returns the Message-ID header value, if well formed.
func rawMessageIDHeader(raw []byte) string {
	h := rawHeader(raw)
	if h == nil {
		return ""
	}
	return threadingHeaderValue(h.Get("Message-ID"))
}

// threadingHeaderValue validates one <msg-id> token for In-Reply-To /
// References; anything with whitespace, control chars or no angle brackets is
// dropped so a client cannot inject headers.
func threadingHeaderValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 3 || len(s) > 998 || s[0] != '<' || s[len(s)-1] != '>' {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] == 0x7f || (s[i] == '<' && i != 0) || (s[i] == '>' && i != len(s)-1) {
			return ""
		}
	}
	return s
}

// threadingHeaderValues returns the validated References list, ensuring the
// In-Reply-To id is its last element, bounded in length.
func threadingHeaderValues(refs []string, inReplyTo string) []string {
	var out []string
	for _, r := range refs {
		if v := threadingHeaderValue(r); v != "" {
			out = append(out, v)
		}
	}
	if v := threadingHeaderValue(inReplyTo); v != "" && (len(out) == 0 || out[len(out)-1] != v) {
		out = append(out, v)
	}
	if len(out) > 20 {
		out = out[len(out)-20:]
	}
	return out
}
