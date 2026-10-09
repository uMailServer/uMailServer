// Package vacation provides vacation auto-reply functionality
package vacation

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config represents vacation auto-reply configuration
type Config struct {
	Enabled     bool      `json:"enabled"`
	StartDate   time.Time `json:"start_date,omitempty"`
	EndDate     time.Time `json:"end_date,omitempty"`
	Subject     string    `json:"subject"`
	Message     string    `json:"message"`
	HTMLMessage string    `json:"html_message,omitempty"`
	// SendOnlyOnce per sender in this interval (default 7 days)
	SendInterval time.Duration `json:"send_interval"`
	// Don't send auto-reply to these addresses
	ExcludeAddresses []string `json:"exclude_addresses,omitempty"`
	// Don't send auto-reply to mailing lists
	IgnoreLists bool `json:"ignore_lists,omitempty"`
	// Don't send auto-reply to bulk/promotional emails
	IgnoreBulk bool `json:"ignore_bulk,omitempty"`
}

// Manager manages vacation auto-reply settings
type Manager struct {
	dataDir   string
	logger    *slog.Logger
	configs   map[string]*Config // key: email address
	mu        sync.RWMutex
	sentCache map[string]map[string]time.Time // user -> normalized sender -> last sent time
	pruneAt   map[string]int                  // user -> sentCache size that triggers the next prune
	cacheMu   sync.RWMutex
}

// minPruneSize is the per-user dedup record count below which expired
// records are not swept (F5208).
const minPruneSize = 1024

// NewManager creates a new vacation manager
func NewManager(dataDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		dataDir:   dataDir,
		logger:    logger,
		configs:   make(map[string]*Config),
		sentCache: make(map[string]map[string]time.Time),
		pruneAt:   make(map[string]int),
	}

	// Load existing configs
	if err := m.loadConfigs(); err != nil {
		logger.Warn("Failed to load vacation configs", "error", err)
	}

	return m
}

// GetConfig gets vacation config for a user
func (m *Manager) GetConfig(email string) (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if config, ok := m.configs[email]; ok {
		// Return a copy
		configCopy := *config
		configCopy.ExcludeAddresses = append([]string(nil), config.ExcludeAddresses...)
		return &configCopy, nil
	}

	// Return default config
	return &Config{
		Enabled:      false,
		Subject:      "Out of Office",
		Message:      "I am currently out of office. I will respond to your email when I return.",
		SendInterval: 7 * 24 * time.Hour, // 7 days
		IgnoreLists:  true,
		IgnoreBulk:   true,
	}, nil
}

