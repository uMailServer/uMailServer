package imap

import (
	"sync"
	"testing"
)

// NotificationHub regression tests for the send-on-closed-channel race.
//
// Notify used to copy the subscriber slice under a read lock, release the
// lock, and only then send. A concurrent Unsubscribe (which takes the write
// lock and close()s the channel) could close a channel that Notify was about
// to send on, panicking the whole process with "send on closed channel". The
// production callers are real: SSE clients subscribe/unsubscribe
// (internal/websocket/sse.go) and IMAP IDLE does too, while every delivery
// fires NotifyNewMessage/NotifyFlagsChanged (internal/imap/mailstore.go).
//
// The fix holds the read lock across the (non-blocking) sends, so a channel
// can never be closed while it is being sent to.

// TestNotificationHub_SequentialDelivery is the control: a purely sequential
// subscribe -> notify -> unsubscribe must deliver the notification and must
// not race. It passes both before and after the fix.
func TestNotificationHub_SequentialDelivery(t *testing.T) {
	hub := NewNotificationHub()
	ch := hub.Subscribe("ctl")
	defer hub.Unsubscribe("ctl", ch)

	hub.NotifyNewMessage("ctl", "INBOX", 1, 1)

	select {
	case n := <-ch:
		if n.Type != NotificationNewMessage {
			t.Fatalf("got type %d, want NotificationNewMessage", n.Type)
		}
	default:
		t.Fatal("notification was not delivered to a ready subscriber")
	}
}

// TestNotificationHub_NotifyDuringUnsubscribe is the regression stress test.
// It runs a producer notifying concurrently with churners that
// subscribe/unsubscribe. On the buggy code the producer sends on a channel
// that Unsubscribe just closed and the test process panics (test failure).
// On the fixed code the read lock is held across every send, so no send can
// race a close and the run completes cleanly. Run with -race for a
// deterministic report; even without -race the panic reproduces reliably.
func TestNotificationHub_NotifyDuringUnsubscribe(t *testing.T) {
	hub := NewNotificationHub()

	const workers = 8
	const roundsPerWorker = 200

	var wg sync.WaitGroup

	// Producer: deliver notifications while churners come and go.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < roundsPerWorker*workers; i++ {
			hub.Notify("churn", MailboxNotification{
				Type: NotificationNewMessage,
				User: "churn",
			})
		}
	}()

	// Churners: rapidly subscribe and unsubscribe (each unsubscribe closes
	// the channel), maximising overlap with the producer's sends.
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < roundsPerWorker; i++ {
				ch := hub.Subscribe("churn")
				hub.Unsubscribe("churn", ch)
			}
		}()
	}

	wg.Wait()
}
