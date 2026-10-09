package jmap

// Regression tests for the Email/query negative-offset defect: handleEmailQuery
// cast the client-supplied `position` to int and clamped only the upper bound,
// so position: -1 drove a negative index into the ids slice (slice-bounds
// panic, per-request). F5147 later corrected the semantics: per RFC 8620
// §5.5 a negative position is an offset from the end of the results (added
// to total, then clamped at 0), not a request for the first page.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/umailserver/umailserver/internal/storage"
)

var emailQueryOffsetSecret = fmt.Sprintf("%x", sha256.Sum256([]byte("email-query-offset-fixture")))

func newEmailQueryServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	msgStore, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatalf("message store: %v", err)
	}

	user := "alice@example.com"
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	for i, id := range []string{"m1", "m2"} {
		meta := &storage.MessageMetadata{
			MessageID:    id,
			UID:          uint32(i + 1),
			Subject:      "msg " + id,
			From:         "sender@example.com",
			To:           user,
			Size:         100,
			InternalDate: time.Now(),
		}
		if err := db.StoreMessageMetadata(user, "INBOX", uint32(i+1), meta); err != nil {
			t.Fatalf("store metadata %s: %v", id, err)
		}
	}

	srv := NewServer(db, msgStore, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{JWTSecret: emailQueryOffsetSecret})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func emailQueryToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "alice@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(emailQueryOffsetSecret))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return signed
}

// emailQueryPost drives one Email/query call and returns the parsed args.
func emailQueryPost(t *testing.T, ts *httptest.Server, position float64) (int, map[string]interface{}, []string) {
	t.Helper()
	body := map[string]interface{}{
		"using": []string{"urn:ietf:params:jmap:core"},
		"methodCalls": []map[string]interface{}{
			{"name": "Email/query", "args": map[string]interface{}{
				"accountId": "alice@example.com",
				"position":  position,
			}, "id": "c0"},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest("POST", ts.URL+"/jmap/api", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+emailQueryToken(t))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("position %v: %v", position, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("position %v: status %d, want 200", position, resp.StatusCode)
	}
	// RFC 8620 §3.4: method responses are ["Name", {arguments}, "callId"].
	var parsed struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(parsed.MethodResponses) == 0 {
		t.Fatalf("no method responses")
	}
	var entry []json.RawMessage
	if err := json.Unmarshal(parsed.MethodResponses[0], &entry); err != nil {
		t.Fatalf("methodResponses[0] is not RFC 8620 array form: %v (%s)", err, parsed.MethodResponses[0])
	}
	var args map[string]interface{}
	if err := json.Unmarshal(entry[1], &args); err != nil {
		t.Fatalf("methodResponses[0] arguments: %v", err)
	}
	idsRaw, _ := args["ids"].([]interface{})
	var ids []string
	for _, id := range idsRaw {
		if s, ok := id.(string); ok {
			ids = append(ids, s)
		}
	}
	return int(args["total"].(float64)), args, ids
}

func TestEmailQueryNegativePositionIsOffsetFromEnd(t *testing.T) {
	ts := newEmailQueryServer(t)

	// Control: position 0 returns both messages in storage order.
	total, _, ids := emailQueryPost(t, ts, 0)
	if total != 2 || len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" {
		t.Fatalf("CONTROL FAILED (harness): position 0 = total %d ids %v", total, ids)
	}

	// Page-boundary control: position 1 returns only the second message.
	total, _, ids = emailQueryPost(t, ts, 1)
	if total != 2 || len(ids) != 1 || ids[0] != "m2" {
		t.Fatalf("CONTROL FAILED (harness): position 1 = total %d ids %v", total, ids)
	}

	// F5147: RFC 8620 §5.5 - a negative position is an offset from the end
	// of the results (added to total), so -1 selects the last message and
	// the response position is its 0-based index. It must not panic.
	total, args, ids := emailQueryPost(t, ts, -1)
	if total != 2 || len(ids) != 1 || ids[0] != "m2" || args["position"] != float64(1) {
		t.Fatalf("FAIL: position -1 = total %d ids %v position %v, want [m2] at position 1", total, ids, args["position"])
	}

	// Offsets past the start clamp to 0 and return the first page.
	total, args, ids = emailQueryPost(t, ts, -100)
	if total != 2 || len(ids) != 2 || args["position"] != float64(0) {
		t.Fatalf("FAIL: position -100 = total %d ids %v position %v, want the first page of 2 at position 0", total, ids, args["position"])
	}
}
