package api

// F7775: the webmail message view returned the body after the header block
// verbatim, so quoted-printable/base64 text stayed encoded and multipart
// messages showed their MIME boundaries.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func viewBody(t *testing.T, raw string) string {
	t.Helper()
	h, _, id := mailConversionFixture(t, raw, nil)
	m, err := h.getEmailFromStorage("reader@example.com", "INBOX", id)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := h.getEmailsFromStorage("reader@example.com", "INBOX"); err != nil || len(list) != 1 || list[0].Body != m.Body {
		t.Fatalf("list body differs from the message view: %v %v", list, err)
	}
	return m.Body
}

func TestMailViewDecodesBody(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"no MIME headers", "Subject: s\r\n\r\nplain =41 body", "plain =41 body"},
		{"quoted-printable", "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nGr=C3=BC=C3=9Fe =\r\nbitti", "Grüße bitti"},
		{"base64 with line breaks", "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nR3LD\r\nvMOfZQ==\r\n", "Grüße"},
		{"latin-5", "Content-Type: text/plain; charset=iso-8859-9\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n=DE=FDk", "Şık"},
		{"html only", "Content-Type: text/html; charset=utf-8\r\n\r\n<p>Hi</p>", "<p>Hi</p>"},
		{"mixed with alternative and attachment",
			"Content-Type: multipart/mixed; boundary=out\r\n\r\n" +
				"--out\r\nContent-Type: multipart/alternative; boundary=in\r\n\r\n" +
				"--in\r\nContent-Type: text/html\r\n\r\n<b>html</b>\r\n" +
				"--in\r\nContent-Type: text/plain\r\n\r\nplain wins\r\n--in--\r\n" +
				"--out\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=a.txt\r\n\r\nnot the body\r\n--out--\r\n",
			"plain wins"},
		{"attachment only", "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: application/pdf\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n--b--\r\n", ""},
	}
	for _, c := range cases {
		if got := viewBody(t, c.raw); got != c.want {
			t.Errorf("%s: body %q, want %q", c.name, got, c.want)
		}
	}
}

// A non-ASCII draft is stored quoted-printable (F7774) and reads back as
// the text the user typed.
func TestDraftRoundTripsNonASCIIBody(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	text := "Merhaba, çalışma notları: " + strings.Repeat("ğüşiöç ", 200)
	payload, _ := json.Marshal(map[string]interface{}{"to": []string{"bob@example.com"}, "subject": "s", "body": text})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mail/draft", strings.NewReader(string(payload)))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
	w := httptest.NewRecorder()
	h.handleMailDraft(w, r)
	var resp struct{ ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || w.Code != http.StatusOK {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	m, err := h.getEmailFromStorage("reader@example.com", "Drafts", resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != strings.TrimSpace(text) {
		t.Fatalf("draft reads back as %.60q", m.Body)
	}
}
