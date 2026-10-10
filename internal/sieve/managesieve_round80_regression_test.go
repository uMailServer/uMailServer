package sieve

// Regression tests for audit findings F5610–F5615 (ManageSieve quota, SASL
// decoding/state/authzid, TLS-before-auth, quoted scripts; see
// .temp_files/ledger_internal_sieve_manager_go.md, Round 80). Every test
// drives the real handleConn over net.Pipe and reuses the reg5460Conn helpers
// from managesieve_rfc5804_test.go.

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type reg80Srv struct {
	srv   *ManageSieveServer
	calls atomic.Int32
}

func reg80Dial(t *testing.T, mgr *Manager, tlsCfg *tls.Config, creds map[string]string) (*reg5460Conn, *reg80Srv) {
	t.Helper()
	if mgr == nil {
		mgr = NewManager()
	}
	a := &reg80Srv{srv: NewManageSieveServer(mgr, tlsCfg)}
	a.srv.SetAuthHandler(func(u, p string) bool {
		a.calls.Add(1)
		want, ok := creds[u]
		return ok && want == p
	})
	client, server := net.Pipe()
	a.srv.wg.Add(1)
	go a.srv.handleConn(server)
	_ = client.SetDeadline(time.Now().Add(20 * time.Second))
	c := &reg5460Conn{t: t, mgr: mgr, client: client, r: bufio.NewReader(client)}
	c.greet = c.block()
	if len(c.greet) == 0 || !strings.HasPrefix(c.greet[len(c.greet)-1], "OK") {
		t.Fatalf("INVALID greeting %q", c.greet)
	}
	return c, a
}

var reg80Creds = map[string]string{"user": "pass", "other": "pw2", "u@example.com": "p@ss!"}

func reg80Plain(authz, authc, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(authz + "\x00" + authc + "\x00" + pass))
}

// line sends payload and returns the next single response line (SASL
// challenges are not OK/NO terminated).
func reg80Line(c *reg5460Conn, payload string) string {
	c.write(payload)
	l, err := c.r.ReadString('\n')
	if err != nil {
		return "ERR:" + err.Error()
	}
	return strings.TrimRight(l, "\r\n")
}

func reg80Big(n int) string {
	// A valid script of exactly n octets: one comment line.
	return "#" + strings.Repeat("x", n-2) + "\n"
}

// ---------- F5610: per-user script quota ----------

func TestF5610_Control(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	for i := 0; i < 150; i++ {
		if got := c.put("same", "keep;"); !strings.HasPrefix(got, "OK") {
			t.Fatalf("INVALID control: replace #%d -> %q", i, got)
		}
	}
	t.Logf("CONTROL EXPECTED: 150 replacements of one name OK, 1 script | ACTUAL: %d scripts\n", len(c.mgr.ListScripts("user")))
	if len(c.mgr.ListScripts("user")) != 1 {
		t.Fatal("INVALID control")
	}
}

func TestF5610_Regression(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	var last string
	for i := 0; i < 101; i++ {
		last = c.put(fmt.Sprintf("s%d", i), "keep;")
	}
	count := len(c.mgr.ListScripts("user"))

	d, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer d.client.Close()
	d.auth()
	var lastBig string
	for i := 0; i < 11; i++ {
		lastBig = d.put(fmt.Sprintf("b%d", i), reg80Big(maxManageSieveScriptSize))
	}
	total := 0
	for _, n := range d.mgr.ListScripts("user") {
		total += len(d.mgr.GetScriptSource("user", n))
	}
	reg5460Defect(t, "F5610",
		strings.HasPrefix(last, "NO (QUOTA/MAXSCRIPTS)") && count == 100 &&
			strings.HasPrefix(lastBig, "NO (QUOTA)") && total <= 10*1024*1024,
		"101st distinct script -> NO (QUOTA/MAXSCRIPTS), 100 kept; 11th 1 MiB script -> NO (QUOTA), <= 10 MiB kept",
		[]any{last, fmt.Sprint("count=", count), lastBig, fmt.Sprint("bytes=", total)})
}

