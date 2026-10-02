package jmap

// Regression test for the JMAP wire-format defect: MethodCall was parsed and
// Response emitted in a non-standard OBJECT form ({"name","args","id"}) while
// RFC 8620 §3.3/§3.4 normatively define method calls and method responses as
// 3-element ARRAYS ["Name", {arguments}, "callId"]. Every RFC-compliant JMAP
// client was rejected with 400 invalidArguments, and responses were emitted in
// a form such clients cannot parse. The fix decodes the array form (plus the
// legacy object form for compatibility with clients written against this
// server's pre-RFC wire format) and emits method responses in array form.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// rfcWireTestSecret is runtime-derived so no credential literal is embedded in
// the test source; it only needs to be identical for token minting and server
// configuration within this test.
var rfcWireTestSecret = fmt.Sprintf("%x", sha256.Sum256([]byte("rfc8620-wire-form-fixture")))

func rfcWireToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "alice@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(rfcWireTestSecret))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return signed
}

func rfcWireServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServer(nil, nil, nil, Config{JWTSecret: rfcWireTestSecret})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func rfcWirePost(t *testing.T, ts *httptest.Server, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/jmap/api", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rfcWireToken(t))
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

// assertUnknownMethodResponse checks a 200 body whose single method response is
// the RFC 8620 array-form reply for an unknown method: ["error",{"type":"unknownMethod"},id].
func assertUnknownMethodResponse(t *testing.T, body []byte) {
	t.Helper()
	// Raw wire form: methodResponses[0] must be a 3-element array per RFC 8620 §3.4.
	var wire struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("response body is not a JMAP response object: %v", err)
	}
	if len(wire.MethodResponses) != 1 {
		t.Fatalf("methodResponses count = %d, want 1", len(wire.MethodResponses))
	}
	var entry []json.RawMessage
	if err := json.Unmarshal(wire.MethodResponses[0], &entry); err != nil {
		t.Fatalf("methodResponses[0] is not RFC 8620 array form: %v (%s)", err, wire.MethodResponses[0])
	}
	if len(entry) != 3 {
		t.Fatalf("methodResponses[0] has %d elements, want 3", len(entry))
	}
	var name string
	if err := json.Unmarshal(entry[0], &name); err != nil || name != "error" {
		t.Fatalf("methodResponses[0][0] = %s, want \"error\"", wire.MethodResponses[0])
	}
	// Typed decode through the production unmarshaler.
	var parsed ResponseObject
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("typed decode of response failed: %v", err)
	}
	if got, _ := parsed.MethodResponses[0].Args["type"].(string); got != "unknownMethod" {
		t.Fatalf("methodResponses[0].args.type = %q, want \"unknownMethod\"", got)
	}
}

func TestRFC8620ArrayFormRequestAcceptedAndResponseArrayForm(t *testing.T) {
	ts := rfcWireServer(t)

	// RFC 8620 §3.3: a method call is ["Name", {arguments}, "callId"].
	req := map[string]interface{}{
		"using":       []string{"urn:ietf:params:jmap:core"},
		"methodCalls": []interface{}{[]interface{}{"Core/echo", map[string]interface{}{"ping": 1}, "c0"}},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	code, body := rfcWirePost(t, ts, raw)
	if code != 200 {
		t.Fatalf("FAIL: RFC 8620 array-form request status = %d, want 200 (body: %s)", code, body)
	}
	assertUnknownMethodResponse(t, body)
}

func TestLegacyObjectFormMethodCallStillAccepted(t *testing.T) {
	ts := rfcWireServer(t)

	// Control: the pre-RFC object form this server historically accepted
	// must keep working through the legacy-tolerance decode path.
	req := map[string]interface{}{
		"using": []string{"urn:ietf:params:jmap:core"},
		"methodCalls": []map[string]interface{}{
			{"name": "Core/echo", "args": map[string]interface{}{"ping": 1}, "id": "c0"},
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	code, body := rfcWirePost(t, ts, raw)
	if code != 200 {
		t.Fatalf("CONTROL FAILED: legacy object-form request status = %d, want 200 (body: %s)", code, body)
	}
	assertUnknownMethodResponse(t, body)
}

func TestMethodCallArrayMalformedFormsRejected(t *testing.T) {
	ts := rfcWireServer(t)

	cases := []struct {
		name  string
		calls []interface{}
	}{
		{"two-element array", []interface{}{[]interface{}{"Core/echo", map[string]interface{}{}}}},
		{"four-element array", []interface{}{[]interface{}{"Core/echo", map[string]interface{}{}, "c0", "extra"}}},
		{"non-object arguments", []interface{}{[]interface{}{"Core/echo", "args", "c0"}}},
		{"non-string call id", []interface{}{[]interface{}{"Core/echo", map[string]interface{}{}, 42}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := map[string]interface{}{
				"using":       []string{"urn:ietf:params:jmap:core"},
				"methodCalls": tc.calls,
			}
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			code, _ := rfcWirePost(t, ts, raw)
			if code != 400 {
				t.Fatalf("FAIL: malformed method call (%s) status = %d, want 400 invalidArguments", tc.name, code)
			}
		})
	}
}
