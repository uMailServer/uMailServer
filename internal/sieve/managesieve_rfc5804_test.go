package sieve

// Regression tests for audit findings F5460–F5467 (RFC 5804 ManageSieve
// conformance; see .temp_files/ledger_internal_sieve_manager_go.md, Round 65).
// Every test drives the real handleConn over net.Pipe.

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

type reg5460Conn struct {
	t      *testing.T
	mgr    *Manager
	client net.Conn
	r      *bufio.Reader
	greet  []string
}

func reg5460TLSConfigs(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	srv := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	cli := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: "localhost"}
	return srv, cli
}

func reg5460Dial(t *testing.T, tlsCfg *tls.Config) *reg5460Conn {
	t.Helper()
	mgr := NewManager()
	srv := NewManageSieveServer(mgr, tlsCfg)
	srv.SetAuthHandler(func(u, p string) bool { return u == "user" && p == "pass" })
	client, server := net.Pipe()
	srv.wg.Add(1)
	go srv.handleConn(server)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	c := &reg5460Conn{t: t, mgr: mgr, client: client, r: bufio.NewReader(client)}
	c.greet = c.block()
	if len(c.greet) == 0 || !strings.HasPrefix(c.greet[len(c.greet)-1], "OK") {
		t.Fatalf("INVALID greeting %q", c.greet)
	}
	return c
}

// block reads response lines up to and including the OK/NO/BYE line.
func (c *reg5460Conn) block() []string {
	var out []string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return append(out, "ERR:"+err.Error())
		}
		line = strings.TrimRight(line, "\r\n")
		out = append(out, line)
		if strings.HasPrefix(line, "OK") || strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "BYE") {
			return out
		}
	}
}

func (c *reg5460Conn) write(payload string) {
	go func() { _, _ = c.client.Write([]byte(payload)) }()
}

// cmd sends payload and returns the response block.
func (c *reg5460Conn) cmd(payload string) []string {
	c.write(payload)
	return c.block()
}

// last returns the final line of the response to payload.
func (c *reg5460Conn) last(payload string) string {
	b := c.cmd(payload)
	return b[len(b)-1]
}

var reg5460Creds = base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))

func (c *reg5460Conn) auth() {
	if got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg5460Creds + "\"\r\n"); !strings.HasPrefix(got, "OK") {
		c.t.Fatalf("INVALID auth %q", got)
	}
}

func (c *reg5460Conn) put(name, script string) string {
	return c.last(fmt.Sprintf("PUTSCRIPT \"%s\" {%d+}\r\n%s\r\n", name, len(script), script))
}

// readManageSieveGreeting reads the capability greeting up to its final
// OK/NO/BYE line and returns that line (F5460: the greeting is multi-line).
func readManageSieveGreeting(r *bufio.Reader) (string, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return line, err
		}
		if strings.HasPrefix(line, "OK") || strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "BYE") {
			return line, nil
		}
	}
}

