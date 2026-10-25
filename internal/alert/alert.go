// Package alert provides production alerting capabilities for uMailServer.
// It supports webhook and email notifications for various system conditions.
package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == "[REDACTED]" {
		value = ""
	}
	*s = SecureString(value)
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

	// statePath, when set, persists lastAlert so cooldowns survive restarts.
	statePath string

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

	// A zero/negative MaxAlerts means "use the default" rather than "drop
	// every alert" (F7203).
	if config.MaxAlerts <= 0 {
		config.MaxAlerts = DefaultConfig().MaxAlerts
	}
	m := &Manager{
		config:         config,
		logger:         logger,
		lastAlert:      make(map[string]time.Time),
		hourStart:      time.Now(),
		allowPrivateIP: false, // Default: block private IPs for security
	}
	// Every redirect hop is re-checked with the SSRF guard (F6295): the guard
	// otherwise only saw the configured URL, so a public endpoint could
	// redirect the alert payload to an internal host.
	// The dialer re-checks the address actually connected to (F6500), so a
	// hostname resolving to a blocked range (or rebinding between validation
	// and connect) cannot reach internal services. No proxy: it would hide
	// the real target.
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: m.dialControl}
	m.httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			IdleConnTimeout:     90 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if !m.isValidWebhookURL(req.URL.String()) {
				return fmt.Errorf("redirect target rejected by SSRF check: %s", req.URL.Redacted())
			}
			return nil
		},
	}
	return m
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
	return m.sendKeyed(name, name, severity, message, details)
}

// sendKeyed is Send with an explicit deduplication key. Alerts that share a
// name but concern different subjects (e.g. two certificates) must use
// distinct keys, or the first one suppresses the rest (F5178).
func (m *Manager) sendKeyed(key, name string, severity Severity, message string, details map[string]interface{}) error {
	if !m.config.Enabled {
		return nil
	}

	// Check rate limiting
	prev, hadPrev, at, reserved := m.reserveAt(key)
	if !reserved {
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

	if len(errs) > 0 {
		// F7150: nothing was delivered when every configured channel failed,
		// so release the cooldown instead of silencing the alert for
		// MinInterval. The hourly count stays consumed to bound retries.
		attempted := 0
		if m.config.WebhookURL != "" {
			attempted++
		}
		if m.config.SMTPServer != "" && len(m.config.ToAddresses) > 0 {
			attempted++
		}
		if len(errs) == attempted {
			m.release(key, prev, hadPrev, at)
		}
		return errs[0]
	}
	return nil
}

// release undoes the cooldown reservation made by reserve at time at. It is a
// no-op once a newer reservation replaced it (F7622): a slow failed delivery
// that outlived MinInterval must not erase the cooldown of a later send of the
// same alert that succeeded.
func (m *Manager) release(key string, prev time.Time, hadPrev bool, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.lastAlert[key]; !ok || !cur.Equal(at) {
		return
	}
	if hadPrev {
		m.lastAlert[key] = prev
	} else {
		delete(m.lastAlert, key)
	}
	m.saveStateLocked()
}

// reserveAt reserves the next alert slot for name, enforcing the MinInterval
// cooldown per alert name. It records the reservation and returns the previous
// send time, whether one existed, the new send time, and whether the slot was
// granted. Send routes alert deliveries through this gate.
func (m *Manager) reserveAt(name string) (prev time.Time, hadPrev bool, at time.Time, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// F6502: prune expired cooldown entries so the map stays bounded, and
	// roll the hourly budget window (MaxAlerts) exactly as the pre-migration
	// shouldSend gate did.
	now := time.Now()
	if now.Sub(m.hourStart) >= time.Hour {
		m.hourStart = now
		m.hourlyCount = 0
	}
	for k, v := range m.lastAlert {
		if now.Sub(v) >= m.config.MinInterval {
			delete(m.lastAlert, k)
		}
	}
	if m.config.MaxAlerts > 0 && m.hourlyCount >= m.config.MaxAlerts {
		return time.Time{}, false, time.Time{}, false
	}
	if lastSent, exists := m.lastAlert[name]; exists {
		if time.Since(lastSent) < m.config.MinInterval {
			return time.Time{}, false, time.Time{}, false
		}
	}

	prev, hadPrev = m.lastAlert[name]
	at = time.Now()
	m.lastAlert[name] = at
	m.hourlyCount++
	m.saveStateLocked()
	return prev, hadPrev, at, true
}

// SetStateFile enables persistence of alert cooldown times at path. Existing
// state is loaded so a restart does not re-send alerts still in cooldown. A
// missing or corrupt file is treated as empty state. Call before first use.
func (m *Manager) SetStateFile(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statePath = path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var saved map[string]time.Time
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("alert state %s: %w", path, err)
	}
	now := time.Now()
	for k, t := range saved {
		// Ignore expired and future (clock-skewed) timestamps.
		if t.After(now) || now.Sub(t) >= m.config.MinInterval {
			continue
		}
		if cur, ok := m.lastAlert[k]; !ok || t.After(cur) {
			m.lastAlert[k] = t
		}
	}
	return nil
}

