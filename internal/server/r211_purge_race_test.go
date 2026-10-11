package server

import (
	"strings"
	"testing"
)

// F6880: a delivery that passed the account check just before the account row
// was deleted must not write after the purge and recreate the mailbox.
func TestR211DeliveryRacingAccountPurgeDoesNotResurrect(t *testing.T) {
	s := newDeliveryAuditServer(t)
	fired := false
	deliverAfterAccountCheckHook = func(email string) {
		if fired || email != "bob@test.com" {
			return
		}
		fired = true
		// The admin deletes the account and purges, exactly like
		// api.purgeAccountData (lock, then purge).
		if err := s.database.DeleteAccount("test.com", "bob"); err != nil {
			t.Errorf("delete account: %v", err)
		}
		unlock := accountPurger{srv: s}.LockDelivery(email)
		defer unlock()
		if err := s.msgStore.PurgeUser(email); err != nil {
			t.Errorf("purge store: %v", err)
		}
		if err := s.storageDB.PurgeUser(email); err != nil {
			t.Errorf("purge db: %v", err)
		}
	}
	t.Cleanup(func() { deliverAfterAccountCheckHook = nil })

	err := s.deliverLocal("bob", "test.com", "s@ext.com", []byte(deliveryAuditMsg))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("deliverLocal err = %v; want user does not exist", err)
	}
	if !fired {
		t.Fatal("hook not fired")
	}
	if n := inboxCount(t, s, "bob@test.com"); n != 0 {
		t.Fatalf("purged mailbox resurrected with %d message(s)", n)
	}
}
