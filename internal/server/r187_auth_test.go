package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func (e *r187Env) jmapStatus(tok string) int {
	e.t.Helper()
	st, _ := e.httpGet(fmt.Sprintf("http://127.0.0.1:%d/jmap/session", e.jmap), tok)
	return st
}

func (e *r187Env) restStatus(tok string) int {
	e.t.Helper()
	st, _ := e.rest(tok, "GET", "/api/v1/mail/inbox", nil)
	return st
}

// F6646: a self-service password change and an admin password reset revoke
// every earlier session on both REST and JMAP, with no clock-second grace
// (tokens minted immediately before the change must die immediately).
func TestR187_PasswordChangeResetRevokesSessions(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"}, &db.AccountData{LocalPart: "root", IsAdmin: true})
	e.start()
	defer e.stop()

	t1, st := e.restLogin("bob@test.com", e.password)
	if st != 200 || t1 == "" {
		t.Fatalf("login %d", st)
	}
	if e.restStatus(t1) != 200 || e.jmapStatus(t1) != 200 {
		t.Fatalf("fresh token rejected: rest=%d jmap=%d", e.restStatus(t1), e.jmapStatus(t1))
	}
	newPw := "N3w-Passw0rd!x"
	st, raw := e.rest(t1, "POST", "/api/v1/account/password", map[string]string{"current_password": e.password, "new_password": newPw})
	if st != 200 {
		t.Fatalf("change password %d %s", st, raw)
	}
	if s := e.restStatus(t1); s != 401 {
		t.Errorf("REST accepts pre-change token after password change: %d", s)
	}
	if s := e.jmapStatus(t1); s != 401 {
		t.Errorf("JMAP accepts pre-change token after password change: %d", s)
	}
	if _, st := e.restLogin("bob@test.com", e.password); st == 200 {
		t.Errorf("old password still logs in over REST")
	}
	t2, st := e.restLogin("bob@test.com", newPw)
	if st != 200 {
		t.Fatalf("new password login %d", st)
	}
	if e.restStatus(t2) != 200 || e.jmapStatus(t2) != 200 {
		t.Errorf("post-change token rejected: rest=%d jmap=%d", e.restStatus(t2), e.jmapStatus(t2))
	}
	// IMAP/POP3 follow the new password too.
	ic := e2eDial(t, e.imap, true)
	ic.line()
	if o := ic.imapCmd("l", `LOGIN "bob@test.com" "`+e.password+`"`); strings.Contains(o, "l OK") {
		t.Errorf("IMAP accepts old password")
	}
	if o := ic.imapCmd("m", `LOGIN "bob@test.com" "`+newPw+`"`); !strings.Contains(o, "m OK") {
		t.Errorf("IMAP rejects new password: %s", o)
	}
	pc := e2eDial(t, e.pop3, true)
	pc.line()
	pc.send("USER bob@test.com")
	pc.line()
	pc.send("PASS %s", newPw)
	if l := pc.line(); !strings.HasPrefix(l, "+OK") {
		t.Errorf("POP3 rejects new password (APOP/hash divergence?): %s", l)
	}

	// Admin reset.
	at, _ := e.restLogin("root@test.com", e.password)
	if at == "" {
		t.Fatal("admin login")
	}
	resetPw := "R3set-Passw0rd!y"
	st, raw = e.rest(at, "PUT", "/api/v1/accounts/bob@test.com", map[string]any{"password": resetPw})
	if st != 200 {
		t.Fatalf("admin reset %d %s", st, raw)
	}
	if s := e.restStatus(t2); s != 401 {
		t.Errorf("REST accepts pre-reset token: %d", s)
	}
	if s := e.jmapStatus(t2); s != 401 {
		t.Errorf("JMAP accepts pre-reset token: %d", s)
	}
	t3, st := e.restLogin("bob@test.com", resetPw)
	if st != 200 || e.jmapStatus(t3) != 200 {
		t.Errorf("post-reset login/session: login=%d", st)
	}
	// Disabling the account kills sessions as well.
	f := false
	if st, raw = e.rest(at, "PUT", "/api/v1/accounts/bob@test.com", map[string]any{"is_active": &f}); st != 200 {
		t.Fatalf("disable %d %s", st, raw)
	}
	if e.restStatus(t3) == 200 || e.jmapStatus(t3) == 200 {
		t.Errorf("disabled account token still works: rest=%d jmap=%d", e.restStatus(t3), e.jmapStatus(t3))
	}
}

