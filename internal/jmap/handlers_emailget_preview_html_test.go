package jmap

// Regression test for the round-54 defect: bodyProps' preview computation
// used the chosen text part's value verbatim, and round 51's text-part
// fallback makes an html-only message's textBody the text/html part — so
// the RFC 8621 §4.1.2 preview of an html-only email was the RAW HTML SOURCE
// (tags, entities) instead of "a plaintext fragment". The fix strips tags
// and decodes entities (htmlToPreviewText, stdlib html.UnescapeString) when
// the preview source part is text/html, before the existing white-space
// collapse + 256-byte truncation; text/plain previews are unchanged and a
// mixed message still previews from the plain part.

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

const r54DurPDFMark = "JVBERi0xLjQK"

func r54DurImport(t *testing.T, server *Server, msgStore *storage.MessageStore, raw string) string {
	t.Helper()
	blobID, err := msgStore.StoreMessage("user@example.com", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "c1",
	}
	resp := server.handleEmailImport("user@example.com", call)
	created, _ := resp.Args["created"].(map[string]Email)
	e, ok := created["i1"]
	if !ok {
		t.Fatalf("FAIL: created[i1] missing (notCreated=%v)", resp.Args["notCreated"])
	}
	return e.ID
}

func r54DurGetFull(t *testing.T, server *Server, id string) Email {
	t.Helper()
	call := MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"ids": []interface{}{id},
		},
		ID: "c2",
	}
	resp := server.handleEmailGet("user@example.com", call)
	list, ok := resp.Args["list"].([]Email)
	if !ok || len(list) != 1 {
		t.Fatalf("FAIL: full-form list = %v", resp.Args["list"])
	}
	return list[0]
}

func TestEmailGetHtmlOnlyPreviewStripsTags(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R54 durable html\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<html><body><p>Hello <b>round54</b>, welcome to the &amp; garden.</p></body></html>"
	id := r54DurImport(t, server, msgStore, raw)

	e := r54DurGetFull(t, server, id)
	if strings.ContainsAny(e.Preview, "<>") || strings.Contains(e.Preview, "&amp;") {
		t.Fatalf("FAIL: html-only preview = %q; §4.1.2 requires a plaintext fragment (tags stripped, entities decoded)", e.Preview)
	}
	if e.Preview != "Hello round54, welcome to the & garden." {
		t.Fatalf("FAIL: html-only preview = %q; want \"Hello round54, welcome to the & garden.\"", e.Preview)
	}
}

func TestEmailGetHtmlOnlyPreviewCapped(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("<p>filler words to exceed the cap. </p>", 20)
	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R54 durable long\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<html><body>" + long + "</body></html>"
	id := r54DurImport(t, server, msgStore, raw)

	e := r54DurGetFull(t, server, id)
	if len(e.Preview) == 0 || len(e.Preview) > 256 {
		t.Fatalf("FAIL: html-only preview len = %d; §4.1.2 caps preview at 256 characters", len(e.Preview))
	}
	if strings.ContainsAny(e.Preview, "<>") {
		t.Fatalf("FAIL: html-only preview contains markup: %q", e.Preview)
	}
}

// Controls: text/plain previews unchanged (round-52 semantics); a mixed
// message previews from the plain part, not the HTML part; an attachment on
// an html-only message keeps hasAttachment true.
func TestEmailGetPreviewControlsAfterHtmlStrip(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}

	plain := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R54 durable plain\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\nJust a short plain body."
	ePlain := r54DurGetFull(t, server, r54DurImport(t, server, msgStore, plain))
	if ePlain.Preview != "Just a short plain body." {
		t.Fatalf("CONTROL FAILED: plain preview = %q; want the raw body fragment", ePlain.Preview)
	}

	mixed := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R54 durable mixed\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=r54d\r\n\r\n" +
		"--r54d\r\nContent-Type: text/plain\r\n\r\nPlain wins for preview.\r\n" +
		"--r54d\r\nContent-Type: text/html\r\n\r\n<b>HTML</b> would lose.\r\n" +
		"--r54d--\r\n"
	eMixed := r54DurGetFull(t, server, r54DurImport(t, server, msgStore, mixed))
	if eMixed.Preview != "Plain wins for preview." {
		t.Fatalf("CONTROL FAILED: mixed preview = %q; want the plain part's fragment", eMixed.Preview)
	}

	htmlAtt := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R54 durable html+att\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=r54m\r\n\r\n" +
		"--r54m\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Body only in <i>HTML</i>.</p>\r\n" +
		"--r54m\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\n\r\n" +
		r54DurPDFMark + "\r\n" +
		"--r54m--\r\n"
	eHtmlAtt := r54DurGetFull(t, server, r54DurImport(t, server, msgStore, htmlAtt))
	if !eHtmlAtt.HasAttachment {
		t.Fatalf("CONTROL FAILED: html+attachment HasAttachment = false")
	}
	if eHtmlAtt.Preview != "Body only in HTML." {
		t.Fatalf("CONTROL FAILED: html+attachment preview = %q; want \"Body only in HTML.\"", eHtmlAtt.Preview)
	}
}
