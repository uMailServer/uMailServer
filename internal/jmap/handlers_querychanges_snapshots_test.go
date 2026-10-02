package jmap

// Regression tests for the /queryChanges snapshot design: Email/query must
// snapshot its full ordered result, and Email/queryChanges must diff that
// snapshot against a fresh run to return REAL deltas (RFC 8620 §5.6). Before
// this design, Email/queryChanges answered stateMismatch for any token that
// was not the current one (the round-43 stub), so set-made changes never
// surfaced as deltas. Part (a) of the design — set-made changes appearing in
// Email/changes — was already satisfied by the storage journal (verified live
// below as a control).

import (
	"testing"
)

const snapUser = "user@example.com"

func TestQueryChangesReturnsRealDeltasForSetMadeMove(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(snapUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(snapUser, "Archive"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, snapUser, 1, "m1")
	seedSeen(t, db, snapUser, 2, "m2")

	inboxID := getMailboxIDFromName("INBOX")
	archiveID := getMailboxIDFromName("Archive")

	// Email/query snapshots the full ordered result.
	qResp := server.handleEmailQuery(snapUser, MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": snapUser,
			"filter":    map[string]interface{}{"inMailbox": inboxID},
		},
		ID: "q-1",
	})
	if qResp.Name != "Email/query" {
		t.Fatalf("CONTROL FAILED (harness): Email/query returned %s", qResp.Name)
	}
	queryState, _ := qResp.Args["queryState"].(string)
	if queryState == "" {
		t.Fatalf("CONTROL FAILED (harness): empty queryState")
	}

	// The client must have run the Archive query before the move for its
	// snapshot to exist (RFC 8620 §5.6: queryChanges works per query the
	// client has actually run; without a snapshot the answer is stateMismatch).
	aqResp := server.handleEmailQuery(snapUser, MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": snapUser,
			"filter":    map[string]interface{}{"inMailbox": archiveID},
		},
		ID: "q-2",
	})
	archiveQueryState, _ := aqResp.Args["queryState"].(string)
	if archiveQueryState == "" {
		t.Fatalf("CONTROL FAILED (harness): empty Archive queryState")
	}

	// The set-made change: move m1 from INBOX to Archive.
	sinceToken := server.stateToken(snapUser)
	setResp := server.handleEmailSet(snapUser, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": snapUser,
			"update": map[string]interface{}{
				"m1": map[string]interface{}{
					"mailboxIds": map[string]interface{}{"archive": true},
				},
			},
		},
		ID: "set-1",
	})
	if _, ok := setResp.Args["updated"].(map[string]interface{})["m1"]; !ok {
		t.Fatalf("CONTROL FAILED (harness): move did not apply: %v", setResp.Args)
	}

	// Part (a): the set-made change appears in Email/changes.
	chResp := server.handleEmailChanges(snapUser, MethodCall{
		Name: "Email/changes",
		Args: map[string]interface{}{
			"accountId":  snapUser,
			"sinceState": sinceToken,
		},
		ID: "ch-1",
	})
	if !changedIDs(chResp, "m1") {
		t.Fatalf("FAIL: the set-made move is missing from Email/changes: %v", chResp.Args)
	}

	// Part (b): Email/queryChanges returns REAL deltas for the INBOX query
	// (m1 left the result at its old position 0) instead of stateMismatch.
	qcResp := server.handleEmailQueryChanges(snapUser, MethodCall{
		Name: "Email/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": queryState,
			"filter":          map[string]interface{}{"inMailbox": inboxID},
		},
		ID: "qc-1",
	})
	if qcResp.Name == "error" {
		t.Fatalf("FAIL: Email/queryChanges returned the %v stub instead of real deltas", qcResp.Args)
	}
	removed, ok := qcResp.Args["removed"].([]int)
	if !ok {
		t.Fatalf("FAIL: removed = %v, want []int positions", qcResp.Args["removed"])
	}
	if len(removed) != 1 || removed[0] != 0 {
		t.Fatalf("FAIL: removed = %v, want [0] (m1 left the INBOX query at position 0)", removed)
	}

	// The Archive side of the same move: m1 was added at position 0.
	qc2 := server.handleEmailQueryChanges(snapUser, MethodCall{
		Name: "Email/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": archiveQueryState,
			"filter":          map[string]interface{}{"inMailbox": archiveID},
		},
		ID: "qc-2",
	})
	if qc2.Name == "error" {
		t.Fatalf("FAIL: Archive-side queryChanges returned %v", qc2.Args)
	}
	added, ok := qc2.Args["added"].([]map[string]interface{})
	if !ok {
		t.Fatalf("FAIL: added = %v, want []map[string]interface{}", qc2.Args["added"])
	}
	if len(added) != 1 || added[0]["id"] != "m1" || added[0]["index"] != 0 {
		t.Fatalf("FAIL: added = %v, want [{index:0 id:m1}]", added)
	}

	// The new query state must advance past the set's journal entry.
	newState, _ := qcResp.Args["newQueryState"].(string)
	if newState == "" || newState == queryState {
		t.Fatalf("FAIL: newQueryState = %q, want an advanced state", newState)
	}
}