// F6647: login identifiers are case-insensitive on every protocol, and mail
// addressed in any case lands in the same mailbox.
func TestR187_CaseInsensitiveLogin(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"}, &db.AccountData{LocalPart: "alice"})
	e.start()
	defer e.stop()
	variants := []string{"BOB@TEST.COM", "Bob@Test.Com"}
	for _, v := range variants {
		// SMTP AUTH via submission with mixed-case user and recipient.
		e.submit(v, e.password, v, []string{v}, strings.Replace(e2eMsg, "<e2e1@", "<c"+fmt.Sprint(len(v))+v[:2]+"@", 1))
		// IMAP
		ic := e2eDial(t, e.imap, true)
		ic.line()
		if o := ic.imapCmd("l", `LOGIN "`+v+`" "`+e.password+`"`); !strings.Contains(o, "l OK") {
			t.Errorf("IMAP LOGIN %s: %s", v, o)
		}
		// POP3
		pc := e2eDial(t, e.pop3, true)
		pc.line()
		pc.send("USER %s", v)
		pc.line()
		pc.send("PASS %s", e.password)
		if l := pc.line(); !strings.HasPrefix(l, "+OK") {
			t.Errorf("POP3 login %s: %s", v, l)
		} else {
			pc.pop3Cmd("QUIT")
		}
		// REST
		// F6647 product defect: api.handleLogin splits the email with
		// parseEmail (no lowercasing) and db.GetAccount is case-sensitive, so
		// REST login with a mixed-case address is 401 although SMTP/IMAP/POP3
		// accept it (policy: addresses are case-insensitive system-wide).
		{ // F6647 fixed in R193: gate removed
			if tok, st := e.restLogin(v, e.password); st != 200 || tok == "" {
				t.Errorf("REST login %s: %d", v, st)
			} else if e.jmapStatus(tok) != 200 || e.restStatus(tok) != 200 {
				t.Errorf("REST token of %s rejected by REST/JMAP", v)
			}
		}
	}
	r187Wait(t, "both mixed-case deliveries", func() bool {
		time.Sleep(100 * time.Millisecond)
		return e.mailboxCount("bob@test.com", "INBOX") == 2
	})
	// Wrong password still rejected for a mixed-case name (not a bypass).
	if _, st := e.restLogin("BOB@test.com", "wrong-pass-123"); st == 200 {
		t.Errorf("wrong password accepted")
	}
}

// F6648: alias, plus-address, mixed-case alias and catch-all delivery.
func TestR187_AliasPlusCatchAll(t *testing.T) {
	e := newR187(t)
	e.seed(&db.AccountData{LocalPart: "bob"}, &db.AccountData{LocalPart: "carol"})
	d, _ := db.Open(e.cfg.Database.Path)
	if err := d.CreateAlias(&db.AliasData{Domain: "test.com", Alias: "sales", Target: "bob@test.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	dom, err := d.GetDomain("test.com")
	if err != nil {
		t.Fatal(err)
	}
	dom.CatchAllTarget = "carol@test.com"
	if err := d.UpdateDomain(dom); err != nil {
		t.Fatal(err)
	}
	d.Close()
	e.start()
	defer e.stop()
	rcpts := []string{"SALES@test.com", "bob+tag@TEST.com", "Bob+a+b@test.com", "nobody@test.com", "unknown.user@test.com"}
	for i, r := range rcpts {
		msg := strings.Replace(e2eMsg, "e2e1@", fmt.Sprintf("ap%d@", i), 1)
		e.inbound("s@ext.example", []string{r}, msg)
	}
	r187Wait(t, "deliveries", func() bool {
		return e.mailboxCount("bob@test.com", "INBOX") == 3 && e.mailboxCount("carol@test.com", "INBOX") == 2
	})
}
