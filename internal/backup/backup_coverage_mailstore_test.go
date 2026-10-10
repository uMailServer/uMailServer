package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// r74ServerWired builds Manager the way the server does
// (server_api.go: NewManager(DataDir, storageDB, msgStore)) with the message
// store at <DataDir>/mail/messages (server.New).
func r74ServerWired(t *testing.T, dataDir string) (*Manager, *storage.MessageStore) {
	t.Helper()
	ms, err := storage.NewMessageStore(filepath.Join(dataDir, "mail", "messages"))
	if err != nil {
		t.Fatal(err)
	}
	sdb, err := storage.OpenDatabase(filepath.Join(dataDir, "mail", "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sdb.Close() })
	return NewManager(dataDir, sdb, ms), ms
}

// TestManagerUsesLiveMessageStore (F5552): BackupUser/BackupFull/Restore must
// operate on the server's message store, not the legacy <dataDir>/messages.
func TestManagerUsesLiveMessageStore(t *testing.T) {
	root := t.TempDir()
	m, ms := r74ServerWired(t, filepath.Join(root, "src"))
	const user = "alice@example.com"
	id, err := ms.StoreMessage(user, []byte("Subject: a\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.BackupUser(user, filepath.Join(root, "alice.tar.gz"), BackupOptions{}); err != nil {
		t.Fatalf("BackupUser: %v", err)
	}
	full := filepath.Join(root, "full.tar.gz")
	if err := m.BackupFull(full, BackupOptions{}); err != nil {
		t.Fatalf("BackupFull: %v", err)
	}

	dstDir := filepath.Join(root, "dst")
	m2, ms2 := r74ServerWired(t, dstDir)
	if err := m2.Restore(full, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := ms2.ReadMessage(user, id); err != nil {
		t.Fatalf("restored message not readable from the message store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "messages")); !os.IsNotExist(err) {
		t.Fatalf("restore wrote to legacy <dataDir>/messages (stat err=%v)", err)
	}
}

// TestManagerLegacyMessagesLayout (F5552 control): without mail/messages the
// legacy <dataDir>/messages layout is still used.
func TestManagerLegacyMessagesLayout(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "messages", "carol", "cur"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "messages", "carol", "cur", "m1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewManager(base, nil, nil).BackupUser("carol", filepath.Join(base, "carol.tar.gz"), BackupOptions{}); err != nil {
		t.Fatalf("legacy BackupUser: %v", err)
	}
}
