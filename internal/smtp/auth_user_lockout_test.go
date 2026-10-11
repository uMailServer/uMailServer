package smtp

import (
	"bufio"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

type addrConn struct {
	net.Conn
	addr net.Addr
}

func (c addrConn) RemoteAddr() net.Addr { return c.addr }

// runAuth drives one AUTH command on a fresh session from the given source IP
// and returns the final reply line.
func runAuth(t *testing.T, srv *Server, ip string, lines ...string) string {
	t.Helper()
	c1, c2 := net.Pipe()
	defer c2.Close()
	sess := NewSession(addrConn{c1, &net.TCPAddr{IP: net.ParseIP(ip), Port: 1000}}, srv)
	sess.reader = bufio.NewReader(c1)
	go func() {
		for {
			line, err := sess.reader.ReadString('\n')
			if err != nil {
				return
			}
			if sess.HandleCommand(strings.TrimRight(line, "\r\n")) != nil {
				return
			}
		}
	}()
	rd := bufio.NewReader(c2)
	var resp string
	lines = append([]string{"EHLO t"}, lines...)
	for i, l := range lines {
		_ = c2.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.WriteString(c2, l+"\r\n"); err != nil {
			t.Fatal(err)
		}
		resp, _ = rd.ReadString('\n')
		if i == 0 {
			for !strings.HasPrefix(resp, "250 ") {
				resp, _ = rd.ReadString('\n')
			}
		}
	}
	return strings.TrimSpace(resp)
}

func lkB64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func newLockoutServer(submission bool) *Server {
	srv := NewServer(&Config{IsSubmission: submission, AllowInsecure: true, Hostname: "mx.test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthHandler(func(u, p string) (bool, error) { return u == "alice@example.com" && p == "secret", nil })
	srv.SetAuthLimits(3, time.Hour)
	return srv
}

// F6330: failures from many IPs and spelling variants of one account lock it.
func TestAuthUserLockoutAcrossIPsAndCase(t *testing.T) {
	srv := newLockoutServer(true)
	variants := []string{"alice@example.com", "ALICE@example.com", " Alice@Example.COM "}
	for i, u := range variants {
		ip := "203.0.113." + string(rune('1'+i))
		got := runAuth(t, srv, ip, "AUTH PLAIN "+lkB64("\x00"+u+"\x00wrong"))
		if !strings.HasPrefix(got, "535") {
			t.Fatalf("variant %q: %s", u, got)
		}
	}
	// Fourth, from a fresh IP, even with the correct password, must be locked.
	got := runAuth(t, srv, "198.51.100.9", "AUTH PLAIN "+lkB64("\x00alice@example.com\x00secret"))
	if !strings.Contains(got, "Too many") {
		t.Fatalf("PLAIN not locked out: %s", got)
	}
	got = runAuth(t, srv, "198.51.100.10", "AUTH LOGIN "+lkB64("Alice@example.com"))
	if !strings.Contains(got, "Too many") {
		t.Fatalf("LOGIN initial-response not locked out: %s", got)
	}
	got = runAuth(t, srv, "198.51.100.11", "AUTH LOGIN", lkB64("ALICE@example.com"))
	if !strings.Contains(got, "Too many") {
		t.Fatalf("LOGIN not locked out: %s", got)
	}
	// Other accounts are unaffected.
	got = runAuth(t, srv, "198.51.100.12", "AUTH PLAIN "+lkB64("\x00bob@example.com\x00x"))
	if strings.Contains(got, "Too many") {
		t.Fatalf("unrelated user locked: %s", got)
	}
}

// Success clears the per-user counter.
func TestAuthUserLockoutClearedOnSuccess(t *testing.T) {
	srv := newLockoutServer(true)
	for i := 0; i < 2; i++ {
		runAuth(t, srv, "203.0.113.50", "AUTH PLAIN "+lkB64("\x00alice@example.com\x00bad"))
	}
	if got := runAuth(t, srv, "203.0.113.51", "AUTH PLAIN "+lkB64("\x00alice@example.com\x00secret")); !strings.HasPrefix(got, "235") {
		t.Fatalf("expected success: %s", got)
	}
	if srv.isUserLockedOut("alice@example.com") || len(srv.userFailures) != 0 {
		t.Fatal("counter not cleared")
	}
}
