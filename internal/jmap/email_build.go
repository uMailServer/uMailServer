package jmap

// RFC 8621 §4.6 Email/set creation: the client submits Email properties and
// the server synthesizes the RFC 5322 message. Constraint violations SHOULD
// be rejected with "invalidProperties"; the server MUST generate a
// Message-ID (RFC 5322 §3.6.4) and Date (§3.6.1) when absent, and the final
// message MAY be invalid per RFC 5322 (a half-finished draft) — strict
// conformance is checked at EmailSubmission. So no net/mail formatting or
// address validation happens here: header values are emitted as given.
//
// Documented v1 limitations (recorded round-49): "header:Form" properties
// and "bodyStructure" are rejected with invalidProperties; charsets other
// than utf-8 are base64-encoded without transcoding; no header folding.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/umailserver/umailserver/internal/storage"
)

// emailBuildError carries a §4.6 creation rejection for the Email/set
// notCreated map: setErrType is "invalidProperties" or "blobNotFound"
// (with notFound listing the missing blob ids, per §4.6).
type emailBuildError struct {
	setErrType  string
	properties  []string
	notFound    []string
	description string
}

func (e *emailBuildError) fail() ([]byte, *emailBuildError) { return nil, e }

// buildPart is a parsed EmailBodyPart of a creation record.
type buildPart struct {
	partID      string
	blobID      string
	partType    string
	charset     string
	disposition string
	cid         string
	filename    string
}

