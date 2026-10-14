package jmap

// Regression test for the illegal SetError type on invalid Mailbox/set
// create records: the create loop typed notCreated entries with
// "invalidArguments" — a *method-level* error name (RFC 8620 §3.6.1) that is
// not part of the §2.6 SetError type set — where §2.6 assigns
// "invalidProperties" to "(create; update): The record given is invalid in
// some way", with a SHOULD to list the invalid properties. The in-repo F6189
// duplicate-name site already used that shape; the non-object-value and
// missing-name sites did not.

import (
	"testing"
)

func TestMailboxSetCreateInvalidRecordsUseInvalidProperties(t *testing.T) {
	const user = "user@example.com"

	t.Run("non-object create value", func(t *testing.T) {
		server, _, _, cleanup := setupTestServer(t)
		defer cleanup()

		resp := server.handleMailboxSet(user, MethodCall{
			Name: "Mailbox/set",
			Args: map[string]interface{}{
				"accountId": user,
				"create": map[string]interface{}{
					"m1": "not-an-object",
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Mailbox)
		if _, ok := created["m1"]; ok {
			t.Fatalf("FAIL: non-object create value was created; want rejection")
		}
		notCreated, _ := resp.Args["notCreated"].(map[string]interface{})
		errObj, ok := notCreated["m1"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither created nor reported in notCreated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notCreated SetError type = %q, want \"invalidProperties\" (RFC 8620 §2.6: the record given is invalid per the type specification; \"invalidArguments\" is a method-level error name, §3.6.1)", got)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		server, _, _, cleanup := setupTestServer(t)
		defer cleanup()

		resp := server.handleMailboxSet(user, MethodCall{
			Name: "Mailbox/set",
			Args: map[string]interface{}{
				"accountId": user,
				"create": map[string]interface{}{
					"m2": map[string]interface{}{"parentId": "x"},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Mailbox)
		if _, ok := created["m2"]; ok {
			t.Fatalf("FAIL: name-less create was created; want rejection")
		}
		notCreated, _ := resp.Args["notCreated"].(map[string]interface{})
		errObj, ok := notCreated["m2"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither created nor reported in notCreated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notCreated SetError type = %q, want \"invalidProperties\" (RFC 8620 §2.6)", got)
		}
		props, ok := errObj["properties"].([]string)
		if !ok || len(props) != 1 || props[0] != "name" {
			t.Fatalf("FAIL: SetError lacks properties list naming \"name\" (RFC 8620 §2.6 SHOULD): %v", errObj)
		}
	})

	t.Run("valid create control", func(t *testing.T) {
		server, db, _, cleanup := setupTestServer(t)
		defer cleanup()

		resp := server.handleMailboxSet(user, MethodCall{
			Name: "Mailbox/set",
			Args: map[string]interface{}{
				"accountId": user,
				"create": map[string]interface{}{
					"m3": map[string]interface{}{"name": "ControlMailbox"},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Mailbox)
		if _, ok := created["m3"]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: valid create rejected: %v", resp.Args)
		}
		mailboxes, _ := db.ListMailboxes(user)
		found := false
		for _, m := range mailboxes {
			if m == "ControlMailbox" {
				found = true
			}
		}
		if !found {
			t.Fatalf("FAIL: CONTROL FAILED: mailbox ControlMailbox not stored after create")
		}
	})
}
