package jmap

// Round 52: RFC 8621 §4.1.2 preview + hasAttachment for Email/get. Both are
// "immutable; server-set": hasAttachment SHOULD be true when the attachments
// list has at least one item without "Content-Disposition: inline"; preview
// is a plaintext fragment of the body (white space collapsed) and MUST NOT
// exceed 256 characters. Round 51's body machinery (collectMIMEParts/
// bodyProps) supplies both; this file pins them on the full form ([]Email
// fields) and the projected form, plus the plain-only control.

import (
	"strings"
	"testing"
)

const previewUser = "preview-user@example.com"

var previewLongPlain = strings.Repeat("Round52 preview sentence. ", 16)

var previewMixedMIME = "From: a@x.test\r\nTo: b@y.test\r\nSubject: Preview mixed\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"P52\"\r\n\r\n" +
	"--P52\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + previewLongPlain + "\r\n" +
	"--P52\r\nContent-Type: application/pdf; name=\"doc.pdf\"\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n" +
	"--P52--\r\n"

const previewPlainMIME = "From: a@x.test\r\nTo: b@y.test\r\nSubject: Preview plain\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n\r\nJust a short plain body.\r\n"

func previewSetup(t *testing.T, data string) (*Server, string, func()) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	if err := db.CreateMailbox(previewUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(previewUser, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(previewUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": previewUser,
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "c-import",
	})
	created, _ := resp.Args["created"].(map[string]Email)
	i1, ok := created["i1"]
	if !ok {
		t.Fatalf("INVALID setup: import failed: %v", resp.Args)
	}
	return server, i1.ID, cleanup
}

func previewGet(t *testing.T, server *Server, id string, props []string) *Email {
	t.Helper()
	pv := make([]interface{}, len(props))
	for i, p := range props {
		pv[i] = p
	}
	args := map[string]interface{}{
		"accountId": previewUser,
		"ids":       []interface{}{id},
	}
	if props != nil {
		// Omit the key entirely for the full form: an EMPTY properties
		// array means "ids only" (RFC 8620 §5.1), not "all properties".
		args["properties"] = pv
	}
	resp := server.handleEmailGet(previewUser, MethodCall{
		Name: "Email/get",
		Args: args,
		ID:   "c1",
	})
	// Full-form list is []Email; the projected form is []interface{}.
	if emails, ok := resp.Args["list"].([]Email); ok {
		if len(emails) != 1 {
			t.Fatalf("FAIL: list = %v", resp.Args)
		}
		return &emails[0]
	}
	l, ok := resp.Args["list"].([]interface{})
	if !ok || len(l) != 1 {
		t.Fatalf("FAIL: list = %v", resp.Args)
	}
	obj, _ := l[0].(map[string]interface{})
	e := &Email{}
	if p, ok := obj["preview"].(string); ok {
		e.Preview = p
	}
	if ha, ok := obj["hasAttachment"].(bool); ok {
		e.HasAttachment = ha
	}
	return e
}

func TestEmailGetPreviewAndHasAttachment(t *testing.T) {
	server, id, cleanup := previewSetup(t, previewMixedMIME)
	defer cleanup()

	// Full form.
	e := previewGet(t, server, id, nil)
	if !e.HasAttachment {
		t.Fatalf("FAIL: full-form HasAttachment = false; want true for a message with a non-inline attachment")
	}
	if len([]rune(e.Preview)) > 256 {
		t.Fatalf("FAIL: full-form preview is %d characters; MUST NOT exceed 256 (§4.1.2)", len([]rune(e.Preview)))
	}
	if !strings.HasPrefix(e.Preview, "Round52 preview sentence.") {
		t.Fatalf("FAIL: full-form preview = %q; want a fragment of the plain body", e.Preview)
	}

	// Projected form.
	e = previewGet(t, server, id, []string{"preview", "hasAttachment"})
	if !e.HasAttachment {
		t.Fatalf("FAIL: projected HasAttachment = false; want true")
	}
	if len([]rune(e.Preview)) > 256 || !strings.HasPrefix(e.Preview, "Round52 preview sentence.") {
		t.Fatalf("FAIL: projected preview = %q; want a ≤256-char plain fragment", e.Preview)
	}
}

func TestEmailGetPlainOnlyHasAttachmentFalse(t *testing.T) {
	server, id, cleanup := previewSetup(t, previewPlainMIME)
	defer cleanup()

	e := previewGet(t, server, id, []string{"preview", "hasAttachment"})
	if e.HasAttachment {
		t.Fatalf("FAIL: plain-only HasAttachment = true; want false")
	}
	if !strings.Contains(e.Preview, "Just a short plain body.") {
		t.Fatalf("FAIL: plain-only preview = %q; want the body fragment", e.Preview)
	}
}
