package server

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

const e2eMsg = "From: alice@test.com\nTo: bob@test.com\nSubject: hello e2e\nMessage-ID: <e2e1@test.com>\nDate: Mon, 02 Jan 2006 15:04:05 +0000\n\nbody line one\n"

func TestE2E_SubmissionToIMAP(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "alice"}, &db.AccountData{LocalPart: "bob"})
	e.start()
	defer e.stop()
	e.submit("alice@test.com", e.password, "alice@test.com", []string{"bob@test.com"}, e2eMsg)

	time.Sleep(300 * time.Millisecond)
	c := e2eDial(t, e.imap, true)
	t.Log(c.line())
	out := c.imapCmd("a1", `LOGIN "bob@test.com" "`+e.password+`"`)
	if !strings.Contains(out, "a1 OK") {
		t.Fatalf("login: %s", out)
	}
	out = c.imapCmd("a2", "SELECT INBOX")
	t.Log(out)
	if !strings.Contains(out, "1 EXISTS") {
		t.Fatalf("select: %s", out)
	}
	out = c.imapCmd("a3", "FETCH 1 (BODY[HEADER] RFC822.SIZE)")
	t.Log(out)
	if !strings.Contains(out, "Return-Path: <alice@test.com>") {
		t.Errorf("missing Return-Path")
	}
}

// inbound delivers a message through the MX listener (port "smtp", no auth).
func (e *e2eEnv) inbound(from string, rcpts []string, msg string) {
	e.t.Helper()
	c := e2eDial(e.t, e.smtp, false)
	c.expectSMTP(220)
	c.send("EHLO mx.sender.test")
	c.expectSMTP(250)
	c.send("MAIL FROM:<%s>", from)
	c.expectSMTP(250)
	for _, r := range rcpts {
		c.send("RCPT TO:<%s>", r)
		c.expectSMTP(250)
	}
	c.send("DATA")
	c.expectSMTP(354)
	c.send("%s\r\n.", strings.ReplaceAll(strings.TrimRight(msg, "\r\n"), "\n", "\r\n"))
	c.expectSMTP(250)
	c.send("QUIT")
}

func TestE2E_InboundAuthResultsAndPOP3(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
	defer e.stop()
	e.inbound("sender@ext.example", []string{"bob@test.com"}, strings.Replace(e2eMsg, "alice@test.com", "sender@ext.example", 1))
	time.Sleep(300 * time.Millisecond)
	c := e2eDial(t, e.imap, true)
	c.line()
	c.imapCmd("a1", `LOGIN "bob@test.com" "`+e.password+`"`)
	c.imapCmd("a2", "SELECT INBOX")
	out := c.imapCmd("a3", "FETCH 1 (BODY[HEADER])")
	t.Log(out)
	if !strings.Contains(out, "Return-Path: <sender@ext.example>") || !strings.Contains(out, "Authentication-Results:") {
		t.Errorf("missing headers")
	}
}

func (e *e2eEnv) quotaUsed(local string) int64 {
	e.t.Helper()
	a, err := e.srv.database.GetAccount("test.com", local)
	if err != nil {
		e.t.Fatal(err)
	}
	return a.QuotaUsed
}

func (e *e2eEnv) imapLogin(user string) *e2eConn {
	e.t.Helper()
	c := e2eDial(e.t, e.imap, true)
	c.line()
	if out := c.imapCmd("l1", `LOGIN "`+user+`" "`+e.password+`"`); !strings.Contains(out, "l1 OK") {
		e.t.Fatalf("login: %s", out)
	}
	return c
}

func (e *e2eEnv) pop3Login(user string) *e2eConn {
	e.t.Helper()
	c := e2eDial(e.t, e.pop3, true)
	c.line()
	c.send("USER %s", user)
	if l := c.line(); !strings.HasPrefix(l, "+OK") {
		e.t.Fatalf("USER: %s", l)
	}
	c.send("PASS %s", e.password)
	if l := c.line(); !strings.HasPrefix(l, "+OK") {
		e.t.Fatalf("PASS: %s", l)
	}
	return c
}

