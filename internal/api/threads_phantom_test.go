package api

// Regression test for the threads REST API phantom-success defect: the
// five storage delegates in threads.go were placeholders ("This is a
// placeholder"), so every operation reported fake outcomes —
// POST /api/v1/threads/{id}/read returned 200 {"status":"success"}
// without marking anything read, DELETE /api/v1/threads/{id} returned
// 200 {"status":"deleted"} without deleting anything, and the list
// endpoint returned an empty page even when delivery-path thread data
// existed in storage. The fix wires the delegates to the real storage
// primitives (s.mailDB), so success responses reflect effects that
// actually happened.
//
// Hermetic: real temp storage + accounts database; handlers called
// directly with the authMiddleware-injected user context (server.go
// sets "user" in production).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

func startThreadsTestServer(t *testing.T) (*Server, *storage.Database, string, string) {
	t.Helper()

	storageDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storageDB.Close() })

	accountsDB, err := db.Open(t.TempDir() + "/accounts.db")
	if err != nil {
		t.Fatalf("open accounts db: %v", err)
	}
	t.Cleanup(func() { _ = accountsDB.Close() })

	server := NewServer(accountsDB, nil, Config{})
	server.SetMailDB(storageDB)

	const user = "alice@example.com"
	if err := storageDB.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	threadID, err := storageDB.GetOrCreateThreadID(user, "INBOX", "Re: project", "", nil)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	// One unread message (no \Seen flag) in the thread. UID is set because
	// production writers always store it alongside the bucket key.
	if err := storageDB.StoreMessageMetadata(user, "INBOX", 1, &storage.MessageMetadata{
		UID:       1,
		MessageID: "m1",
		ThreadID:  threadID,
		Subject:   "Re: project",
		Flags:     []string{},
	}); err != nil {
		t.Fatalf("store message: %v", err)
	}
	// The Thread aggregate row (the IMAP delivery path maintains it via
	// UpdateThread; GetThreads/GetThread read it).
	if err := storageDB.UpdateThread(user, &storage.Thread{
		ThreadID:     threadID,
		Subject:      "Re: project",
		MessageCount: 1,
		UnreadCount:  1,
	}); err != nil {
		t.Fatalf("create thread row: %v", err)
	}

	return server, storageDB, user, threadID
}

func threadsRequest(t *testing.T, server *Server, method, target, user string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	r = r.WithContext(context.WithValue(context.Background(), "user", user))
	w := httptest.NewRecorder()

	switch {
	case strings.HasSuffix(target, "/read"):
		server.handleThreadMarkRead(w, r)
	case method == http.MethodDelete:
		server.handleThreadDelete(w, r)
	default:
		t.Fatalf("unsupported test request %s %s", method, target)
	}
	return w
}

func TestThreadMarkReadActuallyMarksRead(t *testing.T) {
	server, storageDB, user, threadID := startThreadsTestServer(t)

	w := threadsRequest(t, server, "POST", "/api/v1/threads/"+threadID+"/read", user)
	if w.Code != http.StatusOK {
		t.Fatalf("FAIL: mark-read returned %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	meta, err := storageDB.GetMessageMetadata(user, "INBOX", 1)
	if err != nil {
		t.Fatalf("FAIL: load message after mark-read: %v", err)
	}
	seen := false
	for _, f := range meta.Flags {
		if f == "\\Seen" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("FAIL: phantom success — mark-read returned 200 but the \\Seen flag was never set (flags: %v)", meta.Flags)
	}
}

func TestThreadDeleteActuallyDeletes(t *testing.T) {
	server, storageDB, user, threadID := startThreadsTestServer(t)

	w := threadsRequest(t, server, "DELETE", "/api/v1/threads/"+threadID, user)
	if w.Code != http.StatusOK {
		t.Fatalf("FAIL: delete returned %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	uids, err := storageDB.GetMessageUIDs(user, "INBOX")
	if err != nil {
		t.Fatalf("FAIL: list messages after delete: %v", err)
	}
	if len(uids) != 0 {
		t.Fatalf("FAIL: phantom success — delete returned 200 but the message is still in storage (uids: %v)", uids)
	}
	if _, err := storageDB.GetThread(user, threadID); err == nil {
		t.Fatalf("FAIL: phantom success — delete returned 200 but the thread row still exists")
	}
}

func TestThreadListReturnsRealThreads(t *testing.T) {
	server, _, user, _ := startThreadsTestServer(t)

	r := httptest.NewRequest("GET", "/api/v1/threads", nil)
	r = r.WithContext(context.WithValue(context.Background(), "user", user))
	w := httptest.NewRecorder()
	server.handleThreads(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("FAIL: list returned %d, want 200", w.Code)
	}
	var resp ThreadListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("FAIL: decode list response: %v", err)
	}
	if len(resp.Threads) != 1 {
		t.Fatalf("FAIL: phantom empty — list returned %d threads, want 1 (the seeded thread exists in storage)", len(resp.Threads))
	}
	if resp.Threads[0].ThreadID == "" {
		t.Fatalf("FAIL: listed thread has an empty ThreadID")
	}
}
