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
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

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

// pushHTTPClient bounds every request to a push service. webpush-go otherwise
// uses &http.Client{} with no timeout, so an endpoint chosen by a subscriber
// that never answers would block the sender forever (F5182). The transport
// refuses to connect to loopback/private/link-local addresses (checked after
// DNS resolution, so DNS rebinding cannot reach internal hosts, F5760) and
// redirects are not followed.
var pushHTTPClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: newPushTransport(),
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func newPushTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	t := base.Clone()
	t.Proxy = nil
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if addr, err := netip.ParseAddr(host); err == nil && isForbiddenAddr(addr) {
				return fmt.Errorf("push endpoint address %s is not allowed", host)
			}
			return nil
		},
	}
	t.DialContext = dialer.DialContext
	return t
}

// isForbiddenAddr reports whether addr is not a public unicast address.
func isForbiddenAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() ||
		addr.IsInterfaceLocalMulticast() {
		return true
	}
	// 100.64.0.0/10 carrier-grade NAT
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1]&0xC0 == 64 {
			return true
		}
	}
	return false
}

// validateEndpoint rejects push endpoints that are not https URLs to a
// public host (F5760). Subscriptions are subscriber-controlled, so without
// this the server would POST to arbitrary internal URLs.
func validateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid push endpoint: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("push endpoint must use https")
	}
	if u.User != nil {
		return fmt.Errorf("push endpoint must not contain credentials")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return fmt.Errorf("push endpoint has no host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("push endpoint host %q is not allowed", host)
	}
	if addr, err := netip.ParseAddr(host); err == nil && isForbiddenAddr(addr) {
		return fmt.Errorf("push endpoint address %s is not allowed", host)
	}
	return nil
}

var validIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Service manages push notifications
type Service struct {
	config        Config
	logger        *slog.Logger
	dataDir       string
	subscriptions map[string]*Subscription // key: subscription ID
	userSubs      map[string][]string      // user ID -> subscription IDs
	mu            sync.RWMutex
}

// NewService creates a new push notification service. VAPID keys are loaded
// from `<dataDir>/vapid.json` if present, otherwise generated and persisted.
// Subject defaults to "mailto:admin@umailserver.local".
func NewService(dataDir string, logger *slog.Logger) (*Service, error) {
	return NewServiceWithConfig(dataDir, Config{}, logger)
}

// NewServiceWithConfig creates a push service with operator-supplied
// overrides. When override.VAPIDPublicKey AND override.VAPIDPrivateKey are
// both set, they short-circuit the on-disk file (and skip generation). When
// override.Subject is non-empty it overrides the default / persisted subject.
// All other fields fall back to the on-disk config or generated defaults.
func NewServiceWithConfig(dataDir string, override Config, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		dataDir:       dataDir,
		logger:        logger,
		subscriptions: make(map[string]*Subscription),
		userSubs:      make(map[string][]string),
	}

	if override.VAPIDPublicKey != "" && override.VAPIDPrivateKey != "" {
		service.config = Config{
			VAPIDPublicKey:  override.VAPIDPublicKey,
			VAPIDPrivateKey: override.VAPIDPrivateKey,
			Subject:         override.Subject,
		}
		if service.config.Subject == "" {
			service.config.Subject = "mailto:admin@umailserver.local"
		}
		logger.Info("Push VAPID keys loaded from configuration override")
	} else {
		// Load or generate VAPID keys from disk
		cfg, err := service.loadOrGenerateConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load VAPID config: %w", err)
		}
		service.config = *cfg
		if override.Subject != "" {
			service.config.Subject = override.Subject
		}
	}

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