func TestE2E_QuotaConsistency(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "alice"}, &db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	defer e.stop()
	q := func(step string) int64 {
		v := e.quotaUsed("bob")
		t.Logf("QUOTA after %-28s = %d", step, v)
		return v
	}
	q("start")
	e.submit("alice@test.com", e.password, "alice@test.com", []string{"bob@test.com"}, e2eMsg)
	time.Sleep(300 * time.Millisecond)
	afterSMTP := q("SMTP delivery")

	c := e.imapLogin("bob@test.com")
	apMsg := "From: x@y.z\r\nSubject: appended\r\nMessage-ID: <ap@x>\r\n\r\nappend body\r\n"
	out := c.imapCmd("a1", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(apMsg), apMsg))
	t.Log(out)
	afterAppend := q("IMAP APPEND")
	t.Logf("append delta=%d len=%d", afterAppend-afterSMTP, len(apMsg))
	c.imapCmd("a2", "SELECT INBOX")
	c.imapCmd("a3", "STORE 2 +FLAGS (\\Deleted)")
	out = c.imapCmd("a4", "EXPUNGE")
	t.Log(out)
	afterExpunge := q("IMAP EXPUNGE")
	if afterExpunge != afterSMTP {
		t.Errorf("DIVERGE: expunge of appended message: quota %d want %d", afterExpunge, afterSMTP)
	}
	c.imapCmd("a5", "LOGOUT")

	// POP3 DELE
	p := e.pop3Login("bob@test.com")
	p.send("STAT")
	t.Log(p.line())
	p.send("DELE 1")
	t.Log(p.line())
	p.send("QUIT")
	t.Log(p.line())
	time.Sleep(200 * time.Millisecond)
	if v := q("POP3 DELE"); v != 0 {
		t.Errorf("DIVERGE: after POP3 DELE of last message quota=%d want 0", v)
	}
}

func (e *e2eEnv) jmapToken(user string) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "exp": time.Now().Add(time.Hour).Unix(), "type": "access"})
	s, err := tok.SignedString([]byte(e.cfg.Security.JWTSecret))
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *e2eEnv) jmapDo(token, method, path string, ctype string, body []byte) map[string]any {
	e.t.Helper()
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", e.jmap, path), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		e.t.Fatalf("jmap %s %s: status %d body %s", method, path, resp.StatusCode, raw)
	}
	if resp.StatusCode >= 300 {
		e.t.Fatalf("jmap %s %s: status %d body %s", method, path, resp.StatusCode, raw)
	}
	return m
}

func (e *e2eEnv) jmapCall(token string, calls ...any) []any {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"}, "methodCalls": calls})
	m := e.jmapDo(token, "POST", "/jmap/api", "application/json", body)
	return m["methodResponses"].([]any)
}

func TestE2E_JMAPSeesDeliveryAndQuota(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "alice"}, &db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	defer e.stop()
	e.submit("alice@test.com", e.password, "alice@test.com", []string{"bob@test.com"}, e2eMsg)
	time.Sleep(300 * time.Millisecond)
	tok := e.jmapToken("bob@test.com")
	sess := e.jmapDo(tok, "GET", "/jmap/session", "", nil)
	t.Logf("session accounts=%v", sess["accounts"])
	resp := e.jmapCall(tok, []any{"Email/query", map[string]any{"accountId": "bob@test.com"}, "c1"})
	t.Logf("query: %v", resp)
	ids := resp[0].([]any)[1].(map[string]any)["ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("JMAP Email/query ids=%v", ids)
	}
	before := e.quotaUsed("bob")

	// import
	imp := "From: j@x.y\r\nSubject: jmap import\r\nMessage-ID: <j@x>\r\n\r\njmap body\r\n"
	up := e.jmapDo(tok, "POST", "/jmap/upload/bob@test.com", "message/rfc822", []byte(imp))
	t.Logf("upload: %v", up)
	blob := up["blobId"].(string)
	mb := e.jmapCall(tok, []any{"Mailbox/get", map[string]any{"accountId": "bob@test.com"}, "m1"})
	var inboxID string
	for _, x := range mb[0].([]any)[1].(map[string]any)["list"].([]any) {
		mm := x.(map[string]any)
		if mm["role"] == "inbox" {
			inboxID = mm["id"].(string)
		}
	}
	r := e.jmapCall(tok, []any{"Email/import", map[string]any{"accountId": "bob@test.com", "emails": map[string]any{"k1": map[string]any{"blobId": blob, "mailboxIds": map[string]any{inboxID: true}}}}, "i1"})
	t.Logf("import: %v", r)
	afterImp := e.quotaUsed("bob")
	t.Logf("quota before=%d afterImport=%d len=%d", before, afterImp, len(imp))
	if afterImp-before != int64(len(imp)) {
		t.Errorf("DIVERGE import delta %d want %d", afterImp-before, len(imp))
	}
	created := r[0].([]any)[1].(map[string]any)["created"].(map[string]any)["k1"].(map[string]any)
	r = e.jmapCall(tok, []any{"Email/set", map[string]any{"accountId": "bob@test.com", "destroy": []string{created["id"].(string)}}, "d1"})
	t.Logf("destroy: %v", r)
	if got := e.quotaUsed("bob"); got != before {
		t.Errorf("DIVERGE after JMAP destroy quota=%d want %d", got, before)
	}
}

