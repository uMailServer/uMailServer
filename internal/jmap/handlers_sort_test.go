package jmap

// Regression tests for the JMAP Email/query sort-direction inversion.
//
// RFC 8620 §2.7.1: the Email/query `sort` argument is an array of comparators
// ({property, isAscending}); the server MUST return results ordered according to
// the requested comparator, honoring isAscending. A previous implementation
// inverted `ascending` for the date properties (receivedAt/sentAt) in
// sortMessages, so an explicit isAscending:true was returned reversed.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// seedSortMessages stores three messages with strictly increasing InternalDate
// (msg-a oldest) and alphabetically ordered subjects (Alpha, Bravo, Charlie).
func seedSortMessages(t *testing.T, db *storage.Database) {
	t.Helper()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		_ = err // mailbox may already exist
	}
	seeds := []struct {
		id   string
		subj string
		date time.Time
	}{
		{"msg-a", "Alpha", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"msg-b", "Bravo", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		{"msg-c", "Charlie", time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)},
	}
	for i, s := range seeds {
		meta := &storage.MessageMetadata{
			UID:          uint32(i + 1),
			MessageID:    s.id,
			Subject:      s.subj,
			From:         "sender@example.com",
			To:           "recipient@example.com",
			Size:         int64(100 * (i + 1)),
			InternalDate: s.date,
			Date:         s.date.Format(time.RFC3339),
			Flags:        []string{},
		}
		if err := db.StoreMessageMetadata("user@example.com", "INBOX", uint32(i+1), meta); err != nil {
			t.Fatalf("StoreMessageMetadata(%s) error: %v", s.id, err)
		}
	}
}

// queryEmailIDs issues a real Email/query through handleEmailQuery and returns
// the ordered message ids.
func queryEmailIDs(t *testing.T, server *Server, sortArg []interface{}) []string {
	t.Helper()
	call := MethodCall{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId": "user@example.com",
			"sort":      sortArg,
		},
		ID: "call-1",
	}
	resp := server.handleEmailQuery("user@example.com", call)
	raw, ok := resp.Args["ids"]
	if !ok {
		t.Fatalf("response missing ids: %#v", resp.Args)
	}
	ids, ok := raw.([]string)
	if !ok {
		t.Fatalf("ids is %T, want []string", raw)
	}
	return ids
}

func assertEmailOrder(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d ids (%v), want %d (%v)", label, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: order = %v, want %v (RFC 8620 §2.7.1)", label, got, want)
		}
	}
}

func sortComparator(property string, ascending bool) []interface{} {
	return []interface{}{
		map[string]interface{}{"property": property, "isAscending": ascending},
	}
}

// TestEmailQuery_SortDate_AscendingHonored: explicit isAscending:true for
// receivedAt must return oldest-first.
func TestEmailQuery_SortDate_AscendingHonored(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)

	ids := queryEmailIDs(t, server, sortComparator("receivedAt", true))
	assertEmailOrder(t, ids, []string{"msg-a", "msg-b", "msg-c"}, "receivedAt ascending")
}

// TestEmailQuery_SortDate_DescendingHonored: explicit isAscending:false for
// receivedAt must return newest-first.
func TestEmailQuery_SortDate_DescendingHonored(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)

	ids := queryEmailIDs(t, server, sortComparator("receivedAt", false))
	assertEmailOrder(t, ids, []string{"msg-c", "msg-b", "msg-a"}, "receivedAt descending")
}

// TestEmailQuery_SortDate_DefaultIsDescending: a date sort with isAscending
// omitted (the Comparator zero value) defaults to descending.
func TestEmailQuery_SortDate_DefaultIsDescending(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)

	sortArg := []interface{}{map[string]interface{}{"property": "receivedAt"}}
	ids := queryEmailIDs(t, server, sortArg)
	assertEmailOrder(t, ids, []string{"msg-c", "msg-b", "msg-a"}, "receivedAt default")
}

// TestEmailQuery_SortSentAt_AscendingHonored: the other date property must also
// honor an explicit ascending request.
func TestEmailQuery_SortSentAt_AscendingHonored(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)

	ids := queryEmailIDs(t, server, sortComparator("sentAt", true))
	assertEmailOrder(t, ids, []string{"msg-a", "msg-b", "msg-c"}, "sentAt ascending")
}

// TestEmailQuery_SortSubject_Control: a non-date property is unaffected by the
// date inversion and must keep working (control that passed before the fix).
func TestEmailQuery_SortSubject_Control(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	seedSortMessages(t, db)

	ids := queryEmailIDs(t, server, sortComparator("subject", true))
	assertEmailOrder(t, ids, []string{"msg-a", "msg-b", "msg-c"}, "subject ascending")
}

// TestEmailQuery_SortDate_SingleMessage boundary: one message sorts identically
// in either direction.
func TestEmailQuery_SortDate_SingleMessage(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		_ = err
	}
	meta := &storage.MessageMetadata{
		UID:          1,
		MessageID:    "only",
		Subject:      "Only",
		From:         "sender@example.com",
		To:           "recipient@example.com",
		Size:         10,
		InternalDate: time.Date(2024, 5, 5, 0, 0, 0, 0, time.UTC),
		Date:         "2024-05-05T00:00:00Z",
		Flags:        []string{},
	}
	if err := db.StoreMessageMetadata("user@example.com", "INBOX", 1, meta); err != nil {
		t.Fatalf("StoreMessageMetadata error: %v", err)
	}

	ids := queryEmailIDs(t, server, sortComparator("receivedAt", true))
	assertEmailOrder(t, ids, []string{"only"}, "receivedAt ascending single")
	ids = queryEmailIDs(t, server, sortComparator("receivedAt", false))
	assertEmailOrder(t, ids, []string{"only"}, "receivedAt descending single")
}
