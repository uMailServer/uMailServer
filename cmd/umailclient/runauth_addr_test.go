package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
)

// authSMTPServer is the r287 SMTP honeypot bound to the requested address
// (use "[::1]:0" or "127.0.0.1:0"): it answers the SMTP handshake (220 /
// EHLO advertising AUTH PLAIN / AUTH 235 / MAIL+RCPT 250 / DATA 354 /
// dot 250 / QUIT 221) and reports every accepted connection on the returned
// channel.
func authSMTPServer(t *testing.T, listenAddr string) (port int, conns chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		t.Skipf("listener unavailable (%s): %v", listenAddr, err)
	}
	t.Cleanup(func() { ln.Close() })
	conns = make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- struct{}{}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				fmt.Fprint(c, "220 fake\r\n")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					u := strings.ToUpper(strings.TrimSpace(l))
					switch {
					case strings.HasPrefix(u, "EHLO"):
						// Advertise AUTH PLAIN: smtp.SendMail refuses to
						// authenticate against a server without the extension.
						fmt.Fprint(c, "250-fake\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(u, "AUTH"):
						fmt.Fprint(c, "235 ok\r\n")
					case strings.HasPrefix(u, "DATA"):
						fmt.Fprint(c, "354 go\r\n")
						for {
							d, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if d == ".\r\n" {
								break
							}
						}
						fmt.Fprint(c, "250 queued\r\n")
					case strings.HasPrefix(u, "QUIT"):
						fmt.Fprint(c, "221 bye\r\n")
						return
					default:
						fmt.Fprint(c, "250 ok\r\n")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, conns
}

// Regression: runAuth must build host:port with formatAddr like every other
// dial path in the file (sendMessage, runInbox, runDSN). It used
// fmt.Sprintf("%s:%d", ...), so an IPv6 host produced "::1:43111" and the
// dial failed with "too many colons in address" before any connection was
// even attempted.
func TestRunAuth_IPv6Host_ReachesServerWithBracketedAddr(t *testing.T) {
	port, conns := authSMTPServer(t, "[::1]:0")
	code, out := r287RunMain(t, fmt.Sprintf("auth --host ::1 --port %d --user u --pass p", port))

	if strings.Contains(out, "too many colons") {
		t.Fatalf("runAuth built a malformed IPv6 host:port (dial failed): %s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("against [::1]:%d", port)) {
		t.Fatalf("runAuth did not print the bracketed IPv6 addr [::1]:%d: %s", port, out)
	}
	if code != 0 {
		t.Fatalf("exit %d (want 0: AUTH test against the honeypot), output: %s", code, out)
	}
	select {
	case <-conns:
	default:
		t.Fatal("the SMTP honeypot never received the connection")
	}
}

// Control: the IPv4 path is unaffected by the addr fix and must keep
// working on both the broken and fixed code.
func TestRunAuth_IPv4Host_Control(t *testing.T) {
	port, _ := authSMTPServer(t, "127.0.0.1:0")
	code, out := r287RunMain(t, fmt.Sprintf("auth --host 127.0.0.1 --port %d --user u --pass p", port))
	if code != 0 {
		t.Fatalf("control exit %d: %s", code, out)
	}
	if !strings.Contains(out, "AUTH successful!") {
		t.Fatalf("control output missing success: %s", out)
	}
}