// diskBytes sums every file under the message-store dir that belongs to the user.
func (e *e2eEnv) diskBytes(user string) (n int64, files []string) {
	root := filepath.Join(e.dir)
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if strings.Contains(rel, user) && !strings.HasSuffix(rel, ".db") && !strings.Contains(rel, "queue") {
			n += fi.Size()
			files = append(files, rel)
		}
		return nil
	})
	return
}

func TestE2E_CrossProtocolVisibilityAndDisk(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "alice"}, &db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	defer e.stop()
	tok := e.jmapToken("bob@test.com")
	imp := "From: j@x.y\r\nSubject: jmap import\r\nMessage-ID: <j@x>\r\n\r\njmap body\r\n"
	up := e.jmapDo(tok, "POST", "/jmap/upload/bob@test.com", "message/rfc822", []byte(imp))
	blob := up["blobId"].(string)
	e.jmapCall(tok, []any{"Email/import", map[string]any{"accountId": "bob@test.com", "emails": map[string]any{"k1": map[string]any{"blobId": blob, "mailboxIds": map[string]any{"inbox": true}}}}, "i1"})
	n, files := e.diskBytes("bob@test.com")
	t.Logf("after JMAP import: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)

	c := e.imapLogin("bob@test.com")
	out := c.imapCmd("a1", "SELECT INBOX")
	if !strings.Contains(out, "1 EXISTS") {
		t.Errorf("IMAP does not see JMAP-imported message: %s", out)
	}
	apMsg := "From: x@y.z\r\nSubject: appended\r\nMessage-ID: <ap@x>\r\n\r\nappend body\r\n"
	c.imapCmd("a2", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(apMsg), apMsg))
	resp := e.jmapCall(tok, []any{"Email/query", map[string]any{"accountId": "bob@test.com"}, "c1"})
	if ids := resp[0].([]any)[1].(map[string]any)["ids"].([]any); len(ids) != 2 {
		t.Errorf("JMAP does not see IMAP-appended message: %v", ids)
	}
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after APPEND: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
	out = c.imapCmd("a3", "COPY 1:2 Archive")
	t.Log(out)
	c.imapCmd("a3b", "CREATE Archive")
	out = c.imapCmd("a4", "COPY 1:2 Archive")
	t.Log(out)
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after COPY: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
	out = c.imapCmd("a5", "SELECT Archive")
	c.imapCmd("a6", "STORE 1:2 +FLAGS (\\Deleted)")
	c.imapCmd("a7", "EXPUNGE")
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after expunge in Archive: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
	c.imapCmd("a8", "SELECT INBOX")
	c.imapCmd("a9", "COPY 1:2 Archive")
	c.imapCmd("a10", "UNSELECT")
	out = c.imapCmd("a11", "DELETE Archive")
	t.Log(out)
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after DELETE Archive: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
}

