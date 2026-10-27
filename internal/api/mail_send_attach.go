package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"unicode/utf8"
)

// Attachment caps for POST /api/v1/mail/send (F7340). The assembled message
// must still fit the existing 25MB message limit (checked by the caller).
const (
	maxSendAttachments     = 20
	maxSendAttachmentBytes = 10 << 20 // decoded, per file
	maxSendAttachmentTotal = 15 << 20 // decoded, all files
)

// SendAttachment is one base64-encoded file in a send request.
type SendAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Data        string `json:"data"`
}

type decodedAttachment struct {
	filename    string
	contentType string
	data        []byte
}

// sanitizeAttachmentName strips path components and control characters.
func sanitizeAttachmentName(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "_")
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029:
			return -1
		case r == '/' || r == '\\' || r == '"':
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(strings.TrimSpace(s), ".")
	if len(s) > 255 {
		s = s[:255]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// validateAttachments enforces the caps and decodes the payloads.
func validateAttachments(in []SendAttachment) ([]decodedAttachment, error) {
	if len(in) > maxSendAttachments {
		return nil, fmt.Errorf("too many attachments (max %d)", maxSendAttachments)
	}
	out := make([]decodedAttachment, 0, len(in))
	total := 0
	for _, a := range in {
		name := sanitizeAttachmentName(a.Filename)
		if name == "" {
			return nil, errors.New("attachment filename required")
		}
		ct := strings.TrimSpace(a.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.Contains(mt, "/") || strings.ContainsAny(ct, "\r\n") {
			return nil, errors.New("invalid attachment content type")
		}
		// Reject before decoding so oversized payloads cost no memory.
		if base64.StdEncoding.DecodedLen(len(a.Data)) > maxSendAttachmentBytes+3 {
			return nil, errors.New("attachment too large")
		}
		data, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil {
			return nil, errors.New("invalid attachment encoding")
		}
		if len(data) > maxSendAttachmentBytes {
			return nil, errors.New("attachment too large")
		}
		total += len(data)
		if total > maxSendAttachmentTotal {
			return nil, errors.New("attachments too large in total")
		}
		out = append(out, decodedAttachment{name, mt, data})
	}
	return out, nil
}

// buildMixedBody returns the Content-Type header value and the multipart/mixed
// body (text part + attachments).
// writeTextBody writes the Content-Type of a single-part text/plain message,
// the blank line and the body. Plain ASCII with short lines goes out as-is
// (7bit); anything else is quoted-printable like the text part of
// buildMixedBody, since the 7bit default allows neither 8-bit bytes nor lines
// over 998 octets (RFC 5322 §2.1.1, RFC 2045 §2.7) (F7774).
func writeTextBody(sb *strings.Builder, text string) {
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	if is7bitText(text) {
		sb.WriteString("\r\n")
		sb.WriteString(text)
		return
	}
	sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	qw := quotedprintable.NewWriter(sb)
	_, _ = qw.Write([]byte(text))
	_ = qw.Close()
}

func is7bitText(text string) bool {
	lineLen := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 0x80 {
			return false
		}
		if c == '\n' {
			lineLen = 0
			continue
		}
		if c == '\r' {
			continue
		}
		if lineLen++; lineLen > 998 {
			return false
		}
	}
	return true
}

func buildMixedBody(text string, atts []decodedAttachment) (string, []byte, error) {
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", nil, err
	}
	boundary := "umail-" + hex.EncodeToString(rnd[:])
	var b bytes.Buffer
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	qw := quotedprintable.NewWriter(&b)
	if _, err := qw.Write([]byte(text)); err != nil {
		return "", nil, err
	}
	if err := qw.Close(); err != nil {
		return "", nil, err
	}
	b.WriteString("\r\n")
	for _, a := range atts {
		cd := mime.FormatMediaType("attachment", map[string]string{"filename": a.filename})
		if cd == "" {
			return "", nil, errors.New("invalid attachment filename")
		}
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + a.contentType + "\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("Content-Disposition: " + cd + "\r\n\r\n")
		enc := base64.StdEncoding.EncodeToString(a.data)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return `multipart/mixed; boundary="` + boundary + `"`, b.Bytes(), nil
}
