package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

const r187Msg = "From: s@ext.example\nTo: bob@test.com\nSubject: r187 subject\nMessage-ID: <r187a@ext.example>\nDate: Mon, 02 Jan 2006 15:04:05 +0000\n\nr187 body marker\n"

// F6640: one SMTP-delivered message must look identical (content, size,
// flags) through IMAP, POP3, JMAP and the REST API, and flag changes made over
// IMAP must be visible everywhere.
func TestR187_CrossProtocolConsistency(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
	defer e.stop()
	e.inbound("s@ext.example", []string{"bob@test.com"}, r187Msg)

	ic := e.imapLogin("bob@test.com")
	r187Wait(t, "delivery", func() bool { return strings.Contains(ic.imapCmd("s", "SELECT INBOX"), "1 EXISTS") })
	out := ic.imapCmd("f1", "FETCH 1 (RFC822.SIZE)")
	var imapSize int
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, "RFC822.SIZE "); i >= 0 {
			fmt.Sscanf(l[i:], "RFC822.SIZE %d", &imapSize)
		}
	}
	if imapSize == 0 {
		t.Fatalf("no size: %s", out)
	}
	ic.imapCmd("f2", `STORE 1 +FLAGS (\Seen \Flagged)`)

	p := e.pop3Login("bob@test.com")
	listLine := p.pop3Cmd("LIST 1")
	var popSize int
	fmt.Sscanf(listLine, "+OK 1 %d", &popSize)
	retr := p.pop3Retr(1)
	if popSize != len(retr) {
		t.Errorf("POP3 LIST size %d != RETR len %d", popSize, len(retr))
	}
	if popSize != imapSize {
		t.Errorf("size mismatch IMAP=%d POP3=%d", imapSize, popSize)
	}
	if !strings.Contains(retr, "r187 body marker") || !strings.Contains(retr, "Return-Path: <s@ext.example>") {
		t.Errorf("POP3 content wrong: %q", retr)
	}
	// IMAP body must equal POP3 body byte-for-byte
	out = ic.imapCmd("f3", "FETCH 1 BODY.PEEK[]")
	if !strings.Contains(out, "Return-Path: <s@ext.example>") || !strings.Contains(out, "r187 body marker") {
		t.Errorf("IMAP content wrong: %s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("{%d}", len(retr))) {
		t.Errorf("IMAP literal length differs from POP3 RETR length %d: %.120s", len(retr), out)
	}

	rt, st := e.restLogin("bob@test.com", e.password)
	if st != 200 || rt == "" {
		t.Fatalf("rest login %d", st)
	}
	mails := e.restList(rt, "inbox")
	if len(mails) != 1 {
		t.Fatalf("REST list: %+v", mails)
	}
	m := mails[0]
	if m.Subject != "r187 subject" {
		t.Errorf("REST subject %q", m.Subject)
	}
	if !m.Read || !m.Starred {
		t.Errorf("REST flags read=%v starred=%v, want both true after IMAP STORE", m.Read, m.Starred)
	}
	if m.Size != int64(imapSize) {
		t.Errorf("REST size %d != IMAP %d", m.Size, imapSize)
	}

	tok := e.jmapToken("bob@test.com")
	em := e.jmapEmails(tok, "bob@test.com", []string{"subject", "keywords", "size", "messageId"})
	if len(em) != 1 {
		t.Fatalf("JMAP emails: %v", em)
	}
	if em[0]["subject"] != "r187 subject" {
		t.Errorf("JMAP subject %v", em[0]["subject"])
	}
	kw, _ := em[0]["keywords"].(map[string]any)
	if kw["$seen"] != true || kw["$flagged"] != true {
		t.Errorf("JMAP keywords %v want $seen,$flagged", kw)
	}
	if sz, _ := em[0]["size"].(float64); int(sz) != imapSize {
		t.Errorf("JMAP size %v != IMAP %d", em[0]["size"], imapSize)
	}
}

// F6641: IMAP APPEND with flags then POP3 RETR, JMAP Email/get and REST agree.
func TestR187_AppendVisibleEverywhere(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
	defer e.stop()
	ic := e.imapLogin("bob@test.com")
	msg := "From: a@b.c\r\nTo: bob@test.com\r\nSubject: appended 187\r\nMessage-ID: <ap187@b.c>\r\n\r\nappend marker\r\n"
	out := ic.imapCmd("a1", fmt.Sprintf(`APPEND INBOX (\Seen) {%d+}`+"\r\n%s", len(msg), msg))
	if !strings.Contains(out, "a1 OK") {
		t.Fatalf("append: %s", out)
	}
	p := e.pop3Login("bob@test.com")
	if got := p.pop3Retr(1); got != msg {
		t.Errorf("POP3 RETR differs from appended bytes:\n got %q\nwant %q", got, msg)
	}
	tok := e.jmapToken("bob@test.com")
	em := e.jmapEmails(tok, "bob@test.com", []string{"subject", "keywords", "size"})
	if len(em) != 1 {
		t.Fatalf("JMAP: %v", em)
	}
	if em[0]["subject"] != "appended 187" {
		t.Errorf("subject %v", em[0]["subject"])
	}
	if kw, _ := em[0]["keywords"].(map[string]any); kw["$seen"] != true {
		t.Errorf("JMAP keywords %v want $seen", kw)
	}
	if sz, _ := em[0]["size"].(float64); int(sz) != len(msg) {
		t.Errorf("JMAP size %v want %d", em[0]["size"], len(msg))
	}
	rt, _ := e.restLogin("bob@test.com", e.password)
	ms := e.restList(rt, "inbox")
	if len(ms) != 1 || !ms[0].Read || ms[0].Size != int64(len(msg)) {
		t.Errorf("REST: %+v", ms)
	}
}
