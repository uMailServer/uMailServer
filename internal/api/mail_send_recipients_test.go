package api

// Regression tests for F5136 (the 100-recipient cap counted only To, so Bcc
// and Cc carried any number of envelope recipients) and F5137 (recipients
// were queued verbatim as SMTP envelope addresses: empty strings, display
// names and trailing "> NOTIFY=..." ESMTP parameters were accepted).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/queue"
)

func f5136Session(t *testing.T) (*queue.Manager, func(string) int) {
	t.Helper()
	srv, qm := webmailSendServer(t)
	body, _ := json.Marshal(map[string]string{"email": "sender@sendtest.invalid", "password": strings.Repeat("fixture", 3)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	var resp struct {
		Token string `json:"token"`
	}
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &resp) != nil || resp.Token == "" {
		t.Fatalf("login = %d", rr.Code)
	}
	return qm, func(b string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", strings.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		return rr.Code
	}
}

func f5136Queued(t *testing.T, qm *queue.Manager) int {
	t.Helper()
	entries, err := qm.GetPendingEntries()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	return len(entries)
}

func f5136List(prefix string, n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = fmt.Sprintf("%q", fmt.Sprintf("%s%d@remote.example", prefix, i))
	}
	return "[" + strings.Join(s, ",") + "]"
}

func TestMailSendRecipientCap_CountsAllEnvelopeRecipients(t *testing.T) {
	qm, send := f5136Session(t)
	// 1 To + 500 Bcc, and 40 To + 40 Cc + 21 Bcc (101): both over the cap.
	for _, b := range []string{
		`{"to":["a@remote.example"],"bcc":` + f5136List("b", 500) + `,"subject":"s","body":"b"}`,
		`{"to":` + f5136List("t", 40) + `,"cc":` + f5136List("c", 40) + `,"bcc":` + f5136List("b", 21) + `,"subject":"s","body":"b"}`,
	} {
		if c := send(b); c != http.StatusBadRequest {
			t.Fatalf("F5136: over-cap send = %d, want 400", c)
		}
	}
	if n := f5136Queued(t, qm); n != 0 {
		t.Fatalf("F5136: rejected sends queued %d entries", n)
	}
	// Boundary: exactly 100 envelope recipients are accepted.
	if c := send(`{"to":` + f5136List("t", 40) + `,"cc":` + f5136List("c", 40) + `,"bcc":` + f5136List("b", 20) + `,"subject":"s","body":"b"}`); c != http.StatusOK {
		t.Fatalf("100 recipients = %d, want 200", c)
	}
	if n := f5136Queued(t, qm); n != 100 {
		t.Fatalf("queued %d, want 100", n)
	}
}

func TestMailSendRecipients_MustBeBareAddresses(t *testing.T) {
	qm, send := f5136Session(t)
	for _, bad := range []string{
		"victim@remote.example> NOTIFY=NEVER ORCPT=rfc822;x@y",
		"",
		"no-at-sign",
		"Bob <bob@remote.example>",
		"<bob@remote.example>",
		" bob@remote.example",
		"a@remote.example, b@remote.example",
	} {
		for _, field := range []string{"to", "cc", "bcc"} {
			rcpt := map[string][]string{"to": {"ok@remote.example"}}
			rcpt[field] = append(rcpt[field], bad)
			body, _ := json.Marshal(map[string]any{"to": rcpt["to"], "cc": rcpt["cc"], "bcc": rcpt["bcc"], "subject": "s", "body": "b"})
			if c := send(string(body)); c != http.StatusBadRequest {
				t.Fatalf("F5137: %s=%q accepted with %d, want 400", field, bad, c)
			}
		}
	}
	if n := f5136Queued(t, qm); n != 0 {
		t.Fatalf("F5137: rejected sends queued %d entries", n)
	}
	if c := send(`{"to":["first.last+tag@sub.remote.example"],"cc":["x@remote.example"],"subject":"s","body":"b"}`); c != http.StatusOK {
		t.Fatalf("valid addresses = %d, want 200", c)
	}
}