// saveStateLocked writes unexpired cooldowns atomically. Caller holds m.mu.
// Failures are logged, never fatal: alerting must not depend on disk state.
func (m *Manager) saveStateLocked() {
	if m.statePath == "" {
		return
	}
	live := make(map[string]time.Time, len(m.lastAlert))
	for k, t := range m.lastAlert {
		if time.Since(t) < m.config.MinInterval {
			live[k] = t
		}
	}
	data, err := json.Marshal(live)
	if err == nil {
		tmp := m.statePath + ".tmp"
		if err = os.WriteFile(tmp, data, 0o600); err == nil {
			if err = os.Rename(tmp, m.statePath); err != nil {
				_ = os.Remove(tmp)
			}
		}
	}
	if err != nil {
		m.logger.Warn("failed to persist alert state", "path", filepath.Base(m.statePath), "error", err)
	}
}

// sendWebhook sends alert via HTTP webhook
func (m *Manager) sendWebhook(alert Alert) error {
	// Validate webhook URL to prevent SSRF attacks
	if !m.isValidWebhookURL(m.config.WebhookURL) {
		return fmt.Errorf("webhook URL is not allowed")
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
		// url.Error embeds the full URL, which may carry credentials/tokens (F6296).
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
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

// blockedNets lists non-routable ranges net.IP's helpers do not cover,
// including the deprecated IPv4-compatible IPv6 range ::/96.
var blockedNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "240.0.0.0/4", "::/96", "64:ff9b::/96", "2002::/16", "fec0::/10", "100::/64"} {
		_, n, _ := net.ParseCIDR(c)
		nets = append(nets, n)
	}
	return nets
}()

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// dialControl runs after DNS resolution with the concrete address (F6500).
func (m *Manager) dialControl(_, address string, _ syscall.RawConn) error {
	m.mu.RLock()
	allowPrivate := m.allowPrivateIP
	m.mu.RUnlock()
	if allowPrivate {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("alert: bad dial address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || isBlockedIP(ip) {
		return fmt.Errorf("alert: dial to %s rejected by SSRF check", address)
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
		// Unspecified (0.0.0.0, ::) reaches the local host (F6297).
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
	}

	return true
}

// headerSafe strips CR/LF so an alert name can never inject mail headers (F6872).
func headerSafe(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}

// sendEmail sends alert via SMTP
func (m *Manager) sendEmail(alert Alert) error {
	// Build email body
	// RFC 5322 section 3.6 requires Date and From (F5179).
	var body strings.Builder
	fmt.Fprintf(&body, "Date: %s\r\n", alert.Timestamp.Format(time.RFC1123Z))
	fmt.Fprintf(&body, "From: %s\r\n", m.config.FromAddress)
	fmt.Fprintf(&body, "To: %s\r\n", strings.Join(m.config.ToAddresses, ", "))
	fmt.Fprintf(&body, "Subject: [%s] uMailServer Alert: %s\r\n", strings.ToUpper(string(alert.Severity)), headerSafe(alert.Name))
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	body.WriteString("\r\n")
	fmt.Fprintf(&body, "Alert: %s\r\n", alert.Name)
	fmt.Fprintf(&body, "Severity: %s\r\n", alert.Severity)
	fmt.Fprintf(&body, "Time: %s\r\n", alert.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(&body, "Message: %s\r\n", alert.Message)

	if len(alert.Details) > 0 {
		body.WriteString("\r\nDetails:\r\n")
		for key, value := range alert.Details {
			fmt.Fprintf(&body, "  %s: %v\r\n", key, value)
		}
	}

	addr := fmt.Sprintf("%s:%d", m.config.SMTPServer, m.config.SMTPPort)

	var auth smtp.Auth
	if m.config.SMTPUsername != "" {
		auth = smtp.PlainAuth("", m.config.SMTPUsername, string(m.config.SMTPPassword), m.config.SMTPServer)
	}

	err := sendMailTimeout(addr, m.config.SMTPServer, auth, m.config.FromAddress, m.config.ToAddresses, []byte(body.String()))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// CheckDiskSpace checks disk usage and alerts if threshold exceeded
func (m *Manager) CheckDiskSpace(usagePercent float64, path string) {
	if usagePercent >= m.config.DiskThreshold {
		_ = m.sendKeyed(
			"disk_space_critical:"+path,
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
		_ = m.sendKeyed(
			"disk_space_warning:"+path,
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
		_ = m.sendKeyed(
			"tls_certificate_expired:"+domain,
			"tls_certificate_expired",
			SeverityCritical,
			fmt.Sprintf("TLS certificate for %s has EXPIRED", domain),
			map[string]interface{}{
				"domain":            domain,
				"days_until_expiry": daysUntilExpiry,
			},
		)
	} else if daysUntilExpiry <= m.config.TLSWarningDays {
		_ = m.sendKeyed(
			"tls_certificate_expiring:"+domain,
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

var alertSeq atomic.Uint64

// generateAlertID generates a unique alert ID
func generateAlertID() string {
	now := time.Now()
	return fmt.Sprintf("%d-%d-%d", now.Unix(), now.Nanosecond(), alertSeq.Add(1))
}

// smtpTimeout bounds the whole SMTP exchange (F6501); net/smtp.SendMail has
// no timeout, so an unresponsive server hung the alert checker forever.
var smtpTimeout = 30 * time.Second

// sendMailTimeout mirrors smtp.SendMail (opportunistic STARTTLS) under a
// connection deadline.
func sendMailTimeout(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := (&net.Dialer{Timeout: smtpTimeout}).Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
