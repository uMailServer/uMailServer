package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const r228Multipart = "From: a@example.com\r\nTo: reader@example.com\r\nCc: Bob <bob@example.com>, carol@example.com\r\nBcc: secret@example.com\r\nSubject: hi\r\n" +
	"Content-Type: multipart/mixed; boundary=XX\r\n\r\n" +
	"--XX\r\nContent-Type: text/plain\r\n\r\nbody\r\n" +
	"--XX\r\nContent-Type: text/html; name=\"x.html\"\r\nContent-Disposition: attachment; filename=\"../../evil\\\"name.html\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
	"PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==\r\n--XX--\r\n"

func r228Get(h *MailHandler, url string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", "reader@example.com"))
	rec := httptest.NewRecorder()
	if strings.HasPrefix(url, "/attachment") {
		h.handleMailAttachment(rec, req)
	} else {
		h.handleMailMessage(rec, req)
	}
	return rec
}

func TestR228MessageExposesCcNotBccAndAttachments(t *testing.T) {
	h, _, id := mailConversionFixture(t, r228Multipart, nil)
	rec := r228Get(h, "/message?id="+id)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "secret@example.com") {
		t.Fatal("Bcc leaked")
	}
	var out struct {
		Account     string           `json:"account"`
		CC          []string         `json:"cc"`
		Attachments []AttachmentInfo `json:"attachments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Account != "reader@example.com" {
		t.Fatalf("account=%q", out.Account)
	}
	if len(out.CC) != 2 || out.CC[0] != "bob@example.com" || out.CC[1] != "carol@example.com" {
		t.Fatalf("cc=%v", out.CC)
	}
	if len(out.Attachments) != 1 || out.Attachments[0].Size != 25 || out.Attachments[0].Index != 0 {
		t.Fatalf("attachments=%+v", out.Attachments)
	}
}

func TestR228AttachmentDownloadSafe(t *testing.T) {
	h, _, id := mailConversionFixture(t, r228Multipart, nil)
	rec := r228Get(h, "/attachment?id="+id+"&index=0")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	want, _ := base64.StdEncoding.DecodeString("PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==")
	if rec.Body.String() != string(want) {
		t.Fatalf("body %q", rec.Body.String())
	}
	hd := rec.Header()
	if hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("headers %v", hd)
	}
	cd := hd.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || strings.ContainsAny(cd, "\r\n") || strings.Contains(cd, "..") || strings.Contains(cd, "/") {
		t.Fatalf("unsafe disposition %q", cd)
	}
	for _, u := range []string{"/attachment?id=" + id + "&index=1", "/attachment?id=" + id + "&index=-1", "/attachment?id=" + id, "/attachment?id=nope&index=0"} {
		if c := r228Get(h, u).Code; c < 400 {
			t.Fatalf("%s: status %d", u, c)
		}
	}
}

func TestR228AttachmentSizeCap(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(make([]byte, maxAttachmentBytes+10))
	raw := "From: a@example.com\r\nSubject: big\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n--XX\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=a.bin\r\nContent-Transfer-Encoding: base64\r\n\r\n" + big + "\r\n--XX--\r\n"
	h, _, id := mailConversionFixture(t, raw, nil)
	if c := r228Get(h, "/attachment?id="+id+"&index=0").Code; c != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", c)
	}
}
