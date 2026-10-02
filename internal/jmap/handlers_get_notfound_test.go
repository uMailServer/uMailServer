package jmap

// Regression test for the /get notFound defect: Mailbox/get, Thread/get and
// Identity/get filtered their results by the requested "ids" but hardcoded
// "notFound": []string{} in the response — requested-but-missing ids were
// silently dropped. RFC 8620 §4.2 (/get): "notFound" must list the requested
// ids that were NOT found, so a client can detect a typo'd or deleted id
// instead of reading the request as fully satisfied. Email/get in the same
// file already computed notFound correctly (the reference implementation —
// guarded here too). Thread/get had a second facet: GetThreadMessages
// returning empty-with-nil for a nonexistent id produced a PHANTOM empty
// thread in "list" instead of a notFound entry.

import (
	"testing"
)

func TestMailboxGetReportsMissingIdsInNotFound(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}

	resp := server.handleMailboxGet(user, MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": user,
			"ids":       []interface{}{"inbox", "nonexistent"},
		},
		ID: "call-1",
	})

	list, _ := resp.Args["list"].([]Mailbox)
	foundInbox := false
	for _, mbox := range list {
		if mbox.ID == "inbox" {
			foundInbox = true
		}
		if mbox.ID == "nonexistent" {
			t.Fatalf("FAIL: nonexistent id leaked into list")
		}
	}
	if !foundInbox {
		t.Fatalf("FAIL: INBOX missing from list — valid ids must still resolve")
	}
	notFound, _ := resp.Args["notFound"].([]string)
	if len(notFound) != 1 || notFound[0] != "nonexistent" {
		t.Fatalf("FAIL: notFound = %v, want [nonexistent] — RFC 8620 §4.2", notFound)
	}

	// Control: a request with only valid ids keeps notFound empty.
	resp = server.handleMailboxGet(user, MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": user,
			"ids":       []interface{}{"inbox"},
		},
		ID: "call-2",
	})
	notFound, _ = resp.Args["notFound"].([]string)
	if len(notFound) != 0 {
		t.Fatalf("FAIL: notFound = %v for fully-resolved request, want empty", notFound)
	}
}

func TestThreadGetReportsMissingThreadAndNoPhantom(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m1") // ThreadID: thread-m1

	resp := server.handleThreadGet(user, MethodCall{
		Name: "Thread/get",
		Args: map[string]interface{}{
			"accountId": user,
			"ids":       []interface{}{"thread-m1", "no-such-thread"},
		},
		ID: "call-1",
	})

	list, _ := resp.Args["list"].([]Thread)
	foundReal := false
	for _, thread := range list {
		if thread.ID == "no-such-thread" {
			t.Fatalf("FAIL: phantom empty thread returned for a nonexistent id")
		}
		if thread.ID == "thread-m1" {
			foundReal = true
			if len(thread.EmailIDs) != 1 || thread.EmailIDs[0] != "m1" {
				t.Fatalf("FAIL: thread-m1 emails = %v, want [m1]", thread.EmailIDs)
			}
		}
	}
	if !foundReal {
		t.Fatalf("FAIL: thread-m1 missing from list — valid ids must still resolve")
	}
	notFound, _ := resp.Args["notFound"].([]string)
	if len(notFound) != 1 || notFound[0] != "no-such-thread" {
		t.Fatalf("FAIL: notFound = %v, want [no-such-thread] — RFC 8620 §4.2", notFound)
	}
}

func TestIdentityGetReportsMissingIdsInNotFound(t *testing.T) {
	const user = "user@example.com"
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp := server.handleIdentityGet(user, MethodCall{
		Name: "Identity/get",
		Args: map[string]interface{}{
			"accountId": user,
			"ids":       []interface{}{"default", "nonexistent"},
		},
		ID: "call-1",
	})

	list, _ := resp.Args["list"].([]Identity)
	foundDefault := false
	for _, identity := range list {
		if identity.ID == "default" {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Fatalf("FAIL: default identity missing from list — valid ids must still resolve")
	}
	notFound, _ := resp.Args["notFound"].([]string)
	if len(notFound) != 1 || notFound[0] != "nonexistent" {
		t.Fatalf("FAIL: notFound = %v, want [nonexistent] — RFC 8620 §4.2", notFound)
	}
}

func TestEmailGetNotFoundReferenceStillWorks(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m1")

	// Guard: Email/get is the reference implementation of notFound in this
	// file — it must keep reporting requested-but-missing ids.
	resp := server.handleEmailGet(user, MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": user,
			"ids":       []interface{}{"m1", "nope"},
		},
		ID: "call-1",
	})

	notFound, _ := resp.Args["notFound"].([]string)
	if len(notFound) != 1 || notFound[0] != "nope" {
		t.Fatalf("FAIL: Email/get notFound = %v, want [nope] (reference behavior regressed)", notFound)
	}
	emails, _ := resp.Args["list"].([]Email)
	if len(emails) != 1 {
		t.Fatalf("FAIL: Email/get list = %d entries, want 1", len(emails))
	}
}
