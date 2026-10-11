package jmap

// Round 167 regression proofs: Email/query filter operators, unsupported
// filter/sort, multi-comparator sort, isAscending default (RFC 8620 §5.5),
// and strict argument typing for /get, /set and /query.

import (
	"sort"
	"testing"
)

const r167User = "user@example.com"

func r167Query(t *testing.T, server *Server, args map[string]interface{}) Response {
	t.Helper()
	args["accountId"] = r167User
	return server.processMethodCall(r167User, MethodCall{Name: "Email/query", Args: args, ID: "c1"})
}

func r167Ids(t *testing.T, resp Response) []string {
	t.Helper()
	ids, ok := resp.Args["ids"].([]string)
	if !ok {
		t.Fatalf("no ids in response: %#v", resp.Args)
	}
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func r167EqualSet(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v want %v", label, got, want)
		}
	}
}

func r167ErrType(resp Response) string {
	if resp.Name != "error" {
		return ""
	}
	s, _ := resp.Args["type"].(string)
	return s
}

func TestR167_FilterOperatorNOT(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	resp := r167Query(t, server, map[string]interface{}{"filter": map[string]interface{}{
		"operator":   "NOT",
		"conditions": []interface{}{map[string]interface{}{"subject": "Alpha"}},
	}})
	r167EqualSet(t, r167Ids(t, resp), []string{"msg-b", "msg-c"}, "NOT subject=Alpha")
}

func TestR167_FilterOperatorOR_AND(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	resp := r167Query(t, server, map[string]interface{}{"filter": map[string]interface{}{
		"operator": "OR",
		"conditions": []interface{}{
			map[string]interface{}{"subject": "Alpha"},
			map[string]interface{}{"subject": "Bravo"},
		},
	}})
	r167EqualSet(t, r167Ids(t, resp), []string{"msg-a", "msg-b"}, "OR")
	resp = r167Query(t, server, map[string]interface{}{"filter": map[string]interface{}{
		"operator": "AND",
		"conditions": []interface{}{
			map[string]interface{}{"subject": "a"},
			map[string]interface{}{"minSize": 150},
		},
	}})
	r167EqualSet(t, r167Ids(t, resp), []string{"msg-b", "msg-c"}, "AND")
}

func TestR167_UnsupportedFilterRejected(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	for _, f := range []map[string]interface{}{
		{"body": "x"},
		{"bogusProperty": 1},
		{"operator": "XOR", "conditions": []interface{}{}},
	} {
		resp := r167Query(t, server, map[string]interface{}{"filter": f})
		got := r167ErrType(resp)
		if got != "unsupportedFilter" && got != "invalidArguments" {
			t.Fatalf("filter %v: type = %q (%v), want unsupportedFilter/invalidArguments", f, got, resp.Args)
		}
	}
	resp := r167Query(t, server, map[string]interface{}{"filter": map[string]interface{}{"body": "x"}})
	if r167ErrType(resp) != "unsupportedFilter" {
		t.Fatalf("body filter: %v", resp.Args)
	}
}

func TestR167_UnsupportedSortRejected(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	resp := r167Query(t, server, map[string]interface{}{"sort": []interface{}{
		map[string]interface{}{"property": "bogus", "isAscending": true},
	}})
	if r167ErrType(resp) != "unsupportedSort" {
		t.Fatalf("got %v want unsupportedSort", resp.Args)
	}
}

func TestR167_MultiComparatorSort(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db) // identical "from"
	ids := queryEmailIDs(t, server, []interface{}{
		map[string]interface{}{"property": "from", "isAscending": true},
		map[string]interface{}{"property": "subject", "isAscending": false},
	})
	assertEmailOrder(t, ids, []string{"msg-c", "msg-b", "msg-a"}, "from asc, subject desc")
}

// RFC 8620 §5.5: isAscending defaults to true.
func TestR167_IsAscendingDefaultsTrue(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	ids := queryEmailIDs(t, server, []interface{}{map[string]interface{}{"property": "receivedAt"}})
	assertEmailOrder(t, ids, []string{"msg-a", "msg-b", "msg-c"}, "isAscending omitted")
}

func TestR167_QueryArgTyping(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	for _, a := range []map[string]interface{}{
		{"position": "1"},
		{"limit": "5"},
		{"calculateTotal": "yes"},
		{"anchor": 5},
		{"sort": "receivedAt"},
		{"filter": "x"},
	} {
		resp := r167Query(t, server, a)
		if r167ErrType(resp) != "invalidArguments" {
			t.Fatalf("args %v: got %v want invalidArguments", a, resp.Args)
		}
	}
}

func TestR167_GetIdsTyping(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	for _, ids := range []interface{}{"msg-a", []interface{}{1, "msg-a"}, map[string]interface{}{}} {
		resp := server.processMethodCall(r167User, MethodCall{Name: "Email/get", ID: "c1",
			Args: map[string]interface{}{"accountId": r167User, "ids": ids}})
		if r167ErrType(resp) != "invalidArguments" {
			t.Fatalf("ids %v: got %v want invalidArguments", ids, resp.Args)
		}
	}
}

func TestR167_SetArgTyping(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	for _, a := range []map[string]interface{}{
		{"update": []interface{}{}},
		{"create": "x"},
		{"destroy": "msg-a"},
		{"destroy": []interface{}{5}},
		{"ifInState": 5},
	} {
		a["accountId"] = r167User
		resp := server.processMethodCall(r167User, MethodCall{Name: "Email/set", ID: "c1", Args: a})
		if r167ErrType(resp) != "invalidArguments" {
			t.Fatalf("args %v: got %v want invalidArguments", a, resp.Args)
		}
	}
	// nothing destroyed by the rejected calls
	if len(server.findEmailCopies(r167User, "msg-a")) == 0 {
		t.Fatal("msg-a destroyed by invalid request")
	}
}

func TestR167_SetUnmodeledKeywordAndMailboxFalse(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	patches := []map[string]interface{}{
		{"keywords/custom": true},
		{"keywords": map[string]interface{}{"custom": true}},
		{"keywords/$seen": "yes"},
		{"mailboxIds": map[string]interface{}{"INBOX": false}},
	}
	for _, p := range patches {
		resp := server.processMethodCall(r167User, MethodCall{Name: "Email/set", ID: "c1",
			Args: map[string]interface{}{"accountId": r167User,
				"update": map[string]interface{}{"msg-a": p}}})
		nu, _ := resp.Args["notUpdated"].(map[string]interface{})
		e, _ := nu["msg-a"].(map[string]interface{})
		if e == nil {
			t.Fatalf("patch %v accepted: %v", p, resp.Args)
		}
		if tp := e["type"]; tp != "invalidProperties" && tp != "invalidPatch" {
			t.Fatalf("patch %v: type %v", p, tp)
		}
	}
}

func TestR167_UpdateAndDestroySameIDWillDestroy(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)
	resp := server.processMethodCall(r167User, MethodCall{Name: "Email/set", ID: "c1",
		Args: map[string]interface{}{"accountId": r167User,
			"update":  map[string]interface{}{"msg-a": map[string]interface{}{"keywords/$seen": true}},
			"destroy": []interface{}{"msg-a"}}})
	nu, _ := resp.Args["notUpdated"].(map[string]interface{})
	e, _ := nu["msg-a"].(map[string]interface{})
	if e == nil || e["type"] != "willDestroy" {
		t.Fatalf("want willDestroy, got %v", resp.Args)
	}
}