func TestE2E_DuplicateContentRefcount(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	defer e.stop()
	c := e.imapLogin("bob@test.com")
	apMsg := "From: x@y.z\r\nSubject: dup\r\nMessage-ID: <dup@x>\r\n\r\ndup body\r\n"
	c.imapCmd("a1", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(apMsg), apMsg))
	c.imapCmd("a2", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(apMsg), apMsg))
	n, files := e.diskBytes("bob@test.com")
	t.Logf("after 2 identical APPENDs: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
	c.imapCmd("a3", "SELECT INBOX")
	c.imapCmd("a4", "STORE 1 +FLAGS (\\Deleted)")
	c.imapCmd("a5", "EXPUNGE")
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after expunge 1 of 2: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
	out := c.imapCmd("a6", "FETCH 1 (BODY[TEXT])")
	if !strings.Contains(out, "dup body") {
		t.Errorf("DATA LOSS: surviving duplicate unreadable after expunging the other: %s", out)
	}
	c.imapCmd("a7", "STORE 1 +FLAGS (\\Deleted)")
	c.imapCmd("a8", "EXPUNGE")
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after expunge both: quota=%d disk=%d files=%v", e.quotaUsed("bob"), n, files)
	if e.quotaUsed("bob") != 0 {
		t.Errorf("quota leak %d", e.quotaUsed("bob"))
	}
}

// sieveUpload logs into the running ManageSieve listener and activates script.
func (e *e2eEnv) sieveUpload(user, script string) {
	e.t.Helper()
	addr := e.srv.manageSieveServer.Addr().String()
	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		e.t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	await := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				e.t.Fatalf("sieve read: %v", err)
			}
			if strings.HasPrefix(l, "OK") || strings.HasPrefix(l, "NO") || strings.HasPrefix(l, "BYE") {
				return l
			}
		}
	}
	await()
	for _, cmd := range []string{
		"AUTHENTICATE \"PLAIN\" \"" + b64("\x00"+user+"\x00"+e.password) + "\"\r\n",
		fmt.Sprintf("PUTSCRIPT \"main\" {%d+}\r\n%s\r\n", len(script), script),
		"SETACTIVE \"main\"\r\n",
	} {
		fmt.Fprint(c, cmd)
		if l := await(); !strings.HasPrefix(l, "OK") {
			e.t.Fatalf("sieve %q -> %q", strings.SplitN(cmd, " ", 2)[0], l)
		}
	}
}

func (e *e2eEnv) pending() []*db.QueueEntry {
	e.t.Helper()
	ents, err := e.srv.queue.GetPendingEntries()
	if err != nil {
		e.t.Fatal(err)
	}
	return ents
}

func (e *e2eEnv) mailboxCount(user, mbox string) int {
	e.t.Helper()
	c := e.imapLogin(user)
	defer c.imapCmd("z", "LOGOUT")
	out := c.imapCmd("s", `SELECT "`+mbox+`"`)
	var n int
	for _, l := range strings.Split(out, "\n") {
		if strings.HasSuffix(strings.TrimSpace(l), " EXISTS") {
			fmt.Sscanf(strings.TrimSpace(l), "* %d EXISTS", &n)
		}
	}
	return n
}

func TestE2E_SieveSurvivesRestart(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
	script := `require ["fileinto","vacation"];
if header :contains "subject" "invoice" { fileinto "Invoices"; stop; }
if header :contains "subject" "fwd" { redirect "elsewhere@ext.example"; }
vacation :days 1 :subject "away" "I am away";
`
	e.sieveUpload("bob@test.com", script)
	mk := func(subj string) string {
		return strings.Replace(e2eMsg, "hello e2e", subj, 1)
	}
	e.inbound("sender@ext.example", []string{"bob@test.com"}, mk("your invoice"))
	time.Sleep(300 * time.Millisecond)
	if n := e.mailboxCount("bob@test.com", "Invoices"); n != 1 {
		t.Errorf("before restart: Invoices=%d want 1", n)
	}
	if n := e.mailboxCount("bob@test.com", "INBOX"); n != 0 {
		t.Errorf("before restart: INBOX=%d want 0", n)
	}

	e.stop()
	e.start()
	defer e.stop()
	e.inbound("sender@ext.example", []string{"bob@test.com"}, strings.Replace(mk("another invoice"), "e2e1@", "e2e2@", 1))
	time.Sleep(300 * time.Millisecond)
	if n := e.mailboxCount("bob@test.com", "Invoices"); n != 2 {
		t.Errorf("after restart: Invoices=%d want 2 (sieve script lost on restart?)", n)
	}
	e.inbound("sender@ext.example", []string{"bob@test.com"}, strings.Replace(mk("please fwd"), "e2e1@", "e2e3@", 1))
	time.Sleep(300 * time.Millisecond)
	var redirected, vac bool
	for _, p := range e.pending() {
		t.Logf("queue: from=%q to=%v", p.From, p.To)
		for _, to := range p.To {
			if to == "elsewhere@ext.example" {
				redirected = true
			}
			if to == "sender@ext.example" {
				vac = true
			}
		}
	}
	if !redirected {
		t.Errorf("redirect not queued")
	}
	if !vac {
		t.Errorf("vacation reply not queued")
	}
}

