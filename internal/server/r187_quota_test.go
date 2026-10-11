package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func (e *r187Env) jmapDestroy(tok, acct, id string) {
	e.t.Helper()
	r := e.jmapCall(tok, []any{"Email/set", map[string]any{"accountId": acct, "destroy": []string{id}}, "d"})
	res, _ := r[0].([]any)[1].(map[string]any)
	if d, _ := res["destroyed"].([]any); len(d) != 1 {
		e.t.Fatalf("JMAP destroy %s: %v", id, r)
	}
}

// r187TwinSetup appends two byte-identical messages to bob's INBOX over IMAP.
func r187TwinSetup(t *testing.T) (*r187Env, *e2eConn, string) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	t.Cleanup(e.stop)
	msg := "From: a@b.c\r\nTo: bob@test.com\r\nSubject: twin 187\r\nMessage-ID: <tw187@b.c>\r\n\r\ntwin marker\r\n"
	ic := e.imapLogin("bob@test.com")
	for i := 0; i < 2; i++ {
		if out := ic.imapCmd(fmt.Sprintf("a%d", i), fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(msg), msg)); !strings.Contains(out, " OK") {
			t.Fatalf("append: %s", out)
		}
	}
	// Identical copies share one content-addressed blob; quota tracks the
	// physical blob (TestE2E_DuplicateDeliveryQuotaRelease): L until the last
	// reference is gone.
	if q := e.quotaUsed("bob"); q != int64(len(msg)) {
		t.Fatalf("quota after 2 identical appends %d want %d", q, len(msg))
	}
	return e, ic, msg
}

// survivor asserts exactly one copy remains, readable over IMAP and POP3, with
// quota still charged for the shared blob.
func (e *r187Env) r187Survivor(t *testing.T, via, msg string) {
	t.Helper()
	ic := e.imapLogin("bob@test.com")
	if out := ic.imapCmd("s", "SELECT INBOX"); !strings.Contains(out, "* 1 EXISTS") {
		t.Errorf("after %s delete of one copy IMAP: %s (want 1 EXISTS)", via, out)
	} else if o := ic.imapCmd("f", "FETCH 1 (BODY[TEXT])"); !strings.Contains(o, "twin marker") {
		t.Errorf("after %s delete surviving copy unreadable over IMAP: %s", via, o)
	}
	ic.imapCmd("o", "LOGOUT")
	p := e.pop3Login("bob@test.com")
	if st := p.pop3Cmd("STAT"); !strings.HasPrefix(st, "+OK 1 ") {
		t.Errorf("after %s delete POP3 STAT %q want 1 message", via, st)
	} else if got := p.pop3Retr(1); got != msg {
		t.Errorf("after %s delete POP3 RETR corrupt: %q", via, got)
	}
	p.pop3Cmd("QUIT")
	if q := e.quotaUsed("bob"); q != int64(len(msg)) {
		t.Errorf("after %s delete of one of two shared-blob copies quota=%d want %d (blob still on disk)", via, q, len(msg))
	}
	if n, _ := e.diskBytes("bob@test.com"); n != int64(len(msg)) {
		t.Errorf("after %s delete blob bytes on disk %d want %d", via, n, len(msg))
	}
}

func TestR187_SharedBlob_IMAPExpunge(t *testing.T) {
	e, ic, msg := r187TwinSetup(t)
	ic.imapCmd("s", "SELECT INBOX")
	ic.imapCmd("d", `STORE 1 +FLAGS (\Deleted)`)
	ic.imapCmd("x", "EXPUNGE")
	e.r187Survivor(t, "IMAP", msg)
}

// F6644 (product bug): every POP3 DELE releases quota twice. The production
// pop3MailstoreAdapter.DeleteMessage (server/adapters.go) calls
// BboltMailstore.ExpungeUIDs, which already releases the blob's bytes
// (reference-aware), and pop3.Session.handleUpdateCommand (pop3/server.go
// ~1140) then calls quotaRelease(msg.Size) again via server_pop3.go.
// Repro 1: IMAP APPEND 4 distinct 83-byte messages (quota 332); POP3 DELE 1,
// QUIT -> quota 166 (want 249). Repro 2: append the same bytes twice, POP3
// DELE 1 -> quota 0 although one message + blob remain.
func TestR187_SharedBlob_POP3Dele(t *testing.T) {
	e, _, msg := r187TwinSetup(t)
	p := e.pop3Login("bob@test.com")
	p.pop3Cmd("DELE 1")
	p.pop3Cmd("QUIT")
	time.Sleep(150 * time.Millisecond)
	e.r187Survivor(t, "POP3", msg)
}

func TestR187_SharedBlob_JMAPDestroy(t *testing.T) {
	e, _, msg := r187TwinSetup(t)
	tok := e.jmapToken("bob@test.com")
	em := e.jmapEmails(tok, "bob@test.com", []string{"subject"})
	t.Logf("JMAP sees %d emails for 2 identical copies: %v", len(em), em)
	if len(em) != 2 {
		t.Errorf("JMAP Email/query shows %d emails, IMAP/POP3 show 2", len(em))
	}
	if len(em) == 0 {
		return
	}
	e.jmapDestroy(tok, "bob@test.com", em[0]["id"].(string))
	e.r187Survivor(t, "JMAP", msg)
}

