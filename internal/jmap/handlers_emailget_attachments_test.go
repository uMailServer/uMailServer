package jmap

// Round 51: RFC 8621 §4.1.4 attachments list for Email/get. Round 50's
// body machinery (augmentEmailBodyStruct / augmentEmailProps) emitted only
// textBody/htmlBody/bodyValues — collectTextParts dropped non-text parts and
// Content-Disposition:attachment parts entirely. Round 51 collects every
// MIME part (collectMIMEParts, server-assigned partIds in document order)
// and classifies: attachments = every part not in textBody/htmlBody.
// Regression pins: textBody/htmlBody/bodyValues keep working (round 50) and
// text parts are never classified as attachments.

import (
	"testing"
)

const attachmentsUser = "attachments-user@example.com"

const attachmentsMIME = "From: a@x.test\r\nTo: b@y.test\r\nSubject: Attach51\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"A51\"\r\n\r\n" +
	"--A51\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHello plain attach51\r\n" +
	"--A51\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello html attach51</p>\r\n" +
	"--A51\r\nContent-Type: application/pdf; name=\"report.pdf\"\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n" +
	"--A51\r\nContent-Type: image/png; name=\"pic.png\"\r\nContent-Disposition: attachment; filename=\"pic.png\"\r\nContent-Transfer-Encoding: base64\r\n\r\niVBORw0KGgo=\r\n" +
	"--A51--\r\n"

func attachmentsSetup(t *testing.T) (*Server, string, func()) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	if err := db.CreateMailbox(attachmentsUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(attachmentsUser, []byte(attachmentsMIME))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(attachmentsUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": attachmentsUser,
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

func attachmentsGet(t *testing.T, server *Server, id string, props []string, args map[string]interface{}) *Email {
	t.Helper()
	if args == nil {
		args = map[string]interface{}{}
	}
	args["accountId"] = attachmentsUser
	args["ids"] = []interface{}{id}
	if props != nil {
		// handleEmailGet asserts properties as []interface{} (the wire
		// path decodes JSON arrays as such).
		pv := make([]interface{}, len(props))
		for i, p := range props {
			pv[i] = p
		}
		args["properties"] = pv
	}
	resp := server.handleEmailGet(attachmentsUser, MethodCall{
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
	// Projected entries are map[string]interface{} whose values keep the
	// Go types (projectEmail copies the struct fields); rebuild the typed
	// Email from whichever properties were requested.
	l, ok := resp.Args["list"].([]interface{})
	if !ok || len(l) != 1 {
		t.Fatalf("FAIL: list = %v", resp.Args)
	}
	obj, _ := l[0].(map[string]interface{})
	e := &Email{}
	if atts, ok := obj["attachments"].([]EmailBodyPart); ok {
		e.Attachments = atts
	}
	if tb, ok := obj["textBody"].([]EmailBodyPart); ok {
		e.TextBody = tb
	}
	if hb, ok := obj["htmlBody"].([]EmailBodyPart); ok {
		e.HTMLBody = hb
	}
	return e
}

func TestEmailGetFullFormIncludesAttachments(t *testing.T) {
	server, id, cleanup := attachmentsSetup(t)
	defer cleanup()

	e := attachmentsGet(t, server, id, nil, nil)
	if len(e.Attachments) != 2 {
		t.Fatalf("FAIL: full-form attachments = %+v; want 2 attachment parts (RFC 8621 §4.1.4)", e.Attachments)
	}
	types := map[string]bool{}
	for _, a := range e.Attachments {
		types[a.Type] = true
		if a.PartID == "" {
			t.Fatalf("FAIL: attachment %+v has no server-assigned partId", a)
		}
		if a.Size <= 0 {
			t.Fatalf("FAIL: attachment %+v has size <= 0", a)
		}
	}
	if !types["application/pdf"] || !types["image/png"] {
		t.Fatalf("FAIL: attachment types = %v; want application/pdf + image/png", types)
	}
	// Round-50 regression: text bodies stay populated.
	if len(e.TextBody) == 0 || len(e.HTMLBody) == 0 {
		t.Fatalf("FAIL: textBody/htmlBody missing: %+v", e)
	}
}

func TestEmailGetProjectedAttachments(t *testing.T) {
	server, id, cleanup := attachmentsSetup(t)
	defer cleanup()

	e := attachmentsGet(t, server, id, []string{"attachments"}, nil)
	if len(e.Attachments) != 2 {
		t.Fatalf("FAIL: projected attachments = %+v; want 2", e.Attachments)
	}
	names := map[string]bool{}
	for _, a := range e.Attachments {
		names[a.Name] = true
	}
	if !names["report.pdf"] || !names["pic.png"] {
		t.Fatalf("FAIL: attachment names = %v; want report.pdf + pic.png", names)
	}
}

func TestEmailGetTextPartsNeverAttachments(t *testing.T) {
	server, id, cleanup := attachmentsSetup(t)
	defer cleanup()

	e := attachmentsGet(t, server, id, []string{"attachments", "textBody", "htmlBody"}, nil)
	for _, a := range e.Attachments {
		if len(a.Type) >= 5 && a.Type[:5] == "text/" {
			t.Fatalf("FAIL: text part classified as attachment: %+v", a)
		}
	}
	if len(e.TextBody) == 0 || len(e.HTMLBody) == 0 {
		t.Fatalf("FAIL: round-50 regression — textBody/htmlBody missing: %+v", e)
	}
}

func TestEmailGetDraftWithoutAttachments(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(attachmentsUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(attachmentsUser, []byte("Subject: plain\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(attachmentsUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": attachmentsUser,
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
		t.Fatalf("INVALID setup: %v", resp.Args)
	}

	e := attachmentsGet(t, server, i1.ID, []string{"attachments"}, nil)
	if len(e.Attachments) != 0 {
		t.Fatalf("FAIL: attachmentless message reported attachments: %+v", e.Attachments)
	}
}
