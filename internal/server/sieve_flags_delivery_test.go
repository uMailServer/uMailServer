package server

// Regression tests: the IMAP flags a Sieve script sets with imap4flags
// (RFC 5232) were dropped at delivery, so addflag "\\Seen" or
// fileinto :flags "\\Flagged" stored the message with \Recent only. The
// implicit keep also ignored the internal flags variable (RFC 5232 §3).

import (
	"strings"
	"testing"
)

// sieveDTFlags returns the flags of the only message in folder.
func sieveDTFlags(t *testing.T, srv *Server, mailbox, folder string) []string {
	t.Helper()
	uids, err := srv.storageDB.GetMessageUIDs(mailbox, folder)
	if err != nil || len(uids) != 1 {
		t.Fatalf("%s: uids=%v err=%v, want exactly one message", folder, uids, err)
	}
	meta, err := srv.storageDB.GetMessageMetadata(mailbox, folder, uids[0])
	if err != nil {
		t.Fatalf("%s: metadata: %v", folder, err)
	}
	return meta.Flags
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

func TestSieveDeliveryImplicitKeepCarriesFlags(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require \"imap4flags\";\naddflag \"\\\\Seen\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if flags := sieveDTFlags(t, srv, "alice@test.com", "INBOX"); !hasFlag(flags, `\Seen`) {
		t.Fatalf("implicit keep flags = %v, want \\Seen", flags)
	}
	if _, _, unseen, _ := srv.storageDB.GetMailboxCounts("alice@test.com", "INBOX"); unseen != 0 {
		t.Fatalf("unseen = %d, want 0", unseen)
	}
}

func TestSieveDeliveryFileintoFlagsPerFolder(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com",
		"require [\"fileinto\", \"copy\", \"imap4flags\"];\nfileinto :copy :flags \"\\\\Flagged\" \"Work\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)

	work := sieveDTFlags(t, srv, "alice@test.com", "Work")
	if !hasFlag(work, `\Flagged`) {
		t.Fatalf("Work flags = %v, want \\Flagged", work)
	}
	// :copy keeps the INBOX copy, and the :flags of the fileinto do not
	// leak onto it.
	if inbox := sieveDTFlags(t, srv, "alice@test.com", "INBOX"); hasFlag(inbox, `\Flagged`) {
		t.Fatalf("INBOX flags = %v, want no \\Flagged", inbox)
	}
}