// SetConfig sets vacation config for a user
func (m *Manager) SetConfig(email string, config *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate config
	if config.Enabled {
		if config.Subject == "" {
			return fmt.Errorf("subject is required when vacation is enabled")
		}
		if config.Message == "" {
			return fmt.Errorf("message is required when vacation is enabled")
		}
		if config.SendInterval == 0 {
			config.SendInterval = 7 * 24 * time.Hour
		}
	}

	// Save to disk before publishing the configuration.
	if err := m.saveConfig(email, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	// Store a copy so later changes to the caller's value neither bypass this
	// method nor hide a change from startsNewVacation.
	stored := *config
	stored.ExcludeAddresses = append([]string(nil), config.ExcludeAddresses...)
	prev := m.configs[email]
	m.configs[email] = &stored
	if stored.Enabled && startsNewVacation(prev, &stored) {
		// A new absence (re-enabled, or new text or period) must reach senders
		// who were answered about the previous one (F5209, RFC 5230 §4.2).
		m.forgetReplies(email)
	}

	m.logger.Info("Vacation config updated",
		"email", email,
		"enabled", config.Enabled,
	)

	return nil
}

// ShouldSendAutoReply checks if auto-reply should be sent
func (m *Manager) ShouldSendAutoReply(user, sender string, headers map[string]string) bool {
	config, ok := m.eligibleConfig(user, sender, headers)
	if !ok {
		return false
	}

	// Check send interval (don't spam the same sender)
	m.cacheMu.RLock()
	lastSent, ok := m.sentCache[user][normalizeAddress(sender)]
	m.cacheMu.RUnlock()

	return !ok || time.Since(lastSent) >= config.SendInterval
}

// CheckAndRecordAutoReply atomically checks if auto-reply should be sent and records it.
// This prevents race conditions where two concurrent deliveries could both pass the check
// before either records the send (VULN-007).
func (m *Manager) CheckAndRecordAutoReply(user, sender string, headers map[string]string) bool {
	config, ok := m.eligibleConfig(user, sender, headers)
	if !ok {
		return false
	}

	// Atomically check send interval and record
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	key := normalizeAddress(sender)
	if lastSent, ok := m.sentCache[user][key]; ok && time.Since(lastSent) < config.SendInterval {
		return false
	}
	m.recordLocked(user, key, config.SendInterval)
	return true
}

// RecordAutoReplySent records that an auto-reply was sent
// Deprecated: Use CheckAndRecordAutoReply instead for atomic check-and-record.
func (m *Manager) RecordAutoReplySent(user, sender string) {
	m.mu.RLock()
	var interval time.Duration
	if config, ok := m.configs[user]; ok {
		interval = config.SendInterval
	}
	m.mu.RUnlock()

	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	m.recordLocked(user, normalizeAddress(sender), interval)
}

// recordLocked stores the send time for sender and, once the user's records
// have grown past the prune threshold, drops those older than interval so
// the store stays bounded by the senders seen within one interval (F5208).
// The caller holds cacheMu.
func (m *Manager) recordLocked(user, sender string, interval time.Duration) {
	userCache := m.sentCache[user]
	if userCache == nil {
		userCache = make(map[string]time.Time)
		m.sentCache[user] = userCache
	}
	now := time.Now()
	userCache[sender] = now

	if len(userCache) < max(m.pruneAt[user], minPruneSize) || interval <= 0 {
		return
	}
	for addr, sent := range userCache {
		if now.Sub(sent) >= interval {
			delete(userCache, addr)
		}
	}
	m.pruneAt[user] = 2 * len(userCache)
}

// forgetReplies drops the dedup records of user.
func (m *Manager) forgetReplies(user string) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	delete(m.sentCache, user)
	delete(m.pruneAt, user)
}

// startsNewVacation reports whether next describes a different absence than
// prev: previously disabled or absent, or changed text or period.
func startsNewVacation(prev, next *Config) bool {
	return prev == nil || !prev.Enabled ||
		prev.Subject != next.Subject || prev.Message != next.Message ||
		prev.HTMLMessage != next.HTMLMessage ||
		!prev.StartDate.Equal(next.StartDate) || !prev.EndDate.Equal(next.EndDate)
}

// eligibleConfig returns the user's config when every check other than the
// per-sender interval allows a reply to sender.
func (m *Manager) eligibleConfig(user, sender string, headers map[string]string) (*Config, bool) {
	m.mu.RLock()
	config, ok := m.configs[user]
	m.mu.RUnlock()

	if !ok || !config.Enabled {
		return nil, false
	}

	// Check date range
	now := time.Now()
	if !config.StartDate.IsZero() && now.Before(config.StartDate) {
		return nil, false
	}
	if !config.EndDate.IsZero() && now.After(config.EndDate) {
		return nil, false
	}

	// RFC 3834 §2: never answer a null return path, and do not answer
	// mailer daemons or list owner/request addresses (F5206).
	addr := normalizeAddress(sender)
	if isAutomatedSender(addr) {
		return nil, false
	}

	// Check exclude addresses (case-insensitive, F5207)
	for _, excluded := range config.ExcludeAddresses {
		if normalizeAddress(excluded) == addr {
			return nil, false
		}
	}

	// Header values such as Precedence are case-insensitive (F5206).
	precedence := strings.ToLower(strings.TrimSpace(headers["Precedence"]))

	// Check headers for mailing list
	if config.IgnoreLists {
		if headers["List-Id"] != "" || headers["List-Unsubscribe"] != "" {
			return nil, false
		}
		if precedence == "list" || precedence == "bulk" {
			return nil, false
		}
	}

	// Check headers for bulk mail
	if config.IgnoreBulk {
		if precedence == "bulk" || precedence == "junk" {
			return nil, false
		}
		if headers["X-Mailer"] == "MassMailer" {
			return nil, false
		}
	}

	// Don't reply to auto-generated messages
	if autoSubmitted := strings.TrimSpace(headers["Auto-Submitted"]); autoSubmitted != "" && !strings.EqualFold(autoSubmitted, "no") {
		return nil, false
	}
	if headers["X-Auto-Response-Suppress"] != "" {
		return nil, false
	}

	// Don't reply if this address already appears in the mail loop chain
	if loopHeader := headers["X-Mail-Loop"]; loopHeader != "" {
		for _, loopAddr := range strings.Split(loopHeader, ",") {
			if strings.EqualFold(strings.TrimSpace(loopAddr), user) {
				return nil, false
			}
		}
	}

	return config, true
}