// buildRFC5322Email validates the §4.6 creation constraints against
// createData and synthesizes the raw message bytes. bodyValues supply the
// content of partId body parts; blobId parts are read from the user's blob
// store (missing ids produce blobNotFound).
func buildRFC5322Email(user string, createData map[string]interface{}, msgStore *storage.MessageStore) ([]byte, *emailBuildError) {
	invalid := func(properties []string, description string) ([]byte, *emailBuildError) {
		return nil, &emailBuildError{setErrType: "invalidProperties", properties: properties, description: description}
	}

	// §4.6 constraint: "The headers property MUST NOT be given on either
	// the top-level Email or an EmailBodyPart."
	if _, has := createData["headers"]; has {
		return invalid([]string{"headers"}, `The "headers" property MUST NOT be given on creation (RFC 8621 §4.6)`)
	}
	// v1 limitation: raw header:Form properties are not supported.
	for key := range createData {
		if strings.HasPrefix(key, "header:") {
			return invalid([]string{key}, "header:Form properties are not supported by this server's v1 creation (documented round-49 limitation)")
		}
	}
	// v1 limitation: bodyStructure composition is not supported.
	if _, has := createData["bodyStructure"]; has {
		return invalid([]string{"bodyStructure"}, "bodyStructure is not supported by this server's v1 creation (documented round-49 limitation)")
	}

	// F7382: header values are emitted verbatim, so a CR or LF in any of them
	// would inject extra headers into the synthesized message.
	if prop := lineBreakProperty(createData); prop != "" {
		return invalid([]string{prop}, prop+" MUST NOT contain CR or LF characters")
	}

	bodyValues, _ := createData["bodyValues"].(map[string]interface{})
	for _, v := range bodyValues {
		bv, ok := v.(map[string]interface{})
		if !ok {
			return invalid([]string{"bodyValues"}, "bodyValues entries must be objects")
		}
		// §4.6: isEncodingProblem and isTruncated MUST be false or omitted.
		for _, flag := range []string{"isEncodingProblem", "isTruncated"} {
			if b, has := bv[flag].(bool); has && b {
				return invalid([]string{"bodyValues"}, fmt.Sprintf("%s MUST be false or omitted on creation (RFC 8621 §4.6)", flag))
			}
		}
	}

	parseList := func(prop, requiredType string) ([]buildPart, *emailBuildError) {
		v, has := createData[prop]
		if !has || v == nil {
			return nil, nil
		}
		list, ok := v.([]interface{})
		if !ok {
			return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: prop + " must be an array"}
		}
		if prop != "attachments" && len(list) > 1 {
			return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: prop + " MUST contain exactly one body part (RFC 8621 §4.6)"}
		}
		parts := make([]buildPart, 0, len(list))
		for _, entry := range list {
			m, ok := entry.(map[string]interface{})
			if !ok {
				return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: prop + " entries must be objects"}
			}
			partID, _ := m["partId"].(string)
			blobID, _ := m["blobId"].(string)
			// §4.6: "The client may specify a partId OR a blobId, but not both."
			if (partID != "") == (blobID != "") {
				return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: "A part may specify a partId OR a blobId, but not both (RFC 8621 §4.6)"}
			}
			partType, _ := m["type"].(string)
			if partType == "" {
				partType = requiredType
			} else if requiredType != "" && partType != requiredType {
				return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: fmt.Sprintf("%s parts MUST be of type %q (RFC 8621 §4.6)", prop, requiredType)}
			}
			charset, hasCharset := m["charset"].(string)
			_, hasSize := m["size"]
			// §4.6: charset MUST be omitted if a partId is given (the
			// server chooses the encoding); size MUST be omitted too.
			if partID != "" && hasCharset {
				return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: "The charset property MUST be omitted when partId is given (RFC 8621 §4.6)"}
			}
			if partID != "" && hasSize {
				return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: "The size property MUST be omitted when partId is given (RFC 8621 §4.6)"}
			}
			disposition, _ := m["disposition"].(string)
			cid, _ := m["cid"].(string)
			name, _ := m["name"].(string)
			for field, val := range map[string]string{"type": partType, "charset": charset, "disposition": disposition, "cid": cid, "name": name} {
				if hasLineBreak(val) {
					return nil, &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: prop + " part " + field + " MUST NOT contain CR or LF characters"}
				}
			}
			parts = append(parts, buildPart{partID: partID, blobID: blobID, partType: partType, charset: charset, disposition: disposition, cid: cid, filename: name})
		}
		return parts, nil
	}

	textBody, berr := parseList("textBody", "text/plain")
	if berr != nil {
		return berr.fail()
	}
	htmlBody, berr := parseList("htmlBody", "text/html")
	if berr != nil {
		return berr.fail()
	}
	attachments, berr := parseList("attachments", "")
	if berr != nil {
		return berr.fail()
	}

	// §4.6 blobNotFound: every referenced blob must exist. MessageExists is
	// checked up front so the full notFound list is reported at once.
	var missing []string
	for _, p := range allParts(textBody, htmlBody, attachments) {
		if p.blobID != "" && !msgStore.MessageExists(user, p.blobID) {
			missing = append(missing, p.blobID)
		}
	}
	if len(missing) > 0 {
		return nil, &emailBuildError{
			setErrType:  "blobNotFound",
			notFound:    missing,
			description: "At least one blob id given for an EmailBodyPart doesn't exist (RFC 8621 §4.6)",
		}
	}

	// Headers. Conventional RFC 5322 order; Message-ID and Date are
	// generated when absent (§4.6: the server MUST generate and set both).
	var headers [][2]string
	if ids := headerStringList(createData["messageId"]); len(ids) > 0 {
		headers = append(headers, [2]string{"Message-ID", strings.Join(ids, " ")})
	} else {
		headers = append(headers, [2]string{"Message-ID", generateRFC5322MessageID()})
	}
	date := time.Now().Format(time.RFC1123Z)
	if sentAt, has := createData["sentAt"].(string); has && sentAt != "" {
		if t, err := time.Parse(time.RFC3339, sentAt); err == nil {
			date = t.Format(time.RFC1123Z)
		}
	}
	headers = append(headers, [2]string{"Date", date})
	for _, pair := range []struct{ prop, header string }{
		{"from", "From"}, {"sender", "Sender"}, {"replyTo", "Reply-To"},
		{"to", "To"}, {"cc", "Cc"}, {"bcc", "Bcc"},
	} {
		if v, has := createData[pair.prop]; has && v != nil {
			formatted, berr := formatAddressList(pair.prop, v)
			if berr != nil {
				return berr.fail()
			}
			if formatted != "" {
				headers = append(headers, [2]string{pair.header, formatted})
			}
		}
	}
	if subject, has := createData["subject"].(string); has {
		// F7772: non-ASCII text becomes an RFC 2047 encoded-word.
		headers = append(headers, [2]string{"Subject", mime.QEncoding.Encode("utf-8", subject)})
	}
	if ids := headerStringList(createData["inReplyTo"]); len(ids) > 0 {
		headers = append(headers, [2]string{"In-Reply-To", strings.Join(ids, " ")})
	}
	if ids := headerStringList(createData["references"]); len(ids) > 0 {
		headers = append(headers, [2]string{"References", strings.Join(ids, " ")})
	}
	headers = append(headers, [2]string{"MIME-Version", "1.0"})

	// Body: textBody[0] else htmlBody[0] else an empty text/plain part;
	// attachments compose a multipart/mixed wrapper around the main part.
	main := buildPart{partType: "text/plain"}
	if len(textBody) == 1 {
		main = textBody[0]
	} else if len(htmlBody) == 1 {
		main = htmlBody[0]
	}

	var body []byte
	if len(attachments) == 0 {
		hdrs, content, berr := renderPart(main, user, bodyValues, msgStore)
		if berr != nil {
			return berr.fail()
		}
		headers = append(headers, hdrs...)
		body = content
	} else {
		boundary := randomMIMEBoundary()
		headers = append(headers, [2]string{"Content-Type", `multipart/mixed; boundary="` + boundary + `"`})
		var sb strings.Builder
		for _, p := range append([]buildPart{main}, attachments...) {
			hdrs, content, berr := renderPart(p, user, bodyValues, msgStore)
			if berr != nil {
				return berr.fail()
			}
			sb.WriteString("--" + boundary + "\r\n")
			for _, h := range hdrs {
				sb.WriteString(h[0] + ": " + h[1] + "\r\n")
			}
			sb.WriteString("\r\n")
			sb.Write(content)
			sb.WriteString("\r\n")
		}
		sb.WriteString("--" + boundary + "--\r\n")
		body = []byte(sb.String())
	}

	var out strings.Builder
	for _, h := range headers {
		out.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	out.WriteString("\r\n")
	out.Write(body)
	return []byte(out.String()), nil
}