func reg5460Has(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func reg5460Defect(t *testing.T, id string, ok bool, expected string, actual any) {
	t.Helper()
	t.Logf("%s EXPECTED: %s\n%s ACTUAL:   %q\n", id, expected, id, actual)
	if !ok {
		t.Fatalf("DEFECT %s", id)
	}
}

// ---------- F5460: CAPABILITY / capability greeting ----------

func TestF5460_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	got := c.last("NOOP\r\n")
	t.Logf("CONTROL EXPECTED: NOOP OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5460_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	g := c.greet
	capb := c.cmd("CAPABILITY\r\n")
	after := c.last("NOOP\r\n")
	ok := reg5460Has(g, `"IMPLEMENTATION" "`) && reg5460Has(g, `"SASL" "PLAIN`) && reg5460Has(g, `"SIEVE" "`) &&
		reg5460Has(g, `"VERSION" "1.0"`) && len(capb) == len(g) && strings.HasPrefix(capb[len(capb)-1], "OK") &&
		strings.HasPrefix(after, "OK")
	reg5460Defect(t, "F5460", ok, `greeting + CAPABILITY list IMPLEMENTATION/SASL/SIEVE/VERSION then OK; session stays open`,
		[]any{g, capb, after})
}

func TestF5460_Edges(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	var sieveLine string
	for _, l := range c.greet {
		if strings.HasPrefix(l, `"SIEVE" `) {
			sieveLine = l
		}
	}
	for ext := range supportedExtensions {
		if !strings.Contains(sieveLine, ext) {
			t.Fatalf("SIEVE capability %q lacks %q", sieveLine, ext)
		}
	}
	if strings.Contains(sieveLine, "variables") {
		t.Fatalf("SIEVE advertises unsupported variables: %q", sieveLine)
	}
	if reg5460Has(c.greet, `"STARTTLS"`) {
		t.Fatalf("STARTTLS advertised without TLS config: %q", c.greet)
	}
	c.auth()
	b := c.cmd("capability\r\n") // case-insensitive, also after AUTHENTICATE
	if len(b) != len(c.greet) || !strings.HasPrefix(b[len(b)-1], "OK") {
		t.Fatalf("CAPABILITY after auth: %q", b)
	}
	if got := c.last("CAPABILITY extra\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("CAPABILITY with argument: %q", got)
	}
}

// ---------- F5461: STARTTLS ----------

func TestF5461_Control(t *testing.T) {
	srvCfg, _ := reg5460TLSConfigs(t)
	c := reg5460Dial(t, srvCfg)
	defer c.client.Close()
	got := c.last("NOOP\r\n")
	t.Logf("CONTROL EXPECTED: NOOP OK with TLS configured | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5461_Regression(t *testing.T) {
	srvCfg, cliCfg := reg5460TLSConfigs(t)
	c := reg5460Dial(t, srvCfg)
	defer c.client.Close()
	adv := reg5460Has(c.greet, `"STARTTLS"`)
	got := c.last("STARTTLS\r\n")
	handshake := "not attempted"
	if strings.HasPrefix(got, "OK") {
		tc := tls.Client(c.client, cliCfg)
		if err := tc.Handshake(); err != nil {
			handshake = err.Error()
		} else {
			handshake = "ok"
		}
	}
	reg5460Defect(t, "F5461", adv && strings.HasPrefix(got, "OK") && handshake == "ok",
		`greeting advertises "STARTTLS"; STARTTLS -> OK + TLS handshake`, []any{c.greet, got, handshake})
}

func TestF5461_Edges(t *testing.T) {
	srvCfg, cliCfg := reg5460TLSConfigs(t)
	c := reg5460Dial(t, srvCfg)
	defer c.client.Close()
	if got := c.last("STARTTLS\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("STARTTLS: %q", got)
	}
	tc := tls.Client(c.client, cliCfg)
	if err := tc.Handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	c.client = tc
	c.r = bufio.NewReader(tc)
	// RFC 5804 §2.2: capabilities are re-issued after TLS, without STARTTLS.
	caps := c.block()
	if !reg5460Has(caps, `"SASL" "PLAIN`) || reg5460Has(caps, `"STARTTLS"`) || !strings.HasPrefix(caps[len(caps)-1], "OK") {
		t.Fatalf("post-TLS capabilities: %q", caps)
	}
	if got := c.last("STARTTLS\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("second STARTTLS: %q", got)
	}
	c.auth()
	if got := c.put("s", "keep;"); !strings.HasPrefix(got, "OK") || c.mgr.GetScriptSource("user", "s") != "keep;" {
		t.Fatalf("PUTSCRIPT over TLS: %q", got)
	}

	// No TLS config: STARTTLS is refused with NO and the session continues.
	p := reg5460Dial(t, nil)
	defer p.client.Close()
	if got := p.last("STARTTLS\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("STARTTLS without TLS config: %q", got)
	}
	if got := p.last("NOOP\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("session after refused STARTTLS: %q", got)
	}

	// STARTTLS after AUTHENTICATE is not allowed (non-authenticated state only).
	q := reg5460Dial(t, srvCfg)
	defer q.client.Close()
	q.auth()
	if got := q.last("STARTTLS\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("STARTTLS after auth: %q", got)
	}
}

// ---------- F5462: NO responses keep the session; response codes ----------

func TestF5462_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	c.put("s", "keep;")
	b := c.cmd("GETSCRIPT \"s\"\r\n")
	t.Logf("CONTROL EXPECTED: GETSCRIPT existing -> literal + OK | ACTUAL: %q\n", b)
	if !strings.HasPrefix(b[len(b)-1], "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5462_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	missing := c.last("GETSCRIPT \"missing\"\r\n")
	next := c.last("NOOP\r\n")
	reg5460Defect(t, "F5462", strings.HasPrefix(missing, "NO (NONEXISTENT)") && strings.HasPrefix(next, "OK"),
		`GETSCRIPT missing -> NO (NONEXISTENT) "..."; next NOOP -> OK`, []string{missing, next})
}

func TestF5462_Edges(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	if got := c.last("BOGUS\r\n"); !strings.HasPrefix(got, "NO ") {
		t.Fatalf("unknown command: %q", got)
	}
	if got := c.last("LISTSCRIPTS\r\n"); !strings.HasPrefix(got, "NO ") {
		t.Fatalf("pre-auth LISTSCRIPTS: %q", got)
	}
	c.auth()
	// Invalid script: NO carries the parser error, session continues.
	got := c.put("bad", "fileinto \"A\" keep;")
	if !strings.HasPrefix(got, `NO "`) || !strings.Contains(got, "validation") {
		t.Fatalf("invalid PUTSCRIPT: %q", got)
	}
	if strings.ContainsAny(got[:len(got)-1], "\r\n") {
		t.Fatalf("NO text not single-line: %q", got)
	}
	if got := c.last("SETACTIVE \"nope\"\r\n"); !strings.HasPrefix(got, "NO (NONEXISTENT)") {
		t.Fatalf("SETACTIVE missing: %q", got)
	}
	if got := c.last("NOOP\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("session after NO: %q", got)
	}
	// Oversized literal: the octets cannot be skipped safely, so the
	// server answers NO and closes the connection (no desync).
	b := c.cmd("PUTSCRIPT \"big\" {2000000+}\r\n")
	if !strings.HasPrefix(b[len(b)-1], "NO") {
		t.Fatalf("oversized literal: %q", b)
	}
	if line, err := c.r.ReadString('\n'); err == nil {
		t.Fatalf("connection stayed open after oversized literal: %q", line)
	}

	// LOGOUT: OK then close, no trailing NO.
	d := reg5460Dial(t, nil)
	defer d.client.Close()
	if got := d.last("LOGOUT\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("LOGOUT: %q", got)
	}
	if line, err := d.r.ReadString('\n'); err == nil {
		t.Fatalf("data after LOGOUT: %q", line)
	}

	// Failed AUTHENTICATE still answers NO and closes (one attempt per connection).
	e := reg5460Dial(t, nil)
	defer e.client.Close()
	bad := base64.StdEncoding.EncodeToString([]byte("\x00user\x00wrong"))
	if got := e.last("AUTHENTICATE \"PLAIN\" \"" + bad + "\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("bad auth: %q", got)
	}
	if line, err := e.r.ReadString('\n'); err == nil {
		t.Fatalf("connection stayed open after failed auth: %q", line)
	}
}

// ---------- F5463: DELETESCRIPT of the active script ----------

func TestF5463_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	c.put("a", "keep;")
	c.put("b", "keep;")
	c.last("SETACTIVE \"a\"\r\n")
	got := c.last("DELETESCRIPT \"b\"\r\n")
	t.Logf("CONTROL EXPECTED: delete inactive -> OK, gone | ACTUAL: %q src=%q\n", got, c.mgr.GetScriptSource("user", "b"))
	if !strings.HasPrefix(got, "OK") || c.mgr.GetScriptSource("user", "b") != "" {
		t.Fatal("INVALID control")
	}
}

