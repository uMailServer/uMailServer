package jmap

import (
	"fmt"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// threadQueryIDs issues a real Thread/query through the handler and returns the
// ids it produced. The handler stores threadIDs (a []string) into Args, so the
// assertion has to read that concrete type.
func threadQueryIDs(t *testing.T, position float64, limit float64) []string {
	t.Helper()
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	user := "user@example.com"
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	// Two messages sharing one thread, so the query has a thread to page over.
	for i := 0; i < 2; i++ {
		meta := &storage.MessageMetadata{
			UID:          uint32(i + 1),
			MessageID:    fmt.Sprintf("msg-%d", i),
			ThreadID:     "thread-1",
			Subject:      fmt.Sprintf("Subject %d", i),
			From:         fmt.Sprintf("sender%d@example.com", i),
			To:           "rcpt@example.com",
			Size:         int64(100 * (i + 1)),
			InternalDate: time.Now().Add(time.Duration(-i) * time.Hour),
			Flags:        []string{},
		}
		if err := db.StoreMessageMetadata(user, "INBOX", uint32(i+1), meta); err != nil {
			t.Fatalf("StoreMessageMetadata: %v", err)
		}
	}

	resp := server.handleThreadQuery(user, MethodCall{
		Name: "Thread/query",
		Args: map[string]interface{}{
			"accountId": user,
			"position":  position,
			"limit":     limit,
		},
		ID: "call-1",
	})
	if resp.Name != "Thread/query" {
		t.Fatalf("response name = %q, want Thread/query", resp.Name)
	}
	raw, ok := resp.Args["ids"].([]string)
	if !ok {
		t.Fatalf("ids has type %T, want []string", resp.Args["ids"])
	}
	return raw
}

// A client-supplied "position" is a page offset and may arrive negative.
// Clamping only the upper bound left start negative, so threadIDs[start:end]
// raised a slice-bounds error. The offset must be floored at 0 instead.
func TestThreadQuery_NegativePositionIsClampedToZero(t *testing.T) {
	cases := []struct {
		name     string
		position float64
	}{
		{"minus one", -1},
		{"far negative", -5},
		{"fractional negative", -0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Thread/query with position=%v panicked: %v; position must be clamped to a non-negative offset", tc.position, r)
				}
			}()
			ids := threadQueryIDs(t, tc.position, 30)
			// Clamped to 0, the first page is returned.
			if len(ids) != 1 {
				t.Errorf("position=%v: got %d ids, want 1 (clamped to the first page)", tc.position, len(ids))
			}
		})
	}
}

// Boundary cases around the page window that must keep working: the first
// page, a position past the end (empty page), and no position at all.
func TestThreadQuery_PageBoundariesAreUnchanged(t *testing.T) {
	cases := []struct {
		name     string
		position float64
		wantIDs  int
	}{
		{"first page", 0, 1},
		{"past the end returns empty", 99, 0},
		{"omitted position defaults to first page", 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids := threadQueryIDs(t, tc.position, 30)
			if len(ids) != tc.wantIDs {
				t.Errorf("position=%v: got %d ids, want %d", tc.position, len(ids), tc.wantIDs)
			}
		})
	}
}
