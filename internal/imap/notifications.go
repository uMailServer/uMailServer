package imap

import (
	"sync"
	"time"
)

// NotificationType represents the type of mailbox notification
type NotificationType int

const (
	// NotificationNewMessage indicates a new message was delivered
	NotificationNewMessage NotificationType = 1
	// NotificationExpunge indicates a message was expunged
	NotificationExpunge NotificationType = 2
	// NotificationFlagsChanged indicates flags were changed
	NotificationFlagsChanged NotificationType = 3
	// NotificationMailboxUpdate indicates general mailbox update
	NotificationMailboxUpdate NotificationType = 4
)

// MailboxNotification represents a notification about mailbox changes
type MailboxNotification struct {
	Type       NotificationType
	User       string
	Mailbox    string
	MessageUID uint32
	SeqNum     uint32
	Flags      []string
	Timestamp  time.Time
}

// NotificationHub manages notifications for IMAP IDLE and other real-time features
type NotificationHub struct {
	subscribers map[string][]chan MailboxNotification // user -> channels
	mu          sync.RWMutex
}

// NewNotificationHub creates a new notification hub
func NewNotificationHub() *NotificationHub {
	return &NotificationHub{
		subscribers: make(map[string][]chan MailboxNotification),
	}
}

// Subscribe subscribes a session to notifications for a user
func (h *NotificationHub) Subscribe(user string) chan MailboxNotification {
	ch := make(chan MailboxNotification, 100) // Buffer to prevent blocking

	h.mu.Lock()
	h.subscribers[user] = append(h.subscribers[user], ch)
	h.mu.Unlock()

	return ch
}

// Unsubscribe removes a subscription
func (h *NotificationHub) Unsubscribe(user string, ch chan MailboxNotification) {
	h.mu.Lock()
	defer h.mu.Unlock()

	channels := h.subscribers[user]
	for i, c := range channels {
		if c == ch {
			// Close and remove the channel
			close(c)
			h.subscribers[user] = append(channels[:i], channels[i+1:]...)
			break
		}
	}
}

// Notify sends a notification to all subscribers for a user
func (h *NotificationHub) Notify(user string, notification MailboxNotification) {
	h.mu.RLock()
	channels := h.subscribers[user]
	h.mu.RUnlock()

	notification.Timestamp = time.Now()

	for _, ch := range channels {
		// Non-blocking send; drop if subscriber is slow
		select {
		case ch <- notification:
		default:
			// Channel is full or blocked, skip this notification
		}
	}
}

// NotifyNewMessage notifies subscribers about a new message
func (h *NotificationHub) NotifyNewMessage(user, mailbox string, uid, seqNum uint32) {
	h.Notify(user, MailboxNotification{
		Type:       NotificationNewMessage,
		User:       user,
		Mailbox:    mailbox,
		MessageUID: uid,
		SeqNum:     seqNum,
	})
}

// NotifyExpunge notifies subscribers about an expunged message
func (h *NotificationHub) NotifyExpunge(user, mailbox string, seqNum uint32) {
	h.Notify(user, MailboxNotification{
		Type:    NotificationExpunge,
		User:    user,
		Mailbox: mailbox,
		SeqNum:  seqNum,
	})
}

// NotifyFlagsChanged notifies subscribers about flag changes
func (h *NotificationHub) NotifyFlagsChanged(user, mailbox string, uid, seqNum uint32, flags []string) {
	h.Notify(user, MailboxNotification{
		Type:       NotificationFlagsChanged,
		User:       user,
		Mailbox:    mailbox,
		MessageUID: uid,
		SeqNum:     seqNum,
		Flags:      flags,
	})
}

// NotifyMailboxUpdate notifies subscribers about general mailbox updates
func (h *NotificationHub) NotifyMailboxUpdate(user, mailbox string) {
	h.Notify(user, MailboxNotification{
		Type:    NotificationMailboxUpdate,
		User:    user,
		Mailbox: mailbox,
	})
}

// Global notification hub instance
var globalHub = NewNotificationHub()

// GetNotificationHub returns the global notification hub
func GetNotificationHub() *NotificationHub {
	return globalHub
}
