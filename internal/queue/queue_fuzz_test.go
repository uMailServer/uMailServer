package queue

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// fuzzLineSafe reports whether s could have arrived through a line-oriented
// protocol (SMTP/IMAP) i.e. holds no CR, LF or NUL.
func fuzzLineSafe(ss ...string) bool {
	for _, s := range ss {
		if strings.ContainsAny(s, "\r\n\x00") {
			return false
		}
	}
	return true
}

func FuzzGenerateDSN(f *testing.F) {
	f.Add("a@example.com", "b@example.org", "b@example.org", "mx.example.org", "550 5.1.1 no such user", "Subject: hi\r\n\r\nbody\r\n", uint8(0), uint8(0))
	f.Add("", "", "", "", "550 \"quoted \\\" text\"", "no separator at all", uint8(1), uint8(1))
	f.Add("<>", "x", "y", "unknown", "smtp; 421 x\ty", "A: b\n\nc", uint8(1), uint8(2))
	f.Fuzz(func(t *testing.T, from, to, rcpt, remote, diag, orig string, ret, kind uint8) {
		d := &DSN{
			ReportedDomain: "umailserver",
			ArrivalDate:    time.Unix(1700000000, 0),
			OriginalFrom:   from,
			OriginalTo:     to,
			Recipient:      DSNRecipient{Original: rcpt},
			RemoteMTA:      remote,
		}
		var out []byte
		var err error
		r := DSNRet(ret % 2)
		switch kind % 4 {
		case 0:
			out, err = GenerateDSN(d, []byte(orig), r)
		case 1:
			out, err = GenerateFailureDSN(d, []byte(orig), r, diag)
		case 2:
			out, err = GenerateSuccessDSN(d, []byte(orig), r)
		default:
			out, err = GenerateDelayDSN(d)
		}
		if err != nil {
			return
		}
		ParseDSNNotify(diag)
		ParseDSNRet(diag)
		diagnosticText(diag)
		extractHeaders(orig)
		if got := diagnosticText(diag); len(got) > maxDiagnosticLen || strings.ContainsAny(got, "\r\n") {
			t.Fatalf("diagnosticText not one bounded line: %q", got)
		}
		if !fuzzLineSafe(from, to, rcpt, remote, diag) {
			return
		}
		// Output size must stay bounded regardless of the original size.
		if len(out) > len(from)+len(to)+len(rcpt)+len(remote)+maxDiagnosticLen+maxDSNOriginalSize+8192 && r == DSNRetFull && len(orig) > maxDSNOriginalSize {
			t.Fatalf("DSN of %d-byte original is %d bytes", len(orig), len(out))
		}
		msg, err := mail.ReadMessage(bytes.NewReader(out))
		if err != nil {
			return // e.g. address text that breaks header syntax; not an injection
		}
		allowed := map[string]bool{"From": true, "To": true, "Subject": true, "Content-Type": true, "Date": true, "Message-Id": true, "Mime-Version": true, "Auto-Submitted": true}
		for k, v := range msg.Header {
			if !allowed[k] || len(v) != 1 {
				t.Fatalf("unexpected/duplicate top-level header %q (%d values)", k, len(v))
			}
		}
		mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
		if err != nil || mt != "multipart/report" {
			t.Fatalf("bad content-type %q: %v", msg.Header.Get("Content-Type"), err)
		}
		mr := multipart.NewReader(msg.Body, params["boundary"])
		parts := 0
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("multipart: %v", err)
			}
			parts++
			io.Copy(io.Discard, p)
		}
		if parts != 3 {
			t.Fatalf("DSN has %d parts, want 3", parts)
		}
	})
}

func FuzzGenerateMDN(f *testing.F) {
	f.Add("From: a@b.c\r\nDisposition-Notification-To: a@b.c\r\n\r\nx", "bob@example.com", "alice@example.com", "<id@x>", "<irt@x>", uint8(0), "umailserver")
	f.Add("", "", "", "", "", uint8(9), "")
	f.Fuzz(func(t *testing.T, orig, from, to, id, irt string, disp uint8, dom string) {
		out, err := GenerateMDN([]byte(orig), from, to, id, irt, MDNDisposition(disp%6), dom)
		if err != nil {
			return
		}
		ParseDispositionHeader([]byte(orig))
		ParseMDNAddress(orig)
		if !fuzzLineSafe(from, to, id, irt, dom) {
			return
		}
		msg, err := mail.ReadMessage(bytes.NewReader(out))
		if err != nil {
			return
		}
		allowed := map[string]bool{"From": true, "To": true, "Subject": true, "Content-Type": true, "Date": true, "Message-Id": true, "References": true, "Mime-Version": true}
		for k, v := range msg.Header {
			if !allowed[k] || len(v) != 1 {
				t.Fatalf("unexpected/duplicate MDN header %q", k)
			}
		}
	})
}

func FuzzVERP(f *testing.F) {
	f.Add("mail.example.com", "john@example.org")
	f.Add("", "@")
	f.Add("h", "a@b@c")
	f.Fuzz(func(t *testing.T, host, rcpt string) {
		v := EncodeVERP(host, rcpt)
		if !strings.HasPrefix(v, "bounce-") {
			t.Fatalf("bad VERP %q", v)
		}
		splitEmail(rcpt)
	})
}

// F6120: a message with no header/body separator (or huge headers) made the
// "headers only" DSN embed the entire original, defeating maxDSNOriginalSize.
func TestGenerateDSN_HeaderOnlyReturnIsBounded(t *testing.T) {
	big := strings.Repeat("X-A: "+strings.Repeat("a", 70)+"\r\n", 20000) // ~1.5MB, no blank line
	for _, ret := range []DSNRet{DSNRetHeaders, DSNRetFull} {
		out, err := GenerateDSN(&DSN{ReportedDomain: "umailserver"}, []byte(big), ret)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > maxDSNOriginalSize+8192 {
			t.Fatalf("ret=%d: DSN is %d bytes for a %d-byte original, want <= %d", ret, len(out), len(big), maxDSNOriginalSize+8192)
		}
	}
}
