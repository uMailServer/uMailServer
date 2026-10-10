package auth

// Regression tests for F5270 (LDAP login rate limiter).
// A fake in-memory LDAP server (net.Pipe + BER) drives LDAPClient.Authenticate
// end to end; no network.

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

const ldapRLUserDN = "uid=alice,dc=example,dc=org"

type ldapRLServer struct {
	password  string
	userBinds atomic.Int64 // binds attempted with the user DN (password checks reaching LDAP)
}

func ldapRLResult(msgID int64, appTag ber.Tag, code int64) *ber.Packet {
	p := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "LDAP Response")
	p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, msgID, "MessageID"))
	op := ber.Encode(ber.ClassApplication, ber.TypeConstructed, appTag, nil, "Result")
	op.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, "resultCode"))
	op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "matchedDN"))
	op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "diagnostic"))
	p.AppendChild(op)
	return p
}

func ldapRLEntry(msgID int64) *ber.Packet {
	p := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "LDAP Response")
	p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, msgID, "MessageID"))
	op := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ldap.ApplicationSearchResultEntry, nil, "Entry")
	op.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, ldapRLUserDN, "DN"))
	attrs := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "Attributes")
	attr := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "Attribute")
	attr.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "mail", "type"))
	vals := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "vals")
	vals.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "alice@example.org", "val"))
	attr.AppendChild(vals)
	attrs.AppendChild(attr)
	op.AppendChild(attrs)
	p.AppendChild(op)
	return p
}

func (s *ldapRLServer) serve(c net.Conn) {
	defer c.Close()
	for {
		pkt, err := ber.ReadPacket(c)
		if err != nil || len(pkt.Children) < 2 {
			return
		}
		id, _ := pkt.Children[0].Value.(int64)
		op := pkt.Children[1]
		switch op.Tag {
		case ldap.ApplicationBindRequest:
			dn, _ := op.Children[1].Value.(string)
			pw := op.Children[2].Data.String()
			code := int64(ldap.LDAPResultSuccess)
			if dn == ldapRLUserDN {
				s.userBinds.Add(1)
				if pw != s.password {
					code = int64(ldap.LDAPResultInvalidCredentials)
				}
			}
			_, _ = c.Write(ldapRLResult(id, ldap.ApplicationBindResponse, code).Bytes())
		case ldap.ApplicationSearchRequest:
			_, _ = c.Write(ldapRLEntry(id).Bytes())
			_, _ = c.Write(ldapRLResult(id, ldap.ApplicationSearchResultDone, 0).Bytes())
		case ldap.ApplicationUnbindRequest:
			return
		}
	}
}

// ldapRLClient returns an LDAPClient whose pool dials the fake server.
func ldapRL5270Client(t *testing.T, srv *ldapRLServer) *LDAPClient {
	t.Helper()
	var wg sync.WaitGroup
	c := &LDAPClient{config: LDAPConfig{
		Enabled: true, BindDN: "cn=svc,dc=example,dc=org", BindPassword: "svc",
		BaseDN: "dc=example,dc=org", UserFilter: "(uid=%s)", EmailAttribute: "mail",
		NameAttribute: "cn", GroupAttribute: "memberOf",
	}}
	c.pool = newLDAPPool(func() (pooledLDAPConn, error) {
		cl, sv := net.Pipe()
		wg.Add(1)
		go func() { defer wg.Done(); srv.serve(sv) }()
		lc := ldap.NewConn(cl, false)
		lc.Start()
		return lc, nil
	}, 2)
	t.Cleanup(func() { c.Close(); wg.Wait() })
	return c
}

// TestLDAPRateLimit_SuccessfulLoginsNeverLockOut: before F5270 every allowed
// attempt was counted, so the sixth successful login within 15 minutes was
// refused even with zero failures (IMAP/SMTP/CalDAV reconnects).
func TestLDAPRateLimit_SuccessfulLoginsNeverLockOut(t *testing.T) {
	srv := &ldapRLServer{password: "right"}
	c := ldapRL5270Client(t, srv)
	for i := 1; i <= 8; i++ {
		if _, err := c.Authenticate("alice", "right"); err != nil {
			t.Fatalf("successful login #%d refused: %v", i, err)
		}
	}
	c.loginMu.Lock()
	n := len(c.loginAttempts)
	c.loginMu.Unlock()
	if n != 0 {
		t.Fatalf("successful logins left %d rate-limit entries, want 0", n)
	}
}

// TestLDAPRateLimit_FiveFailuresReachBind: exactly five wrong passwords are
// checked against LDAP before the lockout (before F5270 each failure counted
// twice, so only three were).
func TestLDAPRateLimit_FiveFailuresReachBind(t *testing.T) {
	srv := &ldapRLServer{password: "right"}
	c := ldapRL5270Client(t, srv)
	for i := 0; i < 7; i++ {
		_, _ = c.Authenticate("alice", "wrong")
	}
	if got := srv.userBinds.Load(); got != 5 {
		t.Fatalf("wrong-password binds reaching LDAP = %d, want 5", got)
	}
	if _, err := c.Authenticate("alice", "right"); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("login after 5 failures: err=%v, want rate limit", err)
	}
}

// TestLDAPRateLimit_SuccessResetsFailures: a successful login clears earlier
// failures, so occasional typos never accumulate into a lockout.
func TestLDAPRateLimit_SuccessResetsFailures(t *testing.T) {
	srv := &ldapRLServer{password: "right"}
	c := ldapRL5270Client(t, srv)
	for round := 0; round < 3; round++ {
		for i := 0; i < 4; i++ {
			if _, err := c.Authenticate("alice", "wrong"); err == nil || !strings.Contains(err.Error(), "invalid credentials") {
				t.Fatalf("round %d failure %d: err=%v, want invalid credentials", round, i, err)
			}
		}
		if _, err := c.Authenticate("alice", "right"); err != nil {
			t.Fatalf("round %d: login after 4 failures refused: %v", round, err)
		}
	}
}
