package server

// F7780: vacation replies wrote the subject as raw UTF-8 header bytes and
// declared every body "8bit".

import (
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

func queuedVacationMessage(t *testing.T, srv *Server, rcpt string) *mail.Message {
	t.Helper()
	entries, err := srv.queue.GetPendingEntries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.To) == 1 && e.To[0] == rcpt {
			raw, err := os.ReadFile(e.MessagePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range strings.Split(string(raw), "\r\n") {
				if len(l) > 998 {
					t.Fatalf("line of %d octets in the reply", len(l))
				}
			}
			head := string(raw[:strings.Index(string(raw), "\r\n\r\n")])
			for i := 0; i < len(head); i++ {
				if head[i] >= 0x80 {
					t.Fatalf("raw 8-bit byte in the reply header:\n%s", head)
				}
			}
			msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			return msg
		}
	}
	t.Fatalf("no queued reply to %s", rcpt)
	return nil
}

func decodedVacationBody(t *testing.T, msg *mail.Message) string {
	t.Helper()
	var r io.Reader = msg.Body
	if cte := strings.ToLower(msg.Header.Get("Content-Transfer-Encoding")); cte == "quoted-printable" {
		r = quotedprintable.NewReader(r)
	} else if cte != "7bit" {
		t.Fatalf("Content-Transfer-Encoding %q", cte)
	}
	b, _ := io.ReadAll(r)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestVacationRepliesEncodeNonASCII(t *testing.T) {
	subject := "Tatildeyim, döndüğümde yanıtlarım"
	body := "Merhaba,\n" + strings.Repeat("çok uzun satır ", 100) + "\nİyi günler"

	srv := junkRTServer(t, false)
	srv.handleSieveVacation("bob@remote.example", "alice@test.com", sieve.VacationAction{Subject: subject, Body: body, Seconds: 60})
	msg := queuedVacationMessage(t, srv, "bob@remote.example")
	if dec, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject")); err != nil || dec != subject {
		t.Errorf("sieve subject %q, err %v", dec, err)
	}
	if got := decodedVacationBody(t, msg); got != body {
		t.Errorf("sieve body differs (%d vs %d bytes)", len(got), len(body))
	}

	srv = junkRTServer(t, false)
	srv.sendVacationReply("alice@test.com", "dave@remote.example",
		`{"enabled":true,"subject":"`+subject+`","message":"`+strings.ReplaceAll(body, "\n", `\n`)+`"}`)
	msg = queuedVacationMessage(t, srv, "dave@remote.example")
	if dec, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject")); err != nil || dec != subject {
		t.Errorf("account subject %q, err %v", dec, err)
	}
	if got := decodedVacationBody(t, msg); got != body {
		t.Errorf("account body differs (%d vs %d bytes)", len(got), len(body))
	}
}

func TestVacationPlainReplyStaysPlain(t *testing.T) {
	srv := junkRTServer(t, false)
	srv.sendVacationReply("alice@test.com", "erin@remote.example", `{"enabled":true,"subject":"Out of office","message":"Back Monday."}`)
	msg := queuedVacationMessage(t, srv, "erin@remote.example")
	if msg.Header.Get("Subject") != "Out of office" || msg.Header.Get("Content-Transfer-Encoding") != "7bit" {
		t.Errorf("subject %q, CTE %q", msg.Header.Get("Subject"), msg.Header.Get("Content-Transfer-Encoding"))
	}
	if got := decodedVacationBody(t, msg); got != "Back Monday." {
		t.Errorf("body %q", got)
	}
}
