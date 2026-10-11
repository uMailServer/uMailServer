package jmap

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// F6362: Email/destroy must not delete a blob a pending upload still
// references (same content, same per-user content-addressed file).
func TestF6362DestroyKeepsBlobOfPendingUpload(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	defer srv.Stop()
	_ = db.CreateMailbox(r146User, "INBOX")
	id := r146Upload(t, srv, []byte("Subject: pend\r\n\r\nbody\r\n"))
	// A message row for the same content, as an IMAP APPEND dedupe would add.
	if err := db.StoreMessageMetadata(r146User, "INBOX", 1, &storage.MessageMetadata{UID: 1, MessageID: id, Size: 5}); err != nil {
		t.Fatal(err)
	}
	var adjusted int64
	srv.SetQuotaAdjustFunc(func(_ string, d int64) error { adjusted += d; return nil })
	srv.handleEmailSet(r146User, MethodCall{Name: "Email/set", ID: "d", Args: map[string]interface{}{
		"accountId": r146User, "destroy": []interface{}{id},
	}})
	if !store.MessageExists(r146User, id) {
		t.Fatal("blob of a pending upload was deleted by Email/destroy")
	}
	if adjusted != 0 {
		t.Fatalf("quota released (%d) although no bytes were freed", adjusted)
	}
	// The upload is still importable.
	if r := r146Import(srv, id, "inbox"); len(r.Args["created"].(map[string]Email)) != 1 {
		t.Fatalf("pending upload no longer importable: %v", r.Args)
	}
	// And the sweeper still collects an expired, unreferenced upload.
	orphan := r146Upload(t, srv, []byte("Subject: orphan2\r\n\r\nzzz\r\n"))
	if n := srv.SweepUploads(time.Now().Add(3 * time.Hour)); n != 1 || store.MessageExists(r146User, orphan) {
		t.Fatalf("sweep removed %d, orphan exists=%v", n, store.MessageExists(r146User, orphan))
	}
}
