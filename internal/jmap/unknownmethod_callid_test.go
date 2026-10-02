package jmap

// Regression test for the unknownMethod call-id defect: dispatchMethodCall's
// default branch returned Response{Name:"error", Args:{type:"unknownMethod"}}
// WITHOUT copying call.ID, so the wire response was ["error",{...},""].
// RFC 8620 §3.4: a method response is ["Name", {arguments}, "callId"] where
// the call id is "the call id of the method call that it is responding to" —
// clients match responses to calls by it, and §3.6.1 routes unknown-method
// errors through regular method responses. Every other dispatch path already
// echoed call.ID; the fallback was the lone inconsistent site.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var unknownMethodTestSecret = fmt.Sprintf("%x", sha256.Sum256([]byte("unknownmethod-callid-fixture")))

func unknownMethodToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "alice@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(unknownMethodTestSecret))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return signed
}

func unknownMethodServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServer(nil, nil, nil, Config{JWTSecret: unknownMethodTestSecret})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func unknownMethodPost(t *testing.T, ts *httptest.Server, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/jmap/api", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+unknownMethodToken(t))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestUnknownMethodResponseEchoesCallID(t *testing.T) {
	ts := unknownMethodServer(t)

	req := map[string]interface{}{
		"using":       []string{"urn:ietf:params:jmap:core"},
		"methodCalls": []interface{}{[]interface{}{"Core/echo", map[string]interface{}{"ping": 1}, "c0"}},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	code, body := unknownMethodPost(t, ts, raw)
	if code != 200 {
		t.Fatalf("FAIL: status = %d, want 200 (body: %s)", code, body)
	}

	// Raw wire form: the third element of methodResponses[0] is the echoed id.
	var wire struct {
		MethodResponses [][]json.RawMessage `json:"methodResponses"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(wire.MethodResponses) != 1 {
		t.Fatalf("methodResponses count = %d, want 1", len(wire.MethodResponses))
	}
	var gotID string
	if err := json.Unmarshal(wire.MethodResponses[0][2], &gotID); err != nil || gotID != "c0" {
		t.Fatalf("FAIL: unknownMethod response call id = %q (%v), want \"c0\"", gotID, err)
	}

	// Typed decode through the production unmarshaler: name and error type intact.
	var parsed ResponseObject
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("typed decode: %v", err)
	}
	if parsed.MethodResponses[0].Name != "error" {
		t.Fatalf("FAIL: response name = %q, want \"error\"", parsed.MethodResponses[0].Name)
	}
	if got, _ := parsed.MethodResponses[0].Args["type"].(string); got != "unknownMethod" {
		t.Fatalf("FAIL: args.type = %q, want \"unknownMethod\"", got)
	}
	if parsed.MethodResponses[0].ID != "c0" {
		t.Fatalf("FAIL: typed response ID = %q, want \"c0\"", parsed.MethodResponses[0].ID)
	}
}

func TestUnknownMethodCallIDCorrelationAcrossCalls(t *testing.T) {
	ts := unknownMethodServer(t)

	req := map[string]interface{}{
		"using": []string{"urn:ietf:params:jmap:core"},
		"methodCalls": []interface{}{
			[]interface{}{"Core/echo", map[string]interface{}{"ping": 1}, "c1"},
			[]interface{}{"Core/echo", map[string]interface{}{"ping": 2}, "c2"},
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	code, body := unknownMethodPost(t, ts, raw)
	if code != 200 {
		t.Fatalf("FAIL: status = %d, want 200 (body: %s)", code, body)
	}

	var parsed ResponseObject
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(parsed.MethodResponses) != 2 {
		t.Fatalf("FAIL: methodResponses count = %d, want 2", len(parsed.MethodResponses))
	}
	ids := []string{parsed.MethodResponses[0].ID, parsed.MethodResponses[1].ID}
	sort.Strings(ids)
	if ids[0] != "c1" || ids[1] != "c2" {
		t.Fatalf("FAIL: response call ids = %v, want each call's own id (c1, c2) — client cannot correlate errors to calls", ids)
	}
	for _, resp := range parsed.MethodResponses {
		if got, _ := resp.Args["type"].(string); got != "unknownMethod" {
			t.Fatalf("FAIL: args.type = %q, want \"unknownMethod\"", got)
		}
	}
}
