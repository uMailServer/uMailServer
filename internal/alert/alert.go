// Package alert provides production alerting capabilities for uMailServer.
// It supports webhook and email notifications for various system conditions.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SecureString is a string that masks its value when serialized to JSON
// to prevent accidental credential exposure in logs or API responses.
type SecureString string

// MarshalJSON returns a masked representation of the string
func (s SecureString) MarshalJSON() ([]byte, error) {
	if s == "" {
		return []byte(`""`), nil
	}
	return []byte(`"[REDACTED]"`), nil
}

// UnmarshalJSON allows direct unmarshaling (the actual value is stored)
func (s *SecureString) UnmarshalJSON(data []byte) error {
	if string(data) == `"[REDACTED]"` {
		*s = ""
		return nil
	}
	// Remove quotes and set the value
	*s = SecureString(strings.Trim(string(data), `"`))
	return nil
}

// String returns the actual string value (use with caution in logging)
func (s SecureString) String() string {
	return string(s)
}

// Severity represents the severity level of an alert
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Alert represents a single alert notification
type Alert struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Severity  Severity               `json:"severity"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// Config holds alert manager configuration
type Config struct {
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Webhook settings
	WebhookURL      string            `yaml:"webhook_url,omitempty" json:"webhook_url,omitempty"`
	WebhookHeaders  map[string]string `yaml:"webhook_headers,omitempty" json:"webhook_headers,omitempty"`
	WebhookTemplate string            `yaml:"webhook_template,omitempty" json:"webhook_template,omitempty"`

	// Email settings
	SMTPServer   string       `yaml:"smtp_server,omitempty" json:"smtp_server,omitempty"`
	SMTPPort     int          `yaml:"smtp_port,omitempty" json:"smtp_port,omitempty"`
	SMTPUsername string       `yaml:"smtp_username,omitempty" json:"smtp_username,omitempty"`
	SMTPPassword SecureString `yaml:"smtp_password,omitempty" json:"smtp_password,omitempty"`
	FromAddress  string       `yaml:"from_address,omitempty" json:"from_address,omitempty"`
	ToAddresses  []string     `yaml:"to_addresses,omitempty" json:"to_addresses,omitempty"`
	UseTLS       bool         `yaml:"use_tls,omitempty" json:"use_tls,omitempty"`

	// Rate limiting
	MinInterval time.Duration `yaml:"min_interval" json:"min_interval"` // Min time between same alerts
	MaxAlerts   int           `yaml:"max_alerts" json:"max_alerts"`     // Max alerts per hour

	// Thresholds
	DiskThreshold   float64 `yaml:"disk_threshold" json:"disk_threshold"`     // Disk usage % (e.g., 85)
	MemoryThreshold float64 `yaml:"memory_threshold" json:"memory_threshold"` // Memory usage % (e.g., 90)
	ErrorThreshold  float64 `yaml:"error_threshold" json:"error_threshold"`   // Error rate % (e.g., 5)
	TLSWarningDays  int     `yaml:"tls_warning_days" json:"tls_warning_days"` // Days before expiry
	QueueThreshold  int     `yaml:"queue_threshold" json:"queue_threshold"`   // Queue backlog count
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		MinInterval:     5 * time.Minute,
		MaxAlerts:       100,
		DiskThreshold:   85.0,
		MemoryThreshold: 90.0,
		ErrorThreshold:  5.0,
		TLSWarningDays:  7,
		QueueThreshold:  1000,
	}
}

// Manager handles alert generation and delivery
type Manager struct {
	config Config
	logger Logger

	// Rate limiting
	lastAlert   map[string]time.Time // alert name -> last sent time
	hourlyCount int
	hourStart   time.Time
	mu          sync.RWMutex

	// HTTP client for webhooks
	httpClient *http.Client

	// Security: allowPrivateIP permits localhost/private IPs (for testing only)
	allowPrivateIP bool
}

// Logger interface for alert manager
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// noopLogger is a no-op logger implementation
type noopLogger struct{}

// Info is a no-op
func (n *noopLogger) Info(msg string, args ...interface{}) { _ = msg }

// Warn is a no-op
func (n *noopLogger) Warn(msg string, args ...interface{}) { _ = msg }

// Error is a no-op
func (n *noopLogger) Error(msg string, args ...interface{}) { _ = msg }

// NewManager creates a new alert manager
func NewManager(config Config, logger Logger) *Manager {
	if logger == nil {
		logger = &noopLogger{}
	}

	return &Manager{
		config:         config,
		logger:         logger,
		lastAlert:      make(map[string]time.Time),
		hourStart:      time.Now(),
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		allowPrivateIP: false, // Default: block private IPs for security
	}
}

// SetAllowPrivateIP allows private IP addresses for webhooks (use with caution, mainly for testing)
func (m *Manager) SetAllowPrivateIP(allow bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowPrivateIP = allow
}

// IsEnabled returns true if alerting is enabled
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

// Send creates and sends an alert
func (m *Manager) Send(name string, severity Severity, message string, details map[string]interface{}) error {
	if !m.config.Enabled {
		return nil
	}

	// Check rate limiting
	if !m.shouldSend(name) {
		return nil
	}

	alert := Alert{
		ID:        generateAlertID(),
		Name:      name,
		Severity:  severity,
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
	}

	var errs []error

	// Send via webhook if configured
	if m.config.WebhookURL != "" {
		if err := m.sendWebhook(alert); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}

	// Send via email if configured
	if m.config.SMTPServer != "" && len(m.config.ToAddresses) > 0 {
		if err := m.sendEmail(alert); err != nil {
			errs = append(errs, fmt.Errorf("email: %w", err))
		}
	}

	m.recordAlert(name)

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// shouldSend checks rate limiting before sending
func (m *Manager) shouldSend(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset hourly count if hour has passed
	if time.Since(m.hourStart) > time.Hour {
		m.hourlyCount = 0
		m.hourStart = time.Now()
	}

	// Check max alerts per hour
	if m.hourlyCount >= m.config.MaxAlerts {
		return false
	}

	// Check min interval for same alert type
	if lastSent, ok := m.lastAlert[name]; ok {
		if time.Since(lastSent) < m.config.MinInterval {
			return false
		}
	}

	return true
}

// recordAlert records that an alert was sent
func (m *Manager) recordAlert(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastAlert[name] = time.Now()
	m.hourlyCount++
}

// sendWebhook sends alert via HTTP webhook
func (m *Manager) sendWebhook(alert Alert) error {
	// Validate webhook URL to prevent SSRF attacks
	if !m.isValidWebhookURL(m.config.WebhookURL) {
		return fmt.Errorf("webhook URL is not allowed: %s", m.config.WebhookURL)
	}

	payload, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		"POST",
		m.config.WebhookURL,
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range m.config.WebhookHeaders {
		req.Header.Set(key, value)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain and discard response body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// isValidWebhookURL checks if the URL is safe (not localhost or private IP) to prevent SSRF attacks
func (m *Manager) isValidWebhookURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Only allow http and https schemes
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Get the hostname
	hostname := u.Hostname()
	if hostname == "" {
		return false
	}

	// If private IPs are allowed (testing mode), skip checks
	m.mu.RLock()
	allowPrivate := m.allowPrivateIP
	m.mu.RUnlock()
	if allowPrivate {
		return true
	}

	// Block localhost variants
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return false
	}

	// Block private IP ranges
	ip := net.ParseIP(hostname)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
	}

	return true
}

// sendEmail sends alert via SMTP
func (m *Manager) sendEmail(alert Alert) error {
	// Build email body
	var body strings.Builder
	body.WriteString(fmt.Sprintf("Subject: [%s] uMailServer Alert: %s\r\n", strings.ToUpper(string(alert.Severity)), alert.Name))
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	body.WriteString("\r\n")
	body.WriteString(fmt.Sprintf("Alert: %s\r\n", alert.Name))
	body.WriteString(fmt.Sprintf("Severity: %s\r\n", alert.Severity))
	body.WriteString(fmt.Sprintf("Time: %s\r\n", alert.Timestamp.Format(time.RFC3339)))
	body.WriteString(fmt.Sprintf("Message: %s\r\n", alert.Message))

	if len(alert.Details) > 0 {
		body.WriteString("\r\nDetails:\r\n")
		for key, value := range alert.Details {
			body.WriteString(fmt.Sprintf("  %s: %v\r\n", key, value))
		}
	}

	addr := fmt.Sprintf("%s:%d", m.config.SMTPServer, m.config.SMTPPort)

	var auth smtp.Auth
	if m.config.SMTPUsername != "" {
		auth = smtp.PlainAuth("", m.config.SMTPUsername, string(m.config.SMTPPassword), m.config.SMTPServer)
	}

	err := smtp.SendMail(addr, auth, m.config.FromAddress, m.config.ToAddresses, []byte(body.String()))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// CheckDiskSpace checks disk usage and alerts if threshold exceeded
func (m *Manager) CheckDiskSpace(usagePercent float64, path string) {
	if usagePercent >= m.config.DiskThreshold {
		_ = m.Send(
			"disk_space_critical",
			SeverityCritical,
			fmt.Sprintf("Disk usage is %.1f%% on %s", usagePercent, path),
			map[string]interface{}{
				"usage_percent": usagePercent,
				"threshold":     m.config.DiskThreshold,
				"path":          path,
			},
		)
	} else if usagePercent >= m.config.DiskThreshold-10 {
		// Warning at 10% below threshold
		_ = m.Send(
			"disk_space_warning",
			SeverityWarning,
			fmt.Sprintf("Disk usage is %.1f%% on %s", usagePercent, path),
			map[string]interface{}{
				"usage_percent": usagePercent,
				"threshold":     m.config.DiskThreshold,
				"path":          path,
			},
		)
	}
}

// CheckMemory checks memory usage and alerts if threshold exceeded
func (m *Manager) CheckMemory(usagePercent float64, usedMB, totalMB uint64) {
	if usagePercent >= m.config.MemoryThreshold {
		_ = m.Send(
			"memory_critical",
			SeverityCritical,
			fmt.Sprintf("Memory usage is %.1f%% (%dMB / %dMB)", usagePercent, usedMB, totalMB),
			map[string]interface{}{
				"usage_percent": usagePercent,
				"threshold":     m.config.MemoryThreshold,
				"used_mb":       usedMB,
				"total_mb":      totalMB,
			},
		)
	}
}

// CheckErrorRate checks error rate and alerts if threshold exceeded
func (m *Manager) CheckErrorRate(errorRate float64, window string) {
	if errorRate >= m.config.ErrorThreshold {
		_ = m.Send(
			"high_error_rate",
			SeverityWarning,
			fmt.Sprintf("Error rate is %.2f%% over %s", errorRate, window),
			map[string]interface{}{
				"error_rate": errorRate,
				"threshold":  m.config.ErrorThreshold,
				"window":     window,
			},
		)
	}
}

// CheckTLSCertificate checks TLS certificate expiry and alerts if near expiry
func (m *Manager) CheckTLSCertificate(domain string, daysUntilExpiry int) {
	if daysUntilExpiry <= 0 {
		_ = m.Send(
			"tls_certificate_expired",
			SeverityCritical,
			fmt.Sprintf("TLS certificate for %s has EXPIRED", domain),
			map[string]interface{}{
				"domain":            domain,
				"days_until_expiry": daysUntilExpiry,
			},
		)
	} else if daysUntilExpiry <= m.config.TLSWarningDays {
		_ = m.Send(
			"tls_certificate_expiring",
			SeverityWarning,
			fmt.Sprintf("TLS certificate for %s expires in %d days", domain, daysUntilExpiry),
			map[string]interface{}{
				"domain":            domain,
				"days_until_expiry": daysUntilExpiry,
				"warning_days":      m.config.TLSWarningDays,
			},
		)
	}
}

// CheckQueueBacklog checks queue size and alerts if threshold exceeded
func (m *Manager) CheckQueueBacklog(queueSize int) {
	if queueSize >= m.config.QueueThreshold {
		_ = m.Send(
			"queue_backlog",
			SeverityWarning,
			fmt.Sprintf("Queue backlog is %d messages", queueSize),
			map[string]interface{}{
				"queue_size": queueSize,
				"threshold":  m.config.QueueThreshold,
			},
		)
	}
}

// Info sends an informational alert
func (m *Manager) Info(name string, message string, details map[string]interface{}) error {
	return m.Send(name, SeverityInfo, message, details)
}

// Warn sends a warning alert
func (m *Manager) Warn(name string, message string, details map[string]interface{}) error {
	return m.Send(name, SeverityWarning, message, details)
}

// Critical sends a critical alert
func (m *Manager) Critical(name string, message string, details map[string]interface{}) error {
	return m.Send(name, SeverityCritical, message, details)
}

// GetStats returns current alert statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Reset hourly count if needed
	hourlyCount := m.hourlyCount
	if time.Since(m.hourStart) > time.Hour {
		hourlyCount = 0
	}

	return map[string]interface{}{
		"enabled":       m.config.Enabled,
		"hourly_count":  hourlyCount,
		"max_alerts":    m.config.MaxAlerts,
		"min_interval":  m.config.MinInterval.String(),
		"recent_alerts": len(m.lastAlert),
	}
}

// generateAlertID generates a unique alert ID
func generateAlertID() string {
	return fmt.Sprintf("%d-%d", time.Now().Unix(), time.Now().Nanosecond())
}
