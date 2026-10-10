package api

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
)

// F5990: a non-ASCII subject must be stored/queued as an RFC 2047 encoded-word,
// never as raw 8-bit header bytes, and must round-trip.
func TestMailSendEncodesNonASCIISubject(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "x", nil)
	body := `{"to":["a@example.org"],"subject":"Merhaba dünya ✓","body":"hi"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
	rec := httptest.NewRecorder()
	h.handleMailSend(rec, r)
	if rec.Code != 200 {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, err := h.msgStore.ReadMessage("reader@example.com", resp["id"])
	if err != nil {
		t.Fatal(err)
	}
	head := string(raw[:strings.Index(string(raw), "\r\n\r\n")])
	for i := 0; i < len(head); i++ {
		if head[i] >= 0x80 {
			t.Fatalf("regression F5990: raw 8-bit byte in headers: %q", head)
		}
	}
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || dec != "Merhaba dünya ✓" {
		t.Fatalf("subject round-trip: %q %v", dec, err)
	}
}
