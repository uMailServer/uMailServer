package metrics

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
)

// SimpleMetrics holds basic metrics without external dependencies
type SimpleMetrics struct {
	smtpConnections  uint64
	smtpMessages     uint64
	smtpAuthFailures uint64
	imapConnections  uint64
	deliveriesTotal  uint64
	deliveriesFailed uint64
	spamDetected     uint64
	hamDetected      uint64
	apiRequests      uint64
}

var (
	instance *SimpleMetrics
	once     sync.Once
)

// Get returns singleton metrics instance
func Get() *SimpleMetrics {
	once.Do(func() {
		instance = &SimpleMetrics{}
	})
	return instance
}

// SMTP metrics
func (m *SimpleMetrics) SMTPConnection() {
	atomic.AddUint64(&m.smtpConnections, 1)
}

func (m *SimpleMetrics) SMTPMessageReceived() {
	atomic.AddUint64(&m.smtpMessages, 1)
}

func (m *SimpleMetrics) SMTPAuthFailure() {
	atomic.AddUint64(&m.smtpAuthFailures, 1)
}

// IMAP metrics
func (m *SimpleMetrics) IMAPConnection() {
	atomic.AddUint64(&m.imapConnections, 1)
}

// Delivery metrics
func (m *SimpleMetrics) DeliverySuccess() {
	atomic.AddUint64(&m.deliveriesTotal, 1)
}

func (m *SimpleMetrics) DeliveryFailed() {
	atomic.AddUint64(&m.deliveriesFailed, 1)
}

// Spam metrics
func (m *SimpleMetrics) SpamDetected() {
	atomic.AddUint64(&m.spamDetected, 1)
}

func (m *SimpleMetrics) HamDetected() {
	atomic.AddUint64(&m.hamDetected, 1)
}

// API metrics
func (m *SimpleMetrics) APIRequest() {
	atomic.AddUint64(&m.apiRequests, 1)
}

// GetStats returns current statistics
func (m *SimpleMetrics) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"smtp": map[string]uint64{
			"connections":   atomic.LoadUint64(&m.smtpConnections),
			"messages":      atomic.LoadUint64(&m.smtpMessages),
			"auth_failures": atomic.LoadUint64(&m.smtpAuthFailures),
		},
		"imap": map[string]uint64{
			"connections": atomic.LoadUint64(&m.imapConnections),
		},
		"delivery": map[string]uint64{
			"success": atomic.LoadUint64(&m.deliveriesTotal),
			"failed":  atomic.LoadUint64(&m.deliveriesFailed),
		},
		"spam": map[string]uint64{
			"detected": atomic.LoadUint64(&m.spamDetected),
			"ham":      atomic.LoadUint64(&m.hamDetected),
		},
		"api": map[string]uint64{
			"requests": atomic.LoadUint64(&m.apiRequests),
		},
	}
}

// HTTPHandler returns metrics as JSON
func (m *SimpleMetrics) HTTPHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m.GetStats())
}
