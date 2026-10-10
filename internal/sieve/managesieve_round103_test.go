package sieve

// Regression tests for round 103 (F5850-F5852): ManageSieve script-name
// validation, STARTTLS without a certificate, auth brute-force lockout.

import (
	"crypto/tls"
	"strings"
	"testing"
)

func TestF5850_EmptyAndControlScriptNameRejected(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	if got := c.put("", "keep;"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("empty name accepted: %q", got)
	}
	if got := c.put("a\x01b", "keep;"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("control-char name accepted: %q", got)
	}
	if got := c.put(strings.Repeat("n", 2000), "keep;"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("overlong name accepted")
	}
	// session stays in sync after refused literals
	if got := c.put("ok", "keep;"); !strings.HasPrefix(got, "OK") {
		t.Fatalf("control failed: %q", got)
	}
}

func TestF5850_RenameToInvalidNameRejected(t *testing.T) {
	c, _ := reg80Dial(t, nil, nil, reg80Creds)
	defer c.client.Close()
	c.auth()
	c.put("a", "keep;")
	if got := c.last(`RENAMESCRIPT "a" "x` + "\x01" + `y"` + "\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("rename to control name accepted: %q", got)
	}
}

func TestF5851_STARTTLSWithoutCertificateRefused(t *testing.T) {
	c, _ := reg80Dial(t, nil, &tls.Config{}, reg80Creds)
	defer c.client.Close()
	for _, l := range c.greet {
		if strings.Contains(l, "STARTTLS") {
			t.Fatalf("STARTTLS advertised without certificate: %q", c.greet)
		}
	}
	if got := c.last("STARTTLS\r\n"); !strings.HasPrefix(got, "NO") {
		t.Fatalf("STARTTLS answered %q", got)
	}
}

func TestF5852_AuthBruteForceLockout(t *testing.T) {
	mgr := NewManager()
	srv := NewManageSieveServer(mgr, nil)
	srv.SetAuthHandler(func(u, p string) bool { return u == "user" && p == "pass" })
	srv.SetAuthLimits(3, 1<<40)
	for i := 0; i < 3; i++ {
		if srv.authLocked("1.2.3.4") {
			t.Fatalf("locked early at %d", i)
		}
		srv.recordAuthFailure("1.2.3.4")
	}
	if !srv.authLocked("1.2.3.4") {
		t.Fatal("not locked after limit")
	}
	if srv.authLocked("5.6.7.8") {
		t.Fatal("other IP locked")
	}
	srv.clearAuthFailures("1.2.3.4")
	if srv.authLocked("1.2.3.4") {
		t.Fatal("not cleared")
	}
}
