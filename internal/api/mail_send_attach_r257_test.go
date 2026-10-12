package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r257Send(t *testing.T, payload map[string]interface{}) (*httptest.ResponseRecorder, *MailHandler) {
	t.Helper()
	h, _, _ := mailConversionFixture(t, "x", nil)
	b, _ := json.Marshal(payload)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", strings.NewReader(string(b)))
	r = r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
	rec := httptest.NewRecorder()
	h.handleMailSend(rec, r)
	return rec, h
}

func r257Att(name, ct string, data []byte) map[string]interface{} {
	return map[string]interface{}{"filename": name, "contentType": ct, "data": base64.StdEncoding.EncodeToString(data)}
}

// F7340: attachments are accepted and built into multipart/mixed.
func TestR257SendAttachmentsMultipart(t *testing.T) {
	rec, h := r257Send(t, map[string]interface{}{
		"to": []string{"a@example.org"}, "subject": "s", "body": "hello",
		"attachments": []interface{}{r257Att("rep\r\nBcc: x@y.z\".txt", "text/plain", []byte("file-bytes"))},
	})
	if rec.Code != 200 {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, _ := h.msgStore.ReadMessage("reader@example.com", resp["id"])
	s := string(raw)
	if strings.Contains(s, "\r\nBcc:") {
		t.Fatalf("header injection via filename: %q", s)
	}
	hdr, body, _ := strings.Cut(s, "\r\n\r\n")
	_ = hdr
	var ct string
	for _, l := range strings.Split(s, "\r\n") {
		if strings.HasPrefix(l, "Content-Type:") {
			ct = strings.TrimPrefix(l, "Content-Type: ")
			break
		}
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content-type %q", ct)
	}
	mr := multipart.NewReader(strings.NewReader(body), params["boundary"])
	p1, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(p1)
	if strings.TrimSpace(string(b1)) != "hello" {
		t.Fatalf("body part %q", b1)
	}
	p2, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if p2.FileName() == "" || strings.ContainsAny(p2.FileName(), "\r\n") {
		t.Fatalf("filename %q", p2.FileName())
	}
	b2, _ := io.ReadAll(p2)
	dec, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(b2), "\r\n", ""))
	if string(dec) != "file-bytes" {
		t.Fatalf("payload %q", dec)
	}
}

func TestR257SendAttachmentLimits(t *testing.T) {
	big := make([]byte, 11<<20)
	many := []interface{}{}
	for i := 0; i < 21; i++ {
		many = append(many, r257Att("a.txt", "text/plain", []byte("x")))
	}
	cases := map[string][]interface{}{
		"too many":    many,
		"file big":    {r257Att("a.bin", "application/octet-stream", big)},
		"total big":   {r257Att("a", "x/y", big[:9<<20]), r257Att("b", "x/y", big[:9<<20])},
		"bad base64":  {map[string]interface{}{"filename": "a", "contentType": "x/y", "data": "***"}},
		"no filename": {r257Att("", "x/y", []byte("x"))},
		"bad ctype":   {r257Att("a", "text/plain\r\nBcc: q@q.q", []byte("x"))},
	}
	for name, atts := range cases {
		rec, _ := r257Send(t, map[string]interface{}{"to": []string{"a@example.org"}, "subject": "s", "body": "b", "attachments": atts})
		if rec.Code != 400 && rec.Code != 413 {
			t.Errorf("%s: got %d", name, rec.Code)
		}
	}
}