func TestF5610_Edges(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	for i := 0; i < 100; i++ {
		if got := c.put(fmt.Sprintf("s%d", i), "keep;"); !strings.HasPrefix(got, "OK") {
			t.Fatalf("put #%d: %q", i, got)
		}
	}
	// At the limit: replacing an existing script still works.
	if got := c.put("s5", "discard;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("replace at limit: %q", got)
	}
	// HAVESPACE reports the count quota for a new name, not for an existing one.
	if got := c.last("HAVESPACE \"new\" 10\r\n"); !strings.HasPrefix(got, "NO (QUOTA/MAXSCRIPTS)") {
		t.Fatalf("HAVESPACE new at limit: %q", got)
	}
	if got := c.last("HAVESPACE \"s5\" 10\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("HAVESPACE existing at limit: %q", got)
	}
	// Session continues after the quota NO; RENAMESCRIPT does not add a script.
	if got := c.last("RENAMESCRIPT \"s1\" \"r1\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("rename at limit: %q", got)
	}
	// Deleting one frees a slot.
	if got := c.last("DELETESCRIPT \"s0\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("delete: %q", got)
	}
	if got := c.put("fresh", "keep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("put after delete: %q", got)
	}
	// Quotas are per user.
	o, _ := reg80Dial(t, c.mgr, nil, reg80Creds)
	defer o.client.Close()
	if got := o.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("", "other", "pw2") + "\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("other auth: %q", got)
	}
	if got := o.put("x", "keep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("other user put: %q", got)
	}

	// Storage boundary: exactly 10 x 1 MiB fits; an 11th does not; replacing
	// one at the limit with the same size fits (the old copy is not counted).
	d, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer d.client.Close()
	d.auth()
	big := reg80Big(maxManageSieveScriptSize)
	for i := 0; i < 10; i++ {
		if got := d.put(fmt.Sprintf("b%d", i), big); !strings.HasPrefix(got, "OK") {
			t.Fatalf("big #%d: %q", i, got)
		}
	}
	if got := d.last(fmt.Sprintf("HAVESPACE \"b10\" %d\r\n", 1)); !strings.HasPrefix(got, "NO (QUOTA)") {
		t.Fatalf("HAVESPACE over storage: %q", got)
	}
	if got := d.put("b10", "keep;"); !strings.HasPrefix(got, "NO (QUOTA)") {
		t.Fatalf("put over storage: %q", got)
	}
	if got := d.put("b3", big); !strings.HasPrefix(got, "OK") {
		t.Fatalf("replace at storage limit: %q", got)
	}
	if got := d.put("b3", "keep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("shrink: %q", got)
	}
	if got := d.put("b10", "keep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("put after shrink: %q", got)
	}
	// The Manager API (not ManageSieve) is unchanged.
	m := NewManager()
	for i := 0; i < 101; i++ {
		if err := m.StoreScript("api", fmt.Sprintf("s%d", i), "keep;"); err != nil {
			t.Fatalf("API StoreScript #%d: %v", i, err)
		}
	}
}

// ---------- F5611: strict base64 SASL decoding ----------

func TestF5611_Control(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	reg80Line(c, "AUTHENTICATE \"LOGIN\"\r\n")
	reg80Line(c, "\""+base64.StdEncoding.EncodeToString([]byte("u@example.com"))+"\"\r\n")
	got := reg80Line(c, "\""+base64.StdEncoding.EncodeToString([]byte("p@ss!"))+"\"\r\n")
	t.Logf("CONTROL EXPECTED: base64 LOGIN OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5611_Regression(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	ch1 := reg80Line(c, "AUTHENTICATE \"LOGIN\"\r\n")
	ch2 := reg80Line(c, "\"u@example.com\"\r\n")
	got := ch2
	if !strings.HasPrefix(ch2, "NO") {
		got = reg80Line(c, "\"p@ss!\"\r\n")
	}
	reg5460Defect(t, "F5611", strings.HasPrefix(got, "NO"),
		"raw (non-base64) LOGIN responses -> NO", []any{ch1, ch2, got})
}

