package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/circuitbreaker"
	"github.com/umailserver/umailserver/internal/db"
)

// Manager handles webhook delivery
type Manager struct {
	db             *db.DB
	client         *http.Client
	secret         string
	hooks          []*Webhook
	allowPrivateIP bool // For testing; if true, allows localhost/private IPs
	cbManager      *circuitbreaker.Manager
	mu             sync.RWMutex
}

// Webhook represents a webhook configuration
type Webhook struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// Event represents a webhook event
type Event struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Event types
const (
	EventMailReceived    = "mail.received"
	EventMailSent        = "mail.sent"
	EventDeliveryFailed  = "delivery.failed"
	EventDeliverySuccess = "delivery.success"
	EventSpamDetected    = "spam.detected"
	EventLoginSuccess    = "auth.login.success"
	EventLoginFailed     = "auth.login.failed"
)

// NewManager creates webhook manager
func NewManager(database *db.DB, secret string) *Manager {
	return &Manager{
		db:             database,
		client:         &http.Client{Timeout: 30 * time.Second},
		secret:         secret,
		hooks:          make([]*Webhook, 0),
		allowPrivateIP: false, // Default: block private IPs for security
		cbManager:      circuitbreaker.NewManager(),
	}
}

// SetAllowPrivateIP allows private IP addresses for webhooks (use with caution, mainly for testing)
func (m *Manager) SetAllowPrivateIP(allow bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowPrivateIP = allow
}

// Trigger sends event to matching webhooks
func (m *Manager) Trigger(eventType string, data interface{}) {
	event := Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	m.mu.RLock()
	hooks := make([]*Webhook, len(m.hooks))
	copy(hooks, m.hooks)
	m.mu.RUnlock()

	// Send asynchronously
	for _, hook := range hooks {
		if !hook.Active {
			continue
		}
		if !m.eventMatches(hook.Events, eventType) {
			continue
		}
		go m.send(hook, event)
	}
}

// send delivers webhook with retry logic and circuit breaker
func (m *Manager) send(hook *Webhook, event Event) {
	defer func() {
		if r := recover(); r != nil {
			// Log via fmt since logger may not be available
			fmt.Printf("webhook: panic in send: %v\n", r)
		}
	}()

	// Validate URL to prevent SSRF
	if !m.isValidWebhookURL(hook.URL) {
		fmt.Printf("webhook: invalid URL (SSRF protection): %s\n", hook.URL)
		return
	}

	// Get circuit breaker for this webhook URL
	cb := m.cbManager.Get(hook.URL)

	// Check if circuit allows the request
	if !cb.Allow() {
		fmt.Printf("webhook: circuit breaker open for %s\n", hook.URL)
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		cb.RecordFailure()
		return
	}

	// Retry logic: 3 attempts with exponential backoff
	var lastErr error
	success := false

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		req, err := http.NewRequest("POST", hook.URL, bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "uMailServer-Webhook/1.0")
		req.Header.Set("X-Webhook-ID", hook.ID)
		req.Header.Set("X-Webhook-Event", event.Type)
		req.Header.Set("X-Webhook-Timestamp", fmt.Sprintf("%d", event.Timestamp.Unix()))
		req.Header.Set("X-Webhook-Attempt", fmt.Sprintf("%d", attempt+1))

		// Sign payload if secret configured
		if m.secret != "" {
			sig := m.sign(payload)
			req.Header.Set("X-Webhook-Signature", sig)
		}

		resp, err := m.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Success: 2xx status code
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			success = true
			break
		}

		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)

		// Don't retry on 4xx errors (client errors)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			break
		}
	}

	// Record success or failure for circuit breaker
	if success {
		cb.RecordSuccess()
	} else {
		cb.RecordFailure()
		fmt.Printf("webhook: delivery failed after 3 attempts: %s, error: %v\n", hook.URL, lastErr)
	}
}

// isValidWebhookURL checks if the URL is safe (not localhost or private IP)
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
	if m.allowPrivateIP {
		return true
	}

	// Block localhost variants
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return false
	}

	// Block private IP ranges
	ip := net.ParseIP(hostname)
	if ip != nil {
		// Check for private IP ranges
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
	}

	return true
}

// sign creates HMAC signature
func (m *Manager) sign(payload []byte) string {
	h := hmac.New(sha256.New, []byte(m.secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// eventMatches checks if event type matches patterns
func (m *Manager) eventMatches(patterns []string, eventType string) bool {
	for _, pattern := range patterns {
		if pattern == eventType {
			return true
		}
		if pattern == "*" {
			return true
		}
		// Support wildcards like "mail.*"
		if len(pattern) > 2 && pattern[len(pattern)-1] == '*' {
			prefix := pattern[:len(pattern)-1]
			if len(eventType) >= len(prefix) && eventType[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// HTTPHandler handles webhook CRUD
func (m *Manager) HTTPHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.handleList(w, r)
	case http.MethodPost:
		m.handleCreate(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (m *Manager) handleList(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	hooks := make([]*Webhook, len(m.hooks))
	copy(hooks, m.hooks)
	m.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"webhooks": hooks})
}

func (m *Manager) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	hook := &Webhook{
		ID:        fmt.Sprintf("wh_%d", time.Now().Unix()),
		URL:       req.URL,
		Events:    req.Events,
		Active:    true,
		CreatedAt: time.Now(),
	}

	m.mu.Lock()
	m.hooks = append(m.hooks, hook)
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(hook)
}

// GetCircuitBreakerMetrics returns circuit breaker metrics for all webhook URLs
func (m *Manager) GetCircuitBreakerMetrics() map[string]circuitbreaker.Metrics {
	return m.cbManager.AllMetrics()
}
