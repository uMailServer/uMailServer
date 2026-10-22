package jmap

import "testing"

// RFC 8620 §2.6 enumerates the SetError types a server may return for a
// failed create, update or destroy: forbidden, overQuota, tooLarge,
// rateLimit, notFound, invalidPatch, willDestroy, invalidProperties,
// singleton. "notSupported" is not among them — the round-35 adjudication
// removed it from the Email paths for exactly this reason. Identity/set
// rejects every write (identities are derived from account settings), so
// each rejection must carry a §2.6-legal type: on a read-only object the
// writes are forbidden.
func TestIdentitySetSetErrorTypesAreSpecLegal(t *testing.T) {
	const user = "user@example.com"
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	legal := map[string]bool{
		"forbidden":         true,
		"overQuota":         true,
		"tooLarge":          true,
		"rateLimit":         true,
		"notFound":          true,
		"invalidPatch":      true,
		"willDestroy":       true,
		"invalidProperties": true,
		"singleton":         true,
	}

	calls := []struct {
		name    string
		args    map[string]interface{}
		carrier string
	}{
		{"create", map[string]interface{}{"create": map[string]interface{}{"c1": map[string]interface{}{}}}, "notCreated"},
		{"update", map[string]interface{}{"update": map[string]interface{}{"default": map[string]interface{}{}}}, "notUpdated"},
		{"destroy", map[string]interface{}{"destroy": []interface{}{"default"}}, "notDestroyed"},
	}

	for _, tc := range calls {
		resp := server.handleIdentitySet(user, MethodCall{Name: "Identity/set", Args: tc.args, ID: "c-" + tc.name})
		entries, ok := resp.Args[tc.carrier].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: %s: %s missing from response args: %v", tc.name, tc.carrier, resp.Args)
		}
		if len(entries) == 0 {
			t.Fatalf("FAIL: %s: no rejection reported (want one per requested id)", tc.name)
		}
		for id, v := range entries {
			entry, _ := v.(map[string]interface{})
			typ, _ := entry["type"].(string)
			if !legal[typ] {
				t.Errorf("%s id %q: SetError type %q is not an RFC 8620 §2.6 SetError type", tc.name, id, typ)
			}
		}
	}
}