func TestE2E_AliasForwardPlusRouting(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob"}, &db.AccountData{LocalPart: "carol", ForwardTo: "far@ext.example", ForwardKeepCopy: true},
		&db.AccountData{LocalPart: "dave", ForwardTo: "far2@ext.example"})
	d, _ := db.Open(e.cfg.Database.Path)
	if err := d.CreateAlias(&db.AliasData{Domain: "test.com", Alias: "sales", Target: "bob@test.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAlias(&db.AliasData{Domain: "test.com", Alias: "boss", Target: "far3@ext.example", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	d.Close()
	e.start()
	defer e.stop()
	for i, rcpt := range []string{"sales@test.com", "bob+news@test.com", "carol@test.com", "dave@test.com", "boss@test.com"} {
		msg := strings.Replace(e2eMsg, "e2e1@", fmt.Sprintf("r%d@", i), 1)
		e.inbound("sender@ext.example", []string{rcpt}, msg)
	}
	time.Sleep(500 * time.Millisecond)
	if n := e.mailboxCount("bob@test.com", "INBOX"); n != 2 {
		t.Errorf("bob INBOX=%d want 2 (alias + plus)", n)
	}
	if n := e.mailboxCount("carol@test.com", "INBOX"); n != 1 {
		t.Errorf("carol keep-copy INBOX=%d want 1", n)
	}
	if n := e.mailboxCount("dave@test.com", "INBOX"); n != 0 {
		t.Errorf("dave INBOX=%d want 0 (forward without copy)", n)
	}
	got := map[string]bool{}
	for _, p := range e.pending() {
		t.Logf("queue from=%q to=%v", p.From, p.To)
		for _, to := range p.To {
			got[to] = true
		}
	}
	for _, w := range []string{"far@ext.example", "far2@ext.example", "far3@ext.example"} {
		if !got[w] {
			t.Errorf("not queued for %s", w)
		}
	}
}

// A message delivered twice with byte-identical content is stored once
// (content-addressed) but must not leak quota when both copies are expunged.
func TestE2E_DuplicateDeliveryQuotaRelease(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 1 << 20})
	e.start()
	defer e.stop()
	data := []byte("From: x@y.z\r\nTo: bob@test.com\r\nSubject: dd\r\nMessage-ID: <dd@x>\r\n\r\nbody\r\n")
	for i := 0; i < 2; i++ {
		if err := e.srv.deliverMessageWithNotify("x@y.z", []string{"bob@test.com"}, nil, data); err != nil {
			t.Fatal(err)
		}
	}
	n, files := e.diskBytes("bob@test.com")
	t.Logf("after 2 identical deliveries: quota=%d disk=%d files=%d", e.quotaUsed("bob"), n, len(files))
	c := e.imapLogin("bob@test.com")
	out := c.imapCmd("a1", "SELECT INBOX")
	if !strings.Contains(out, "2 EXISTS") {
		t.Fatalf("want 2 messages: %s", out)
	}
	c.imapCmd("a2", "STORE 1:2 +FLAGS (\\Deleted)")
	c.imapCmd("a3", "EXPUNGE")
	n, files = e.diskBytes("bob@test.com")
	t.Logf("after expunge: quota=%d disk=%d files=%d", e.quotaUsed("bob"), n, len(files))
	if q := e.quotaUsed("bob"); q != n {
		t.Errorf("DIVERGE quota=%d disk=%d", q, n)
	}
}