// normalizeAddress returns sender in the form used for comparisons: without
// surrounding whitespace or angle brackets, lower-cased. Mail systems treat
// addresses case-insensitively in practice, so "Bob@B.com" and "bob@b.com"
// are one correspondent (F5207).
func normalizeAddress(sender string) string {
	addr := strings.TrimSpace(sender)
	if strings.HasPrefix(addr, "<") && strings.HasSuffix(addr, ">") {
		addr = strings.TrimSpace(addr[1 : len(addr)-1])
	}
	return strings.ToLower(addr)
}

// isAutomatedSender reports whether a normalized address is one RFC 3834 §2
// says must (null path) or should not receive an automatic response.
func isAutomatedSender(addr string) bool {
	if addr == "" {
		return true
	}
	local := addr
	if at := strings.LastIndex(addr, "@"); at >= 0 {
		local = addr[:at]
	}
	return local == "mailer-daemon" ||
		strings.HasPrefix(local, "owner-") ||
		strings.HasSuffix(local, "-request")
}

// GetAutoReplyMessage gets the auto-reply message for a user
func (m *Manager) GetAutoReplyMessage(user string) (subject, textBody, htmlBody string) {
	m.mu.RLock()
	config, ok := m.configs[user]
	m.mu.RUnlock()

	if !ok || !config.Enabled {
		return "", "", ""
	}

	return config.Subject, config.Message, config.HTMLMessage
}

// loadConfigs loads all vacation configs from disk
func (m *Manager) loadConfigs() error {
	if err := os.MkdirAll(m.dataDir, 0o750); err != nil {
		return err
	}

	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filename := strings.TrimSuffix(entry.Name(), ".json")
		email := unsanitizeFilename(filename)
		config, err := m.loadConfigFile(filepath.Join(m.dataDir, entry.Name()))
		if err != nil {
			m.logger.Warn("Failed to load vacation config",
				"email", email,
				"error", err,
			)
			continue
		}

		m.configs[email] = config
	}

	return nil
}

// loadConfigFile loads a single config file
func (m *Manager) loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// saveConfig saves a config to disk
func (m *Manager) saveConfig(email string, config *Config) error {
	if err := os.MkdirAll(m.dataDir, 0o750); err != nil {
		return err
	}

	path := filepath.Join(m.dataDir, sanitizeFilename(email)+".json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// sanitizeFilename sanitizes an email address for use as filename using base64
func sanitizeFilename(email string) string {
	// Use URL-safe base64 encoding for unambiguous filename encoding
	encoded := base64.URLEncoding.EncodeToString([]byte(email))
	// Remove padding for cleaner filenames
	return strings.TrimRight(encoded, "=")
}

// unsanitizeFilename reverses filename sanitization to get the original email
func unsanitizeFilename(filename string) string {
	// If filename contains _at_ (old format marker), use legacy unsanitize
	// This handles existing files created before the base64 migration
	if strings.Contains(filename, "_at_") {
		result := strings.ReplaceAll(filename, "_at_", "@")
		result = strings.ReplaceAll(result, "_", ".")
		return result
	}

	// Otherwise, try base64 decoding
	padding := 4 - len(filename)%4
	if padding < 4 {
		filename += strings.Repeat("=", padding)
	}
	decoded, err := base64.URLEncoding.DecodeString(filename)
	if err != nil || len(decoded) == 0 {
		// Invalid base64, return as-is
		return filename
	}
	return string(decoded)
}

// DeleteConfig deletes vacation config for a user
func (m *Manager) DeleteConfig(email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.configs, email)
	m.forgetReplies(email)

	path := filepath.Join(m.dataDir, sanitizeFilename(email)+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// ListActiveVacations returns all users with active vacation
func (m *Manager) ListActiveVacations() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []string
	now := time.Now()

	for email, config := range m.configs {
		if !config.Enabled {
			continue
		}
		if !config.StartDate.IsZero() && now.Before(config.StartDate) {
			continue
		}
		if !config.EndDate.IsZero() && now.After(config.EndDate) {
			continue
		}
		result = append(result, email)
	}

	return result
}
