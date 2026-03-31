package pop3

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func setupBboltStoreTest(t *testing.T) (*BboltStore, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}
	store := NewBboltStore(db, msgStore)
	return store, func() {
		db.Close()
		msgStore.Close()
	}
}

func TestNewBboltStore(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()
	if store == nil {
		t.Fatal("Expected non-nil store")
	}
}

func TestBboltStoreAuthenticate(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	ok, err := store.Authenticate("user@example.com", "password")
	if err != nil {
		t.Errorf("Authenticate returned error: %v", err)
	}
	if ok {
		t.Error("Expected false from Authenticate")
	}
}

func TestBboltStoreListMessagesEmpty(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	msgs, err := store.ListMessages("user@example.com")
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages for empty mailbox, got %d", len(msgs))
	}
}

func TestBboltStoreGetMessageOutOfRange(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessage("user@example.com", 1)
	if err == nil {
		t.Error("Expected error for out-of-range index")
	}

	_, err = store.GetMessage("user@example.com", 0)
	if err == nil {
		t.Error("Expected error for index 0")
	}

	_, err = store.GetMessage("user@example.com", -1)
	if err == nil {
		t.Error("Expected error for negative index")
	}
}

func TestBboltStoreGetMessageDataInvalidIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageData("user@example.com", 1)
	if err == nil {
		t.Error("Expected error for GetMessageData with invalid index")
	}
}

func TestBboltStoreDeleteMessageInvalidIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	err := store.DeleteMessage("user@example.com", 1)
	if err == nil {
		t.Error("Expected error for DeleteMessage with invalid index")
	}
}

func TestBboltStoreGetMessageCountEmpty(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	count, err := store.GetMessageCount("user@example.com")
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0 for empty mailbox, got %d", count)
	}
}

func TestBboltStoreGetMessageSizeInvalidIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageSize("user@example.com", 1)
	if err == nil {
		t.Error("Expected error for GetMessageSize with invalid index")
	}
}

// Additional tests for higher coverage

func TestBboltStoreListMessagesMultipleUsers(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	for _, user := range []string{"user1@test.com", "user2@test.com"} {
		msgs, err := store.ListMessages(user)
		if err != nil {
			t.Errorf("ListMessages(%s) failed: %v", user, err)
		}
		if len(msgs) != 0 {
			t.Errorf("ListMessages(%s): expected 0, got %d", user, len(msgs))
		}
	}
}

func TestBboltStoreGetMessageDataInvalidIndexZero(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageData("user@example.com", 0)
	if err == nil {
		t.Error("Expected error for GetMessageData with index 0 on empty mailbox")
	}
}

func TestBboltStoreDeleteMessageNegativeIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	err := store.DeleteMessage("user@example.com", -1)
	if err == nil {
		t.Error("Expected error for DeleteMessage with negative index")
	}
}

func TestBboltStoreGetMessageSizeInvalidIndexZero(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageSize("user@example.com", 0)
	if err == nil {
		t.Error("Expected error for GetMessageSize with index 0 on empty mailbox")
	}
}

func TestBboltStoreGetMessageMultipleInvalidIndices(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	for _, idx := range []int{-10, -1, 0, 1, 5, 100} {
		_, err := store.GetMessage("user@example.com", idx)
		if err == nil {
			t.Errorf("Expected error for GetMessage with index %d on empty mailbox", idx)
		}
	}
}

func TestBboltStoreGetMessageCountMultipleUsers(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	for _, user := range []string{"alice@test.com", "bob@test.com"} {
		count, err := store.GetMessageCount(user)
		if err != nil {
			t.Errorf("GetMessageCount(%s) failed: %v", user, err)
		}
		if count != 0 {
			t.Errorf("GetMessageCount(%s): expected 0, got %d", user, count)
		}
	}
}
