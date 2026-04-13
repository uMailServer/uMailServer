// Package push provides Web Push notification support for mobile and desktop clients
package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Config holds push notification configuration
type Config struct {
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	Subject         string // mailto: or https:// URL
}

// Subscription represents a push subscription from a client
type Subscription struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Endpoint   string     `json:"endpoint"`
	P256dh     string     `json:"p256dh"`
	Auth       string     `json:"auth"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeviceInfo DeviceInfo `json:"device_info,omitempty"`
}

// DeviceInfo holds information about the subscribed device
type DeviceInfo struct {
	DeviceType string `json:"device_type,omitempty"` // mobile, desktop, tablet
	OS         string `json:"os,omitempty"`          // iOS, Android, Windows, macOS, Linux
	Browser    string `json:"browser,omitempty"`     // Chrome, Firefox, Safari, Edge
	Name       string `json:"name,omitempty"`        // User-defined device name
}

// Notification represents a push notification
type Notification struct {
	Title              string               `json:"title"`
	Body               string               `json:"body"`
	Icon               string               `json:"icon,omitempty"`
	Badge              string               `json:"badge,omitempty"`
	Image              string               `json:"image,omitempty"`
	Tag                string               `json:"tag,omitempty"`
	Data               map[string]string    `json:"data,omitempty"`
	RequireInteraction bool                 `json:"requireInteraction,omitempty"`
	Actions            []NotificationAction `json:"actions,omitempty"`
}

// NotificationAction represents an action button on the notification
type NotificationAction struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Icon   string `json:"icon,omitempty"`
}

// Service manages push notifications
type Service struct {
	config        Config
	logger        *slog.Logger
	dataDir       string
	subscriptions map[string]*Subscription // key: subscription ID
	userSubs      map[string][]string      // user ID -> subscription IDs
	mu            sync.RWMutex
}

// NewService creates a new push notification service
func NewService(dataDir string, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		dataDir:       dataDir,
		logger:        logger,
		subscriptions: make(map[string]*Subscription),
		userSubs:      make(map[string][]string),
	}

	// Load or generate VAPID keys
	config, err := service.loadOrGenerateConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load VAPID config: %w", err)
	}
	service.config = *config

	// Load existing subscriptions
	if err := service.loadSubscriptions(); err != nil {
		logger.Warn("Failed to load subscriptions", "error", err)
	}

	return service, nil
}

// GetVAPIDPublicKey returns the VAPID public key for client subscription
func (s *Service) GetVAPIDPublicKey() string {
	return s.config.VAPIDPublicKey
}

// Subscribe adds a new push subscription for a user
func (s *Service) Subscribe(userID string, sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not provided
	if sub.ID == "" {
		sub.ID = generateSubscriptionID()
	}

	sub.UserID = userID
	sub.CreatedAt = time.Now()
	sub.UpdatedAt = time.Now()

	// Store subscription
	s.subscriptions[sub.ID] = sub

	// Add to user's subscription list
	s.userSubs[userID] = append(s.userSubs[userID], sub.ID)

	// Persist to disk
	if err := s.saveSubscription(sub); err != nil {
		return fmt.Errorf("failed to save subscription: %w", err)
	}

	s.logger.Info("Push subscription added",
		"user", userID,
		"subscription", sub.ID,
		"device", sub.DeviceInfo.Name,
	)

	return nil
}

// Unsubscribe removes a push subscription
func (s *Service) Unsubscribe(userID, subscriptionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, exists := s.subscriptions[subscriptionID]
	if !exists || sub.UserID != userID {
		return fmt.Errorf("subscription not found")
	}

	// Remove from subscriptions map
	delete(s.subscriptions, subscriptionID)

	// Remove from user's subscription list
	userSubList := s.userSubs[userID]
	for i, id := range userSubList {
		if id == subscriptionID {
			s.userSubs[userID] = append(userSubList[:i], userSubList[i+1:]...)
			break
		}
	}

	// Remove from disk
	if err := s.deleteSubscriptionFile(subscriptionID); err != nil {
		s.logger.Warn("Failed to delete subscription file", "error", err)
	}

	s.logger.Info("Push subscription removed",
		"user", userID,
		"subscription", subscriptionID,
	)

	return nil
}

// GetUserSubscriptions returns all subscriptions for a user
func (s *Service) GetUserSubscriptions(userID string) []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var subs []*Subscription
	for _, id := range s.userSubs[userID] {
		if sub, exists := s.subscriptions[id]; exists {
			subs = append(subs, sub)
		}
	}

	return subs
}

// SendNotification sends a push notification to a specific subscription
func (s *Service) SendNotification(sub *Subscription, notification *Notification) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create webpush subscription
	webSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}

	// Send the push notification
	options := &webpush.Options{
		Subscriber:      s.config.Subject,
		VAPIDPublicKey:  s.config.VAPIDPublicKey,
		VAPIDPrivateKey: s.config.VAPIDPrivateKey,
		TTL:             30,
	}

	resp, err := webpush.SendNotification(payload, webSub, options)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	// Check for expired subscriptions
	if resp.StatusCode == 410 || resp.StatusCode == 404 {
		// Subscription is no longer valid, remove it
		s.Unsubscribe(sub.UserID, sub.ID)
		return fmt.Errorf("subscription expired")
	}

	return nil
}

// SendToUser sends a notification to all devices of a user
func (s *Service) SendToUser(userID string, notification *Notification) error {
	subs := s.GetUserSubscriptions(userID)
	if len(subs) == 0 {
		return nil // No subscriptions, nothing to do
	}

	var lastErr error
	sent := 0
	failed := 0

	for _, sub := range subs {
		if err := s.SendNotification(sub, notification); err != nil {
			lastErr = err
			failed++
			s.logger.Warn("Failed to send notification",
				"user", userID,
				"subscription", sub.ID,
				"error", err,
			)
		} else {
			sent++
		}
	}

	s.logger.Debug("Push notifications sent",
		"user", userID,
		"sent", sent,
		"failed", failed,
	)

	if lastErr != nil {
		return fmt.Errorf("sent %d, failed %d: %w", sent, failed, lastErr)
	}

	return nil
}

// SendNewMailNotification sends a notification for new mail
func (s *Service) SendNewMailNotification(userID, from, subject, preview string) error {
	notification := &Notification{
		Title: "New Email",
		Body:  fmt.Sprintf("From: %s\n%s", from, subject),
		Icon:  "/icons/mail.png",
		Badge: "/icons/badge.png",
		Tag:   "new-mail",
		Data: map[string]string{
			"type":    "new-mail",
			"from":    from,
			"subject": subject,
		},
		Actions: []NotificationAction{
			{Action: "open", Title: "Open"},
			{Action: "dismiss", Title: "Dismiss"},
		},
	}

	if preview != "" {
		notification.Body = fmt.Sprintf("From: %s\n%s\n%s", from, subject, preview)
	}

	return s.SendToUser(userID, notification)
}

// loadOrGenerateConfig loads existing VAPID keys or generates new ones
func (s *Service) loadOrGenerateConfig() (*Config, error) {
	configPath := filepath.Join(s.dataDir, "vapid.json")

	// Try to load existing config
	if data, err := os.ReadFile(filepath.Clean(configPath)); err == nil {
		var config Config
		if err := json.Unmarshal(data, &config); err == nil {
			return &config, nil
		}
	}

	// Generate new VAPID keys
	privateKey, publicKey, err := generateVAPIDKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate VAPID keys: %w", err)
	}

	config := &Config{
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		Subject:         "mailto:admin@umailserver.local",
	}

	// Save config
	if err := os.MkdirAll(s.dataDir, 0750); err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return nil, err
	}

	s.logger.Info("Generated new VAPID keys")

	return config, nil
}

// generateVAPIDKeys generates a new VAPID key pair
func generateVAPIDKeys() (privateKey, publicKey string, err error) {
	// Generate EC P-256 key pair
	curve := elliptic.P256()
	priv, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return "", "", err
	}

	// Encode private key
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	privateKey = base64.RawURLEncoding.EncodeToString(privBytes)

	// Encode public key
	pubBytes := elliptic.Marshal(curve, priv.PublicKey.X, priv.PublicKey.Y)
	publicKey = base64.RawURLEncoding.EncodeToString(pubBytes)

	return privateKey, publicKey, nil
}

// loadSubscriptions loads subscriptions from disk
func (s *Service) loadSubscriptions() error {
	if err := os.MkdirAll(s.dataDir, 0750); err != nil {
		return err
	}

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" || entry.Name() == "vapid.json" {
			continue
		}

		path := filepath.Join(s.dataDir, entry.Name())
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			continue
		}

		var sub Subscription
		if err := json.Unmarshal(data, &sub); err != nil {
			continue
		}

		s.subscriptions[sub.ID] = &sub
		s.userSubs[sub.UserID] = append(s.userSubs[sub.UserID], sub.ID)
	}

	return nil
}

// saveSubscription saves a subscription to disk
func (s *Service) saveSubscription(sub *Subscription) error {
	filename := fmt.Sprintf("sub_%s.json", sub.ID)
	path := filepath.Join(s.dataDir, filename)

	data, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// deleteSubscriptionFile removes a subscription file
func (s *Service) deleteSubscriptionFile(subscriptionID string) error {
	filename := fmt.Sprintf("sub_%s.json", subscriptionID)
	path := filepath.Join(s.dataDir, filename)
	return os.Remove(path)
}

// generateSubscriptionID generates a unique subscription ID
func generateSubscriptionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// UpdateDeviceInfo updates device information for a subscription
func (s *Service) UpdateDeviceInfo(userID, subscriptionID string, info DeviceInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, exists := s.subscriptions[subscriptionID]
	if !exists || sub.UserID != userID {
		return fmt.Errorf("subscription not found")
	}

	sub.DeviceInfo = info
	sub.UpdatedAt = time.Now()

	return s.saveSubscription(sub)
}

// CleanExpiredSubscriptions removes expired subscriptions
func (s *Service) CleanExpiredSubscriptions() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var toDelete []string
	for id, sub := range s.subscriptions {
		// Remove subscriptions older than 90 days without update
		if time.Since(sub.UpdatedAt) > 90*24*time.Hour {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		if sub, exists := s.subscriptions[id]; exists {
			delete(s.subscriptions, id)

			// Remove from user's list
			userID := sub.UserID
			userSubList := s.userSubs[userID]
			for i, sid := range userSubList {
				if sid == id {
					s.userSubs[userID] = append(userSubList[:i], userSubList[i+1:]...)
					break
				}
			}

			s.deleteSubscriptionFile(id)
		}
	}

	if len(toDelete) > 0 {
		s.logger.Info("Cleaned expired subscriptions", "count", len(toDelete))
	}

	return nil
}

// GetStats returns statistics about push subscriptions
func (s *Service) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deviceTypes := make(map[string]int)
	osTypes := make(map[string]int)

	for _, sub := range s.subscriptions {
		deviceTypes[sub.DeviceInfo.DeviceType]++
		osTypes[sub.DeviceInfo.OS]++
	}

	return map[string]interface{}{
		"totalSubscriptions": len(s.subscriptions),
		"totalUsers":         len(s.userSubs),
		"deviceTypes":        deviceTypes,
		"osTypes":            osTypes,
	}
}
