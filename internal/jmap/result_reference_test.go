package jmap

// Regression tests for F4997: RFC 8620 §3.7 result references ("#ids") were
// ignored, so Email/query + Email/get{"#ids": ResultReference} silently
// returned an empty list, and a reference to a failed or unknown call made
// Mailbox/get return every mailbox instead of invalidResultReference.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/storage"
)

type resultRefResp struct {
	Name string
	Args map[string]interface{}
	ID   string
}

func resultRefDo(t *testing.T, calls []interface{}) []resultRefResp {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("INVALID db: %v", err)
	}
	defer db.Close()
	user := "alice@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	for i, id := range []string{"m1", "m2"} {
		_ = db.StoreMessageMetadata(user, "INBOX", uint32(i+1), &storage.MessageMetadata{MessageID: id, UID: uint32(i + 1), Subject: "s " + id, InternalDate: time.Now()})
	}
	srv := NewServer(db, nil, nil, Config{JWTSecret: "result-ref-fixture"})
	ts := httptest.NewServer(srv)
	defer ts.Close()
	body, _ := json.Marshal(map[string]interface{}{"using": []string{}, "methodCalls": calls})
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("result-ref-fixture"))
	req, _ := http.NewRequest("POST", ts.URL+"/jmap/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("INVALID do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		MethodResponses [][]json.RawMessage `json:"methodResponses"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("INVALID response %d: %s", resp.StatusCode, raw)
	}
	var res []resultRefResp
	for _, mr := range out.MethodResponses {
		var r resultRefResp
		_ = json.Unmarshal(mr[0], &r.Name)
		_ = json.Unmarshal(mr[1], &r.Args)
		_ = json.Unmarshal(mr[2], &r.ID)
		res = append(res, r)
	}
	return res
}

func resultRefListLen(r resultRefResp) int {
	l, _ := r.Args["list"].([]interface{})
	return len(l)
}

func TestResultReference_ExplicitIDsControl(t *testing.T) {
	res := resultRefDo(t, []interface{}{
		[]interface{}{"Email/get", map[string]interface{}{"ids": []string{"m1", "m2"}}, "g"},
	})
	fmt.Printf("CONTROL EXPECTED: explicit ids -> 2 emails | ACTUAL: %d\n", resultRefListLen(res[0]))
	if len(res) != 1 || resultRefListLen(res[0]) != 2 {
		t.Fatalf("INVALID control")
	}
}

func TestResultReference_EmailQueryToGet(t *testing.T) {
	res := resultRefDo(t, []interface{}{
		[]interface{}{"Email/query", map[string]interface{}{}, "q"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": map[string]interface{}{"resultOf": "q", "name": "Email/query", "path": "/ids"}}, "g"},
	})
	got := -1
	if len(res) == 2 {
		got = resultRefListLen(res[1])
	}
	fmt.Printf("EXPECTED: Email/get #ids from Email/query -> 2 emails | ACTUAL: %d (%v)\n", got, res)
	if got != 2 {
		t.Errorf("back-reference ignored")
	}
}

func TestResultReference_FailedOrUnknownCall(t *testing.T) {
	res := resultRefDo(t, []interface{}{
		[]interface{}{"Mailbox/query", map[string]interface{}{"accountId": "bob@example.com"}, "q"},
		[]interface{}{"Mailbox/get", map[string]interface{}{"#ids": map[string]interface{}{"resultOf": "q", "name": "Mailbox/query", "path": "/ids"}}, "g"},
		[]interface{}{"Mailbox/get", map[string]interface{}{"#ids": map[string]interface{}{"resultOf": "nope", "name": "Mailbox/query", "path": "/ids"}}, "h"},
	})
	if len(res) != 3 {
		t.Fatalf("INVALID response count %d", len(res))
	}
	for _, r := range res[1:] {
		fmt.Printf("EXPECTED: reference to failed/unknown call -> error invalidResultReference | ACTUAL: %s type=%v list=%d\n", r.Name, r.Args["type"], resultRefListLen(r))
		if r.Name != "error" || r.Args["type"] != "invalidResultReference" {
			t.Errorf("unresolvable reference not rejected (call %s)", r.ID)
		}
	}
}

func resultRefArg(resultOf, name, path string) map[string]interface{} {
	return map[string]interface{}{"resultOf": resultOf, "name": name, "path": path}
}

func TestResultReference_Edges(t *testing.T) {
	res := resultRefDo(t, []interface{}{
		[]interface{}{"Email/get", map[string]interface{}{"ids": []string{"m1", "m2"}}, "a"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": resultRefArg("a", "Email/get", "/list/*/id")}, "b"},
		[]interface{}{"Email/get", map[string]interface{}{"ids": []string{"m1"}, "#ids": resultRefArg("a", "Email/get", "/list/*/id")}, "c"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": resultRefArg("a", "Email/get", "/nope")}, "d"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": resultRefArg("a", "Email/query", "/list/*/id")}, "e"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": resultRefArg("a", "Email/get", "/list/5/id")}, "f"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": resultRefArg("a", "Email/get", "/list/0")}, "g"},
		[]interface{}{"Email/get", map[string]interface{}{"#ids": "garbage"}, "h"},
	})
	if len(res) != 8 {
		t.Fatalf("INVALID count %d", len(res))
	}
	fmt.Printf("EDGE wildcard /list/*/id: EXPECTED 2 | ACTUAL %d\n", resultRefListLen(res[1]))
	if resultRefListLen(res[1]) != 2 {
		t.Errorf("wildcard failed")
	}
	want := map[string]string{"c": "invalidArguments", "d": "invalidResultReference", "e": "invalidResultReference", "f": "invalidResultReference", "h": "invalidResultReference"}
	for _, r := range res[2:] {
		if exp, ok := want[r.ID]; ok {
			fmt.Printf("EDGE call %s: EXPECTED error %s | ACTUAL %s %v\n", r.ID, exp, r.Name, r.Args["type"])
			if r.Name != "error" || r.Args["type"] != exp {
				t.Errorf("edge %s failed", r.ID)
			}
		}
	}
	// "/list/0" resolves to an object, not an id array: the handler sees no ids.
	fmt.Printf("EDGE call g (non-array value): EXPECTED Email/get with empty list | ACTUAL %s list=%d\n", res[6].Name, resultRefListLen(res[6]))
	if res[6].Name != "Email/get" || resultRefListLen(res[6]) != 0 {
		t.Errorf("edge g failed")
	}
}
