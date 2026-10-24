package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// silentTLSServer accepts TCP connections and never sends a byte: the TCP
// connect succeeds, but the TLS handshake blocks forever waiting for the
// server's first flight. This is the black hole an un-deadlined tls.Dial
// hangs on. Held connections are pumped (never written to) and die with the
// test process.
func silentTLSServer(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 512)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

// Regression: the TLS path must honor the same connection budget as the
// plain path (net.DialTimeout, 10s). tls.Dial has no deadline, so a server
// that accepts TCP and never completes the handshake (accept-and-silent
// black hole) blocks sendMessage forever.
func TestSendMessage_TLSHandshakeHasDeadline(t *testing.T) {
	host, port := silentTLSServer(t)
	done := make(chan error, 1)
	go func() {
		c := NewSMTPClient(host, port, "", "", true, false)
		done <- c.Send("probe@localhost", "test@localhost", "probe", "body")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("sendMessage unexpectedly succeeded against a silent TLS server")
		}
		if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
			t.Fatalf("sendMessage failed for the wrong reason (want handshake deadline): %v", err)
		}
	case <-time.After(13 * time.Second):
		t.Fatal("sendMessage still blocked after 13s: tls.Dial has no handshake deadline " +
			"(the plain path budgets 10s via net.DialTimeout)")
	}
}

// Regression for the runInbox path: the same un-deadlined tls.Dial hangs the
// whole command. The command must exit on its own with a connection failure
// within the plain path's budget window, not sit blocked until killed.
func TestRunInbox_TLSHandshakeHasDeadline(t *testing.T) {
	host, port := silentTLSServer(t)
	code, out := r287RunMain(t, fmt.Sprintf("inbox --host %s --port %d --tls", host, port))
	if code == -1 {
		t.Fatalf("runInbox was still blocked after the 20s re-exec context (killed): "+
			"tls.Dial has no handshake deadline; output: %s", out)
	}
	if code != 1 {
		t.Fatalf("runInbox exit code %d (want 1: connection failure), output: %s", code, out)
	}
	if !strings.Contains(out, "Connection failed") {
		t.Fatalf("runInbox output missing connection failure: %s", out)
	}
	if !strings.Contains(out, "timeout") && !strings.Contains(out, "deadline") {
		t.Fatalf("runInbox failed for the wrong reason (want handshake deadline): %s", out)
	}
}
