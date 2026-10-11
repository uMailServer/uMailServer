package auth

import (
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

// F6964: empty credentials never reach the directory (unauthenticated bind).
func TestLDAPAuthenticateRejectsEmptyCredentialsBeforeNetwork(t *testing.T) {
	dials := 0
	c := &LDAPClient{config: LDAPConfig{Enabled: true, BaseDN: "dc=x", UserFilter: "(uid=%s)"}}
	c.pool = newLDAPPool(func() (pooledLDAPConn, error) {
		dials++
		return nil, errors.New("must not dial")
	}, 1)
	for _, cred := range [][2]string{{"alice", ""}, {"", "secret"}, {"", ""}} {
		if u, err := c.Authenticate(cred[0], cred[1]); err == nil || u != nil {
			t.Fatalf("Authenticate(%q,%q) = %v, %v; want error", cred[0], cred[1], u, err)
		}
	}
	if dials != 0 {
		t.Fatalf("empty credentials caused %d directory connections", dials)
	}
	if len(c.loginAttempts) != 0 {
		t.Fatal("empty credentials must not consume the per-user failure budget")
	}
}

// F6963: a server that accepts the TCP connection and never answers StartTLS
// must make the dial fail within the configured timeout, not hang forever.
func TestDialLDAPStartTLSHonoursTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	done := make(chan error, 1)
	go func() {
		conn, err := dialLDAP("ldap", ln.Addr().String(), true, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "x"}, 100*time.Millisecond)
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dial against a silent server succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dialLDAP hung on a silent server: no timeout during StartTLS")
	}
	select {
	case c := <-accepted:
		c.Close()
	default:
	}
}
