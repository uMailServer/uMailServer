package api

// F7774: without attachments the webmail send and draft paths wrote the body
// as-is with no Content-Transfer-Encoding (7bit), so non-ASCII text and lines
// over 998 octets went out unencoded.

import (
	"context"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
)

func bodyEncPost(t *testing.T, h *MailHandler, handler func(http.ResponseWriter, *http.Request), path, text string) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(map[string]interface{}{"to": []string{"bob@example.com"}, "subject": "s", "body": text})
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(payload)))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// checkTextEncoding asserts raw is a 7bit-safe encoding of text and returns
// its Content-Transfer-Encoding.
func checkTextEncoding(t *testing.T, raw []byte, text string) string {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	cte := strings.ToLower(msg.Header.Get("Content-Transfer-Encoding"))
	for _, l := range strings.Split(string(raw), "\r\n") {
		if len(l) > 998 {
			t.Fatalf("line of %d octets", len(l))
		}
	}
	var r io.Reader = msg.Body
	if cte == "quoted-printable" {
		r = quotedprintable.NewReader(r)
	} else {
		for _, c := range []byte(text) {
			if c >= 0x80 {
				t.Fatalf("8-bit text sent with Content-Transfer-Encoding %q", cte)
			}
		}
	}
	body, _ := io.ReadAll(r)
	if got := strings.ReplaceAll(string(body), "\r\n", "\n"); got != text {
		t.Fatalf("decoded body %q, want %q", got, text)
	}
	return cte
}

func TestSendBodyEncoding(t *testing.T) {
	for _, c := range []struct {
		text    string
		wantCTE string
	}{
		{"hello there", ""},
		{strings.Repeat("a", 998), ""},
		{strings.Repeat("a", 999), "quoted-printable"},
		{"Grüße\naus Köln", "quoted-printable"},
		{"x = y\n" + strings.Repeat("z", 1500), "quoted-printable"},
	} {
		h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
		var sent []byte
		h.SetDeliverFunc(func(_ string, _ []string, data []byte) error { sent = data; return nil })
		if w := bodyEncPost(t, h, h.handleMailSend, "/api/v1/mail/send", c.text); w.Code != http.StatusOK {
			t.Fatalf("send: %d %s", w.Code, w.Body.String())
		}
		if cte := checkTextEncoding(t, sent, c.text); cte != c.wantCTE {
			t.Errorf("%.20q: Content-Transfer-Encoding %q, want %q", c.text, cte, c.wantCTE)
		}
	}
}

func TestDraftBodyEncoding(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	text := "Taslak: çok uzun " + strings.Repeat("ş", 700)
	w := bodyEncPost(t, h, h.handleMailDraft, "/api/v1/mail/draft", text)
	if w.Code != http.StatusOK {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	var resp struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	raw, err := h.msgStore.ReadMessage("reader@example.com", resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cte := checkTextEncoding(t, raw, text); cte != "quoted-printable" {
		t.Errorf("draft Content-Transfer-Encoding %q", cte)
	}
}
