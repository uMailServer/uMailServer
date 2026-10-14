package api

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// F7420: DELETE /api/v1/threads/{id} must release the blob and its quota
// bytes, like the mail delete endpoint, instead of leaking them.
func TestR268ThreadDeleteReleasesBlobAndQuota(t *testing.T) {
	server, storageDB, user, threadID := startThreadsTestServer(t)
	ms, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var quota int64
	server.SetMsgStore(ms)
	server.SetQuotaAdjustFunc(func(_ string, d int64) error { quota += d; return nil })
	raw := []byte("From: a@b.c\r\nSubject: x\r\n\r\nbody\r\n")
	hash, err := ms.StoreMessage(user, raw)
	if err != nil {
		t.Fatal(err)
	}
	quota += int64(len(raw))
	meta, _ := storageDB.GetMessageMetadata(user, "INBOX", 1)
	meta.MessageID = hash
	if err := storageDB.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
		t.Fatal(err)
	}

	if w := threadsRequest(t, server, "DELETE", "/api/v1/threads/"+threadID, user); w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if _, err := ms.ReadMessage(user, hash); err == nil {
		t.Fatal("blob leaked after thread delete")
	}
	if quota != 0 {
		t.Fatalf("quota not released: %d", quota)
	}
}
