package server

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// r293Rcpt returns the transcript of an unauthenticated port-25 transaction to
// rcpt (see relayAuditSession) and whether the DATA phase was reached.
func r293Rcpt(t *testing.T, srv *Server, rcpt string) (string, bool) {
	t.Helper()
	tr := relayAuditSession(t, srv, rcpt)
	return tr, strings.Contains(tr, "354")
}

// F7670: RCPT to a mailbox that does not exist on a hosted domain is a
// permanent failure at RCPT time (550 5.1.1), not a temporary 451 after DATA.
func TestRCPTUnknownMailboxRefusedWithPermanentError(t *testing.T) {
	srv := newRelayAuditInbound(t)
	tr, data := r293Rcpt(t, srv, "nobody@test.com")
	if data || strings.Contains(tr, "451") || !strings.Contains(tr, "550 5.1.1") {
		t.Fatalf("unknown mailbox: DATA reached=%v; transcript:\n%s", data, tr)
	}
}

func TestRCPTAcceptsDeliverableAddresses(t *testing.T) {
	srv := newRelayAuditInbound(t)
	if err := srv.database.CreateAlias(&db.AliasData{Domain: "test.com", Alias: "sales", Target: "alice@test.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	for _, rcpt := range []string{
		"alice@test.com",      // existing mailbox
		"ALICE@TEST.COM",      // case-insensitive
		"sales@test.com",      // alias to an existing mailbox
		"alice+news@test.com", // plus address falls back to the base mailbox
		"postmaster@test.com", // RFC 5321 4.5.1: always accepted at RCPT
	} {
		if tr, data := r293Rcpt(t, srv, rcpt); !data {
			t.Errorf("%s refused; transcript:\n%s", rcpt, tr)
		}
	}
}

func TestRCPTRefusesUnknownVariantsAndInactive(t *testing.T) {
	srv := newRelayAuditInbound(t)
	if err := srv.database.CreateAccount(&db.AccountData{Email: "old@test.com", LocalPart: "old", Domain: "test.com", PasswordHash: "x", IsActive: false}); err != nil {
		t.Fatal(err)
	}
	for _, rcpt := range []string{"ghost+tag@test.com", "old@test.com"} {
		if tr, data := r293Rcpt(t, srv, rcpt); data || !strings.Contains(tr, "550 5.1.1") {
			t.Errorf("%s: DATA reached=%v; transcript:\n%s", rcpt, data, tr)
		}
	}
}

func TestRCPTCatchAllAcceptsAnyLocalPart(t *testing.T) {
	srv := newRelayAuditInbound(t)
	d, err := srv.database.GetDomain("test.com")
	if err != nil {
		t.Fatal(err)
	}
	d.CatchAllTarget = "alice@test.com"
	if err := srv.database.UpdateDomain(d); err != nil {
		t.Fatal(err)
	}
	if tr, data := r293Rcpt(t, srv, "anything@test.com"); !data {
		t.Fatalf("catch-all domain refused an unknown local part; transcript:\n%s", tr)
	}
}

// Control: relay policy for non-hosted domains is unchanged (554, not 550).
func TestRCPTExternalDomainStillRelayDenied(t *testing.T) {
	srv := newRelayAuditInbound(t)
	if tr, data := r293Rcpt(t, srv, "victim@external.example"); data || !strings.Contains(tr, "554 5.7.1") {
		t.Fatalf("external recipient: DATA reached=%v; transcript:\n%s", data, tr)
	}
}

// F7730: a catch-all domain accepts unknown local parts only while its target
// mailbox exists and is active; otherwise delivery would fail after DATA with a
// temporary error.
func TestRCPTCatchAllNeedsAUsableTarget(t *testing.T) {
	for name, target := range map[string]string{"missing target": "ghost@test.com", "malformed target": "no-at-sign"} {
		srv := newRelayAuditInbound(t)
		d, err := srv.database.GetDomain("test.com")
		if err != nil {
			t.Fatal(err)
		}
		d.CatchAllTarget = target
		if err := srv.database.UpdateDomain(d); err != nil {
			t.Fatal(err)
		}
		if tr, data := r293Rcpt(t, srv, "anything@test.com"); data || !strings.Contains(tr, "550 5.1.1") {
			t.Errorf("%s: DATA reached=%v; transcript:\n%s", name, data, tr)
		}
	}
	// inactive target
	srv := newRelayAuditInbound(t)
	if err := srv.database.CreateAccount(&db.AccountData{Email: "off@test.com", LocalPart: "off", Domain: "test.com", PasswordHash: "x", IsActive: false}); err != nil {
		t.Fatal(err)
	}
	d, _ := srv.database.GetDomain("test.com")
	d.CatchAllTarget = "off@test.com"
	if err := srv.database.UpdateDomain(d); err != nil {
		t.Fatal(err)
	}
	if tr, data := r293Rcpt(t, srv, "anything@test.com"); data || !strings.Contains(tr, "550 5.1.1") {
		t.Errorf("inactive target: DATA reached=%v; transcript:\n%s", data, tr)
	}
}
