package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF6930RawHasAttachment(t *testing.T) {
	mixed := "From: a@b.c\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\n"
	plain := "From: a@b.c\r\nContent-Type: text/plain\r\n\r\nhi"
	alt := "From: a@b.c\r\nContent-Type: multipart/alternative; boundary=y\r\n\r\n"
	if !rawHasAttachment([]byte(mixed)) {
		t.Fatal("multipart/mixed must report attachment")
	}
	if rawHasAttachment([]byte(plain)) || rawHasAttachment([]byte(alt)) {
		t.Fatal("plain/alternative must not")
	}
	if rawHasAttachment(nil) {
		t.Fatal("empty")
	}
}

func TestF6931ThreadingHeaderInjection(t *testing.T) {
	if threadingHeaderValue("<a@b>\r\nBcc: x@y") != "" || threadingHeaderValue("a@b") != "" {
		t.Fatal("must reject injection / unbracketed")
	}
	got := threadingHeaderValues([]string{"<1@x>", "bad id"}, "<2@x>")
	if strings.Join(got, " ") != "<1@x> <2@x>" {
		t.Fatalf("got %v", got)
	}
	if rawMessageIDHeader([]byte("Message-ID: <q@z>\r\n\r\n")) != "<q@z>" {
		t.Fatal("message-id")
	}
}

func TestF6932SendWritesThreadingHeaders(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "x", nil)
	body := `{"to":["a@example.org"],"subject":"Re: x","body":"hi","inReplyTo":"<o@x>","references":["<r@x>","<evil>\r\nBcc: z@z"]}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
	rec := httptest.NewRecorder()
	h.handleMailSend(rec, r)
	if rec.Code != 200 {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, _ := h.msgStore.ReadMessage("reader@example.com", resp["id"])
	s := string(raw)
	if !strings.Contains(s, "In-Reply-To: <o@x>\r\n") || !strings.Contains(s, "References: <r@x> <o@x>\r\n") || strings.Contains(s, "Bcc:") {
		t.Fatalf("headers wrong: %q", s)
	}
}
