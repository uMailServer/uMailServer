package imap

import (
	"testing"
	"time"
)

func TestNewNotificationHub(t *testing.T) {
	hub := NewNotificationHub()
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
	if hub.subscribers == nil {
		t.Error("expected subscribers map to be initialized")
	}
}

func TestGetNotificationHub(t *testing.T) {
	hub := GetNotificationHub()
	if hub == nil {
		t.Fatal("expected non-nil global hub")
	}
}

func TestNotificationHubSubscribe(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	hub.mu.RLock()
	channels := hub.subscribers["user1"]
	hub.mu.RUnlock()

	if len(channels) != 1 {
		t.Errorf("expected 1 subscriber, got %d", len(channels))
	}
}

func TestNotificationHubUnsubscribe(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")
	hub.Unsubscribe("user1", ch)

	hub.mu.RLock()
	channels := hub.subscribers["user1"]
	hub.mu.RUnlock()

	if len(channels) != 0 {
		t.Errorf("expected 0 subscribers, got %d", len(channels))
	}
}

func TestNotificationHubUnsubscribeNotFound(t *testing.T) {
	hub := NewNotificationHub()

	ch := make(chan MailboxNotification)
	// Should not panic
	hub.Unsubscribe("nonexistent", ch)
}

func TestNotificationHubNotify(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")

	notification := MailboxNotification{
		Type:       NotificationNewMessage,
		User:       "user1",
		Mailbox:    "INBOX",
		MessageUID: 100,
		SeqNum:     1,
	}

	hub.Notify("user1", notification)

	select {
	case received := <-ch:
		if received.Type != NotificationNewMessage {
			t.Errorf("expected type NotificationNewMessage, got %d", received.Type)
		}
		if received.Mailbox != "INBOX" {
			t.Errorf("expected mailbox INBOX, got %s", received.Mailbox)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for notification")
	}
}

func TestNotificationHubNotifyNoSubscribers(t *testing.T) {
	hub := NewNotificationHub()

	notification := MailboxNotification{
		Type:    NotificationNewMessage,
		User:    "nonexistent",
		Mailbox: "INBOX",
	}

	// Should not panic
	hub.Notify("nonexistent", notification)
}

func TestNotificationHubNotifyMultipleSubscribers(t *testing.T) {
	hub := NewNotificationHub()

	ch1 := hub.Subscribe("user1")
	ch2 := hub.Subscribe("user1")

	notification := MailboxNotification{
		Type:       NotificationNewMessage,
		User:       "user1",
		Mailbox:    "INBOX",
		MessageUID: 100,
	}

	hub.Notify("user1", notification)

	// Both subscribers should receive the notification
	select {
	case <-ch1:
		// OK
	case <-time.After(time.Second):
		t.Error("timeout waiting for first subscriber")
	}

	select {
	case <-ch2:
		// OK
	case <-time.After(time.Second):
		t.Error("timeout waiting for second subscriber")
	}
}

func TestNotificationHubNotifyNewMessage(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")

	hub.NotifyNewMessage("user1", "INBOX", 100, 1)

	select {
	case received := <-ch:
		if received.Type != NotificationNewMessage {
			t.Errorf("expected type NotificationNewMessage, got %d", received.Type)
		}
		if received.User != "user1" {
			t.Errorf("expected user user1, got %s", received.User)
		}
		if received.Mailbox != "INBOX" {
			t.Errorf("expected mailbox INBOX, got %s", received.Mailbox)
		}
		if received.MessageUID != 100 {
			t.Errorf("expected uid 100, got %d", received.MessageUID)
		}
		if received.SeqNum != 1 {
			t.Errorf("expected seqNum 1, got %d", received.SeqNum)
		}
		if received.Timestamp.IsZero() {
			t.Error("expected timestamp to be set")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for notification")
	}
}

func TestNotificationHubNotifyExpunge(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")

	hub.NotifyExpunge("user1", "INBOX", 5)

	select {
	case received := <-ch:
		if received.Type != NotificationExpunge {
			t.Errorf("expected type NotificationExpunge, got %d", received.Type)
		}
		if received.SeqNum != 5 {
			t.Errorf("expected seqNum 5, got %d", received.SeqNum)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for notification")
	}
}

func TestNotificationHubNotifyFlagsChanged(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")

	flags := []string{"\\Seen", "\\Answered"}
	hub.NotifyFlagsChanged("user1", "INBOX", 100, 1, flags)

	select {
	case received := <-ch:
		if received.Type != NotificationFlagsChanged {
			t.Errorf("expected type NotificationFlagsChanged, got %d", received.Type)
		}
		if len(received.Flags) != 2 {
			t.Errorf("expected 2 flags, got %d", len(received.Flags))
		}
		if received.Flags[0] != "\\Seen" {
			t.Errorf("expected flag \\Seen, got %s", received.Flags[0])
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for notification")
	}
}

func TestNotificationHubNotifyMailboxUpdate(t *testing.T) {
	hub := NewNotificationHub()

	ch := hub.Subscribe("user1")

	hub.NotifyMailboxUpdate("user1", "INBOX")

	select {
	case received := <-ch:
		if received.Type != NotificationMailboxUpdate {
			t.Errorf("expected type NotificationMailboxUpdate, got %d", received.Type)
		}
		if received.Mailbox != "INBOX" {
			t.Errorf("expected mailbox INBOX, got %s", received.Mailbox)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for notification")
	}
}

func TestMailboxNotificationStruct(t *testing.T) {
	now := time.Now()
	notification := MailboxNotification{
		Type:       NotificationNewMessage,
		User:       "user1",
		Mailbox:    "INBOX",
		MessageUID: 100,
		SeqNum:     1,
		Flags:      []string{"\\Seen"},
		Timestamp:  now,
	}

	if notification.Type != NotificationNewMessage {
		t.Errorf("expected type NotificationNewMessage, got %d", notification.Type)
	}
	if notification.User != "user1" {
		t.Errorf("expected user user1, got %s", notification.User)
	}
}

func TestNotificationTypeConsts(t *testing.T) {
	if NotificationNewMessage != 1 {
		t.Errorf("expected NotificationNewMessage 1, got %d", NotificationNewMessage)
	}
	if NotificationExpunge != 2 {
		t.Errorf("expected NotificationExpunge 2, got %d", NotificationExpunge)
	}
	if NotificationFlagsChanged != 3 {
		t.Errorf("expected NotificationFlagsChanged 3, got %d", NotificationFlagsChanged)
	}
	if NotificationMailboxUpdate != 4 {
		t.Errorf("expected NotificationMailboxUpdate 4, got %d", NotificationMailboxUpdate)
	}
}

func TestNotificationHubMultipleUsers(t *testing.T) {
	hub := NewNotificationHub()

	ch1 := hub.Subscribe("user1")
	ch2 := hub.Subscribe("user2")

	hub.NotifyNewMessage("user1", "INBOX", 100, 1)

	// User1 should receive
	select {
	case <-ch1:
		// OK
	case <-time.After(time.Second):
		t.Error("timeout waiting for user1 notification")
	}

	// User2 should not receive
	select {
	case <-ch2:
		t.Error("user2 should not have received notification")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