func TestQueryChangesUnchangedQueryStillReturnsEmptyDelta(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	qResp := server.handleEmailQuery(snapUser, MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{"accountId": snapUser},
		ID:   "q-1",
	})
	queryState, _ := qResp.Args["queryState"].(string)

	qcResp := server.handleEmailQueryChanges(snapUser, MethodCall{
		Name: "Email/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": queryState,
		},
		ID: "qc-1",
	})
	if qcResp.Name != "Email/queryChanges" {
		t.Fatalf("FAIL: unchanged query got %s (%v)", qcResp.Name, qcResp.Args)
	}
	added, _ := qcResp.Args["added"].([]map[string]interface{})
	removed, _ := qcResp.Args["removed"].([]int)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("FAIL: unchanged query reported deltas: added=%v removed=%v", added, removed)
	}
	if qcResp.Args["newQueryState"] != queryState {
		t.Fatalf("FAIL: newQueryState = %v, want the unchanged %q", qcResp.Args["newQueryState"], queryState)
	}
}

func TestQueryChangesStaleAndMissingStatesStillMismatch(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	for _, since := range []string{"999999", ""} {
		qcResp := server.handleEmailQueryChanges(snapUser, MethodCall{
			Name: "Email/queryChanges",
			Args: map[string]interface{}{
				"accountId":       snapUser,
				"sinceQueryState": since,
			},
			ID: "qc-" + since,
		})
		if qcResp.Name != "error" {
			t.Fatalf("FAIL: sinceQueryState %q got %s, want error/stateMismatch", since, qcResp.Name)
		}
		if et, _ := qcResp.Args["type"].(string); et != "stateMismatch" {
			t.Fatalf("FAIL: sinceQueryState %q got error type %v, want stateMismatch", since, qcResp.Args["type"])
		}
	}
}

func TestMailboxQueryChangesReturnsRealDeltasForCreation(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(snapUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(snapUser, "Archive"); err != nil {
		t.Fatal(err)
	}

	// Mailbox/query snapshots the ordered mailbox-id list.
	qResp := server.handleMailboxQuery(snapUser, MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{"accountId": snapUser},
		ID:   "mq-1",
	})
	if qResp.Name != "Mailbox/query" {
		t.Fatalf("CONTROL FAILED (harness): Mailbox/query returned %s", qResp.Name)
	}
	queryState, _ := qResp.Args["queryState"].(string)
	if queryState == "" {
		t.Fatalf("CONTROL FAILED (harness): empty queryState")
	}

	// The creation: a new mailbox after the snapshot.
	if err := db.CreateMailbox(snapUser, "Projects"); err != nil {
		t.Fatal(err)
	}

	// Mailbox/queryChanges must return the creation as an added delta
	// instead of the stateMismatch stub.
	qcResp := server.handleMailboxQueryChanges(snapUser, MethodCall{
		Name: "Mailbox/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": queryState,
		},
		ID: "mqc-1",
	})
	if qcResp.Name == "error" {
		t.Fatalf("FAIL: Mailbox/queryChanges returned the %v stub instead of real deltas", qcResp.Args)
	}
	added, ok := qcResp.Args["added"].([]map[string]interface{})
	if !ok {
		t.Fatalf("FAIL: added = %v, want []map[string]interface{}", qcResp.Args["added"])
	}
	if len(added) != 1 || added[0]["id"] != "Projects" {
		t.Fatalf("FAIL: added = %v, want exactly the new mailbox Projects", added)
	}

	// The new state must be observable, and a re-queried client sees no
	// further delta.
	newState, _ := qcResp.Args["newQueryState"].(string)
	if newState == "" {
		t.Fatalf("FAIL: empty newQueryState")
	}
	rqResp := server.handleMailboxQuery(snapUser, MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{"accountId": snapUser},
		ID:   "mq-2",
	})
	newToken, _ := rqResp.Args["queryState"].(string)
	qc2 := server.handleMailboxQueryChanges(snapUser, MethodCall{
		Name: "Mailbox/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": newToken,
		},
		ID: "mqc-2",
	})
	added2, _ := qc2.Args["added"].([]map[string]interface{})
	if len(added2) != 0 {
		t.Fatalf("FAIL: re-queried client still sees deltas: %v", added2)
	}
}

