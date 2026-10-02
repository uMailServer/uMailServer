package jmap

// Regression test for the time-derived state tokens emitted by the /get and
// /query handlers: Mailbox/get, Mailbox/query, Email/query and Email/get
// returned state/queryState fields as fmt.Sprintf("state-%d", time.Now())
// — round 6 fixed only Email/set and Email/import. RFC 8620 §2/§4.3: a state
// string returned by any response must be usable as the sinceState of the
// matching */changes call and resume the window AFTER the state the client
// saw. ParseChangeState parses the time-derived tokens as 0, so echoing them
// re-delivered the FULL change window (older, already-known changes
// re-listed). The account's state token is the change-journal sequence
// (storage.CurrentChangeState). The /queryChanges newQueryState sites are
// deliberately untouched: that handler ignores sinceQueryState entirely
// (stub), so its token has no resumption contract to pin here.
//
// changedIDs comes from handlers_emailset_state_token_test.go (same package).

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestGetAndQueryStateTokensResumeChangesWindow(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// Seed m1 through the UID counter: a later GetNextUID must not reuse the
	// slot (direct-metadata seeds that skip the counter get clobbered by the
	// next allocation — the round-36 fixture lesson).
	uid, err := db.GetNextUID(user, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{
		UID: uid, MessageID: "m1", ThreadID: "thread-m1",
		Subject: "s-m1", From: "sender@example.com", To: user,
		Size: 100, Flags: []string{"\\Seen"},
	}); err != nil {
		t.Fatal(err)
	}

	// Email/query's queryState resumes the Email/changes window.
	queryResp := server.handleEmailQuery(user, MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{"accountId": user},
		ID:   "call-1",
	})
	queryState, _ := queryResp.Args["queryState"].(string)
	if queryState == "" {
		t.Fatalf("FAIL: Email/query returned empty queryState")
	}
	emailWindow := server.handleEmailChanges(user, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": queryState, "maxChanges": float64(256)},
		ID:   "call-2",
	})
	if changedIDs(emailWindow, "m1") {
		t.Fatalf("FAIL: Email/changes with Email/query's own queryState re-lists OLDER change m1 — the token is not a state token (RFC 8620 §2)")
	}

	// Email/get's state resumes the Email/changes window.
	getResp := server.handleEmailGet(user, MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{"accountId": user, "ids": []string{"m1"}},
		ID:   "call-3",
	})
	getState, _ := getResp.Args["state"].(string)
	if getState == "" {
		t.Fatalf("FAIL: Email/get returned empty state")
	}
	emailWindow2 := server.handleEmailChanges(user, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": getState, "maxChanges": float64(256)},
		ID:   "call-4",
	})
	if changedIDs(emailWindow2, "m1") {
		t.Fatalf("FAIL: Email/changes with Email/get's own state re-lists OLDER change m1 — the token is not a state token (RFC 8620 §2)")
	}

	// Mailbox/get's state resumes the Mailbox/changes window.
	mailboxGet := server.handleMailboxGet(user, MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{"accountId": user},
		ID:   "call-5",
	})
	mailboxState, _ := mailboxGet.Args["state"].(string)
	if mailboxState == "" {
		t.Fatalf("FAIL: Mailbox/get returned empty state")
	}
	mailboxWindow := server.handleMailboxChanges(user, MethodCall{
		Name: "Mailbox/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": mailboxState, "maxChanges": float64(256)},
		ID:   "call-6",
	})
	if changedIDs(mailboxWindow, "INBOX") {
		t.Fatalf("FAIL: Mailbox/changes with Mailbox/get's own state re-lists OLDER change INBOX — the token is not a state token (RFC 8620 §2)")
	}

	// Mailbox/query's queryState resumes the Mailbox/changes window.
	mailboxQuery := server.handleMailboxQuery(user, MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{"accountId": user},
		ID:   "call-7",
	})
	mailboxQueryState, _ := mailboxQuery.Args["queryState"].(string)
	if mailboxQueryState == "" {
		t.Fatalf("FAIL: Mailbox/query returned empty queryState")
	}
	mailboxWindow2 := server.handleMailboxChanges(user, MethodCall{
		Name: "Mailbox/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": mailboxQueryState, "maxChanges": float64(256)},
		ID:   "call-8",
	})
	if changedIDs(mailboxWindow2, "INBOX") {
		t.Fatalf("FAIL: Mailbox/changes with Mailbox/query's own queryState re-lists OLDER change INBOX")
	}
}

func TestChangesFromPreQueryTokenListsKnownChanges(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// Capture the pre-seed token, then seed THROUGH the counter so m1's
	// creation lands inside the window it opens.
	preSeed, err := db.CurrentChangeState(user)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := db.GetNextUID(user, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{
		UID: uid, MessageID: "m1", ThreadID: "thread-m1",
		Subject: "s-m1", From: "sender@example.com", To: user,
		Size: 100, Flags: []string{"\\Seen"},
	}); err != nil {
		t.Fatal(err)
	}

	// CONTROL: from the pre-seed token, Email/changes lists m1's creation —
	// the journal records storage-layer mutations and the token ecosystem
	// functions end to end.
	window := server.handleEmailChanges(user, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": preSeed, "maxChanges": float64(256)},
		ID:   "call-1",
	})
	if !changedIDs(window, "m1") {
		t.Fatalf("CONTROL FAILED: Email/changes from the pre-seed token does not list m1's creation (journal broken)")
	}
}
