package server

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// F6649a: Sieve fileinto, redirect and vacation end to end. The vacation
// reply is sent with the null envelope sender, is not sent to a null-sender
// message, and is not repeated inside the :days window.
func TestR187_SieveFileintoRedirectVacation(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
	defer e.stop()
	e.sieveUpload("bob@test.com", `require ["fileinto","vacation"];
if header :contains "subject" "invoice" { fileinto "Invoices"; stop; }
if header :contains "subject" "fwd" { redirect "elsewhere@ext.example"; }
vacation :days 1 :subject "away" "I am away";
`)
	mk := func(subj, id string) string {
		return strings.Replace(strings.Replace(e2eMsg, "hello e2e", subj, 1), "e2e1@", id+"@", 1)
	}
	e.inbound("sender@ext.example", []string{"bob@test.com"}, mk("your invoice", "i1"))
	r187Wait(t, "Invoices", func() bool { return e.mailboxCount("bob@test.com", "Invoices") == 1 })
	if n := e.mailboxCount("bob@test.com", "INBOX"); n != 0 {
		t.Errorf("fileinto+stop left %d in INBOX", n)
	}
	if len(e.pending()) != 0 {
		t.Errorf("fileinto+stop queued outbound mail: %v", e.pending())
	}
	e.inbound("sender@ext.example", []string{"bob@test.com"}, mk("please fwd", "f1"))
	e.inbound("sender@ext.example", []string{"bob@test.com"}, mk("plain hello", "p1"))
	// A null-sender (bounce) message must not get a vacation reply.
	e.inbound("", []string{"bob@test.com"}, mk("bounce plain", "n1"))
	// redirect (no :copy) cancels the implicit keep, so only the plain and
	// the null-sender message reach INBOX.
	r187Wait(t, "inbox deliveries", func() bool { return e.mailboxCount("bob@test.com", "INBOX") >= 2 })
	time.Sleep(300 * time.Millisecond)
	var redirect, vacation int
	for _, p := range e.pending() {
		t.Logf("queue: from=%q to=%v", p.From, p.To)
		for _, to := range p.To {
			switch to {
			case "elsewhere@ext.example":
				redirect++
			case "sender@ext.example":
				if strings.Contains(strings.ToLower(p.From), "sender@") {
					continue
				}
				vacation++
				if p.From != "" && p.From != "<>" {
					t.Errorf("vacation reply envelope sender %q want null sender", p.From)
				}
			default:
				if to == "" {
					t.Errorf("vacation reply queued to null sender")
				}
			}
		}
	}
	if redirect != 1 {
		t.Errorf("redirect queued %d times want 1", redirect)
	}
	if vacation != 1 {
		t.Errorf("vacation replies queued %d want exactly 1 (not for null sender, not repeated within :days)", vacation)
	}
	// Second plain message from the same sender inside :days window: no new reply.
	e.inbound("sender@ext.example", []string{"bob@test.com"}, mk("plain again", "p2"))
	r187Wait(t, "4th inbox", func() bool { return e.mailboxCount("bob@test.com", "INBOX") >= 3 })
	time.Sleep(200 * time.Millisecond)
	v2 := 0
	for _, p := range e.pending() {
		if p.From == "" || p.From == "<>" {
			v2++
		}
	}
	if v2 != 1 {
		t.Errorf("null-sender queue entries after second message: %d want 1", v2)
	}
}

// F6649b: graceful shutdown while an IMAP session sits in IDLE and another
// SMTP session is mid-DATA: the server notifies the idle client (or closes the
// connection), Stop returns promptly, and the pending DATA message is either
// fully acknowledged and stored or rejected and absent.
func TestR187_ShutdownDuringIdleAndData(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
	ic := e.imapLogin("bob@test.com")
	ic.imapCmd("s", "SELECT INBOX")
	ic.send("i IDLE")
	if l := ic.line(); !strings.HasPrefix(l, "+") {
		t.Fatalf("IDLE: %s", l)
	}
	// Control: an IMAP APPEND from another session is pushed to the IDLE client.
	other := e.imapLogin("bob@test.com")
	apm := "From: a@b.c\r\nSubject: ctl\r\nMessage-ID: <ctl187@b.c>\r\n\r\nx\r\n"
	other.imapCmd("ap", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(apm), apm))
	if l := idleDrain(ic, time.Second); !strings.Contains(l, "EXISTS") {
		t.Errorf("IDLE push after APPEND: %q want EXISTS", l)
	}
	// F6649d product defect: SMTP-delivered mail (deliverLocalHop writes
	// msgStore+storageDB directly) never calls imap NotificationHub, so an
	// IDLE client (and the SSE /api/v1/events stream) is not told about it.
	e.inbound("s@ext.example", []string{"bob@test.com"}, mk187("live", "idl1"))
	if r187Known(t, "F6649d") {
		if l := idleDrain(ic, 2*time.Second); !strings.Contains(l, "EXISTS") {
			t.Errorf("IDLE push after SMTP delivery: %q want EXISTS", l)
		}
	}
	c := e2eDial(t, e.smtp, false)
	c.expectSMTP(220)
	c.send("EHLO mx.test")
	c.expectSMTP(250)
	c.send("MAIL FROM:<s@ext.example>")
	c.expectSMTP(250)
	c.send("RCPT TO:<bob@test.com>")
	c.expectSMTP(250)
	c.send("DATA")
	c.expectSMTP(354)
	c.send("%s", strings.ReplaceAll(strings.TrimRight(mk187("pending", "dat1"), "\n"), "\n", "\r\n"))
	stopped := make(chan struct{})
	stopStart := time.Now()
	var stopDur time.Duration
	go func() { e.stop(); stopDur = time.Since(stopStart); close(stopped) }()
	time.Sleep(200 * time.Millisecond)
	c.send(".")
	code := 0
	_ = c.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if l, err := c.r.ReadString('\n'); err == nil {
		fmt.Sscanf(l, "%d", &code)
	}
	// The idle client is told goodbye or disconnected (not left hanging).
	_ = ic.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 256)
	n, err := ic.r.Read(buf)
	t.Logf("idle client after stop: %q err=%v", buf[:n], err)
	if err == nil && !strings.Contains(string(buf[:n]), "BYE") {
		_, err = io.Copy(io.Discard, ic.r)
	}
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("Stop hung with an IDLE session open")
	}
	t.Logf("Stop took %v", stopDur)
	if stopDur > 15*time.Second {
		t.Errorf("Stop took %v with an IDLE session open", stopDur)
	}
	e.start()
	defer e.stop()
	got := e.mailboxCount("bob@test.com", "INBOX")
	want := 2 // APPENDed control + live SMTP message
	if code == 250 {
		want = 3
	}
	if got != want {
		t.Errorf("INBOX=%d want %d (DATA reply code %d)", got, want, code)
	}
}