func TestThreadQueryChangesReturnsRealDeltasForNewThread(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(snapUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	seedSeen(t, db, snapUser, 1, "m1")

	// Thread/query snapshots the ordered thread-id list.
	qResp := server.handleThreadQuery(snapUser, MethodCall{
		Name: "Thread/query",
		Args: map[string]interface{}{"accountId": snapUser},
		ID:   "tq-1",
	})
	if qResp.Name != "Thread/query" {
		t.Fatalf("CONTROL FAILED (harness): Thread/query returned %s", qResp.Name)
	}
	queryState, _ := qResp.Args["queryState"].(string)
	if queryState == "" {
		t.Fatalf("CONTROL FAILED (harness): empty queryState")
	}

	// A new thread appears: a second message in its own thread.
	seedSeen(t, db, snapUser, 2, "m2")

	qcResp := server.handleThreadQueryChanges(snapUser, MethodCall{
		Name: "Thread/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": queryState,
		},
		ID: "tqc-1",
	})
	if qcResp.Name == "error" {
		t.Fatalf("FAIL: Thread/queryChanges returned the %v stub instead of real deltas", qcResp.Args)
	}
	added, ok := qcResp.Args["added"].([]map[string]interface{})
	if !ok {
		t.Fatalf("FAIL: added = %v, want []map[string]interface{}", qcResp.Args["added"])
	}
	if len(added) != 1 || added[0]["id"] != "thread-m2" || added[0]["index"] != 1 {
		t.Fatalf("FAIL: added = %v, want [{index:1 id:thread-m2}]", added)
	}

	// A stale token must still mismatch.
	stale := server.handleThreadQueryChanges(snapUser, MethodCall{
		Name: "Thread/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": "999999",
		},
		ID: "tqc-2",
	})
	if stale.Name != "error" {
		t.Fatalf("FAIL: stale token answered %s, want error/stateMismatch", stale.Name)
	}
}

// Identities are read-only and derived from account settings
// (handleIdentitySet rejects all writes), so the Identity/query result is
// invariant: the queryChanges delta is always empty for a live snapshot and
// stateMismatch without one. The instruction's "new identity surfaces as
// added" scenario is structurally impossible in this model — pinned here as
// the honest contract instead of a fabricated creation.
func TestIdentityQueryChangesInvariantList(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	qResp := server.handleIdentityQuery(snapUser, MethodCall{
		Name: "Identity/query",
		Args: map[string]interface{}{"accountId": snapUser},
		ID:   "iq-1",
	})
	if qResp.Name != "Identity/query" {
		t.Fatalf("CONTROL FAILED (harness): Identity/query returned %s", qResp.Name)
	}
	queryState, _ := qResp.Args["queryState"].(string)

	qcResp := server.handleIdentityQueryChanges(snapUser, MethodCall{
		Name: "Identity/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": queryState,
		},
		ID: "iqc-1",
	})
	if qcResp.Name != "Identity/queryChanges" {
		t.Fatalf("FAIL: same-state Identity/queryChanges got %s (%v)", qcResp.Name, qcResp.Args)
	}
	added, _ := qcResp.Args["added"].([]map[string]interface{})
	removed, _ := qcResp.Args["removed"].([]int)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("FAIL: invariant identity list reported deltas: added=%v removed=%v", added, removed)
	}

	stale := server.handleIdentityQueryChanges(snapUser, MethodCall{
		Name: "Identity/queryChanges",
		Args: map[string]interface{}{
			"accountId":       snapUser,
			"sinceQueryState": "999999",
		},
		ID: "iqc-2",
	})
	if stale.Name != "error" {
		t.Fatalf("FAIL: stale token answered %s, want error/stateMismatch", stale.Name)
	}
}
