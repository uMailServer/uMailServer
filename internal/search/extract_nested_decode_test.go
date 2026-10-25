package search

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestExtractMIMEText_NestedEncodedPart pins the nested-multipart decoding
// contract: walkEntity must decode each leaf's transfer encoding exactly once.
// multipart.Reader.NextPart already transparently decodes quoted-printable and
// base64 sub-parts, so the recursion must NOT pass the raw
// Content-Transfer-Encoding header down (a double decode corrupts the
// searchable text the index stores).
func TestExtractMIMEText_NestedEncodedPart(t *testing.T) {
	leafB64 := base64.StdEncoding.EncodeToString([]byte("hello searchable"))
	raw := "From: sender@example.com\r\n" +
		"Subject: nested\r\n" +
		"Content-Type: multipart/mixed; boundary=outer\r\n" +
		"\r\n" +
		"--outer\r\n" +
		"Content-Type: multipart/alternative; boundary=inner\r\n" +
		"\r\n" +
		"--inner\r\n" +
		"Content-Type: text/plain\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		leafB64 + "\r\n" +
		"--inner--\r\n" +
		"--outer--\r\n"

	got := extractMIMEText([]byte(raw))
	if !strings.Contains(got, "hello searchable") {
		t.Fatalf("FAIL: nested base64 leaf not decoded exactly once: %q", got)
	}
	if strings.Contains(got, "Content-Type") || strings.Contains(got, "boundary") {
		t.Fatalf("FAIL: MIME structure leaked into extracted text: %q", got)
	}
}
