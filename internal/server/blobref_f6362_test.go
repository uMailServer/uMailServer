package server

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/storage"
)

// F6362: the orchestrator must wire the reference checker, so a raw
// msgStore.DeleteMessage (REST API, JMAP, ...) cannot delete a body that
// another message of the user still references.
func TestF6362ServerWiresBlobReferenceChecker(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	user := "u@mx.test.com"
	id, err := srv.msgStore.StoreMessage(user, []byte("Subject: dup\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	for uid := uint32(1); uid <= 2; uid++ {
		_ = srv.storageDB.CreateMailbox(user, "INBOX")
		if err := srv.storageDB.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{UID: uid, MessageID: id, Size: 20}); err != nil {
			t.Fatal(err)
		}
	}
	_ = srv.storageDB.DeleteMessage(user, "INBOX", 1)
	if err := srv.msgStore.DeleteMessage(user, id); !errors.Is(err, storage.ErrBlobReferenced) {
		t.Fatalf("raw DeleteMessage = %v, want ErrBlobReferenced", err)
	}
	if !srv.msgStore.MessageExists(user, id) {
		t.Fatal("shared blob deleted")
	}
}