func TestF5463_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	c.put("a", "discard;")
	c.last("SETACTIVE \"a\"\r\n")
	got := c.last("DELETESCRIPT \"a\"\r\n")
	active := c.mgr.GetActiveScriptName("user")
	reg5460Defect(t, "F5463", strings.HasPrefix(got, "NO (ACTIVE)") && active == "a",
		`DELETESCRIPT active -> NO (ACTIVE), script stays active`, []string{got, "active=" + active})
}

func TestF5463_Edges(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	if got := c.last("DELETESCRIPT \"none\"\r\n"); !strings.HasPrefix(got, "NO (NONEXISTENT)") {
		t.Fatalf("delete missing: %q", got)
	}
	c.put("a", "keep;")
	c.last("SETACTIVE \"a\"\r\n")
	if got := c.last("DELETESCRIPT \"a\"\r\n"); !strings.HasPrefix(got, "NO (ACTIVE)") {
		t.Fatalf("delete active: %q", got)
	}
	if got := c.last("SETACTIVE \"\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("deactivate: %q", got)
	}
	if got := c.last("DELETESCRIPT \"a\"\r\n"); !strings.HasPrefix(got, "OK") || c.mgr.GetScriptSource("user", "a") != "" {
		t.Fatalf("delete after deactivate: %q", got)
	}
	// Manager.DeleteScript (non-protocol API) keeps its old semantics.
	m := NewManager()
	_ = m.SetActiveScript("u", "x", "keep;")
	m.DeleteScript("u", "x")
	if m.HasActiveScript("u") {
		t.Fatal("Manager.DeleteScript changed")
	}
}

// ---------- F5464: SETACTIVE "" deactivates ----------

