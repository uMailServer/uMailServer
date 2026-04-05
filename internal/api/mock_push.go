package api

import (
	"github.com/umailserver/umailserver/internal/push"
)

// MockPushService mock for testing
type MockPushService struct {
	SubscribeError       error
	UnsubscribeError     error
	SendNotificationError error
	GetVAPIDPublicKeyResult string

	SubscribeCalls       []string
	UnsubscribeCalls    []struct{ UserID, SubscriptionID string }
	SendNotificationCalls []struct{ UserID string }
}

func (m *MockPushService) Subscribe(userID string, sub *push.Subscription) error {
	if m.SubscribeCalls == nil {
		m.SubscribeCalls = []string{}
	}
	m.SubscribeCalls = append(m.SubscribeCalls, userID)
	return m.SubscribeError
}

func (m *MockPushService) Unsubscribe(userID, subscriptionID string) error {
	if m.UnsubscribeCalls == nil {
		m.UnsubscribeCalls = []struct{ UserID, SubscriptionID string }{}
	}
	m.UnsubscribeCalls = append(m.UnsubscribeCalls, struct{ UserID, SubscriptionID string }{userID, subscriptionID})
	return m.UnsubscribeError
}

func (m *MockPushService) SendNotification(userID string, notif *push.Notification) error {
	if m.SendNotificationCalls == nil {
		m.SendNotificationCalls = []struct{ UserID string }{}
	}
	m.SendNotificationCalls = append(m.SendNotificationCalls, struct{ UserID string }{userID})
	return m.SendNotificationError
}

func (m *MockPushService) GetVAPIDPublicKey() string {
	return m.GetVAPIDPublicKeyResult
}
