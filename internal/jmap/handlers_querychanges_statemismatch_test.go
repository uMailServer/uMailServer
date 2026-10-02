package jmap

// Regression test for the /queryChanges stub defect: all four /queryChanges
// handlers (Mailbox, Email, Thread, Identity) discarded sinceQueryState and
// always answered HTTP 200 with an empty delta (added: [], removed: [],
// hasMoreChanges: false) and a time-derived newQueryState. The server keeps
// no query snapshots, so it cannot compute the delta for any sinceQueryState
// other than the current one; RFC 8620 §5.6 requires a stateMismatch error
// in that case instead of a silently misleading "nothing changed" delta.
// The equal-token case gets an honest empty delta whose newQueryState is the
// journal-derived state /query already emits.

import (
	"testing"
)

// queryChangesHandlers pairs each /queryChanges method name with its handler.
func queryChangesHandlers(s *Server) map[string]func(string, MethodCall) Response {
	return map[string]func(string, MethodCall) Response{
		"Mailbox/queryChanges":  s.handleMailboxQueryChanges,
		"Email/queryChanges":    s.handleEmailQueryChanges,
		"Thread/queryChanges":   s.handleThreadQueryChanges,
		"Identity/queryChanges": s.handleIdentityQueryChanges,
	}
}

func TestQueryChangesRejectsStaleState(t *testing.T) {
	const user = "user@example.com"
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	for name, handler := range queryChangesHandlers(server) {
		resp := handler(user, MethodCall{
			Name: name,
			Args: map[string]interface{}{
				"accountId":       user,
				"sinceQueryState": "999999",
			},
			ID: "c1",
		})
		if resp.Name != "error" {
			t.Fatalf("FAIL: %s with stale token answered %q, want error (RFC 8620 §5.6 stateMismatch)", name, resp.Name)
		}
		if typ, _ := resp.Args["type"].(string); typ != "stateMismatch" {
			t.Fatalf("FAIL: %s error type = %q, want stateMismatch", name, typ)
		}
		if resp.ID != "c1" {
			t.Fatalf("FAIL: %s error call id = %q, want c1", name, resp.ID)
		}
	}
}

func TestQueryChangesAcceptsCurrentState(t *testing.T) {
	const user = "user@example.com"
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Email/queryChanges now derives deltas from query snapshots: the client
	// must have run Email/query so a snapshot exists at the token it
	// presents. Mailbox/Thread/Identity queryChanges keep the round-43
	// current-token contract (no snapshots).
	qResp := server.handleEmailQuery(user, MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{"accountId": user},
		ID:   "q-1",
	})
	current, _ := qResp.Args["queryState"].(string)
	if current == "" {
		t.Fatal("empty queryState from Email/query")
	}

	// Mailbox/queryChanges derives its delta from its own snapshot: run
	// Mailbox/query so the snapshot exists at this same state. The same
	// applies to Thread/query and Identity/query for their handlers.
	server.handleMailboxQuery(user, MethodCall{
		Name: "Mailbox/query",
		Args: map[string]interface{}{"accountId": user},
		ID:   "q-2",
	})
	server.handleThreadQuery(user, MethodCall{
		Name: "Thread/query",
		Args: map[string]interface{}{"accountId": user},
		ID:   "q-3",
	})
	server.handleIdentityQuery(user, MethodCall{
		Name: "Identity/query",
		Args: map[string]interface{}{"accountId": user},
		ID:   "q-4",
	})

	for name, handler := range queryChangesHandlers(server) {
		resp := handler(user, MethodCall{
			Name: name,
			Args: map[string]interface{}{
				"accountId":       user,
				"sinceQueryState": current,
			},
			ID: "c2",
		})
		if resp.Name != name {
			t.Fatalf("FAIL: %s with current token answered %q, want the method name", name, resp.Name)
		}
		newState, _ := resp.Args["newQueryState"].(string)
		if newState != current {
			t.Fatalf("FAIL: %s newQueryState = %q, want the journal-derived current state %q", name, newState, current)
		}
	}
}

func TestQueryChangesRejectsMissingState(t *testing.T) {
	const user = "user@example.com"
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// sinceQueryState is required (RFC 8620 §5.6); an absent token cannot
	// identify any state, so the handlers answer stateMismatch.
	for name, handler := range queryChangesHandlers(server) {
		resp := handler(user, MethodCall{
			Name: name,
			Args: map[string]interface{}{
				"accountId": user,
			},
			ID: "c3",
		})
		if resp.Name != "error" {
			t.Fatalf("FAIL: %s with missing sinceQueryState answered %q, want error", name, resp.Name)
		}
		if typ, _ := resp.Args["type"].(string); typ != "stateMismatch" {
			t.Fatalf("FAIL: %s error type = %q, want stateMismatch", name, typ)
		}
	}
}