// renderPart renders one MIME part: its Content-Type/Transfer-Encoding
// (plus disposition/id) headers and its encoded content.
func renderPart(p buildPart, user string, bodyValues map[string]interface{}, msgStore *storage.MessageStore) ([][2]string, []byte, *emailBuildError) {
	var content []byte
	charset := p.charset
	if charset == "" {
		charset = "utf-8"
	}
	if p.partID != "" {
		bv, _ := bodyValues[p.partID].(map[string]interface{})
		value, _ := bv["value"].(string)
		content = []byte(value)
	} else if p.blobID != "" { // F7381: no partId and no blobId = empty main part
		data, err := msgStore.ReadMessage(user, p.blobID)
		if err != nil {
			return nil, nil, &emailBuildError{
				setErrType:  "blobNotFound",
				notFound:    []string{p.blobID},
				description: "At least one blob id given for an EmailBodyPart doesn't exist (RFC 8621 §4.6)",
			}
		}
		content = data
	}

	encoding := "8bit"
	data := content
	if p.blobID != "" || charset != "utf-8" || !isUTF8(content) {
		// v1 limitation: no charset transcoding — non-utf-8 content is
		// base64-labelled with the requested charset.
		encoding = "base64"
		data = []byte(wrapBase64(base64.StdEncoding.EncodeToString(content)))
	} else if hasLongLine(content) {
		// F7773: RFC 5322 §2.1.1 limits lines to 998 octets; quoted-printable
		// soft-wraps them without changing the decoded text.
		encoding = "quoted-printable"
		var qp bytes.Buffer
		w := quotedprintable.NewWriter(&qp)
		_, _ = w.Write(content)
		_ = w.Close()
		data = qp.Bytes()
	}

	var hdrs [][2]string
	contentType := p.partType
	if strings.HasPrefix(p.partType, "text/") {
		contentType += "; charset=" + charset
	}
	hdrs = append(hdrs, [2]string{"Content-Type", contentType})
	hdrs = append(hdrs, [2]string{"Content-Transfer-Encoding", encoding})
	if p.cid != "" {
		hdrs = append(hdrs, [2]string{"Content-ID", "<" + strings.Trim(p.cid, "<>") + ">"})
	}
	if p.disposition != "" || p.filename != "" {
		disposition := p.disposition
		if disposition == "" {
			disposition = "attachment"
		}
		if p.filename != "" {
			disposition += "; filename=" + quoteIfNeeded(p.filename)
		}
		hdrs = append(hdrs, [2]string{"Content-Disposition", disposition})
	}
	return hdrs, data, nil
}

