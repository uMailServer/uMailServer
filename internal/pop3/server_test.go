package pop3

import (
	"testing"
)

func TestMessageStruct(t *testing.T) {
	msg := &Message{
		Index: 1,
		UID:   "test-uid",
		Size:  1024,
		Data:  []byte("test data"),
	}

	if msg.Index != 1 {
		t.Errorf("expected index 1, got %d", msg.Index)
	}
	if msg.UID != "test-uid" {
		t.Errorf("expected uid test-uid, got %s", msg.UID)
	}
	if msg.Size != 1024 {
		t.Errorf("expected size 1024, got %d", msg.Size)
	}
}

func TestStateConsts(t *testing.T) {
	if StateAuthorization != 0 {
		t.Errorf("expected StateAuthorization to be 0, got %d", StateAuthorization)
	}
	if StateTransaction != 1 {
		t.Errorf("expected StateTransaction to be 1, got %d", StateTransaction)
	}
	if StateUpdate != 2 {
		t.Errorf("expected StateUpdate to be 2, got %d", StateUpdate)
	}
}
