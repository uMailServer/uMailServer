package sieve

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// startLiteralTestServer runs the real ManageSieve per-connection handler
// (handleConn) on a loopback TCP listener. authHandler always accepts
// user/pass so the test can reach the PUTSCRIPT/CHECKSCRIPT commands.
func startLiteralTestServer(t *testing.T) net.Listener {
	t.Helper()
	mgr := NewManager()
	srv := NewManageSieveServer(mgr, nil)
	srv.SetAuthHandler(func(user, pass string) bool {
		return user == "user" && pass == "pass"
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			srv.wg.Add(1)
			go srv.handleConn(c)
		}
	}()
	return ln
}

// dialAndAuth dials, reads the greeting, and completes PLAIN auth.
func dialAndAuth(t *testing.T, ln net.Listener) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)

	if line, err := readManageSieveGreeting(r); err != nil || !strings.HasPrefix(line, "OK") {
		t.Fatalf("greeting: %q err=%v", line, err)
	}
	if _, err := fmt.Fprintf(conn, "AUTHENTICATE PLAIN\r\n"); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	// F5466: the SASL continuation is an empty base64 string, not OK.
	if line, err := r.ReadString('\n'); err != nil || line != "\"\"\r\n" {
		t.Fatalf("auth continue: %q err=%v", line, err)
	}
	creds := base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))
	if _, err := fmt.Fprintf(conn, "%s\r\n", creds); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	if line, err := r.ReadString('\n'); err != nil || !strings.Contains(line, "successful") {
		t.Fatalf("auth result: %q err=%v", line, err)
	}
	return conn, r
}

// assertLiteralThenNextCommand sends `header` with an exact 5-octet script
// ("keep;", no trailing newline per RFC 5804 §2.3) followed immediately by a
// pipelined NOOP, and asserts the server answers BOTH. A spec-compliant
// client never sends a trailing newline after the script, so the server must
// not consume the next command line while reading one.
func assertLiteralThenNextCommand(t *testing.T, ln net.Listener, header string) {
	t.Helper()
	conn, r := dialAndAuth(t, ln)
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "%s\r\n", header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := conn.Write([]byte("keep;")); err != nil { // exactly 5 octets, no CRLF
		t.Fatalf("write script: %v", err)
	}
	if _, err := conn.Write([]byte("NOOP\r\n")); err != nil { // next command, pipelined
		t.Fatalf("write NOOP: %v", err)
	}

	// First response: the literal command's result.
	line, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "OK") {
		t.Fatalf("%s response: %q err=%v", header, line, err)
	}

	// Second response must be the NOOP acknowledgement. A regression that
	// reads a spurious trailing newline after the literal swallows the NOOP,
	// so this read times out.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line2, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("%s: NOOP after the literal got no response — the next command was "+
			"swallowed by a spurious trailing-newline read: %v", header, err)
	}
	if !strings.Contains(line2, "NOOP") {
		t.Fatalf("%s: expected NOOP acknowledgement, got %q", header, line2)
	}
}

// TestManageSieve_LiteralDoesNotSwallowNextCommand pins RFC 5804 §2.3: after a
// PUTSCRIPT/CHECKSCRIPT literal (exactly script-size octets, no trailing
// newline), the next pipelined command must still be executed and answered.
func TestManageSieve_LiteralDoesNotSwallowNextCommand(t *testing.T) {
	ln := startLiteralTestServer(t)
	defer ln.Close()

	t.Run("PutScriptThenNoop", func(t *testing.T) {
		assertLiteralThenNextCommand(t, ln, `PUTSCRIPT "foo" 5`)
	})
	t.Run("CheckScriptThenNoop", func(t *testing.T) {
		assertLiteralThenNextCommand(t, ln, "CHECKSCRIPT 5")
	})

	// Control: a standalone NOOP (no preceding literal command) is answered.
	// Passes before and after the fix, proving the harness/wire/auth is sound
	// so a failure above is specific to the literal read.
	t.Run("Control_StandaloneNoop", func(t *testing.T) {
		conn, r := dialAndAuth(t, ln)
		defer conn.Close()
		if _, err := fmt.Fprintf(conn, "NOOP\r\n"); err != nil {
			t.Fatalf("write NOOP: %v", err)
		}
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("standalone NOOP got no response (harness invalid): %v", err)
		}
		if !strings.Contains(line, "NOOP") {
			t.Fatalf("expected NOOP response, got %q", line)
		}
	})
}
