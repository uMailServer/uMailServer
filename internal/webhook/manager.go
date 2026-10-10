package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/umailserver/umailserver/internal/circuitbreaker"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/tracing"
)

// Manager handles webhook delivery
type Manager struct {
	db              *db.DB
	client          *http.Client
	secret          string
	hooks           []*Webhook
	allowPrivateIP  bool // For testing; if true, allows localhost/private IPs
	cbManager       *circuitbreaker.Manager
	mu              sync.RWMutex
	sem             chan struct{} // Semaphore to limit concurrent webhook deliveries
	tracingProvider *tracing.Provider
}

// SetTracingProvider attaches an OpenTelemetry tracing provider so each
// outbound webhook delivery emits a webhook.deliver span. Nil disables tracing.
func (m *Manager) SetTracingProvider(provider *tracing.Provider) {
	m.tracingProvider = provider
}

// redactURL strips credentials, query and fragment from a webhook URL so it
// is safe for logs and trace attributes (F5970): userinfo and tokens in the
// query string are secrets.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<invalid-url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// redactErr removes the raw hook URL (and any credential-bearing form of it)
// from an error message.
func redactErr(err error, rawURL string) error {
	if err == nil {
		return nil
	}
	msg := strings.ReplaceAll(err.Error(), rawURL, redactURL(rawURL))
	if u, perr := url.Parse(rawURL); perr == nil {
		if u.User != nil {
			if pw, ok := u.User.Password(); ok && pw != "" {
				msg = strings.ReplaceAll(msg, pw, "***")
			}
		}
		if u.RawQuery != "" {
			msg = strings.ReplaceAll(msg, u.RawQuery, "REDACTED")
		}
	}
	return fmt.Errorf("%s", msg)
}