func TestE2E_QueueRecoveryAcrossRestart(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "alice"})
	e.start()
	e.submit("alice@test.com", e.password, "alice@test.com", []string{"far@ext.example", "far2@ext.example"}, e2eMsg)
	time.Sleep(200 * time.Millisecond)
	before := e.pending()
	t.Logf("before restart: %d entries", len(before))
	if len(before) == 0 {
		t.Fatalf("submission to external recipient not queued")
	}
	e.stop()
	e.start()
	defer e.stop()
	after := e.pending()
	t.Logf("after restart: %d entries", len(after))
	if len(after) != len(before) {
		t.Errorf("queue changed over restart: %d -> %d", len(before), len(after))
	}
	for _, p := range after {
		t.Logf("entry %s from=%s to=%v attempts=%d status=%v", p.ID, p.From, p.To, p.RetryCount, p.Status)
		if p.MessagePath != "" {
			if b, err := os.ReadFile(p.MessagePath); err != nil || !strings.Contains(string(b), "hello e2e") {
				t.Errorf("queued message body unreadable after restart: %v", err)
			}
		}
	}
}

func TestE2E_WebhookFiresOnDelivery(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "alice"}, &db.AccountData{LocalPart: "bob"})
	got := make(chan map[string]any, 10)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		t.Logf("webhook hdrs: %v", r.Header)
		got <- m
	}))
	defer hs.Close()
	e.start()
	defer e.stop()
	e.srv.webhookMgr.SetAllowPrivateIP(true)
	body, _ := json.Marshal(map[string]any{"url": hs.URL, "events": []string{"mail.received"}})
	rec := httptest.NewRecorder()
	e.srv.webhookMgr.HTTPHandler(rec, httptest.NewRequest("POST", "/api/v1/webhooks", bytes.NewReader(body)))
	if rec.Code != 201 {
		t.Fatalf("create hook: %d %s", rec.Code, rec.Body)
	}
	e.submit("alice@test.com", e.password, "alice@test.com", []string{"bob@test.com"}, e2eMsg)
	select {
	case m := <-got:
		t.Logf("payload: %v", m)
		d, _ := m["data"].(map[string]any)
		if d["to"] != "bob@test.com" || d["from"] != "alice@test.com" {
			t.Errorf("payload %v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("webhook not fired on local delivery")
	}
}

// F6149: submission listeners apply security.rate_limit.user_per_hour as the
// per-user rolling-hour recipient cap.
func TestE2E_SubmissionRecipientLimitWired(t *testing.T) {
	e := newE2E(t)
	e.cfg.Security.RateLimit.UserPerHour = 2
	e.seed(&db.AccountData{LocalPart: "alice"}, &db.AccountData{LocalPart: "bob"})
	e.start()
	defer e.stop()
	c := e2eDial(t, e.sub, false)
	c.expectSMTP(220)
	c.send("EHLO c.test")
	c.expectSMTP(250)
	c.send("STARTTLS")
	c.expectSMTP(220)
	tc := tls.Client(c.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	c.c, c.r = tc, bufio.NewReader(tc)
	c.send("EHLO c.test")
	c.expectSMTP(250)
	c.send("AUTH PLAIN %s", b64("\x00alice@test.com\x00"+e.password))
	c.expectSMTP(235)
	c.send("MAIL FROM:<alice@test.com>")
	c.expectSMTP(250)
	for i := 0; i < 2; i++ {
		c.send("RCPT TO:<bob@test.com>")
		c.expectSMTP(250)
	}
	c.send("RCPT TO:<bob@test.com>")
	if code, text := c.smtpReply(); code < 400 {
		t.Fatalf("third recipient accepted despite user_per_hour=2: %d %s", code, text)
	}
}

// Graceful shutdown while a DATA transaction is open: the client either gets
// 250 and the message exists exactly once after restart, or gets a failure and
// no message exists (never "250 but lost", never duplicated).
func TestE2E_ShutdownDuringDATA(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob"})
	e.start()
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
	c.send("%s", strings.ReplaceAll(strings.TrimRight(e2eMsg, "\n"), "\n", "\r\n"))
	stopped := make(chan struct{})
	go func() { e.stop(); close(stopped) }()
	time.Sleep(200 * time.Millisecond)
	c.send(".")
	code, text := func() (int, string) {
		defer func() { recover() }()
		_ = c.c.SetReadDeadline(time.Now().Add(10 * time.Second))
		l, err := c.r.ReadString('\n')
		if err != nil {
			return 0, "conn closed: " + err.Error()
		}
		var n int
		fmt.Sscanf(l, "%d", &n)
		return n, l
	}()
	t.Logf("reply to final dot: %d %q", code, text)
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("Stop did not return")
	}
	e.start()
	defer e.stop()
	n := e.mailboxCount("bob@test.com", "INBOX")
	t.Logf("INBOX after restart: %d", n)
	if code == 250 && n != 1 {
		t.Errorf("DATA acknowledged 250 but INBOX has %d messages", n)
	}
	if code != 250 && n != 0 {
		t.Errorf("DATA not acknowledged (%d) but INBOX has %d messages (duplicate on client retry)", code, n)
	}
}

func TestE2E_OverQuotaAllProtocols(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob", QuotaLimit: 700})
	e.start()
	defer e.stop()
	// SMTP: first fits (176 incl. Return-Path), second must be refused.
	e.inbound("s@ext.example", []string{"bob@test.com"}, e2eMsg)
	q1 := e.quotaUsed("bob")
	t.Logf("after first delivery quota=%d", q1)
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
	c.send("%s\r\n.", strings.ReplaceAll(strings.Replace(strings.TrimRight(e2eMsg, "\n"), "e2e1", "e2e9", 1), "\n", "\r\n"))
	code, text := c.smtpReply()
	t.Logf("over-quota SMTP reply: %d %s", code, text)
	if code < 400 {
		t.Errorf("over-quota delivery acknowledged: %d", code)
	}
	if q := e.quotaUsed("bob"); q != q1 {
		t.Errorf("quota changed by refused delivery: %d -> %d", q1, q)
	}
	// IMAP APPEND
	ic := e.imapLogin("bob@test.com")
	big := "From: x@y.z\r\nSubject: big\r\n\r\n" + strings.Repeat("x", 400) + "\r\n"
	out := ic.imapCmd("a1", fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(big), big))
	t.Log(out)
	if strings.Contains(out, "a1 OK") {
		t.Errorf("IMAP APPEND over quota accepted")
	}
	// JMAP upload is refused over quota (413 overQuota)
	tok := e.jmapToken("bob@test.com")
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/jmap/upload/bob@test.com", e.jmap), strings.NewReader(big))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "message/rfc822")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("JMAP upload over quota status %d", resp.StatusCode)
	}
	if q := e.quotaUsed("bob"); q != q1 {
		t.Errorf("quota drifted after refused ops: %d want %d", q, q1)
	}
	n, files := e.diskBytes("bob@test.com")
	t.Logf("disk=%d files=%v quota=%d", n, files, e.quotaUsed("bob"))
}

