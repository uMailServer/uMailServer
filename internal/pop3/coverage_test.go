package pop3

import (	"testing"
)

func TestBboltStoreListMessages_EmptyUser(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	msgs, err := store.ListMessages("")
	if err != nil {
		t.Fatalf("ListMessages('') failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages for empty user, got %d", len(msgs))
	}
}

func TestBboltStoreGetMessage_ExactLowerBound(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	// index=1 is the lower bound for valid indices
	_, err := store.GetMessage("user@test.com", 1)
	if err == nil {
		t.Error("Expected error for GetMessage(1) on empty mailbox")
	}
}

func TestBboltStoreGetMessageData_ZeroIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageData("user@test.com", 0)
	if err == nil {
		t.Error("Expected error for GetMessageData(0) on empty mailbox")
	}
}

func TestBboltStoreDeleteMessage_ZeroIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	err := store.DeleteMessage("user@test.com", 0)
	if err == nil {
		t.Error("Expected error for DeleteMessage(0) on empty mailbox")
	}
}

func TestBboltStoreGetMessageSize_ZeroIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageSize("user@test.com", 0)
	if err == nil {
		t.Error("Expected error for GetMessageSize(0) on empty mailbox")
	}
}