func TestF5464_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	c.put("a", "keep;")
	got := c.last("SETACTIVE \"a\"\r\n")
	t.Logf("CONTROL EXPECTED: SETACTIVE a -> OK | ACTUAL: %q active=%q\n", got, c.mgr.GetActiveScriptName("user"))
	if !strings.HasPrefix(got, "OK") || c.mgr.GetActiveScriptName("user") != "a" {
		t.Fatal("INVALID control")
	}
}

func TestF5464_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	c.put("a", "discard;")
	c.last("SETACTIVE \"a\"\r\n")
	got := c.last("SETACTIVE \"\"\r\n")
	has := c.mgr.HasActiveScript("user")
	reg5460Defect(t, "F5464", strings.HasPrefix(got, "OK") && !has,
		`SETACTIVE "" -> OK, no active script`, fmt.Sprintf("%s hasActive=%v", got, has))
}

func TestF5464_Edges(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	if got := c.last("SETACTIVE \"\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("deactivate with nothing active/no scripts: %q", got)
	}
	c.put("a", "keep;")
	c.last("SETACTIVE \"a\"\r\n")
	if got := c.last("SETACTIVE \"nope\"\r\n"); !strings.HasPrefix(got, "NO") || c.mgr.GetActiveScriptName("user") != "a" {
		t.Fatalf("SETACTIVE missing changed active: %q", got)
	}
	if got := c.last("SETACTIVE \"\"\r\n"); !strings.HasPrefix(got, "OK") || c.mgr.HasActiveScript("user") {
		t.Fatalf("deactivate: %q", got)
	}
	if got := c.last("SETACTIVE \"\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("repeat deactivate: %q", got)
	}
	b := c.cmd("LISTSCRIPTS\r\n")
	if reg5460Has(b, `"a" ACTIVE`) || !reg5460Has(b, `"a"`) {
		t.Fatalf("LISTSCRIPTS after deactivate: %q", b)
	}
}

// ---------- F5465: HAVESPACE / RENAMESCRIPT ----------

func TestF5465_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	got := c.put("a", "keep;")
	t.Logf("CONTROL EXPECTED: PUTSCRIPT OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5465_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	c.put("a", "keep;")
	c.last("SETACTIVE \"a\"\r\n")
	hs := c.last("HAVESPACE \"b\" 100\r\n")
	rn := c.last("RENAMESCRIPT \"a\" \"b\"\r\n")
	active := c.mgr.GetActiveScriptName("user")
	reg5460Defect(t, "F5465", strings.HasPrefix(hs, "OK") && strings.HasPrefix(rn, "OK") && active == "b" &&
		c.mgr.GetScriptSource("user", "a") == "",
		`HAVESPACE -> OK; RENAMESCRIPT a b -> OK, active follows to b`, []string{hs, rn, "active=" + active})
}

func TestF5465_Edges(t *testing.T) {
	p := reg5460Dial(t, nil)
	defer p.client.Close()
	if got := p.last("HAVESPACE \"a\" 1\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("pre-auth HAVESPACE: %q", got)
	}
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	if got := c.last("HAVESPACE \"a\" 1048576\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("HAVESPACE at limit: %q", got)
	}
	if got := c.last("HAVESPACE \"a\" 1048577\r\n"); !strings.HasPrefix(got, "NO (QUOTA/MAXSIZE)") {
		t.Fatalf("HAVESPACE over limit: %q", got)
	}
	if got := c.last("HAVESPACE \"a\" x\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("HAVESPACE bad size: %q", got)
	}
	c.put("a", "keep;")
	c.put("b", "discard;")
	if got := c.last("RENAMESCRIPT \"a\" \"b\"\r\n"); !strings.HasPrefix(got, "NO (ALREADYEXISTS)") || c.mgr.GetScriptSource("user", "b") != "discard;" {
		t.Fatalf("rename onto existing: %q", got)
	}
	if got := c.last("RENAMESCRIPT \"zz\" \"c\"\r\n"); !strings.HasPrefix(got, "NO (NONEXISTENT)") {
		t.Fatalf("rename missing: %q", got)
	}
	if got := c.last("RENAMESCRIPT \"a\" \"c\"\r\n"); !strings.HasPrefix(got, "OK") || c.mgr.GetScriptSource("user", "c") != "keep;" || c.mgr.HasActiveScript("user") {
		t.Fatalf("rename inactive: %q", got)
	}
	if got := c.last("RENAMESCRIPT \"c\" \"c\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("rename onto itself: %q", got)
	}
	if got := c.last("RENAMESCRIPT \"c\" \"\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("rename to empty: %q", got)
	}
	if got := c.last("NOOP\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("session: %q", got)
	}
}