// Subscribe adds a new push subscription for a user. The caller's sub is not
// retained (a copy is stored); sub.ID is filled in on return.
func (s *Service) Subscribe(userID string, sub *Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}
	if err := validateEndpoint(sub.Endpoint); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not provided
	if sub.ID == "" {
		sub.ID = generateSubscriptionID()
		if sub.ID == "" {
			return fmt.Errorf("failed to generate subscription ID")
		}
	}
	// The ID becomes part of a file name (F5761).
	if !validIDRe.MatchString(sub.ID) {
		return fmt.Errorf("invalid subscription ID")
	}
	// A client-chosen ID must not overwrite another user's subscription (F5762).
	if existing, ok := s.subscriptions[sub.ID]; ok {
		if existing.UserID != userID {
			return fmt.Errorf("subscription ID already in use")
		}
		s.removeLocked(sub.ID)
	}

	sub.UserID = userID
	sub.CreatedAt = time.Now()
	sub.UpdatedAt = sub.CreatedAt

	// A push endpoint identifies exactly one browser subscription (RFC 8030),
	// so a re-registration replaces the previous record instead of adding a
	// duplicate that would deliver every notification again (F5181). The
	// endpoint now belongs to userID, even if another user registered it.
	for id, existing := range s.subscriptions {
		if existing.Endpoint == sub.Endpoint {
			s.removeLocked(id)
		}
	}

	stored := *sub
	// Persist first so a failed write leaves no phantom in-memory record (F5763).
	if err := s.saveSubscription(&stored); err != nil {
		return fmt.Errorf("failed to save subscription: %w", err)
	}
	s.subscriptions[stored.ID] = &stored
	s.userSubs[userID] = append(s.userSubs[userID], stored.ID)

	s.logger.Info("Push subscription added",
		"user", userID,
		"subscription", stored.ID,
		"device", stored.DeviceInfo.Name,
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

	s.removeLocked(subscriptionID)

	s.logger.Info("Push subscription removed",
		"user", userID,
		"subscription", subscriptionID,
	)

	return nil
}

// removeLocked deletes a subscription from memory and disk. s.mu must be held.
func (s *Service) removeLocked(subscriptionID string) {
	sub, exists := s.subscriptions[subscriptionID]
	if !exists {
		return
	}
	delete(s.subscriptions, subscriptionID)

	userSubList := s.userSubs[sub.UserID]
	for i, id := range userSubList {
		if id == subscriptionID {
			s.userSubs[sub.UserID] = append(userSubList[:i], userSubList[i+1:]...)
			break
		}
	}
	if len(s.userSubs[sub.UserID]) == 0 {
		delete(s.userSubs, sub.UserID)
	}

	if err := s.deleteSubscriptionFile(subscriptionID); err != nil {
		s.logger.Warn("Failed to delete subscription file", "error", err)
	}
}

// GetUserSubscriptions returns all subscriptions for a user
func (s *Service) GetUserSubscriptions(userID string) []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var subs []*Subscription
	for _, id := range s.userSubs[userID] {
		if sub, exists := s.subscriptions[id]; exists {
			c := *sub // copy: callers must not race with UpdateDeviceInfo (F5764)
			subs = append(subs, &c)
		}
	}

	return subs
}

// SendNotification sends a push notification to a specific subscription
func (s *Service) SendNotification(sub *Subscription, notification *Notification) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	if notification == nil {
		return fmt.Errorf("notification is nil")
	}
	if err := validateEndpoint(sub.Endpoint); err != nil {
		return err
	}

	payload, err := marshalPayload(notification)
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
		Subscriber:      vapidSubscriber(s.config.Subject),
		VAPIDPublicKey:  s.config.VAPIDPublicKey,
		VAPIDPrivateKey: s.config.VAPIDPrivateKey,
		TTL:             30,
		HTTPClient:      pushHTTPClient,
	}

	resp, err := webpush.SendNotification(payload, webSub, options)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for expired subscriptions
	if resp.StatusCode == 410 || resp.StatusCode == 404 {
		// Subscription is no longer valid, remove it
		_ = s.Unsubscribe(sub.UserID, sub.ID)
		return fmt.Errorf("subscription expired")
	}

	// Any other non-2xx answer (400, 403 VAPID rejection, 413, 429, 5xx)
	// means the notification was not accepted (F5180).
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("push service returned HTTP %d", resp.StatusCode)
	}

	s.touch(sub.ID)

	return nil
}

// maxPayload is the largest plaintext webpush-go accepts in one aes128gcm
// record (4096 - 16 tag - 86 header - 1 delimiter).
const maxPayload = 3993

// marshalPayload encodes n, shrinking an oversized notification (an
// attacker-controlled mail subject can be arbitrarily long) so that it is
// still delivered instead of failing with ErrMaxPadExceeded (F5765).
func marshalPayload(n *Notification) ([]byte, error) {
	payload, err := json.Marshal(n)
	if err != nil || len(payload) <= maxPayload {
		return payload, err
	}
	c := *n
	c.Data = nil
	c.Body = truncateRunes(c.Body, 500)
	c.Title = truncateRunes(c.Title, 200)
	payload, err = json.Marshal(&c)
	if err != nil || len(payload) <= maxPayload {
		return payload, err
	}
	c.Actions = nil
	c.Body = truncateRunes(c.Body, 100)
	return json.Marshal(&c)
}

