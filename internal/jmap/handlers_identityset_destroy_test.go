package jmap

// Regression test for the Identity/set destroy-response defect: the handler
// (a read-only stub — create and update answer notSupported) populated
// notDestroyed only for the literal "default" id, so destroying any other
// identity id put it in NEITHER destroyed nor notDestroyed. RFC 8620 §5.3
// requires every id from the destroy array to appear in exactly one of the
// two; a client otherwise cannot determine the outcome. Since identities are
// derived from account settings, every destroy must be rejected as
// notSupported — the default special case contradicted the handler's own
// read-only semantics.

import (
	"testing"
)

func identitySetDestroyCall(user string, ids ...string) MethodCall {
	destroy := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		destroy = append(destroy, id)
	}
	return MethodCall{
		Name: "Identity/set",
		Args: map[string]interface{}{
			"accountId": user,
			"destroy":   destroy,
		},
		ID: "call-1",
	}
}

func TestIdentitySet_EveryDestroyedIDIsReportedNotSupported(t *testing.T) {
	const user = "user@example.com"
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp := server.handleIdentitySet(user, identitySetDestroyCall(user, "some-other", "default"))

	nd, ok := resp.Args["notDestroyed"].(map[string]interface{})
	if !ok {
		t.Fatalf("FAIL: notDestroyed missing from response args: %v", resp.Args)
	}

	// RFC 8620 §5.3: every destroyed id must be reported.
	for _, id := range []string{"some-other", "default"} {
		if _, present := nd[id]; !present {
			t.Fatalf("FAIL: destroy id %q got no response entry (destroyed=%v notDestroyed=%v) — RFC 8620 §5.3 requires one of destroyed/notDestroyed per id", id, resp.Args["destroyed"], nd)
		}
		entry, _ := nd[id].(map[string]interface{})
		if entry["type"] != "notSupported" {
			t.Fatalf("FAIL: destroy id %q reported as %v, want notSupported (identities are read-only)", id, entry)
		}
	}

	// And nothing may be reported as destroyed: identities are derived.
	destroyed, _ := resp.Args["destroyed"].([]string)
	if len(destroyed) != 0 {
		t.Fatalf("FAIL: read-only identities reported as destroyed: %v", destroyed)
	}
}