// Webhook represents a webhook configuration
type Webhook struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	Events     []string  `json:"events"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	ResolvedIP string    `json:"-"` // Cached resolved IP for DNS rebinding protection
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
	m := &Manager{
		db:             database,
		secret:         secret,
		hooks:          make([]*Webhook, 0),
		allowPrivateIP: false, // Default: block private IPs for security
		cbManager:      circuitbreaker.NewManager(),
		sem:            make(chan struct{}, 50), // Limit concurrent webhook deliveries
	}
	// The SSRF guard is enforced again on the address actually dialed
	// (F5175/F5176): isValidWebhookURL resolves the host once, but the
	// transport resolves it again, so a DNS answer that changes between the
	// two (rebinding) or any other route to a blocked address is refused at
	// connect time. No proxy is used: a proxy would hide the real target.
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: m.dialControl}
	m.client = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		// The SSRF guard in send() only validates the operator-configured URL.
		// A redirect could otherwise bounce the signed payload at an internal
		// host that isValidWebhookURL never saw, so every hop is re-checked
		// with the same guard. Returning nil leaves Go's own 10-redirect cap
		// in force.
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if valid, _ := m.isValidWebhookURL(req.URL.String()); !valid {
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

	// Send asynchronously with semaphore to limit concurrent deliveries
	for _, hook := range hooks {
		if !hook.Active {
			continue
		}
		if !m.eventMatches(hook.Events, eventType) {
			continue
		}
		// Acquire semaphore to limit concurrent webhook deliveries
		select {
		case m.sem <- struct{}{}:
			go func(h *Webhook, e Event) {
				defer func() { <-m.sem }() // Release semaphore when done
				m.send(h, e)
			}(hook, event)
		default:
			// Semaphore full, skip this webhook to prevent resource exhaustion
			fmt.Printf("webhook: delivery skipped for %s (max concurrent deliveries reached)\n", redactURL(hook.URL))
		}
	}
}

// send delivers webhook with retry logic and circuit breaker
func (m *Manager) send(hook *Webhook, event Event) {
	if m.tracingProvider != nil && m.tracingProvider.IsEnabled() {
		_, span := m.tracingProvider.StartSpanWithKind(context.Background(), "webhook.deliver", tracing.SpanKindClient)
		defer span.End()
		tracing.SetStringAttribute(span, "webhook.id", hook.ID)
		tracing.SetStringAttribute(span, "webhook.url", redactURL(hook.URL))
		tracing.SetStringAttribute(span, "webhook.event", event.Type)
		// Defer attribute capture to track final outcome.
		var (
			finalAttempts int
			finalErr      error
			finalSuccess  bool
		)
		defer func() {
			tracing.SetIntAttribute(span, "webhook.attempts", finalAttempts)
			tracing.SetBoolAttribute(span, "webhook.success", finalSuccess)
			if finalErr != nil {
				tracing.RecordError(span, finalErr)
				tracing.SetStatus(span, tracing.StatusError, finalErr.Error())
			} else if finalSuccess {
				tracing.SetStatus(span, tracing.StatusOk, "")
			}
		}()
		m.sendInner(hook, event, &finalAttempts, &finalErr, &finalSuccess)
		return
	}
	var attempts int
	var err error
	var success bool
	m.sendInner(hook, event, &attempts, &err, &success)
}

// sendInner does the actual delivery work; send wraps it with tracing.
func (m *Manager) sendInner(hook *Webhook, event Event, attempts *int, finalErr *error, success *bool) {
	defer func() {
		if r := recover(); r != nil {
			// Log via fmt since logger may not be available
			fmt.Printf("webhook: panic in send: %v\n", r)
		}
	}()

	// Validate URL; the dialer re-checks every address it connects to.
	// hook is shared by concurrent deliveries, so it is not written here
	// (F5177).
	if valid, _ := m.isValidWebhookURL(hook.URL); !valid {
		fmt.Printf("webhook: invalid URL (SSRF protection): %s\n", redactURL(hook.URL))
		*finalErr = fmt.Errorf("invalid webhook URL")
		return
	}

	// Get circuit breaker for this webhook URL
	cb := m.cbManager.Get(hook.URL)

	// Check if circuit allows the request
	if !cb.Allow() {
		fmt.Printf("webhook: circuit breaker open for %s\n", redactURL(hook.URL))
		*finalErr = fmt.Errorf("circuit breaker open")
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		cb.RecordFailure()
		*finalErr = err
		return
	}

	// Retry logic: 3 attempts with exponential backoff
	var lastErr error
	delivered := false

	for attempt := 0; attempt < 3; attempt++ {
		*attempts = attempt + 1
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
			_ = resp.Body.Close()
			delivered = true
			break
		}

		_ = resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)

		// Don't retry on 4xx errors (client errors), except 408/429 which
		// signal a transient condition (F5971).
		if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
			resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
			break
		}
	}

	// Record success or failure for circuit breaker
	if delivered {
		cb.RecordSuccess()
		*success = true
	} else {
		cb.RecordFailure()
		fmt.Printf("webhook: delivery failed after %d attempts: %s, error: %v\n", *attempts, redactURL(hook.URL), redactErr(lastErr, hook.URL))
		*finalErr = redactErr(lastErr, hook.URL)
	}
}

// isValidWebhookURL checks if the URL is safe (not localhost or private IP)
// Performs DNS resolution at validation time and caches the resolved IP
// to prevent DNS rebinding attacks.
func (m *Manager) isValidWebhookURL(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, ""
	}

	// Only allow http and https schemes
	if u.Scheme != "http" && u.Scheme != "https" {
		return false, ""
	}

	// Get the hostname
	hostname := u.Hostname()
	if hostname == "" {
		return false, ""
	}

	// If private IPs are allowed (testing mode), skip security checks
	m.mu.RLock()
	allowPrivate := m.allowPrivateIP
	m.mu.RUnlock()
	if allowPrivate {
		return true, ""
	}

	// Block localhost variants
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return false, ""
	}

	// Block private IP ranges (direct IP in URL)
	ip := net.ParseIP(hostname)
	if ip != nil {
		if isBlockedIP(ip) {
			return false, ""
		}
		// Public IP - use it directly
		return true, hostname
	}

	// DNS rebinding protection: Resolve hostname at validation time
	// This prevents attackers from changing DNS records after validation
	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		return false, ""
	}

	// Use first resolved IP (typically A record)
	resolvedIP := ips[0].String()

	// Validate resolved IP is not private (prevent DNS rebinding to internal IPs)
	resolved := net.ParseIP(resolvedIP)
	if resolved == nil || isBlockedIP(resolved) {
		return false, ""
	}

	return true, resolvedIP
}

// isBlockedIP reports whether ip is an address webhooks must not reach.
// The unspecified address (0.0.0.0, ::) is included because connecting to it
// reaches the local host (F5175).
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// blockedNets lists non-routable ranges net.IP's helpers do not cover: CGNAT
// (100.64/10, which hosts e.g. cloud metadata endpoints), benchmarking,
// IETF protocol assignments and reserved space (F5923).
var blockedNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "240.0.0.0/4"} {
		_, n, _ := net.ParseCIDR(c)
		nets = append(nets, n)
	}
	return nets
}()

// dialControl runs after DNS resolution with the concrete address being
// connected to, so it sees the address the request really goes to.
func (m *Manager) dialControl(_, address string, _ syscall.RawConn) error {
	m.mu.RLock()
	allowPrivate := m.allowPrivateIP
	m.mu.RUnlock()
	if allowPrivate {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("webhook: bad dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || isBlockedIP(ip) {
		return fmt.Errorf("webhook: dial to %s rejected by SSRF check", address)
	}
	return nil
}

// DeriveSigningKey derives the webhook HMAC key from a master secret as
// hex(HMAC-SHA256(master, "umailserver webhook signing v1")). Receivers verify
// X-Webhook-Signature with this derived key, so holding it does not reveal the
// master (the JWT signing secret) and cannot mint tokens (F5600). An empty
// master yields "" (deliveries stay unsigned) rather than a public fixed key.
func DeriveSigningKey(master string) string {
	if master == "" {
		return ""
	}
	h := hmac.New(sha256.New, []byte(master))
	h.Write([]byte("umailserver webhook signing v1"))
	return hex.EncodeToString(h.Sum(nil))
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"webhooks": hooks}); err != nil {
		log.Printf("webhook: failed to encode response: %v", err)
	}
}

func (m *Manager) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// F5972: reject non-http(s)/hostless URLs at registration time rather
	// than accepting them and failing every delivery silently.
	if u, err := url.Parse(req.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	hook := &Webhook{
		ID:        newHookID(),
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
	if err := json.NewEncoder(w).Encode(hook); err != nil {
		log.Printf("webhook: failed to encode response: %v", err)
	}
}

// newHookID returns a collision-free webhook ID. A second-resolution
// timestamp gave two hooks created in the same second the same ID (F5924).
func newHookID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wh_%d", time.Now().UnixNano())
	}
	return "wh_" + hex.EncodeToString(b)
}

// GetCircuitBreakerMetrics returns circuit breaker metrics for all webhook URLs
func (m *Manager) GetCircuitBreakerMetrics() map[string]circuitbreaker.Metrics {
	all := m.cbManager.AllMetrics()
	out := make(map[string]circuitbreaker.Metrics, len(all))
	for k, v := range all {
		out[redactURL(k)] = v
	}
	return out
}
