package jmap

// Regression test for the /jmap/api unbounded request-body defect:
// handleAPI did io.ReadAll(r.Body) with no cap, so any authenticated user
// could make the server buffer an arbitrarily large request (memory
// exhaustion). The sibling handleUpload already capped uploads at 50MB via
// http.MaxBytesReader "to prevent DoS"; the fix gives handleAPI the same
// class of budget (maxJMAPAPIRequestBodySize) and answers 413 requestTooLarge
// for over-budget bodies.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// apiLimitTestSecret is runtime-derived so no credential literal is embedded
// in the test source; it only needs to be identical for token minting and
// server configuration within this test.
var apiLimitTestSecret = fmt.Sprintf("%x", sha256.Sum256([]byte("jmap-api-limit-fixture")))

func apiLimitToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "alice@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(apiLimitTestSecret))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return signed
}

func apiLimitServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServer(nil, nil, nil, Config{JWTSecret: apiLimitTestSecret})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func apiLimitPost(t *testing.T, ts *httptest.Server, body []byte) int {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/jmap/api", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiLimitToken(t))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// apiLimitRequestBody builds a VALID JMAP request of at least minBytes whose
// single method call (Core/echo) dispatches to unknownMethod and touches no
// storage.
func apiLimitRequestBody(t *testing.T, minBytes int) []byte {
	t.Helper()
	padding := strings.Repeat("x", minBytes)
	req := map[string]interface{}{
		"using": []string{"urn:ietf:params:jmap:core"},
		"methodCalls": []map[string]interface{}{
			{"name": "Core/echo", "args": map[string]interface{}{"padding": padding}, "id": "c0"},
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestAPIRequestBodySizeLimit(t *testing.T) {
	ts := apiLimitServer(t)

	// Control: a small valid request is processed normally.
	small := apiLimitRequestBody(t, 64)
	if code := apiLimitPost(t, ts, small); code != 200 {
		t.Fatalf("CONTROL FAILED (harness): small request status = %d, want 200", code)
	}

	// Over-budget: a valid request beyond maxJMAPAPIRequestBodySize must be
	// rejected with 413 instead of being buffered whole.
	big := apiLimitRequestBody(t, maxJMAPAPIRequestBodySize+1)
	if code := apiLimitPost(t, ts, big); code != 413 {
		t.Fatalf("FAIL: %d-byte /jmap/api request status = %d, want 413 (unbounded body buffering)", len(big), code)
	}
}