func truncateRunes(str string, n int) string {
	if utf8.RuneCountInString(str) <= n {
		return str
	}
	r := []rune(str)
	return string(r[:n]) + "..."
}

// vapidSubscriber returns the VAPID subject in the form webpush-go expects:
// it prepends "mailto:" to anything not starting with "https:", so a
// configured "mailto:x" would otherwise be sent as "mailto:mailto:x" (F5766),
// which push services such as Apple's reject.
func vapidSubscriber(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "mailto:admin@umailserver.local"
	}
	if len(subject) >= 7 && strings.EqualFold(subject[:7], "mailto:") {
		return subject[7:]
	}
	return subject
}

// touch refreshes UpdatedAt of a subscription that just accepted a push, so
// CleanExpiredSubscriptions does not delete live subscriptions after 90 days
// (F5767). Persisted at most once a day.
func (s *Service) touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[id]
	if !ok || time.Since(sub.UpdatedAt) < 24*time.Hour {
		return
	}
	sub.UpdatedAt = time.Now()
	if err := s.saveSubscription(sub); err != nil {
		s.logger.Warn("Failed to persist subscription refresh", "error", err)
	}
}

// SendToUser sends a notification to all devices of a user
func (s *Service) SendToUser(userID string, notification *Notification) error {
	subs := s.GetUserSubscriptions(userID)
	if len(subs) == 0 {
		return nil // No subscriptions, nothing to do
	}

	// Send concurrently so one slow endpoint (30s timeout) does not delay
	// the user's other devices (F5768).
	var (
		wg      sync.WaitGroup
		rmu     sync.Mutex
		lastErr error
		sent    int
		failed  int
	)
	for _, sub := range subs {
		wg.Add(1)
		go func(sub *Subscription) {
			defer wg.Done()
			err := s.SendNotification(sub, notification)
			rmu.Lock()
			defer rmu.Unlock()
			if err != nil {
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
		}(sub)
	}
	wg.Wait()

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
	from = truncateRunes(from, 200)
	subject = truncateRunes(subject, 300)
	preview = truncateRunes(preview, 500)
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
			// Older versions stored EC private-key DER instead of the scalar
			// expected by webpush-go. Preserve the keypair while correcting it.
			if raw, err := base64.RawURLEncoding.DecodeString(config.VAPIDPrivateKey); err == nil && len(raw) != 32 {
				if key, err := x509.ParseECPrivateKey(raw); err == nil && key.Curve == elliptic.P256() {
					public, err := key.ECDH()
					if err == nil && base64.RawURLEncoding.EncodeToString(public.PublicKey().Bytes()) == config.VAPIDPublicKey {
						config.VAPIDPrivateKey = base64.RawURLEncoding.EncodeToString(key.D.FillBytes(make([]byte, 32)))
						data, err := json.MarshalIndent(config, "", "  ")
						if err != nil {
							return nil, err
						}
						if err := os.WriteFile(configPath, data, 0o600); err != nil {
							return nil, fmt.Errorf("failed to normalize VAPID private key: %w", err)
						}
					}
				}
			}
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
	if err := os.MkdirAll(s.dataDir, 0o750); err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := writeFileAtomic(configPath, data); err != nil {
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
	privBytes := priv.D.FillBytes(make([]byte, 32))
	privateKey = base64.RawURLEncoding.EncodeToString(privBytes)

	// Encode public key
	ecPriv, err := priv.ECDH()
	if err != nil {
		return "", "", err
	}
	pubBytes := ecPriv.PublicKey().Bytes()
	publicKey = base64.RawURLEncoding.EncodeToString(pubBytes)

	return privateKey, publicKey, nil
}

// loadSubscriptions loads subscriptions from disk
func (s *Service) loadSubscriptions() error {
	if err := os.MkdirAll(s.dataDir, 0o750); err != nil {
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

		if !validIDRe.MatchString(sub.ID) || s.subscriptions[sub.ID] != nil {
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

	return writeFileAtomic(path, data)
}

// writeFileAtomic writes via a temp file + rename so a crash never leaves a
// truncated subscription that loadSubscriptions would silently drop (F5769).
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
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
	if _, err := rand.Read(b); err != nil {
		return ""
	}
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
		s.removeLocked(id)
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
