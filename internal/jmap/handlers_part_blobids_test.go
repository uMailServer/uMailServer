package jmap

// Regression test for the round-59 feature: Email/get body parts exposed no
// per-part blobId (EmailBodyPart.BlobID was never set — the blob store is
// whole-message), so RFC 8621 §4.1.4 part content was unreachable via the
// standard download flow: clients could not fetch an attachment at all.
// §4.1.4 makes blobId a standard part property (its examples show per-part
// blobIds) and defines part size as the octets AFTER content-transfer
// decoding. The fix: collectMIMEParts composes the virtual per-part blob id
// "{messageBlobId}.{partId}" (real blob ids are 64-hex without dots, so the
// forms are unambiguous) for every part, non-text parts get their transfer
// encoding decoded (shared decodeTransfer helper) so the advertised size is
// the decoded octet count, and handleDownload resolves the composite form
// by parsing the parent message's MIME and serving the decoded part content
// with the part's media type. The whole-message download path is unchanged.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const r59DurPDF = "%PDF-1.4 fake pdf bytes for the round-59 durable proof"

func r59DurFixture(t *testing.T) (*Server, string) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	defer func() { _ = db }()
	t.Cleanup(cleanup)
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R59 durable\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=r59d\r\n\r\n" +
		"--r59d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Plain body for the round-59 durable proof.\r\n" +
		"--r59d\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\n\r\n" +
		r59DurPDF + "\r\n" +
		"--r59d--\r\n"
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
	return server, e.ID
}

func r59DurFullForm(t *testing.T, server *Server, id string) Email {
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

func r59DurDownload(t *testing.T, server *Server, blobID string) (int, string, string) {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/jmap/download/user@example.com/"+blobID+"/part.bin?accept=application/pdf", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()
	server.handleDownload(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Content-Type"), w.Body.String()
}

func TestPartBlobIDsExposedAndDownloadable(t *testing.T) {
	server, id := r59DurFixture(t)
	email := r59DurFullForm(t, server, id)

	// The message blob id is the prefix of every per-part blob id; recover
	// it from the text part (first part, partId "1").
	if len(email.TextBody) != 1 {
		t.Fatalf("FAIL: textBody = %v", email.TextBody)
	}
	textBlob := email.TextBody[0].BlobID
	dot := strings.LastIndexByte(textBlob, '.')
	if dot <= 0 {
		t.Fatalf("FAIL: text part blobId = %q; want the composite {msgBlobId}.{partId} form", textBlob)
	}
	msgBlobID := textBlob[:dot]

	if got := email.TextBody[0].BlobID; got != msgBlobID+".1" {
		t.Fatalf("FAIL: text part blobId = %q; want %q", got, msgBlobID+".1")
	}
	if len(email.Attachments) != 1 {
		t.Fatalf("FAIL: attachments = %v", email.Attachments)
	}
	att := email.Attachments[0]
	if att.BlobID != msgBlobID+".2" {
		t.Fatalf("FAIL: attachment blobId = %q; want %q", att.BlobID, msgBlobID+".2")
	}
	// §4.1.4: part size is the octets after content-transfer decoding.
	if att.Size != int64(len(r59DurPDF)) {
		t.Fatalf("FAIL: attachment size = %d; want the decoded octet count %d", att.Size, len(r59DurPDF))
	}

	// Download the attachment by its per-part blob id.
	status, contentType, body := r59DurDownload(t, server, att.BlobID)
	if status != http.StatusOK {
		t.Fatalf("FAIL: attachment download status = %d (body: %s)", status, body)
	}
	if body != r59DurPDF {
		t.Fatalf("FAIL: attachment download body = %q; want the decoded part content", body)
	}
	if !strings.Contains(contentType, "application/pdf") {
		t.Fatalf("FAIL: attachment download Content-Type = %q", contentType)
	}

	// Download the text part by its per-part blob id.
	statusT, _, bodyT := r59DurDownload(t, server, textBlob)
	if statusT != http.StatusOK {
		t.Fatalf("FAIL: text part download status = %d (body: %s)", statusT, bodyT)
	}
	if bodyT != "Plain body for the round-59 durable proof." {
		t.Fatalf("FAIL: text part download body = %q", bodyT)
	}
}

// splitPartBlobID unit coverage: real blob ids (64-hex) are NOT part ids;
// malformed composites are rejected.
func TestSplitPartBlobIDWordBoundary(t *testing.T) {
	real := strings.Repeat("a", 64)
	if _, _, ok := splitPartBlobID(real); ok {
		t.Fatalf("FAIL: a real 64-hex blob id must not be treated as a part id")
	}
	if _, _, ok := splitPartBlobID("short.1"); ok {
		t.Fatalf("FAIL: a non-64-char message id must be rejected")
	}
	if _, _, ok := splitPartBlobID(real + "."); ok {
		t.Fatalf("FAIL: an empty part id must be rejected")
	}
	if _, _, ok := splitPartBlobID(real + ".x"); ok {
		t.Fatalf("FAIL: a non-decimal part id must be rejected")
	}
	msgID, partID, ok := splitPartBlobID(real + ".2")
	if !ok || msgID != real || partID != "2" {
		t.Fatalf("FAIL: valid composite not parsed: %q %q %v", msgID, partID, ok)
	}
}

// Control: the whole-message download via the real 64-hex blob id still
// serves the raw RFC 5322 bytes (unchanged path).
func TestWholeMessageDownloadUnchanged(t *testing.T) {
	server, id := r59DurFixture(t)
	email := r59DurFullForm(t, server, id)
	if email.BlobID == "" {
		t.Fatalf("CONTROL FAILED: created object carries no whole-message blobId")
	}
	status, _, body := r59DurDownload(t, server, email.BlobID)
	if status != http.StatusOK {
		t.Fatalf("CONTROL FAILED: whole-message download status = %d (body: %s)", status, body)
	}
	if !strings.Contains(body, "Plain body for the round-59 durable proof.") {
		t.Fatalf("CONTROL FAILED: whole-message download body = %q", body)
	}
}