func TestF5611_Edges(t *testing.T) {
	// Invalid base64 in a PLAIN initial response.
	c, a := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	if got := c.last("AUTHENTICATE \"PLAIN\" \"!!!\"\r\n"); !strings.HasPrefix(got, "NO") || a.calls.Load() != 0 {
		t.Fatalf("PLAIN invalid base64: %q calls=%d", got, a.calls.Load())
	}
	// Unpadded base64 is not base64 per RFC 4648 §3.2 (used by SASL).
	d, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer d.client.Close()
	unpadded := strings.TrimRight(reg80Plain("", "user", "pass"), "=")
	if unpadded == reg80Plain("", "user", "pass") {
		t.Fatal("INVALID edge: credentials need padding")
	}
	if got := d.last("AUTHENTICATE \"PLAIN\" \"" + unpadded + "\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("PLAIN unpadded: %q", got)
	}
	// Empty decodes to empty; valid base64 still decodes.
	if b, err := decodeBase64(""); err != nil || len(b) != 0 {
		t.Fatalf("decodeBase64 empty: %q %v", b, err)
	}
	if b, err := decodeBase64("dGVzdA=="); err != nil || string(b) != "test" {
		t.Fatalf("decodeBase64 valid: %q %v", b, err)
	}
	if _, err := decodeBase64("not-valid-base64!"); err == nil {
		t.Fatal("decodeBase64 invalid: no error")
	}
}

// ---------- F5612: AUTHENTICATE in authenticated state ----------

func TestF5612_Control(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("", "user", "pass") + "\"\r\n")
	t.Logf("CONTROL EXPECTED: first AUTHENTICATE OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5612_Regression(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("", "other", "pw2") + "\"\r\n")
	put := c.put("after", "keep;")
	_, asUser := c.mgr.scriptSource("user", "after")
	_, asOther := c.mgr.scriptSource("other", "after")
	reg5460Defect(t, "F5612", strings.HasPrefix(got, "NO") && strings.HasPrefix(put, "OK") && asUser && !asOther,
		"second AUTHENTICATE -> NO, session stays \"user\"",
		[]any{got, put, fmt.Sprintf("stored as user=%v other=%v", asUser, asOther)})
}

func TestF5612_Edges(t *testing.T) {
	c, a := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	before := a.calls.Load()
	// LOGIN in authenticated state: refused before any challenge is sent.
	if b := reg80Line(c, "AUTHENTICATE \"LOGIN\"\r\n"); !strings.HasPrefix(b, "NO") {
		t.Fatalf("LOGIN after auth: %q", b)
	}
	// Wrong credentials in authenticated state: NO, session kept.
	if got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("", "user", "bad") + "\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("bad re-auth: %q", got)
	}
	if a.calls.Load() != before {
		t.Fatalf("auth handler called in authenticated state: %d -> %d", before, a.calls.Load())
	}
	if got := c.last("NOOP\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("session after refused re-auth: %q", got)
	}
	if got := c.put("k", "keep;"); !strings.HasPrefix(got, "OK") || c.mgr.GetScriptSource("user", "k") != "keep;" {
		t.Fatalf("still user: %q", got)
	}
}

// ---------- F5613: PLAIN authorization identity ----------

func TestF5613_Control(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("user", "user", "pass") + "\"\r\n")
	t.Logf("CONTROL EXPECTED: authzid == authcid OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5613_Regression(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("other", "user", "pass") + "\"\r\n")
	put := c.put("p", "keep;")
	_, asUser := c.mgr.scriptSource("user", "p")
	_, asOther := c.mgr.scriptSource("other", "p")
	t.Logf("F5613 identity check: stored as user=%v other=%v (authzid ignored, no impersonation)\n", asUser, asOther)
	reg5460Defect(t, "F5613", strings.HasPrefix(got, "NO"),
		"authzid \"other\" != authcid \"user\" (no proxy authorization) -> NO",
		[]any{got, put})
}

func TestF5613_Edges(t *testing.T) {
	for _, tc := range []struct{ authz, want string }{
		{"", "OK"}, {"user", "OK"}, {"USER", "OK"}, {"other", "NO"}, {"user\x00x", "NO"},
	} {
		c, _ := reg80Dial(t, nil, nil, reg80Creds)
		got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain(tc.authz, "user", "pass") + "\"\r\n")
		_ = c.client.Close() // test teardown; close error irrelevant
		if !strings.HasPrefix(got, tc.want) {
			t.Fatalf("authzid %q: %q, want %s", tc.authz, got, tc.want)
		}
	}
}

// ---------- F5614: cleartext SASL before STARTTLS ----------

func TestF5614_Control(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("", "user", "pass") + "\"\r\n")
	t.Logf("CONTROL EXPECTED: no TLS configured -> PLAIN OK | ACTUAL: %q greet=%q\n", got, c.greet)
	if !strings.HasPrefix(got, "OK") || !reg5460Has(c.greet, `"SASL" "PLAIN LOGIN"`) {
		t.Fatal("INVALID control")
	}
}

