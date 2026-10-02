package jmap

// Regression test for the non-atomic Email/set patch defect: when any patch
// path was invalid, handleEmailSet recorded notUpdated[id]=invalidPatch but
// the `continue` only exited the inner patch-key loop — the mailboxIds move
// still executed, the mutated metadata was persisted, and updated[id] was
// added, so the same id appeared in BOTH updated and notUpdated while storage
// changed on an update reported as failed. RFC 8620 §4.3: an invalid patch
// rejects the whole update for that object — no partial application, id in
// notUpdated only. (Round 20's keywords test covers the single-key invalid
// patch, where no mutation precedes the rejection; it stays green.)

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// seedSeen stores a message flagged \Seen so a rejected patch can be checked
// for "flags untouched" via synthesis-immune enumeration (GetMessageUIDs +
// GetMessageMetadata; GetMailbox/GetMessageMetadata synthesize defaults for
// missing keys, so absence is probed by enumeration, never by error).
func seedSeen(t *testing.T, db *storage.Database, user string, uid uint32, id string) {
	t.Helper()
	if err := db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{
		UID: uid, MessageID: id, ThreadID: "thread-" + id,
		Subject: "s-" + id, From: "sender@example.com", To: user,
		Size: 100, Flags: []string{"\\Seen"},
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestEmailSetInvalidPatchDoesNotMoveOrDoubleReport(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m1")

	// One patch with an invalid keyword path value AND a valid move target.
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"m1": map[string]interface{}{
					"keywords/$seen": "yes",                                   // invalid: non-bool, non-nil
					"mailboxIds":     map[string]interface{}{"archive": true}, // must NOT be applied
				},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailSet(user, call)

	// Response: id must be in notUpdated(invalidPatch) and NOT in updated.
	if got, _ := resp.Args["notUpdated"].(map[string]interface{})["m1"].(map[string]interface{})["type"].(string); got != "invalidPatch" {
		t.Fatalf("FAIL: notUpdated[m1].type = %q, want \"invalidPatch\"", got)
	}
	if _, present := resp.Args["updated"].(map[string]interface{})["m1"]; present {
		t.Fatalf("FAIL: m1 appears in BOTH updated and notUpdated — contradictory response (RFC 8620 §4.3)")
	}

	// Storage: the move must NOT have happened for the rejected update.
	archiveUIDs, _ := db.GetMessageUIDs(user, "Archive")
	if len(archiveUIDs) != 0 {
		t.Fatalf("FAIL: m1 moved to Archive despite invalidPatch rejection (Archive holds %d message(s))", len(archiveUIDs))
	}
	inboxUIDs, _ := db.GetMessageUIDs(user, "INBOX")
	var kept bool
	for _, uid := range inboxUIDs {
		if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == "m1" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("FAIL: m1 vanished from INBOX (moved despite rejection)")
	}
}

func TestEmailSetInvalidPatchLeavesPropertyFormUnapplied(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m3")

	// Valid keywords property form + invalid path value in the SAME patch:
	// the whole update is rejected, so the property replace must not persist.
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"m3": map[string]interface{}{
					"keywords":       map[string]interface{}{"$flagged": true},
					"keywords/$seen": "yes",
				},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailSet(user, call)

	if _, present := resp.Args["updated"].(map[string]interface{})["m3"]; present {
		t.Fatalf("FAIL: m3 reported updated despite invalidPatch")
	}
	if _, present := resp.Args["notUpdated"].(map[string]interface{})["m3"]; !present {
		t.Fatalf("FAIL: m3 missing from notUpdated")
	}

	// Synthesis-immune probe: persisted flags must still be the seeded \Seen,
	// not the property form's replacement (\Flagged) nor a mixture.
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	for _, uid := range uids {
		meta, _ := db.GetMessageMetadata(user, "INBOX", uid)
		if meta != nil && meta.MessageID == "m3" {
			if len(meta.Flags) != 1 || meta.Flags[0] != "\\Seen" {
				t.Fatalf("FAIL: m3 flags persisted as %v, want untouched [\\Seen]", meta.Flags)
			}
			return
		}
	}
	t.Fatalf("FAIL: m3 not found in INBOX")
}

func TestEmailSetValidPatchStillAppliesAfterAtomicityFix(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, user, 1, "m2")

	// Control: a patch with only the valid property form applies normally.
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"m2": map[string]interface{}{
					"keywords": map[string]interface{}{"$flagged": true},
				},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailSet(user, call)

	if _, present := resp.Args["updated"].(map[string]interface{})["m2"]; !present {
		t.Fatalf("CONTROL FAILED: m2 missing from updated (harness broken)")
	}
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	for _, uid := range uids {
		meta, _ := db.GetMessageMetadata(user, "INBOX", uid)
		if meta != nil && meta.MessageID == "m2" {
			if len(meta.Flags) != 1 || meta.Flags[0] != "\\Flagged" {
				t.Fatalf("CONTROL FAILED: m2 flags = %v, want [\\Flagged]", meta.Flags)
			}
			return
		}
	}
	t.Fatalf("CONTROL FAILED: m2 not found in INBOX")
}
