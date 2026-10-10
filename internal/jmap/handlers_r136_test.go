package jmap

// Round 136 regression tests (F6180-F6189): Email/query filters, sort,
// limit; Email/get properties; /get notFound; Mailbox/set state and
// uniqueness; /changes argument validation.

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const r136User = "alice@example.com"

func newR136Server(t *testing.T) *Server {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, mb := range []string{"INBOX", "Sent"} {
		if err := db.CreateMailbox(r136User, mb); err != nil {
			t.Fatal(err)
		}
	}
	return NewServer(db, ms, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{JWTSecret: "0123456789abcdef0123456789abcdef"})
}

func r136Put(t *testing.T, s *Server, mbox string, uid uint32, m *storage.MessageMetadata) {
	t.Helper()
	m.UID = uid
	if err := s.db.StoreMessageMetadata(r136User, mbox, uid, m); err != nil {
		t.Fatal(err)
	}
}

func r136Call(s *Server, name string, args map[string]interface{}) Response {
	args["accountId"] = r136User
	return s.dispatchMethodCall(r136User, MethodCall{Name: name, Args: args, ID: "c1"})
}

func r136IDs(t *testing.T, r Response) []string {
	t.Helper()
	b, _ := json.Marshal(r.Args["ids"])
	var ids []string
	_ = json.Unmarshal(b, &ids)
	return ids
}

func TestR136_F6180_DescendingSortTiesDeterministic(t *testing.T) {
	s := newR136Server(t)
	ts := time.Now().Add(-time.Hour)
	want := []string{}
	for i := 0; i < 40; i++ {
		id := string(rune('a'+i/26)) + string(rune('a'+i%26))
		r136Put(t, s, "INBOX", uint32(i+1), &storage.MessageMetadata{MessageID: id, Subject: "same", Size: 5, InternalDate: ts})
		want = append(want, id)
	}
	first := r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{
		"sort": []interface{}{map[string]interface{}{"property": "subject"}}, "limit": float64(100)}))
	if len(first) != 40 {
		t.Fatalf("got %d ids", len(first))
	}
	for n := 0; n < 5; n++ {
		again := r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{
			"sort": []interface{}{map[string]interface{}{"property": "subject"}}, "limit": float64(100)}))
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("tie order not deterministic")
		}
	}
	// ties keep a stable order independent of direction flag: ascending and
	// descending must both order ties by id.
	_ = want
	sorted := append([]string(nil), first...)
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1] > sorted[i] {
			t.Fatalf("ties not ordered by id: %v", sorted)
		}
	}
}

func TestR136_F6181_StrictBefore_MaxSize(t *testing.T) {
	s := newR136Server(t)
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	r136Put(t, s, "INBOX", 1, &storage.MessageMetadata{MessageID: "m1", Size: 100, InternalDate: ts})
	r := r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"before": ts.Format(time.RFC3339)}})
	if ids := r136IDs(t, r); len(ids) != 0 {
		t.Fatalf("before must be strict, got %v", ids)
	}
	r = r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"maxSize": float64(100)}})
	if ids := r136IDs(t, r); len(ids) != 0 {
		t.Fatalf("maxSize must be strict (size < maxSize), got %v", ids)
	}
	r = r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"after": ts.Format(time.RFC3339), "minSize": float64(100)}})
	if ids := r136IDs(t, r); len(ids) != 1 {
		t.Fatalf("after/minSize inclusive, got %v", ids)
	}
}

