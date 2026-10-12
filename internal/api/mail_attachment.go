package api

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxAttachmentBytes = 25 << 20 // decoded size cap for one download
	maxMIMEDepth       = 8
	maxMIMEParts       = 200
)

// AttachmentInfo describes one downloadable MIME part of a message.
type AttachmentInfo struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

type rawAttachment struct {
	info AttachmentInfo
	data []byte // decoded content
}

// listAttachments walks the MIME tree of a raw message and returns every
// attachment part (disposition attachment, or a named non-text part) in a
// stable document order, with decoded bytes.
func listAttachments(raw []byte) []rawAttachment {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var out []rawAttachment
	count := 0
	var walk func(hdr textproto.MIMEHeader, body io.Reader, depth int)
	walk = func(hdr textproto.MIMEHeader, body io.Reader, depth int) {
		if depth > maxMIMEDepth || count > maxMIMEParts {
			return
		}
		count++
		ct := hdr.Get("Content-Type")
		mt, params, err := mime.ParseMediaType(ct)
		if err != nil || mt == "" {
			mt, params = "text/plain", nil
		}
		if strings.HasPrefix(mt, "multipart/") && params["boundary"] != "" {
			mr := multipart.NewReader(body, params["boundary"])
			for {
				p, err := mr.NextRawPart()
				if err != nil {
					return
				}
				walk(p.Header, p, depth+1)
			}
		}
		disp, dparams, _ := mime.ParseMediaType(hdr.Get("Content-Disposition"))
		name := dparams["filename"]
		if name == "" {
			name = params["name"]
		}
		isAtt := disp == "attachment" || (name != "" && !strings.HasPrefix(mt, "multipart/"))
		if depth == 0 && disp != "attachment" {
			isAtt = false // a top-level body is not an attachment unless asked
		}
		if !isAtt {
			return
		}
		data := decodePart(hdr.Get("Content-Transfer-Encoding"), body)
		if data == nil {
			return
		}
		out = append(out, rawAttachment{
			info: AttachmentInfo{Index: len(out), Filename: sanitizeFilename(name, len(out)), ContentType: mt, Size: int64(len(data))},
			data: data,
		})
	}
	walk(textproto.MIMEHeader(msg.Header), msg.Body, 0)
	return out
}

// decodePart decodes a transfer-encoded part, bounded by maxAttachmentBytes+1
// so an oversized part is detectable without unbounded memory.
func decodePart(cte string, r io.Reader) []byte {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &stripWS{r})
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	b, _ := io.ReadAll(io.LimitReader(r, maxAttachmentBytes+1))
	if b == nil {
		b = []byte{}
	}
	return b
}

// stripWS drops CR/LF/space so base64 with line breaks decodes.
type stripWS struct{ r io.Reader }

func (s *stripWS) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for i := 0; i < n; i++ {
		if c := p[i]; c != '\r' && c != '\n' && c != ' ' && c != '\t' {
			p[j] = c
			j++
		}
	}
	return j, err
}

// sanitizeFilename reduces a client-supplied name to a safe base name.
func sanitizeFilename(name string, idx int) string {
	if dec, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
		name = dec
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	var sb strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '%' {
			sb.WriteRune('_')
			continue
		}
		sb.WriteRune(r)
	}
	name = strings.TrimSpace(sb.String())
	if len(name) > 200 {
		name = name[:200]
	}
	if name == "" || name == "." || name == ".." || name == "/" {
		return "attachment-" + strconv.Itoa(idx+1)
	}
	return name
}

// handleMailAttachment serves one attachment of the caller's message.
//
//	GET /api/v1/mail/attachment?id=<id>&folder=<folder>&index=<n>
func (h *MailHandler) handleMailAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.mailDB == nil || h.msgStore == nil {
		h.sendError(w, http.StatusServiceUnavailable, "Mail storage not available")
		return
	}
	q := r.URL.Query()
	id := q.Get("id")
	if !validMessageID(id) {
		h.sendError(w, http.StatusBadRequest, "Valid message id required")
		return
	}
	idx, err := strconv.Atoi(q.Get("index"))
	if err != nil || idx < 0 {
		h.sendError(w, http.StatusBadRequest, "Valid attachment index required")
		return
	}
	folder := q.Get("folder")
	if _, ok := h.lookupMailboxes(user, folder); !ok {
		h.sendError(w, http.StatusBadRequest, "Invalid folder")
		return
	}
	mb, _, meta, found := h.findAnywhere(user, folder, id)
	if !found || meta == nil {
		h.sendError(w, http.StatusNotFound, "Email not found")
		return
	}
	_ = mb
	data, err := h.msgStore.ReadMessage(user, meta.MessageID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to read email")
		return
	}
	atts := listAttachments(data)
	if idx >= len(atts) {
		h.sendError(w, http.StatusNotFound, "Attachment not found")
		return
	}
	a := atts[idx]
	if a.info.Size > maxAttachmentBytes {
		h.sendError(w, http.StatusRequestEntityTooLarge, "Attachment too large")
		return
	}
	// Always download as an opaque binary: never render sender-controlled
	// content (HTML/SVG) in our origin.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(a.info.Filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(a.data)))
	_, _ = w.Write(a.data)
}

// contentDispositionAttachment builds a header with an ASCII fallback and an
// RFC 5987 filename*, never containing quotes or control characters.
func contentDispositionAttachment(name string) string {
	ascii := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == ';' || r == '%' {
			r = '_'
		}
		ascii = append(ascii, r)
	}
	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", string(ascii), pctEncode(name))
}

func pctEncode(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}
