package sieve

import (
	"net"
	"sort"
	"testing"
	"time"
)

// F6544: RFC 5228 §2.7.2 — header tests compare the RFC 2047-decoded value.
func TestHeaderTestDecodesEncodedWords_F6544(t *testing.T) {
	script := `require "fileinto";
if header :contains "subject" "Café" { fileinto "Hit"; }`
	msg := &MessageContext{
		From: "a@b.c", To: []string{"u@x.y"},
		Headers: map[string][]string{"Subject": {"=?UTF-8?B?Q2Fmw6k=?= au lait"}},
	}
	acts, err := ExecuteScript(script, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 {
		t.Fatalf("actions: %#v", acts)
	}
	if f, ok := acts[0].(FileintoAction); !ok || f.Folder != "Hit" {
		t.Fatalf("encoded-word subject not decoded for header test: %#v", acts)
	}
}

// F6545: LISTSCRIPTS output must be deterministic.
func TestListScriptsSorted_F6545(t *testing.T) {
	m := NewManager()
	for _, n := range []string{"zeta", "alpha", "mid", "beta"} {
		if err := m.StoreScript("u", n, "keep;"); err != nil {
			t.Fatal(err)
		}
	}
	got := m.ListScripts("u")
	if !sort.StringsAreSorted(got) {
		t.Fatalf("ListScripts not sorted: %v", got)
	}
}

// F6546: ManageSieve caps concurrent connections.
func TestManageSieveConnectionCap_F6546(t *testing.T) {
	srv := NewManageSieveServer(NewManager(), nil)
	srv.SetListenAddrs("127.0.0.1:0", "")
	srv.SetMaxConnections(2)
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 1)
		if _, err := c.Read(buf); err != nil {
			t.Fatalf("conn %d greeting: %v", i, err)
		}
	}
	c, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	if n < 2 || string(buf[:2]) != "NO" {
		t.Fatalf("third connection not refused: %q", buf[:n])
	}
}
