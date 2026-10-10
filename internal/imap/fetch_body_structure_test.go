package imap

import (
	"strings"
	"testing"
)

// Regression tests for FETCH body sections inside nested parts and for
// BODY / BODYSTRUCTURE (RFC 3501 §6.4.5, §7.4.2) (F5494–F5496).

const rfcNestedMessage = "From: a@x\r\nSubject: outer\r\nContent-Type: multipart/mixed; boundary=\"B1\"\r\n\r\n" +
	"--B1\r\nContent-Type: text/plain\r\nContent-ID: <c1>\r\nX-Zeta: 1\r\nX-Alpha: 2\r\n\r\nhello\r\n" +
	"--B1\r\nContent-Type: message/rfc822\r\n\r\nFrom: inner@x\r\nSubject: inner\r\nContent-Type: text/plain\r\n\r\ninner body\r\n" +
	"--B1--\r\n"

// F5494: HEADER / TEXT / sub-part numbers address the message encapsulated
// by a message/rfc822 part.
func TestFetchBody_RFC822PartSections(t *testing.T) {
	for sec, want := range map[string]string{
		"2.HEADER":                  "From: inner@x\r\nSubject: inner\r\nContent-Type: text/plain\r\n\r\n",
		"2.TEXT":                    "inner body",
		"2.1":                       "inner body",
		"2.HEADER.FIELDS (SUBJECT)": "Subject: inner\r\n\r\n",
		"2.MIME":                    "Content-Type: message/rfc822\r\n\r\n",
		"1":                         "hello",
	} {
		if got := string(bodySection([]byte(rfcNestedMessage), sec)); got != want {
			t.Errorf("BODY[%s] = %q, want %q", sec, got, want)
		}
	}
}

// F5495: BODY[n.MIME] is the part header byte for byte, on every request.
func TestFetchBody_MIMEHeaderVerbatim(t *testing.T) {
	want := "Content-Type: text/plain\r\nContent-ID: <c1>\r\nX-Zeta: 1\r\nX-Alpha: 2\r\n\r\n"
	for i := 0; i < 20; i++ {
		if got := string(bodySection([]byte(rfcNestedMessage), "1.MIME")); got != want {
			t.Fatalf("BODY[1.MIME] = %q, want %q", got, want)
		}
	}
}

// F5496: BODY and BODYSTRUCTURE describe the real MIME tree.
func TestFetch_BodyStructureMultipart(t *testing.T) {
	run := rfcSearchSession(t)
	bs := untaggedLine(run("b1 FETCH 3 (BODYSTRUCTURE)"), "* 3 FETCH")
	for _, want := range []string{
		`* 3 FETCH (BODYSTRUCTURE (("TEXT" "PLAIN" NIL "<c1>" NIL "7BIT" 5 0 NIL NIL NIL NIL)("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 69 (`,
		`("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 0 NIL NIL NIL NIL) 4 NIL NIL NIL NIL) "MIXED" ("BOUNDARY" "B1") NIL NIL NIL))`,
	} {
		if !strings.Contains(bs, want) {
			t.Errorf("BODYSTRUCTURE = %q, want it to contain %q", bs, want)
		}
	}
	b := untaggedLine(run("b2 FETCH 3 (BODY)"), "* 3 FETCH")
	if !strings.HasPrefix(b, `* 3 FETCH (BODY (("TEXT" "PLAIN"`) || !strings.HasSuffix(b, `"MIXED"))`) {
		t.Errorf("BODY = %q, want non-extensible multipart structure", b)
	}
}
