package sieve

// Regression tests for audit finding F5040 (see .temp_files/ledger_internal_sieve.md, Round 22).

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

type regF5040Conn struct {
	mgr    *Manager
	client net.Conn
	r      *bufio.Reader
}

func regF5040Dial(t *testing.T) *regF5040Conn {
	t.Helper()
	mgr := NewManager()
	srv := NewManageSieveServer(mgr, nil)
	srv.SetAuthHandler(func(u, p string) bool { return u == "user" && p == "pass" })
	client, server := net.Pipe()
	srv.wg.Add(1)
	go srv.handleConn(server)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	c := &regF5040Conn{mgr: mgr, client: client, r: bufio.NewReader(client)}
	if line, err := readManageSieveGreeting(c.r); err != nil || !strings.HasPrefix(line, "OK") {
		t.Fatalf("INVALID greeting %q %v", line, err)
	}
	return c
}

// send writes payload and returns the next response line ("" on error).
func (c *regF5040Conn) send(payload string) string {
	go func() { _, _ = c.client.Write([]byte(payload)) }()
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "ERR:" + err.Error()
	}
	return strings.TrimSpace(line)
}

var regF5040Creds = base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))

func TestF5040_Control(t *testing.T) {
	c := regF5040Dial(t)
	defer c.client.Close()
	a := c.send("AUTHENTICATE PLAIN\r\n")
	b := c.send(regF5040Creds + "\r\n")
	p := c.send("PUTSCRIPT foo 5\r\nkeep;")
	t.Logf("CONTROL EXPECTED: legacy unquoted flow OK | ACTUAL: %q %q %q\n", a, b, p)
	if !strings.HasPrefix(b, "OK") || !strings.HasPrefix(p, "OK") || c.mgr.GetScriptSource("user", "foo") != "keep;" {
		t.Fatal("INVALID control")
	}
}

func TestF5040_Regression(t *testing.T) {
	c := regF5040Dial(t)
	defer c.client.Close()
	a := c.send(`AUTHENTICATE "PLAIN" "` + regF5040Creds + "\"\r\n")
	p := c.send("PUTSCRIPT \"foo\" {5+}\r\nkeep;\r\n")
	n := c.send("NOOP\r\n")
	src := c.mgr.GetScriptSource("user", "foo")
	t.Logf("EXPECTED: RFC 5804 client syntax accepted (auth OK, PUTSCRIPT OK, NOOP OK, script \"foo\"=keep;) | ACTUAL: auth=%q put=%q noop=%q script=%q\n", a, p, n, src)
	if !strings.HasPrefix(a, "OK") || !strings.HasPrefix(p, "OK") || !strings.HasPrefix(n, "OK") || src != "keep;" {
		t.Fatal("DEFECT F5040: ManageSieve rejects RFC 5804 quoted strings / literals / SASL initial response")
	}
}

func TestF5040_Edges(t *testing.T) {
	c := regF5040Dial(t)
	defer c.client.Close()
	if a := c.send(`AUTHENTICATE "PLAIN" "` + regF5040Creds + "\"\r\n"); !strings.HasPrefix(a, "OK") {
		t.Fatalf("auth %q", a)
	}
	// synchronising-literal form {N} and escaped quote in name.
	if p := c.send("PUTSCRIPT \"a\\\"b\" {5}\r\nkeep;\r\n"); !strings.HasPrefix(p, "OK") {
		t.Fatalf("put {N} %q", p)
	}
	if src := c.mgr.GetScriptSource("user", `a"b`); src != "keep;" {
		t.Fatalf("escaped name stored as? src=%q", src)
	}
	// CHECKSCRIPT with a literal, followed by a pipelined command.
	if p := c.send("CHECKSCRIPT {5+}\r\nkeep;\r\n"); !strings.HasPrefix(p, "OK") {
		t.Fatalf("check %q", p)
	}
	if p := c.send("SETACTIVE \"a\\\"b\"\r\n"); !strings.HasPrefix(p, "OK") {
		t.Fatalf("setactive %q", p)
	}
	if l := c.send("LISTSCRIPTS\r\n"); l != `"a\"b" ACTIVE` {
		t.Fatalf("listscripts first line %q", l)
	}
	if l, _ := c.r.ReadString('\n'); !strings.HasPrefix(l, "OK") {
		t.Fatalf("listscripts end %q", l)
	}
	if g := c.send("GETSCRIPT \"a\\\"b\"\r\n"); g != "{5}" {
		t.Fatalf("getscript %q", g)
	}
	if b, _ := c.r.ReadString('\n'); b != "keep;\r\n" {
		t.Fatalf("getscript body %q", b)
	}
	if b, _ := c.r.ReadString('\n'); !strings.HasPrefix(b, "OK") {
		t.Fatalf("getscript end %q", b)
	}
	if n := c.send("NOOP\r\n"); !strings.HasPrefix(n, "OK") {
		t.Fatalf("noop %q", n)
	}
	// Wrong credentials in an initial response still fail.
	d := regF5040Dial(t)
	defer d.client.Close()
	bad := base64.StdEncoding.EncodeToString([]byte("\x00user\x00nope"))
	if a := d.send(`AUTHENTICATE "PLAIN" "` + bad + "\"\r\n"); strings.HasPrefix(a, "OK") {
		t.Fatalf("bad creds accepted %q", a)
	}
	t.Log("EDGES OK")
}