// ---------- F5466: AUTHENTICATE continuation format ----------

func TestF5466_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	got := c.last("AUTHENTICATE \"PLAIN\" \"" + reg5460Creds + "\"\r\n")
	t.Logf("CONTROL EXPECTED: PLAIN with initial response -> OK | ACTUAL: %q\n", got)
	if !strings.HasPrefix(got, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5466_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.write("AUTHENTICATE \"PLAIN\"\r\n")
	cont, _ := c.r.ReadString('\n')
	cont = strings.TrimRight(cont, "\r\n")
	reg5460Defect(t, "F5466", cont == `""`,
		`PLAIN without initial response -> empty server-challenge string ""`, cont)
}

func TestF5466_Edges(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.write("AUTHENTICATE \"PLAIN\"\r\n")
	if cont, _ := c.r.ReadString('\n'); cont != "\"\"\r\n" {
		t.Fatalf("PLAIN challenge %q", cont)
	}
	if got := c.last("\"" + reg5460Creds + "\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("PLAIN quoted response: %q", got)
	}

	l := reg5460Dial(t, nil)
	defer l.client.Close()
	l.write("AUTHENTICATE \"LOGIN\"\r\n")
	if ch, _ := l.r.ReadString('\n'); ch != "\"VXNlcm5hbWU6\"\r\n" {
		t.Fatalf("LOGIN username challenge %q", ch)
	}
	l.write("\"" + base64.StdEncoding.EncodeToString([]byte("user")) + "\"\r\n")
	if ch, _ := l.r.ReadString('\n'); ch != "\"UGFzc3dvcmQ6\"\r\n" {
		t.Fatalf("LOGIN password challenge %q", ch)
	}
	if got := l.last("\"" + base64.StdEncoding.EncodeToString([]byte("pass")) + "\"\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("LOGIN result %q", got)
	}

	// "*" cancels: NO, session stays open and a fresh AUTHENTICATE works.
	x := reg5460Dial(t, nil)
	defer x.client.Close()
	x.write("AUTHENTICATE \"PLAIN\"\r\n")
	_, _ = x.r.ReadString('\n')
	if got := x.last("\"*\"\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("cancel: %q", got)
	}
	x.auth()

	// Unquoted legacy response still accepted.
	y := reg5460Dial(t, nil)
	defer y.client.Close()
	y.write("AUTHENTICATE PLAIN\r\n")
	_, _ = y.r.ReadString('\n')
	if got := y.last(reg5460Creds + "\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("legacy unquoted response: %q", got)
	}
}

// ---------- F5467: empty script literal {0+} ----------

func TestF5467_Control(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	got2 := c.put("two", "keep;")
	t.Logf("CONTROL EXPECTED: 5-octet literal OK | ACTUAL: %q\n", got2)
	if !strings.HasPrefix(got2, "OK") {
		t.Fatal("INVALID control")
	}
}

func TestF5467_Regression(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	got := c.last("PUTSCRIPT \"empty\" {0+}\r\n\r\n")
	names := c.mgr.ListScripts("user")
	reg5460Defect(t, "F5467", strings.HasPrefix(got, "OK") && len(names) == 1,
		`PUTSCRIPT "empty" {0+} (empty script is valid Sieve) -> OK, stored`, []any{got, names})
}

func TestF5467_Edges(t *testing.T) {
	c := reg5460Dial(t, nil)
	defer c.client.Close()
	c.auth()
	if got := c.last("CHECKSCRIPT {0+}\r\n\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("CHECKSCRIPT empty: %q", got)
	}
	if got := c.last("PUTSCRIPT \"e\" {0}\r\n\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("PUTSCRIPT {0}: %q", got)
	}
	if got := c.last("NOOP\r\n"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("stream desynced after empty literal: %q", got)
	}
	if got := c.last("PUTSCRIPT \"x\" abc\r\n"); !strings.HasPrefix(got, "NO") || c.mgr.GetScriptSource("user", "x") != "" {
		t.Fatalf("non-numeric size accepted: %q", got)
	}
	d := reg5460Dial(t, nil)
	defer d.client.Close()
	d.auth()
	if got := d.last("PUTSCRIPT \"x\" {-1+}\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("negative size: %q", got)
	}
	f := reg5460Dial(t, nil)
	defer f.client.Close()
	f.auth()
	if got := f.last("PUTSCRIPT \"k\" 5\r\nkeep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("legacy bare count: %q", got)
	}
}
