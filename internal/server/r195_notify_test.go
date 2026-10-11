package server

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

// F6649d: local delivery (SMTP, Sieve fileinto, catch-all, ...) must wake
// IMAP IDLE / SSE subscribers through the notification hub.
func TestF6649dLocalDeliveryNotifiesHub(t *testing.T) {
	s := newDeliveryAuditServer(t)
	ch := imap.GetNotificationHub().Subscribe("bob@test.com")
	defer imap.GetNotificationHub().Unsubscribe("bob@test.com", ch)
	if err := s.deliverLocal("bob", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-ch:
		if n.Type != imap.NotificationNewMessage || n.Mailbox != "INBOX" || n.SeqNum != 1 {
			t.Fatalf("notification = %+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification after local delivery")
	}
}