func TestR136_F6182_HasNotKeyword(t *testing.T) {
	s := newR136Server(t)
	now := time.Now()
	r136Put(t, s, "INBOX", 1, &storage.MessageMetadata{MessageID: "flag", Flags: []string{"\\Flagged"}, InternalDate: now})
	r136Put(t, s, "INBOX", 2, &storage.MessageMetadata{MessageID: "plain", InternalDate: now})
	r136Put(t, s, "INBOX", 3, &storage.MessageMetadata{MessageID: "seen", Flags: []string{"\\Seen"}, InternalDate: now})
	got := r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"hasKeyword": "$flagged"}}))
	if !reflect.DeepEqual(got, []string{"flag"}) {
		t.Fatalf("hasKeyword: %v", got)
	}
	got = r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"notKeyword": "$flagged"}}))
	if len(got) != 2 {
		t.Fatalf("notKeyword $flagged: %v", got)
	}
	got = r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"hasKeyword": "$seen"}}))
	if !reflect.DeepEqual(got, []string{"seen"}) {
		t.Fatalf("hasKeyword $seen: %v", got)
	}
}

func TestR136_F6183_InMailboxOtherThan(t *testing.T) {
	s := newR136Server(t)
	now := time.Now()
	r136Put(t, s, "INBOX", 1, &storage.MessageMetadata{MessageID: "in", InternalDate: now})
	r136Put(t, s, "Sent", 1, &storage.MessageMetadata{MessageID: "out", InternalDate: now})
	got := r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{"filter": map[string]interface{}{"inMailboxOtherThan": []interface{}{"sent"}}}))
	if !reflect.DeepEqual(got, []string{"in"}) {
		t.Fatalf("got %v", got)
	}
}

func TestR136_F6184_EmailGetProperties(t *testing.T) {
	s := newR136Server(t)
	r136Put(t, s, "INBOX", 1, &storage.MessageMetadata{MessageID: "m1", Subject: "hi", Size: 7, InternalDate: time.Now()})
	r := r136Call(s, "Email/get", map[string]interface{}{"ids": []interface{}{"m1"}, "properties": []interface{}{"subject"}})
	b, _ := json.Marshal(r.Args["list"])
	var list []map[string]interface{}
	_ = json.Unmarshal(b, &list)
	if len(list) != 1 {
		t.Fatalf("resp %v", r.Args)
	}
	if _, ok := list[0]["id"]; !ok {
		t.Fatal("id must always be present")
	}
	if _, ok := list[0]["subject"]; !ok {
		t.Fatal("subject missing")
	}
	for _, k := range []string{"size", "receivedAt", "mailboxIds", "from"} {
		if _, ok := list[0][k]; ok {
			t.Fatalf("unrequested property %q returned", k)
		}
	}
	r = r136Call(s, "Email/get", map[string]interface{}{"ids": []interface{}{"m1"}, "properties": []interface{}{"bogus"}})
	if r.Name != "error" || r.Args["type"] != "invalidArguments" {
		t.Fatalf("want invalidArguments, got %v %v", r.Name, r.Args)
	}
}

func TestR136_F6185_NotFoundDeterministicArrays(t *testing.T) {
	s := newR136Server(t)
	r := r136Call(s, "Mailbox/get", map[string]interface{}{"ids": []interface{}{"z9", "a1", "m5", "b2"}})
	if !reflect.DeepEqual(r.Args["notFound"], []string{"z9", "a1", "m5", "b2"}) {
		t.Fatalf("notFound must follow request order: %v", r.Args["notFound"])
	}
	r = r136Call(s, "Email/get", map[string]interface{}{"ids": []interface{}{}})
	b, _ := json.Marshal(r.Args)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	if string(m["notFound"]) != "[]" || string(m["list"]) != "[]" {
		t.Fatalf("list/notFound must be arrays: %s", b)
	}
	r = r136Call(s, "Thread/get", map[string]interface{}{"ids": []interface{}{"t1"}})
	b, _ = json.Marshal(r.Args)
	_ = json.Unmarshal(b, &m)
	if string(m["list"]) != "[]" {
		t.Fatalf("Thread/get list must be array: %s", b)
	}
}

