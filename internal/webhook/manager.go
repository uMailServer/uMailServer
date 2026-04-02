package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// Manager handles webhook delivery
type Manager struct {
	db     *db.DB
	client *http.Client
	secret string
	hooks  []*Webhook
	mu     sync.RWMutex
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
		db:     database,
		client: &http.Client{Timeout: 30 * time.Second},
		secret: secret,
		hooks:  make([]*Webhook, 0),
	}
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

// send delivers webhook
func (m *Manager) send(hook *Webhook, event Event) {
	defer func() {
		if r := recover(); r != nil {
			// Log via fmt since logger may not be available
			fmt.Printf("webhook: panic in send: %v\n", r)
		}
	}()

	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	req, err := http.NewRequest("POST", hook.URL, bytes.NewReader(payload))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "uMailServer-Webhook/1.0")
	req.Header.Set("X-Webhook-ID", hook.ID)
	req.Header.Set("X-Webhook-Event", event.Type)
	req.Header.Set("X-Webhook-Timestamp", fmt.Sprintf("%d", event.Timestamp.Unix()))

	// Sign payload if secret configured
	if m.secret != "" {
		sig := m.sign(payload)
		req.Header.Set("X-Webhook-Signature", sig)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
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
