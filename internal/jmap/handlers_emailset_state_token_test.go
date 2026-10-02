package jmap

// Regression test for the non-state newState token defect: handleEmailSet and
// handleEmailImport emitted newState as fmt.Sprintf("state-%d", time.Now())
// and oldState as nil, but RFC 8620 §4.3 defines newState/oldState as "the
// state string that would have been returned by getState" after/before the
// request — tokens a client can feed back into Email/changes as sinceState
// (§2). ParseChangeState parses the time-derived tokens as 0, so echoing the
// server's own newState re-delivered the FULL change window (older, already-
// known changes re-listed). The account's state token is the change-journal
// sequence (storage.CurrentChangeState) — the same format Email/changes
// accepts and returns. Journaling itself was already correct
// (StoreMessageMetadata/DeleteMessage record changes), so set/import
// mutations DO appear in Email/changes from an older token.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// changedIDs reports whether the */changes response lists id in any of
// created/updated/destroyed. Args carry Go types here (direct handler calls).
func changedIDs(resp Response, id string) bool {
	for _, key := range []string{"created", "updated", "destroyed"} {
		list, _ := resp.Args[key].([]string)
		for _, v := range list {
			if v == id {
				return true
			}
		}
	}
	return false
}

func TestEmailSetAndImportNewStateResumesChangesWindow(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// Seed m1 through the UID counter (GetNextUID) so the later import
	// allocates a fresh UID instead of clobbering m1's slot — production
	// writes always pair GetNextUID with the store; direct-metadata seeds
	// must consume the counter too (cf. the round-21 synthesis lesson).
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

	current, err := db.CurrentChangeState(user)
	if err != nil {
		t.Fatal(err)
	}

	// Import m2; its Email ID is the blob ID (meta.MessageID = blobId).
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: m2\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	importResp := server.handleEmailImport(user, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": user,
			"emails": map[string]interface{}{
				"m2": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "call-1",
	})
	newStateImport, _ := importResp.Args["newState"].(string)
	if newStateImport == "" {
		t.Fatalf("FAIL: Email/import returned empty newState")
	}
	if importResp.Args["oldState"] != current {
		t.Fatalf("FAIL: Email/import oldState = %v, want the pre-request journal token %q", importResp.Args["oldState"], current)
	}

	// Echoing the import's own newState must NOT re-list older changes (m1).
	importWindow := server.handleEmailChanges(user, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": newStateImport, "maxChanges": float64(256)},
		ID:   "call-2",
	})
	if changedIDs(importWindow, "m1") {
		t.Fatalf("FAIL: Email/changes with Email/import's own newState re-lists OLDER change m1 — the token is not a state token (RFC 8620 §4.3/§2)")
	}

	// Email/set then advances the state; its newState must resume the window
	// after its own change as well.
	setResp := server.handleEmailSet(user, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"m1": map[string]interface{}{"keywords/$flagged": true},
			},
		},
		ID: "call-3",
	})
	updated, _ := setResp.Args["updated"].(map[string]interface{})
	if _, present := updated["m1"]; !present {
		t.Fatalf("FAIL: m1 missing from updated — set did not apply (notUpdated=%v)", setResp.Args["notUpdated"])
	}
	newStateSet, _ := setResp.Args["newState"].(string)
	if newStateSet == "" {
		t.Fatalf("FAIL: Email/set returned empty newState")
	}
	if newStateSet == newStateImport {
		t.Fatalf("FAIL: Email/set newState %q did not advance past the import state %q — set mutations must move the state (RFC 8620 §2)", newStateSet, newStateImport)
	}
	setWindow := server.handleEmailChanges(user, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": newStateSet, "maxChanges": float64(256)},
		ID:   "call-4",
	})
	if changedIDs(setWindow, "m1") {
		t.Fatalf("FAIL: Email/changes with Email/set's own newState re-lists m1's own change (RFC 8620 §4.3/§2)")
	}
}

func TestEmailChangesFromPreSetTokenListsSetMutation(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m1")

	current, err := db.CurrentChangeState(user)
	if err != nil {
		t.Fatal(err)
	}

	// CONTROL: a set-made change IS visible from a pre-set token — the
	// journal records Email/set mutations (via StoreMessageMetadata).
	setResp := server.handleEmailSet(user, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"m1": map[string]interface{}{"keywords/$flagged": true},
			},
		},
		ID: "call-1",
	})
	if _, present := setResp.Args["updated"].(map[string]interface{})["m1"]; !present {
		t.Fatalf("CONTROL FAILED: m1 missing from updated (harness broken)")
	}

	window := server.handleEmailChanges(user, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": current, "maxChanges": float64(256)},
		ID:   "call-2",
	})
	if !changedIDs(window, "m1") {
		t.Fatalf("CONTROL FAILED: Email/changes from the pre-set token does not list m1's set-made update (journal broken)")
	}
}