func hasLineBreak(s string) bool { return strings.ContainsAny(s, "\r\n") }

// lineBreakProperty returns the first header-bound creation property that
// contains CR or LF, or "" when none does.
func lineBreakProperty(createData map[string]interface{}) string {
	if s, _ := createData["subject"].(string); hasLineBreak(s) {
		return "subject"
	}
	for _, prop := range []string{"messageId", "inReplyTo", "references"} {
		for _, id := range headerStringList(createData[prop]) {
			if hasLineBreak(id) {
				return prop
			}
		}
	}
	for _, prop := range []string{"from", "sender", "replyTo", "to", "cc", "bcc"} {
		list, _ := createData[prop].([]interface{})
		for _, entry := range list {
			m, _ := entry.(map[string]interface{})
			email, _ := m["email"].(string)
			name, _ := m["name"].(string)
			if hasLineBreak(email) || hasLineBreak(name) {
				return prop
			}
		}
	}
	return ""
}

func allParts(lists ...[]buildPart) []buildPart {
	var out []buildPart
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// headerStringList parses a String[]-typed creation property.
func headerStringList(v interface{}) []string {
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, entry := range list {
		if s, ok := entry.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// formatAddressList formats an EmailAddress[] creation property. Addresses
// are emitted as given — a draft may contain addresses that are not valid
// RFC 5322 addr-specs (§4.6 leniency; conformance is checked at
// EmailSubmission).
func formatAddressList(prop string, v interface{}) (string, *emailBuildError) {
	list, ok := v.([]interface{})
	if !ok || len(list) == 0 {
		return "", nil
	}
	entries := make([]string, 0, len(list))
	for _, entry := range list {
		m, ok := entry.(map[string]interface{})
		if !ok {
			return "", &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: prop + " entries must be EmailAddress objects"}
		}
		email, _ := m["email"].(string)
		name, _ := m["name"].(string)
		if email == "" && name == "" {
			return "", &emailBuildError{setErrType: "invalidProperties", properties: []string{prop}, description: prop + " entries must include an email"}
		}
		if name != "" {
			entries = append(entries, displayName(name)+" <"+email+">")
		} else {
			entries = append(entries, email)
		}
	}
	return strings.Join(entries, ", "), nil
}

// quoteIfNeeded quotes a display name containing RFC 5322 specials. Inside
// the quoted-string both '"' and '\' are quoted-pairs (F7771): an unescaped
// trailing backslash would escape the closing quote.
func quoteIfNeeded(name string) string {
	if strings.ContainsAny(name, `"(),.:;<>@[\]`) {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name
}

// displayName renders a display name for an address header: an RFC 2047
// encoded-word when it is not ASCII (F7772), a quoted-string otherwise.
func displayName(name string) string {
	if enc := mime.QEncoding.Encode("utf-8", name); enc != name {
		return enc
	}
	return quoteIfNeeded(name)
}

// generateRFC5322MessageID mirrors internal/queue/dsn.go's collision-hardened
// pattern (crypto/rand with a timestamp fallback).
func generateRFC5322MessageID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		ts := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			b[i] = byte(ts >> (i * 8))
		}
	}
	return fmt.Sprintf("<%d.%s@umailserver>", time.Now().UnixNano(), hex.EncodeToString(b))
}

func randomMIMEBoundary() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("_jmap_%d", time.Now().UnixNano())
	}
	return "_jmap_" + hex.EncodeToString(b)
}

// hasLongLine reports whether any line of b exceeds 998 octets.
func hasLongLine(b []byte) bool {
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSuffix(line, []byte("\r"))) > 998 {
			return true
		}
	}
	return false
}

func isUTF8(b []byte) bool {
	return utf8.Valid(b)
}

func wrapBase64(s string) string {
	const width = 76
	var out strings.Builder
	for len(s) > width {
		out.WriteString(s[:width])
		out.WriteString("\r\n")
		s = s[width:]
	}
	out.WriteString(s)
	return out.String()
}