// One recipient over quota must not make the whole transaction fail with a
// retryable 451 (which would duplicate the message for the other recipient).
func TestE2E_PartialRecipientFailureNoDuplicate(t *testing.T) {
	e := newE2E(t)
	e.seed(&db.AccountData{LocalPart: "bob"}, &db.AccountData{LocalPart: "carol", QuotaLimit: 10})
	e.start()
	defer e.stop()
	c := e2eDial(t, e.smtp, false)
	c.expectSMTP(220)
	c.send("EHLO mx.test")
	c.expectSMTP(250)
	c.send("MAIL FROM:<s@ext.example>")
	c.expectSMTP(250)
	for _, r := range []string{"bob@test.com", "carol@test.com"} {
		c.send("RCPT TO:<%s>", r)
		c.expectSMTP(250)
	}
	c.send("DATA")
	c.expectSMTP(354)
	c.send("%s\r\n.", strings.ReplaceAll(strings.TrimRight(e2eMsg, "\n"), "\n", "\r\n"))
	code, text := c.smtpReply()
	t.Logf("reply: %d %s", code, text)
	time.Sleep(300 * time.Millisecond)
	n := e.mailboxCount("bob@test.com", "INBOX")
	t.Logf("bob INBOX=%d", n)
	if code >= 400 && code < 500 && n == 1 {
		t.Errorf("DUPLICATE RISK: 4xx reply (%d) after bob already received the message", code)
	}
	for _, p := range e.pending() {
		t.Logf("queued: from=%q to=%v", p.From, p.To)
	}
}
