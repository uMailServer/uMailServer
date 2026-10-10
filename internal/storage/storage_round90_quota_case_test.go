package storage

import (
	"fmt"
	"reflect"
	"testing"
)

// CONTROL: unconstrained StoreMessage works (the only write API today).
func TestAudit5720Control(t *testing.T) {
	ms, err := NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	if _, err := ms.StoreMessage("u@example.com", []byte("hello world body")); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
}

// FAILURE: nothing at store level can bound a user's bytes; the only
// enforcement lives in the server's account counter. The store must offer a
// quota-checked write with a typed error callers can map to 452/552.
func TestAudit5720Failure(t *testing.T) {
	ms, _ := NewMessageStore(t.TempDir())
	_, ok := reflect.TypeOf(ms).MethodByName("StoreMessageWithQuota")
	_, ok2 := reflect.TypeOf(ms).MethodByName("UserUsage")
	t.Logf("EXPECTED: StoreMessageWithQuota+UserUsage exist ACTUAL: %v/%v", ok, ok2)
	for i := 0; i < 10; i++ {
		_, _ = ms.StoreMessage("u@example.com", []byte(fmt.Sprintf("%0100d", i)))
	}
	if !ok || !ok2 {
		t.Fatalf("DEFECT F5720: no store-level quota enforcement; 10 writes of 100B all accepted with no limit available")
	}
}
