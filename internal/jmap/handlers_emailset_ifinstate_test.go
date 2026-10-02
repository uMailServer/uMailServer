package jmap

// Regression test for the ignored ifInState defect: handleEmailSet never read
// the ifInState argument, so a stale-state update was applied silently — the
// client's compare-and-swap guard was fiction and concurrent writers could
// lose updates without detection. RFC 8620 §4.3 (/set): if ifInState is
// supplied and does not match the current state, the server MUST reject the
// request with a stateMismatch error (§3.6.1 method-level error
// ["error",{"type":"stateMismatch"},callId]) and process no
// create/update/destroy. The account's current state is the change-journal
// token that Email/changes accepts and returns (storage.CurrentChangeState).

import (
	"testing"
)

func TestEmailSetRejectsStaleIfInState(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m1")

	// Any token that does not match the current journal state must be
	// rejected; "bogus-stale-token" never matches.
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"ifInState": "bogus-stale-token",
			"update": map[string]interface{}{
				"m1": map[string]interface{}{"keywords/$flagged": true},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailSet(user, call)

	if resp.Name != "error" {
		t.Fatalf("FAIL: response name = %q, want \"error\" (stateMismatch replaces the whole /set response)", resp.Name)
	}
	if got, _ := resp.Args["type"].(string); got != "stateMismatch" {
		t.Fatalf("FAIL: error type = %q, want \"stateMismatch\" (RFC 8620 §4.3)", got)
	}
	if resp.ID != "call-1" {
		t.Fatalf("FAIL: error response ID = %q, want \"call-1\" (RFC 8620 §3.4)", resp.ID)
	}

	// Synthesis-immune probe: the rejected update must not touch storage.
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	for _, uid := range uids {
		if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == "m1" {
			if len(meta.Flags) != 1 || meta.Flags[0] != "\\Seen" {
				t.Fatalf("FAIL: m1 flags = %v, want untouched [\\Seen]", meta.Flags)
			}
		}
	}
}

func TestEmailSetAcceptsMatchingIfInState(t *testing.T) {
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

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"ifInState": current,
			"update": map[string]interface{}{
				"m1": map[string]interface{}{"keywords/$flagged": true},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailSet(user, call)

	if resp.Name != "Email/set" {
		t.Fatalf("FAIL: response name = %q, want \"Email/set\" — a matching ifInState must proceed", resp.Name)
	}
	updated, _ := resp.Args["updated"].(map[string]interface{})
	if _, present := updated["m1"]; !present {
		t.Fatalf("FAIL: m1 missing from updated — matching ifInState must apply the update")
	}
}

func TestEmailSetWithoutIfInStateStillApplies(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m1")

	// Control: ifInState is optional — its absence must keep proceeding.
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"m1": map[string]interface{}{"keywords/$flagged": true},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailSet(user, call)

	if resp.Name != "Email/set" {
		t.Fatalf("FAIL: response name = %q, want \"Email/set\"", resp.Name)
	}
	updated, _ := resp.Args["updated"].(map[string]interface{})
	if _, present := updated["m1"]; !present {
		t.Fatalf("FAIL: m1 missing from updated — absent ifInState must apply the update")
	}
}
