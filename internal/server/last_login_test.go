package server

// F7782: IMAP, POP3 and SMTP logins reach the account through
// recordLoginResult; a successful one is recorded as the last login.

import (
	"testing"
	"time"
)

func TestProtocolLoginRecordsLastLogin(t *testing.T) {
	srv := junkRTServer(t, false)
	last := func() time.Time {
		a, err := srv.database.GetAccount("test.com", "alice")
		if err != nil {
			t.Fatal(err)
		}
		return a.LastLoginAt
	}
	if !last().IsZero() {
		t.Fatalf("fixture already has a login: %v", last())
	}
	srv.protoLoginHandler("imap")("alice@test.com", false, "192.0.2.1", "bad password")
	if !last().IsZero() {
		t.Fatalf("a failed login was recorded: %v", last())
	}
	before := time.Now()
	srv.protoLoginHandler("imap")("Alice@Test.com", true, "192.0.2.1", "")
	if got := last(); got.Before(before) {
		t.Fatalf("a successful login (mixed-case address) was not recorded: %v", got)
	}
	// LDAP-only users have no stored account: no error, nothing created.
	srv.protoLoginHandler("smtp")("ldapuser@test.com", true, "192.0.2.1", "")
	if _, err := srv.database.GetAccount("test.com", "ldapuser"); err == nil {
		t.Fatal("recording a login created an account")
	}
}