func TestR187_SharedBlob_RESTDelete(t *testing.T) {
	e, _, msg := r187TwinSetup(t)
	rt, _ := e.restLogin("bob@test.com", e.password)
	ms := e.restList(rt, "inbox")
	if len(ms) != 2 {
		t.Fatalf("REST sees %d want 2", len(ms))
	}
	t.Logf("REST ids %s %s", ms[0].ID, ms[1].ID)
	st, raw := e.rest(rt, "DELETE", "/api/v1/mail/delete?id="+ms[0].ID, nil)
	if st != 200 {
		t.Fatalf("delete %d %s", st, raw)
	}
	e.r187Survivor(t, "REST", msg)
}

// F6642: distinct messages, deleted one per protocol: each delete releases
// exactly that message's size and, once below the limit again, delivery over
// SMTP is accepted again.
func TestR187_QuotaReleasePerProtocol(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	defer e.stop()
	mk := func(i int) string {
		return fmt.Sprintf("From: a@b.c\r\nTo: bob@test.com\r\nSubject: q%d\r\nMessage-ID: <q%d@b.c>\r\n\r\nbody number %d\r\n", i, i, i)
	}
	ic := e.imapLogin("bob@test.com")
	var total int64
	for i := 0; i < 4; i++ {
		m := mk(i)
		ic.imapCmd(fmt.Sprintf("a%d", i), fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(m), m))
		total += int64(len(m))
	}
	if q := e.quotaUsed("bob"); q != total {
		t.Fatalf("quota %d want %d", q, total)
	}
	sizeOf := func(i int) int64 { return int64(len(mk(i))) }
	prev := total
	step := func(via string, i int, strict bool) {
		t.Helper()
		q := e.quotaUsed("bob")
		if d := prev - q; d != sizeOf(i) && strict {
			t.Errorf("%s delete of q%d released %d bytes, want %d (quota %d -> %d)", via, i, d, sizeOf(i), prev, q)
		}
		prev = q
	}
	// IMAP expunge q0 (UID 1)
	ic.imapCmd("s", "SELECT INBOX")
	ic.imapCmd("d", `STORE 1 +FLAGS (\Deleted)`)
	ic.imapCmd("x", "EXPUNGE")
	step("IMAP", 0, true)
	// POP3 DELE q1. F6644 product defect: the production POP3 adapter's
	// DeleteMessage goes through BboltMailstore.ExpungeUIDs, which already
	// releases quota (reference-aware), and pop3.handleUpdateCommand then
	// calls quotaRelease(msg.Size) again: every POP3 DELE releases twice.
	p := e.pop3Login("bob@test.com")
	p.pop3Cmd("DELE 1")
	p.pop3Cmd("QUIT")
	time.Sleep(100 * time.Millisecond)
	step("POP3", 1, true)
	// Resync the counter so later steps are judged independently.
	prev = e.quotaUsed("bob")
	// JMAP destroy q2
	tok := e.jmapToken("bob@test.com")
	for _, m := range e.jmapEmails(tok, "bob@test.com", []string{"subject"}) {
		if m["subject"] == "q2" {
			e.jmapDestroy(tok, "bob@test.com", m["id"].(string))
		}
	}
	step("JMAP", 2, true)
	// REST delete q3
	rt, _ := e.restLogin("bob@test.com", e.password)
	ms := e.restList(rt, "inbox")
	if len(ms) != 1 || ms[0].Subject != "q3" {
		t.Fatalf("REST list: %+v", ms)
	}
	if st, raw := e.rest(rt, "DELETE", "/api/v1/mail/delete?id="+ms[0].ID, nil); st != 200 {
		t.Fatalf("REST delete %d %s", st, raw)
	}
	if q := e.quotaUsed("bob"); q != 0 {
		t.Errorf("REST delete: quota %d want 0", q)
	}
	if n, files := e.diskBytes("bob@test.com"); n != 0 {
		t.Errorf("files left after deleting everything: %v", files)
	}
}

// F6642b: raising nothing, but freeing space makes an over-quota mailbox
// accept SMTP again (quota is a live counter, not sticky).
func TestR187_OverQuotaThenFreed(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 1000})
	e.start()
	defer e.stop()
	big := "From: a@b.c\r\nTo: bob@test.com\r\nSubject: big\r\n\r\n" + strings.Repeat("x", 700) + "\r\n"
	ic := e.imapLogin("bob@test.com")
	if o := ic.imapCmd("a1", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(big), big)); !strings.Contains(o, "a1 OK") {
		t.Fatalf("first append: %s", o)
	}
	big2 := strings.Replace(big, "Subject: big", "Subject: big2", 1)
	if o := ic.imapCmd("a2", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(big2), big2)); strings.Contains(o, "a2 OK") {
		t.Fatalf("over-quota append accepted: %s", o)
	}
	p := e.pop3Login("bob@test.com")
	p.pop3Cmd("DELE 1")
	p.pop3Cmd("QUIT")
	time.Sleep(100 * time.Millisecond)
	e.inbound("s@ext.example", []string{"bob@test.com"}, mk187("after free", "af1"))
	r187Wait(t, "delivery after freeing", func() bool { return e.mailboxCount("bob@test.com", "INBOX") == 1 })
}
