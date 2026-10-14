package jmap

// Regression test for the round-56 defect: htmlToPreviewText (round 54)
// dropped tags but kept all inter-tag text, so the RFC 8621 §4.1.2 preview
// of an HTML email leaked <script> and <style> block CONTENTS (CSS rules,
// JavaScript source) into what the RFC defines as "a plaintext fragment of
// the body" — round 54's documented v1 limitation. The fix: when a dropped
// tag opens a script/style block (word-boundary checked — "<scripty>" is a
// plain tag), skip case-insensitively to the matching close tag; a missing
// close tag drops the remainder, consistent with the unterminated-tag rule.
// Round-54 semantics (tag stripping, entity decoding, 256-char cap) and
// text/plain previews are unchanged.

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func r56DurImport(t *testing.T, server *Server, msgStore *storage.MessageStore, raw string) string {
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

func r56DurPreview(t *testing.T, server *Server, id string) string {
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
	return list[0].Preview
}

func TestEmailGetHtmlPreviewDropsScriptStyleContents(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R56 durable\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<html><head><STYLE>body { color: red; font-size: 12px; }</STYLE></head>" +
		"<body><p>Visible text here.</p>" +
		"<script type=\"text/javascript\">var tracking = 1; document.cookie;</script>" +
		"</body></html>"
	id := r56DurImport(t, server, msgStore, raw)

	prev := r56DurPreview(t, server, id)
	low := strings.ToLower(prev)
	if strings.Contains(low, "color") || strings.Contains(low, "font-size") {
		t.Fatalf("FAIL: preview leaks <style> contents: %q", prev)
	}
	if strings.Contains(low, "tracking") || strings.Contains(low, "document.cookie") {
		t.Fatalf("FAIL: preview leaks <script> contents: %q", prev)
	}
	if !strings.Contains(prev, "Visible text here.") {
		t.Fatalf("FAIL: preview lost the visible text: %q", prev)
	}
}

func TestEmailGetHtmlPreviewUnterminatedStyleDropsRemainder(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R56 durable unterminated\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Before the block.</p><style>p { color: blue; "
	id := r56DurImport(t, server, msgStore, raw)

	prev := r56DurPreview(t, server, id)
	if !strings.Contains(prev, "Before the block.") {
		t.Fatalf("FAIL: preview lost the pre-block text: %q", prev)
	}
	if strings.Contains(strings.ToLower(prev), "color") {
		t.Fatalf("FAIL: preview leaks the unterminated <style> contents: %q", prev)
	}
}

// Controls: round-54 semantics unchanged (tags stripped, entities decoded);
// "<scripty>" is a plain tag by the word-boundary check, so its text
// survives; text/plain previews are the raw body.
func TestEmailGetHtmlPreviewScriptStyleControls(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}

	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R56 control\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Hello <b>round56</b>, welcome to the &amp; garden.</p><scripty>tagnames are text</scripty>"
	prev := r56DurPreview(t, server, r56DurImport(t, server, msgStore, raw))
	if !strings.Contains(prev, "Hello round56, welcome to the & garden.") {
		t.Fatalf("CONTROL FAILED: round-54 tag/entity handling changed: %q", prev)
	}
	if !strings.Contains(prev, "tagnames are text") {
		t.Fatalf("CONTROL FAILED: <scripty> is not a script block; its text must survive: %q", prev)
	}

	plain := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R56 plain\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\nJust a short plain body."
	if prev := r56DurPreview(t, server, r56DurImport(t, server, msgStore, plain)); prev != "Just a short plain body." {
		t.Fatalf("CONTROL FAILED: plain preview = %q", prev)
	}
}
