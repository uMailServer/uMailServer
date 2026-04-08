// Package vacation provides vacation auto-reply functionality
package vacation

import (
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
	sentCache map[string]map[string]time.Time // user -> sender -> last sent time
	cacheMu   sync.RWMutex
}

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

	m.configs[email] = config

	// Save to disk
	if err := m.saveConfig(email, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	m.logger.Info("Vacation config updated",
		"email", email,
		"enabled", config.Enabled,
	)

	return nil
}

// ShouldSendAutoReply checks if auto-reply should be sent
func (m *Manager) ShouldSendAutoReply(user, sender string, headers map[string]string) bool {
	m.mu.RLock()
	config, ok := m.configs[user]
	m.mu.RUnlock()

	if !ok || !config.Enabled {
		return false
	}

	// Check date range
	now := time.Now()
	if !config.StartDate.IsZero() && now.Before(config.StartDate) {
		return false
	}
	if !config.EndDate.IsZero() && now.After(config.EndDate) {
		return false
	}

	// Check exclude addresses
	for _, addr := range config.ExcludeAddresses {
		if addr == sender {
			return false
		}
	}

	// Check headers for mailing list
	if config.IgnoreLists {
		if headers["List-Id"] != "" || headers["List-Unsubscribe"] != "" {
			return false
		}
		if headers["Precedence"] == "list" || headers["Precedence"] == "bulk" {
			return false
		}
	}

	// Check headers for bulk mail
	if config.IgnoreBulk {
		if headers["Precedence"] == "bulk" || headers["Precedence"] == "junk" {
			return false
		}
		if headers["X-Mailer"] == "MassMailer" {
			return false
		}
	}

	// Don't reply to auto-generated messages
	if headers["Auto-Submitted"] != "" && headers["Auto-Submitted"] != "no" {
		return false
	}
	if headers["X-Auto-Response-Suppress"] != "" {
		return false
	}

	// Check send interval (don't spam the same sender)
	m.cacheMu.RLock()
	userCache, ok := m.sentCache[user]
	if !ok {
		m.cacheMu.RUnlock()
		return true
	}
	lastSent, ok := userCache[sender]
	m.cacheMu.RUnlock()

	if ok && time.Since(lastSent) < config.SendInterval {
		return false
	}

	return true
}

// RecordAutoReplySent records that an auto-reply was sent
func (m *Manager) RecordAutoReplySent(user, sender string) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	if m.sentCache[user] == nil {
		m.sentCache[user] = make(map[string]time.Time)
	}
	m.sentCache[user][sender] = time.Now()
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
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
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
	data, err := os.ReadFile(path)
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
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(m.dataDir, sanitizeFilename(email)+".json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// sanitizeFilename sanitizes an email address for use as filename
func sanitizeFilename(email string) string {
	// Replace characters that might be problematic in filenames
	result := strings.ReplaceAll(email, "@", "_at_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ReplaceAll(result, "/", "_")
	result = strings.ReplaceAll(result, "\\", "_")
	return result
}

// unsanitizeFilename reverses filename sanitization to get the original email
func unsanitizeFilename(filename string) string {
	// Replace _at_ with @ first
	result := strings.ReplaceAll(filename, "_at_", "@")
	// Replace remaining _ with . (this is a best effort)
	result = strings.ReplaceAll(result, "_", ".")
	return result
}

// DeleteConfig deletes vacation config for a user
func (m *Manager) DeleteConfig(email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.configs, email)

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