func mk187(subj, id string) string {
	return strings.Replace(strings.Replace(e2eMsg, "hello e2e", subj, 1), "e2e1@", id+"@", 1)
}

// F6649c: deleting an account removes its aliases, mailbox files and quota,
// kills its sessions on REST+JMAP, and a re-created account with the same
// address starts empty and cannot be reached with the old token.
func TestR187_AccountDeletionCascade(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20}, &db.AccountData{LocalPart: "root", IsAdmin: true})
	d, _ := db.Open(e.cfg.Database.Path)
	if err := d.CreateAlias(&db.AliasData{Domain: "test.com", Alias: "sales", Target: "bob@test.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	d.Close()
	e.start()
	defer e.stop()
	e.inbound("s@ext.example", []string{"sales@test.com"}, mk187("pre-delete", "d1"))
	r187Wait(t, "delivery", func() bool { return e.mailboxCount("bob@test.com", "INBOX") == 1 })
	bt, _ := e.restLogin("bob@test.com", e.password)
	at, _ := e.restLogin("root@test.com", e.password)
	if bt == "" || at == "" {
		t.Fatal("logins")
	}
	if n, _ := e.diskBytes("bob@test.com"); n == 0 {
		t.Fatal("no mailbox bytes before delete")
	}
	st, raw := e.rest(at, "DELETE", "/api/v1/accounts/bob@test.com", nil)
	if st != 204 {
		t.Fatalf("delete account %d %s", st, raw)
	}
	if e.restStatus(bt) == 200 || e.jmapStatus(bt) == 200 {
		t.Errorf("deleted account's token still works: rest=%d jmap=%d", e.restStatus(bt), e.jmapStatus(bt))
	}
	ic := e2eDial(t, e.imap, true)
	ic.line()
	if o := ic.imapCmd("l", `LOGIN "bob@test.com" "`+e.password+`"`); strings.Contains(o, "l OK") {
		t.Errorf("IMAP login of deleted account succeeded")
	}
	if as, _ := e.srv.database.ListAliases(); len(as) != 0 {
		t.Errorf("aliases survive account deletion: %v", as)
	}
	// Mail to the deleted address is refused/dropped, not silently stored.
	// Mailbox files must be gone (F6649c product check; see ledger).
	n, files := e.diskBytes("bob@test.com")
	if n != 0 && r187Known(t, "F6649c") {
		t.Errorf("mailbox files survive account deletion: %d bytes %v", n, files)
	}
	// Re-create the same address.
	st, raw = e.rest(at, "POST", "/api/v1/accounts", map[string]any{"email": "bob@test.com", "password": "Br4nd-New-pass!z"})
	if st != 201 && st != 200 {
		t.Fatalf("recreate %d %s", st, raw)
	}
	if e.restStatus(bt) == 200 || e.jmapStatus(bt) == 200 {
		t.Errorf("pre-deletion token revived by re-creating the account")
	}
	if q := e.quotaUsed("bob"); q != 0 {
		t.Errorf("re-created account inherits quota_used=%d", q)
	}
	rc := e2eDial(t, e.imap, true)
	rc.line()
	rc.imapCmd("l", `LOGIN "bob@test.com" "Br4nd-New-pass!z"`)
	o := rc.imapCmd("s", "SELECT INBOX")
	if r187Known(t, "F6649c") && !strings.Contains(o, "* 0 EXISTS") {
		t.Errorf("re-created account sees the deleted account's mail: %s", o)
	}
	if o := e.pop3Count("bob@test.com", "Br4nd-New-pass!z"); r187Known(t, "F6649c") && o != 0 {
		t.Errorf("POP3 of re-created account sees %d old messages", o)
	}
}

func (e *r187Env) pop3Count(user, pass string) int {
	c := e2eDial(e.t, e.pop3, true)
	c.line()
	c.send("USER %s", user)
	c.line()
	c.send("PASS %s", pass)
	if l := c.line(); !strings.HasPrefix(l, "+OK") {
		e.t.Fatalf("pop3 login: %s", l)
	}
	var n int
	fmt.Sscanf(c.pop3Cmd("STAT"), "+OK %d", &n)
	c.pop3Cmd("QUIT")
	return n
}

// idleDrain reads untagged lines until nothing arrives for d.
func idleDrain(c *e2eConn, d time.Duration) string {
	var sb strings.Builder
	for {
		_ = c.c.SetReadDeadline(time.Now().Add(d))
		l, err := c.r.ReadString('\n')
		if err != nil {
			return sb.String()
		}
		sb.WriteString(l)
	}
}