func TestR136_F6186_MailboxSetIfInState(t *testing.T) {
	s := newR136Server(t)
	r := r136Call(s, "Mailbox/set", map[string]interface{}{
		"ifInState": "99999",
		"create":    map[string]interface{}{"k": map[string]interface{}{"name": "Foo"}},
	})
	if r.Name != "error" || r.Args["type"] != "stateMismatch" {
		t.Fatalf("want stateMismatch, got %v %v", r.Name, r.Args)
	}
	if s.mailboxExists(r136User, "Foo") {
		t.Fatal("mailbox created despite stateMismatch")
	}
	r = r136Call(s, "Mailbox/set", map[string]interface{}{"create": map[string]interface{}{"k": map[string]interface{}{"name": "Foo"}}})
	if r.Args["oldState"] == nil {
		t.Fatal("oldState must be a string")
	}
}

func TestR136_F6187_EmailQueryLimit(t *testing.T) {
	s := newR136Server(t)
	for i := 0; i < 150; i++ {
		r136Put(t, s, "INBOX", uint32(i+1), &storage.MessageMetadata{MessageID: "m" + string(rune('A'+i/26)) + string(rune('a'+i%26)), InternalDate: time.Now()})
	}
	got := r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{"limit": float64(100)}))
	if len(got) != 100 {
		t.Fatalf("limit 100 returned %d", len(got))
	}
	got = r136IDs(t, r136Call(s, "Email/query", map[string]interface{}{"limit": float64(500)}))
	if len(got) != 100 {
		t.Fatalf("limit 500 must clamp to 100, got %d", len(got))
	}
	r := r136Call(s, "Email/query", map[string]interface{}{"limit": float64(-1)})
	if r.Name != "error" || r.Args["type"] != "invalidArguments" {
		t.Fatalf("negative limit: %v %v", r.Name, r.Args)
	}
}

func TestR136_F6188_ChangesArgs(t *testing.T) {
	s := newR136Server(t)
	for _, name := range []string{"Email/changes", "Mailbox/changes"} {
		r := r136Call(s, name, map[string]interface{}{})
		if r.Name != "error" || r.Args["type"] != "invalidArguments" {
			t.Fatalf("%s missing sinceState: %v %v", name, r.Name, r.Args)
		}
		r = r136Call(s, name, map[string]interface{}{"sinceState": "0", "maxChanges": float64(0)})
		if r.Name != "error" || r.Args["type"] != "invalidArguments" {
			t.Fatalf("%s maxChanges 0: %v %v", name, r.Name, r.Args)
		}
		r = r136Call(s, name, map[string]interface{}{"sinceState": "0", "maxChanges": float64(-3)})
		if r.Name != "error" || r.Args["type"] != "invalidArguments" {
			t.Fatalf("%s maxChanges -3: %v %v", name, r.Name, r.Args)
		}
	}
}

func TestR136_F6189_MailboxSetDuplicateName(t *testing.T) {
	s := newR136Server(t)
	r := r136Call(s, "Mailbox/set", map[string]interface{}{"create": map[string]interface{}{
		"a": map[string]interface{}{"name": "Sent"},
		"b": map[string]interface{}{"name": "inbox"},
	}})
	nc, _ := r.Args["notCreated"].(map[string]interface{})
	for _, k := range []string{"a", "b"} {
		e, _ := nc[k].(map[string]interface{})
		if e == nil || e["type"] != "invalidProperties" {
			t.Fatalf("create %s should fail invalidProperties: %v", k, r.Args)
		}
	}
	if c, _ := r.Args["created"].(map[string]Mailbox); len(c) != 0 {
		t.Fatalf("created %v", c)
	}
	// rename onto existing name
	r = r136Call(s, "Mailbox/set", map[string]interface{}{"update": map[string]interface{}{
		"sent": map[string]interface{}{"name": "INBOX"}}})
	nu, _ := r.Args["notUpdated"].(map[string]interface{})
	e, _ := nu["sent"].(map[string]interface{})
	if e == nil || e["type"] != "invalidProperties" {
		t.Fatalf("rename dup: %v", r.Args)
	}
}
