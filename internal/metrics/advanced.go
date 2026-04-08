package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Histogram tracks value distribution
type Histogram struct {
	buckets []uint64
	bounds  []float64
	count   uint64
	sum     float64
	mutex   sync.RWMutex
}

// NewHistogram creates a histogram with specified bounds
func NewHistogram(bounds []float64) *Histogram {
	return &Histogram{
		buckets: make([]uint64, len(bounds)+1),
		bounds:  bounds,
	}
}

// Observe records a value
func (h *Histogram) Observe(value float64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	atomic.AddUint64(&h.count, 1)
	h.sum += value

	for i, bound := range h.bounds {
		if value <= bound {
			h.buckets[i]++
			return
		}
	}
	// Value exceeds all bounds
	h.buckets[len(h.buckets)-1]++
}

// Snapshot returns current histogram data
func (h *Histogram) Snapshot() map[string]interface{} {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return map[string]interface{}{
		"count":   atomic.LoadUint64(&h.count),
		"sum":     h.sum,
		"buckets": h.buckets,
		"bounds":  h.bounds,
	}
}

// Gauge tracks a value that can go up and down
type Gauge struct {
	value int64
}

// Set sets the gauge value
func (g *Gauge) Set(value float64) {
	atomic.StoreInt64(&g.value, int64(value*1000)) // Store as fixed-point
}

// Add adds to the gauge value
func (g *Gauge) Add(delta float64) {
	atomic.AddInt64(&g.value, int64(delta*1000))
}

// Get returns current value
func (g *Gauge) Get() float64 {
	return float64(atomic.LoadInt64(&g.value)) / 1000
}

// Counter tracks increasing values
type Counter struct {
	value uint64
}

// Inc increments the counter
func (c *Counter) Inc() {
	atomic.AddUint64(&c.value, 1)
}

// Add adds to the counter
func (c *Counter) Add(delta uint64) {
	atomic.AddUint64(&c.value, delta)
}

// Get returns current value
func (c *Counter) Get() uint64 {
	return atomic.LoadUint64(&c.value)
}

// AdvancedMetrics holds enhanced metrics
type AdvancedMetrics struct {
	// Connection gauges
	ActiveSMTPConnections Gauge
	ActiveIMAPConnections Gauge
	ActivePOP3Connections Gauge

	// Latency histograms
	SMTPCommandLatency   *Histogram
	DeliveryLatency      *Histogram
	DatabaseQueryLatency *Histogram
	APIRequestLatency    *Histogram

	// Message counters
	MessagesReceived  Counter
	MessagesDelivered Counter
	MessagesQueued    Counter
	MessagesBounced   Counter

	// Error counters
	SMTPErrors     Counter
	IMAPErrors     Counter
	DatabaseErrors Counter

	// Rate tracking
	lastMessageTime int64
	messageRate     float64
	rateMutex       sync.RWMutex
}

// NewAdvancedMetrics creates advanced metrics
func NewAdvancedMetrics() *AdvancedMetrics {
	return &AdvancedMetrics{
		SMTPCommandLatency:   NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
		DeliveryLatency:      NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
		DatabaseQueryLatency: NewHistogram([]float64{0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1}),
		APIRequestLatency:    NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}),
	}
}

// RecordMessageRate records message rate
func (am *AdvancedMetrics) RecordMessageRate() {
	now := time.Now().Unix()

	am.rateMutex.Lock()
	defer am.rateMutex.Unlock()

	if am.lastMessageTime > 0 {
		delta := float64(now - am.lastMessageTime)
		if delta > 0 {
			// Exponential moving average
			am.messageRate = 0.7*am.messageRate + 0.3*(1/delta)
		}
	}
	am.lastMessageTime = now
}

// GetMessageRate returns current message rate (messages per second)
func (am *AdvancedMetrics) GetMessageRate() float64 {
	am.rateMutex.RLock()
	defer am.rateMutex.RUnlock()
	return am.messageRate
}

// GetAdvancedStats returns all advanced metrics
func (am *AdvancedMetrics) GetAdvancedStats() map[string]interface{} {
	return map[string]interface{}{
		"connections": map[string]interface{}{
			"smtp_active": am.ActiveSMTPConnections.Get(),
			"imap_active": am.ActiveIMAPConnections.Get(),
			"pop3_active": am.ActivePOP3Connections.Get(),
		},
		"latency": map[string]interface{}{
			"smtp_commands":  am.SMTPCommandLatency.Snapshot(),
			"delivery":       am.DeliveryLatency.Snapshot(),
			"database_query": am.DatabaseQueryLatency.Snapshot(),
			"api_request":    am.APIRequestLatency.Snapshot(),
		},
		"messages": map[string]interface{}{
			"received":  am.MessagesReceived.Get(),
			"delivered": am.MessagesDelivered.Get(),
			"queued":    am.MessagesQueued.Get(),
			"bounced":   am.MessagesBounced.Get(),
			"rate":      am.GetMessageRate(),
		},
		"errors": map[string]interface{}{
			"smtp":     am.SMTPErrors.Get(),
			"imap":     am.IMAPErrors.Get(),
			"database": am.DatabaseErrors.Get(),
		},
	}
}

// Global advanced metrics instance
var advancedInstance *AdvancedMetrics
var advancedOnce sync.Once

// GetAdvanced returns the singleton advanced metrics instance
func GetAdvanced() *AdvancedMetrics {
	advancedOnce.Do(func() {
		advancedInstance = NewAdvancedMetrics()
	})
	return advancedInstance
}

// Timer helps measure operation duration

type Timer struct {
	start time.Time
	hist  *Histogram
}

// NewTimer creates a timer that observes to the histogram when stopped
func NewTimer(h *Histogram) *Timer {
	return &Timer{
		start: time.Now(),
		hist:  h,
	}
}

// ObserveDuration stops the timer and records the duration
func (t *Timer) ObserveDuration() float64 {
	duration := time.Since(t.start).Seconds()
	if t.hist != nil {
		t.hist.Observe(duration)
	}
	return duration
}
