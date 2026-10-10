package imap

import (
	"bufio"
	"crypto/tls"
	"net"
	"strings"
	"testing"
)

func round82HasCap(line, name string) bool {
	for _, f := range strings.Fields(line) {
		if strings.EqualFold(strings.Trim(f, "[]"), name) {
			return true
		}
	}
	return false
}

// F5646: capabilities did not follow the session's transport. A session on
// the implicit-TLS listener advertised STARTTLS (a client following RFC 3501
// §6.2.1 would try nested TLS), a cleartext session advertised AUTH=PLAIN /
// AUTH=LOGIN and no LOGINDISABLED although LOGIN is refused (§6.2.3, §7.2.1),
// and CAPABILITY after STARTTLS still returned the pre-TLS list.
func TestRound82_CapabilitiesFollowTransport(t *testing.T) {
	t.Run("implicit TLS", func(t *testing.T) {
		c, greeting := round82TLSServer(t)
		for _, line := range []string{greeting, round75Line(c.cmd("c1 CAPABILITY"), "* CAPABILITY")} {
			if round82HasCap(line, "STARTTLS") {
				t.Errorf("DEFECT F5646: TLS session advertises STARTTLS: %q", line)
			}
			if !round82HasCap(line, "AUTH=PLAIN") || round82HasCap(line, "LOGINDISABLED") {
				t.Errorf("control: TLS session must offer AUTH=PLAIN and not LOGINDISABLED: %q", line)
			}
		}
		if got := round75Tagged(c.cmd("c2 STARTTLS")); !strings.HasPrefix(got, "c2 BAD") {
			t.Errorf("DEFECT F5646: STARTTLS on a TLS session answered %q, want BAD (nested TLS)", got)
		}
	})

	t.Run("cleartext then STARTTLS", func(t *testing.T) {
		ms, err := NewBboltMailstore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ms.Close() })
		cfg := &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}, MinVersion: tls.VersionTLS12}
		srv := NewServer(&Config{TLSConfig: cfg}, ms)
		srv.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
		clientConn, serverConn := net.Pipe()
		go srv.handleConnection(serverConn)
		t.Cleanup(func() { _ = clientConn.Close() })
		r := bufio.NewReader(clientConn)
		readUntil := func(prefix string) []string {
			var out []string
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					t.Fatalf("read: %v (got %q)", err, out)
				}
				l = strings.TrimRight(l, "\r\n")
				out = append(out, l)
				if strings.HasPrefix(l, prefix) {
					return out
				}
			}
		}
		send := func(line string) { _, _ = clientConn.Write([]byte(line + "\r\n")) }

		greeting := readUntil("* OK")[0]
		send("c1 CAPABILITY")
		caps := round75Line(readUntil("c1 "), "* CAPABILITY")
		for _, line := range []string{greeting, caps} {
			if !round82HasCap(line, "STARTTLS") {
				t.Errorf("control: cleartext session must offer STARTTLS: %q", line)
			}
			if !round82HasCap(line, "LOGINDISABLED") {
				t.Errorf("DEFECT F5646: cleartext session without LOGIN lacks LOGINDISABLED: %q", line)
			}
			if round82HasCap(line, "AUTH=PLAIN") || round82HasCap(line, "AUTH=LOGIN") {
				t.Errorf("DEFECT F5646: cleartext session advertises a plaintext AUTH mechanism it refuses: %q", line)
			}
		}

		send("c2 STARTTLS")
		readUntil("c2 OK")
		tc := tls.Client(clientConn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // test
		if err := tc.Handshake(); err != nil {
			t.Fatal(err)
		}
		r = bufio.NewReader(tc)
		send = func(line string) { _, _ = tc.Write([]byte(line + "\r\n")) }
		send("c3 CAPABILITY")
		caps = round75Line(readUntil("c3 "), "* CAPABILITY")
		if round82HasCap(caps, "STARTTLS") || round82HasCap(caps, "LOGINDISABLED") || !round82HasCap(caps, "AUTH=PLAIN") {
			t.Errorf("DEFECT F5646: CAPABILITY after STARTTLS is stale: %q", caps)
		}
		send("c4 LOGIN user@example.com pw")
		if got := readUntil("c4 "); !strings.HasPrefix(got[len(got)-1], "c4 OK") {
			t.Errorf("LOGIN after STARTTLS: %q", got)
		}
	})
}

// F5647: IMAP4rev2 (RFC 9051) was advertised although its mandatory pieces
// are missing: ENABLE IMAP4rev2 is answered with an empty ENABLED, UNSELECT
// (§6.4.2) is BAD, and LIST ... RETURN (STATUS ...) (LIST-STATUS, §6.3.9)
// returns no STATUS. A client that selects rev2 behaviour from the capability
// then talks a protocol the server does not speak.
func TestRound82_Rev2NotAdvertisedWithoutSupport(t *testing.T) {
	c, _ := round82Server(t, round82ThreeMsgs()...)
	caps := round75Line(c.cmd("c1 CAPABILITY"), "* CAPABILITY")
	if !round82HasCap(caps, "IMAP4rev1") {
		t.Fatalf("control: IMAP4rev1 missing: %q", caps)
	}
	if !round82HasCap(caps, "IMAP4rev2") {
		return // not advertised: nothing to honour
	}
	enabled := round75Line(c.cmd("c2 ENABLE IMAP4rev2"), "* ENABLED")
	listStatus := strings.Join(c.cmd("c3 LIST \"\" \"*\" RETURN (STATUS (MESSAGES))"), "\n")
	c.cmd("c4 SELECT INBOX")
	unselect := round75Tagged(c.cmd("c5 UNSELECT"))
	if !round82HasCap(enabled, "IMAP4rev2") || !strings.Contains(listStatus, "* STATUS") || !strings.HasPrefix(unselect, "c5 OK") {
		t.Errorf("DEFECT F5647: IMAP4rev2 advertised but ENABLED=%q, LIST-STATUS has STATUS=%v, UNSELECT=%q",
			enabled, strings.Contains(listStatus, "* STATUS"), unselect)
	}
}
