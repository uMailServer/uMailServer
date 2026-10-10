package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAudit5721Control(t *testing.T) {
	ms, _ := NewMessageStore(t.TempDir())
	if _, err := ms.StoreMessage("alice", []byte("hello world body")); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	if _, err := ms.StoreMessage("../x", []byte("hello world body")); err == nil {
		t.Fatalf("INVALID: traversal user must already be rejected")
	}
}

// FAILURE: user "." resolves to the store root, so its sharded files land in
// the namespace of every other user's directory.
func TestAudit5721Failure(t *testing.T) {
	root := t.TempDir()
	ms, _ := NewMessageStore(root)
	id, err := ms.StoreMessage(".", []byte("hello world body"))
	statErr := os.ErrNotExist
	if len(id) >= 2 {
		_, statErr = os.Stat(filepath.Join(root, id[:2]))
	}
	t.Logf("EXPECTED: error for user \".\" ACTUAL: err=%v root-level shard dir exists=%v", err, statErr == nil)
	if err == nil {
		t.Fatalf("DEFECT F5721: user \".\" accepted; message written outside any user directory")
	}
}
