package security

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	mu        sync.RWMutex
	buckets   map[string]*Bucket
	config    RateLimitConfig
	db        *db.DB
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	// SMTP rate limits
	SMTPConnectionsPerMinute int
	SMTPMessagesPerMinute    int

	// IMAP rate limits
	IMAPConnectionsPerMinute int
	IMAPCommandsPerMinute    int

	// HTTP rate limits
	HTTPRequestsPerMinute    int
	LoginAttemptsPerMinute   int

	// Global limits
	MaxConcurrentConnections int
}

// DefaultRateLimitConfig returns default rate limiting configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		SMTPConnectionsPerMinute: 30,
		SMTPMessagesPerMinute:    60,
		IMAPConnectionsPerMinute: 50,
		IMAPCommandsPerMinute:    300,
		HTTPRequestsPerMinute:    120,
		LoginAttemptsPerMinute:   10,
		MaxConcurrentConnections: 100,
	}
}

// Bucket represents a token bucket for rate limiting
type Bucket struct {
	tokens     float64
	capacity   float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig, database *db.DB) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*Bucket),
		config:  config,
		db:      database,
	}
}

// Allow checks if a request is allowed for the given key
func (rl *RateLimiter) Allow(key string, limitType string) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if !exists {
		// Create new bucket
		capacity, refillRate := rl.getBucketParams(limitType)
		bucket = &Bucket{
			tokens:     capacity,
			capacity:   capacity,
			refillRate: refillRate,
			lastRefill: time.Now(),
		}
		rl.mu.Lock()
		rl.buckets[key] = bucket
		rl.mu.Unlock()
	}

	return bucket.allow()
}

// AllowN checks if n requests are allowed
func (rl *RateLimiter) AllowN(key string, limitType string, n int) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if !exists {
		capacity, refillRate := rl.getBucketParams(limitType)
		bucket = &Bucket{
			tokens:     capacity,
			capacity:   capacity,
			refillRate: refillRate,
			lastRefill: time.Now(),
		}
		rl.mu.Lock()
		rl.buckets[key] = bucket
		rl.mu.Unlock()
	}

	return bucket.allowN(float64(n))
}

// getBucketParams returns capacity and refill rate for a limit type
func (rl *RateLimiter) getBucketParams(limitType string) (capacity, refillRate float64) {
	switch limitType {
	case "smtp_connection":
		return float64(rl.config.SMTPConnectionsPerMinute), float64(rl.config.SMTPConnectionsPerMinute) / 60.0
	case "smtp_message":
		return float64(rl.config.SMTPMessagesPerMinute), float64(rl.config.SMTPMessagesPerMinute) / 60.0
	case "imap_connection":
		return float64(rl.config.IMAPConnectionsPerMinute), float64(rl.config.IMAPConnectionsPerMinute) / 60.0
	case "imap_command":
		return float64(rl.config.IMAPCommandsPerMinute), float64(rl.config.IMAPCommandsPerMinute) / 60.0
	case "http_request":
		return float64(rl.config.HTTPRequestsPerMinute), float64(rl.config.HTTPRequestsPerMinute) / 60.0
	case "login_attempt":
		return float64(rl.config.LoginAttemptsPerMinute), float64(rl.config.LoginAttemptsPerMinute) / 60.0
	default:
		return 60.0, 1.0 // Default: 60 per minute
	}
}

// allow checks if a single request is allowed
func (b *Bucket) allow() bool {
	return b.allowN(1)
}

// allowN checks if n requests are allowed
func (b *Bucket) allowN(n float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = min(b.capacity, b.tokens+elapsed*b.refillRate)
	b.lastRefill = now

	// Check if we have enough tokens
	if b.tokens >= n {
		b.tokens -= n
		return true
	}

	return false
}

// Reset resets the rate limiter for a key
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, key)
}

// Cleanup removes old buckets that haven't been used
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, bucket := range rl.buckets {
		bucket.mu.Lock()
		age := now.Sub(bucket.lastRefill)
		bucket.mu.Unlock()

		if age > maxAge {
			delete(rl.buckets, key)
		}
	}
}

// StartCleanup starts a background goroutine to clean up old buckets
func (rl *RateLimiter) StartCleanup(interval time.Duration, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			rl.Cleanup(maxAge)
		}
	}()
}

// GetIP extracts the client IP from an address string
func GetIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// RateLimitKey creates a rate limit key from IP and limit type
func RateLimitKey(ip, limitType string) string {
	return fmt.Sprintf("%s:%s", ip, limitType)
}

// AccountKey creates a rate limit key for an account
func AccountKey(email, limitType string) string {
	return fmt.Sprintf("account:%s:%s", email, limitType)
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