func TestF5614_Regression(t *testing.T) {
	srvCfg, _ := reg5460TLSConfigs(t)
	c, a := reg80Dial(t, nil, srvCfg, reg80Creds)
	defer c.client.Close()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg80Plain("", "user", "pass") + "\"\r\n")
	reg5460Defect(t, "F5614",
		strings.HasPrefix(got, "NO (ENCRYPT-NEEDED)") && !reg5460Has(c.greet, `"SASL" "PLAIN`) && a.calls.Load() == 0,
		`TLS available, plain conn: greeting "SASL" "" + STARTTLS; AUTHENTICATE PLAIN -> NO (ENCRYPT-NEEDED), password not checked`,
		[]any{c.greet, got, fmt.Sprint("authHandler calls=", a.calls.Load())})
}

func TestF5614_Edges(t *testing.T) {
	srvCfg, cliCfg := reg5460TLSConfigs(t)
	c, a := reg80Dial(t, nil, srvCfg, reg80Creds)
	defer c.client.Close()
	if !reg5460Has(c.greet, `"SASL" ""`) || !reg5460Has(c.greet, `"STARTTLS"`) {
		t.Fatalf("greeting: %q", c.greet)
	}
	// LOGIN is refused before any challenge, so no password is solicited.
	if b := reg80Line(c, "AUTHENTICATE \"LOGIN\"\r\n"); !strings.HasPrefix(b, "NO (ENCRYPT-NEEDED)") {
		t.Fatalf("LOGIN before TLS: %q", b)
	}
	if got := c.last("LISTSCRIPTS\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("LISTSCRIPTS unauthenticated: %q", got)
	}
	if got := c.last("STARTTLS\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("STARTTLS: %q", got)
	}
	tc := tls.Client(c.client, cliCfg)
	if err := tc.Handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	c.client = tc
	c.r = bufio.NewReader(tc)
	caps := c.block()
	if !reg5460Has(caps, `"SASL" "PLAIN LOGIN"`) || reg5460Has(caps, `"STARTTLS"`) {
		t.Fatalf("post-TLS caps: %q", caps)
	}
	if a.calls.Load() != 0 {
		t.Fatalf("auth handler called before TLS: %d", a.calls.Load())
	}
	c.auth()
	if got := c.put("s", "keep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("PUTSCRIPT over TLS: %q", got)
	}
}

// ---------- F5615: script as a quoted string ----------

func TestF5615_Control(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	got := c.put("lit", "keep;")
	t.Logf("CONTROL EXPECTED: literal PUTSCRIPT OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5615_Regression(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	got := c.last("PUTSCRIPT \"q\" \"keep;\"\r\n")
	after := c.last("NOOP\r\n")
	src, ok := c.mgr.scriptSource("user", "q")
	reg5460Defect(t, "F5615", strings.HasPrefix(got, "OK") && ok && src == "keep;" && strings.HasPrefix(after, "OK"),
		`PUTSCRIPT "q" "keep;" -> OK, stored "keep;", session open`, []any{got, after, src})
}

func TestF5615_Edges(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	if got := c.last("CHECKSCRIPT \"keep;\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("CHECKSCRIPT quoted: %q", got)
	}
	if got := c.last("CHECKSCRIPT \"keep\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("CHECKSCRIPT quoted invalid: %q", got)
	}
	if got := c.last(`PUTSCRIPT "e" "require \"fileinto\"; fileinto \"A B\";"` + "\r\n"); !strings.HasPrefix(got, "OK") ||
		c.mgr.GetScriptSource("user", "e") != `require "fileinto"; fileinto "A B";` {
		t.Fatalf("escaped quoted script: %q %q", got, c.mgr.GetScriptSource("user", "e"))
	}
	if got := c.last("PUTSCRIPT \"empty\" \"\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("empty quoted script: %q", got)
	}
	if _, ok := c.mgr.scriptSource("user", "empty"); !ok {
		t.Fatal("empty quoted script not stored")
	}
	// Unterminated / escaped-final quote: NO, session continues.
	if got := c.last("PUTSCRIPT \"u\" \"keep;\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("unterminated: %q", got)
	}
	if got := c.last(`PUTSCRIPT "u" "keep;\"` + "\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("escaped final quote: %q", got)
	}
	if got := c.last("NOOP\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("session after bad quoted: %q", got)
	}
	// Literal form still works and keeps the stream in sync.
	if got := c.put("lit", "discard;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("literal after quoted: %q", got)
	}
}
