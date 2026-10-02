package jmap

// Regression test for the SearchSnippet subject-label defect:
// generateSearchSnippet matched the subject header case-insensitively
// (strings.HasPrefix(strings.ToLower(line), "subject:")) but stripped the
// header name case-SENSITIVELY (strings.TrimPrefix(line, "subject:") — the
// lowercase literal against the original line), so for any email whose
// Subject header is not exactly lowercase — i.e. the overwhelmingly common
// "Subject: ..." — the returned SearchSnippet.Subject included the raw
// header label ("Subject: Hello World"). The snippet's Subject is the
// email's subject VALUE (RFC 8621 §5.3 semantics), and parseEmailMetadata in
// the same file extracts it case-insensitively via a lowercased header map,
// so the snippet disagreed with Email/get's subject for the same message.

import (
	"strings"
	"testing"
)

func TestSearchSnippetSubjectStripsHeaderNameCaseInsensitively(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}

	// Typical capitalized Subject header (the defect trigger).
	blobCap, err := msgStore.StoreMessage(user, []byte("Subject: Hello World\r\n\r\nbody of the world message\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Lowercase subject header (the only case the old code stripped —
	// control that the code path itself works).
	blobLower, err := msgStore.StoreMessage(user, []byte("subject: hello lower\r\n\r\nbody of the lower message\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	// All-caps subject header (same class as the defect trigger).
	blobCaps, err := msgStore.StoreMessage(user, []byte("SUBJECT: caps header\r\n\r\nbody of the caps message\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	resp := server.handleSearchSnippetGet(user, MethodCall{
		Name: "SearchSnippet/get",
		Args: map[string]interface{}{
			"accountId": user,
			"emailIds":  []interface{}{blobCap, blobLower, blobCaps},
			"search":    map[string]interface{}{"text": "body"},
		},
		ID: "call-1",
	})

	// Args carry Go types here (direct handler call, no JSON round-trip).
	list, _ := resp.Args["list"].([]SearchSnippet)
	if len(list) != 3 {
		t.Fatalf("FAIL: list = %d snippets, want 3", len(list))
	}

	subjects := make(map[string]string)
	for _, snippet := range list {
		subjects[snippet.EmailID] = snippet.Subject
	}
	if subjects[blobCap] != "Hello World" {
		t.Fatalf("FAIL: capitalized subject = %q, want \"Hello World\" (header label leaked)", subjects[blobCap])
	}
	if subjects[blobLower] != "hello lower" {
		t.Fatalf("FAIL: lowercase subject = %q, want \"hello lower\"", subjects[blobLower])
	}
	if subjects[blobCaps] != "caps header" {
		t.Fatalf("FAIL: all-caps subject = %q, want \"caps header\"", subjects[blobCaps])
	}
}

func TestSearchSnippetPreviewStripsHeaders(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// LF line endings: the function's current header/body delimiter is an
	// empty line, which CRLF emails ("\r\n\r\n") do not satisfy — that gap
	// is recorded as a separate lead (round 39 ledger).
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: preview check\nFrom: sender@example.com\n\nthe actual body text\n"))
	if err != nil {
		t.Fatal(err)
	}

	resp := server.handleSearchSnippetGet(user, MethodCall{
		Name: "SearchSnippet/get",
		Args: map[string]interface{}{
			"accountId": user,
			"emailIds":  []interface{}{blobID},
			"search":    map[string]interface{}{"text": "body"},
		},
		ID: "call-1",
	})

	list, _ := resp.Args["list"].([]SearchSnippet)
	if len(list) != 1 {
		t.Fatalf("FAIL: list = %d snippets, want 1", len(list))
	}
	// The preview must contain the BODY, not the header block.
	if list[0].Preview == "" || strings.Contains(list[0].Preview, "Subject:") || strings.Contains(list[0].Preview, "From:") {
		t.Fatalf("FAIL: preview = %q, want body content without header lines", list[0].Preview)
	}
}

func TestSearchSnippetPreviewHandlesCRLFEmails(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// RFC 5322 CRLF email: the round-42 defect — the header/body delimiter
	// (line == "") never matched a CRLF blank line ("\r"), so the body was
	// never collected and Preview was empty for every real-world email.
	// The search text does not occur in the body, so this exercises the
	// leading-text fallback: the full body must still be collected.
	blobCRLF, err := msgStore.StoreMessage(user, []byte("Subject: crlf check\r\nFrom: sender@example.com\r\n\r\nactual body text\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	resp := server.handleSearchSnippetGet(user, MethodCall{
		Name: "SearchSnippet/get",
		Args: map[string]interface{}{
			"accountId": user,
			"emailIds":  []interface{}{blobCRLF},
			"search":    map[string]interface{}{"text": "zzz-no-such-token"},
		},
		ID: "call-1",
	})

	list, _ := resp.Args["list"].([]SearchSnippet)
	if len(list) != 1 {
		t.Fatalf("FAIL: list = %d snippets, want 1", len(list))
	}
	if !strings.Contains(list[0].Preview, "actual body text") {
		t.Fatalf("FAIL: CRLF preview = %q, want body text (body never collected)", list[0].Preview)
	}
	if strings.ContainsRune(list[0].Preview, '\r') {
		t.Fatalf("FAIL: CRLF preview = %q contains raw CR characters", list[0].Preview)
	}
}

// TestSearchSnippetPreviewAnchorsOnSearchMatch pins the RFC 8621 §5.3 SHOULD:
// the preview contains the part of the body where the search match occurred,
// not just the leading text. Without a match (or without search text) the
// preview falls back to the leading body text.
func TestSearchSnippetPreviewAnchorsOnSearchMatch(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}

	// The needle sits deep in the body, far beyond any leading-text window.
	padding := strings.Repeat("filler sentence. ", 30) // ~510 chars
	body := padding + "the quantum flux capacitor hums tonight\n"
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: deep needle\n\n"+body))
	if err != nil {
		t.Fatal(err)
	}

	snippetCall := func(searchText string) MethodCall {
		return MethodCall{
			Name: "SearchSnippet/get",
			Args: map[string]interface{}{
				"accountId": user,
				"emailIds":  []interface{}{blobID},
				"search":    map[string]interface{}{"text": searchText},
			},
			ID: "call-1",
		}
	}

	resp := server.handleSearchSnippetGet(user, snippetCall("quantum flux"))
	list, _ := resp.Args["list"].([]SearchSnippet)
	if len(list) != 1 {
		t.Fatalf("FAIL: list = %d snippets, want 1", len(list))
	}
	if !strings.Contains(list[0].Preview, "quantum flux") {
		t.Fatalf("FAIL: preview = %q, want the match region (RFC 8621 §5.3 SHOULD)", list[0].Preview)
	}

	// Fallback: a non-occurring search keeps the leading body text.
	resp2 := server.handleSearchSnippetGet(user, snippetCall("zzz-no-such-token"))
	list2, _ := resp2.Args["list"].([]SearchSnippet)
	if len(list2) != 1 {
		t.Fatalf("FAIL: fallback list = %d snippets, want 1", len(list2))
	}
	if strings.Contains(list2[0].Preview, "quantum flux") {
		t.Fatalf("FAIL: a non-matching search must not anchor the preview (got %q)", list2[0].Preview)
	}
	if !strings.HasPrefix(list2[0].Preview, "filler") {
		t.Fatalf("FAIL: fallback preview = %q, want the leading body text", list2[0].Preview)
	}
}
