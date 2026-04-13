package ratelimit

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// RateLimiter implements comprehensive rate limiting for email sending
type RateLimiter struct {
	bolt         *bbolt.DB
	config       *Config
	configMu     sync.RWMutex
	ipCounters   map[string]*ipBucket
	ipMu         sync.RWMutex
	userCounters map[string]*userBucket
	userMu       sync.RWMutex
	connLimits   map[string]*connCounter
	connMu       sync.RWMutex
	stopCh       chan struct{}
	stopOnce     sync.Once
}

// Config holds rate limiting configuration
type Config struct {
	// Per-IP limits (inbound connections)
	IPPerMinute   int // messages per minute per IP
	IPPerHour     int // messages per hour per IP
	IPPerDay      int // messages per day per IP
	IPConnections int // concurrent connections per IP

	// Per-user limits (authenticated sending)
	UserPerMinute     int // messages per minute per user
	UserPerHour       int // messages per hour per user
	UserPerDay        int // messages per day per user (quota)
	UserMaxRecipients int // max recipients per message

	// Global limits
	GlobalPerMinute int // global messages per minute
	GlobalPerHour   int // global messages per hour

	// Cleanup interval
	CleanupInterval time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		IPPerMinute:       30,
		IPPerHour:         500,
		IPPerDay:          5000,
		IPConnections:     10,
		UserPerMinute:     60,
		UserPerHour:       1000,
		UserPerDay:        5000,
		UserMaxRecipients: 100,
		GlobalPerMinute:   10000,
		GlobalPerHour:     100000,
		CleanupInterval:   5 * time.Minute,
	}
}

// ipBucket tracks rate limit state for an IP
type ipBucket struct {
	minuteCount int
	minuteReset time.Time
	hourCount   int
	hourReset   time.Time
	dayCount    int
	dayReset    time.Time
}

// userBucket tracks rate limit state for a user
type userBucket struct {
	minuteCount int
	minuteReset time.Time
	hourCount   int
	hourReset   time.Time
	dayCount    int
	dayReset    time.Time
	sentToday   int64 // persisted to bbolt for daily quotas
}

// connCounter tracks concurrent connections per IP
type connCounter struct {
	count int
	until time.Time
}

// Result of a rate limit check
type Result struct {
	Allowed    bool
	Reason     string
	RetryAfter int // seconds until retry is allowed
}

// New creates a new RateLimiter
func New(bolt *bbolt.DB, cfg *Config) *RateLimiter {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	rl := &RateLimiter{
		bolt:         bolt,
		config:       cfg,
		ipCounters:   make(map[string]*ipBucket),
		userCounters: make(map[string]*userBucket),
		connLimits:   make(map[string]*connCounter),
		stopCh:       make(chan struct{}),
	}

	// Initialize bbolt buckets for persistent user quotas
	if rl.bolt != nil {
		if err := rl.bolt.Update(func(tx *bbolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("ratelimit_users"))
			return err
		}); err != nil {
			// Log but don't fail startup - rate limiting can work without persistence
			fmt.Printf("ratelimit: failed to initialize bucket: %v\n", err)
		}
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// GetConfig returns the current rate limit configuration
func (rl *RateLimiter) GetConfig() *Config {
	rl.configMu.RLock()
	defer rl.configMu.RUnlock()
	return rl.config
}

// SetConfig updates the rate limit configuration at runtime
func (rl *RateLimiter) SetConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	rl.configMu.Lock()
	defer rl.configMu.Unlock()
	rl.config = cfg
}

// CheckIP checks rate limits for an IP address (inbound)
func (rl *RateLimiter) CheckIP(ip string) Result {
	rl.ipMu.Lock()
	defer rl.ipMu.Unlock()

	now := time.Now()
	bucket, exists := rl.ipCounters[ip]
	if !exists {
		bucket = &ipBucket{
			minuteReset: now.Add(time.Minute),
			hourReset:   now.Add(time.Hour),
			dayReset:    now.Add(24 * time.Hour),
		}
		rl.ipCounters[ip] = bucket
		return rl.checkIPBucket(bucket)
	}

	// Reset expired windows
	if now.After(bucket.minuteReset) {
		bucket.minuteCount = 0
		bucket.minuteReset = now.Add(time.Minute)
	}
	if now.After(bucket.hourReset) {
		bucket.hourCount = 0
		bucket.hourReset = now.Add(time.Hour)
	}
	if now.After(bucket.dayReset) {
		bucket.dayCount = 0
		bucket.dayReset = now.Add(24 * time.Hour)
	}

	return rl.checkIPBucket(bucket)
}

func (rl *RateLimiter) checkIPBucket(bucket *ipBucket) Result {
	if rl.config.IPPerMinute > 0 && bucket.minuteCount >= rl.config.IPPerMinute {
		retrySecs := int(time.Until(bucket.minuteReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 1
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("IP rate limit exceeded: %d/min", rl.config.IPPerMinute),
			RetryAfter: retrySecs,
		}
	}

	if rl.config.IPPerHour > 0 && bucket.hourCount >= rl.config.IPPerHour {
		retrySecs := int(time.Until(bucket.hourReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 60
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("IP rate limit exceeded: %d/hour", rl.config.IPPerHour),
			RetryAfter: retrySecs,
		}
	}

	if rl.config.IPPerDay > 0 && bucket.dayCount >= rl.config.IPPerDay {
		retrySecs := int(time.Until(bucket.dayReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 3600
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("IP rate limit exceeded: %d/day", rl.config.IPPerDay),
			RetryAfter: retrySecs,
		}
	}

	// Increment counters
	bucket.minuteCount++
	bucket.hourCount++
	bucket.dayCount++

	return Result{Allowed: true}
}

// CheckUser checks rate limits for an authenticated user (outbound sending)
func (rl *RateLimiter) CheckUser(user string) Result {
	rl.userMu.Lock()
	defer rl.userMu.Unlock()

	now := time.Now()
	bucket, exists := rl.userCounters[user]
	if !exists {
		bucket = &userBucket{
			minuteReset: now.Add(time.Minute),
			hourReset:   now.Add(time.Hour),
			dayReset:    now.Add(24 * time.Hour),
		}
		// Load persisted sentToday from bbolt
		if rl.bolt != nil {
			bucket.sentToday = rl.loadUserSentToday(user)
		}
		rl.userCounters[user] = bucket
		return rl.checkUserBucket(user, bucket)
	}

	// Reset expired windows
	if now.After(bucket.minuteReset) {
		bucket.minuteCount = 0
		bucket.minuteReset = now.Add(time.Minute)
	}
	if now.After(bucket.hourReset) {
		bucket.hourCount = 0
		bucket.hourReset = now.Add(time.Hour)
	}
	if now.After(bucket.dayReset) {
		bucket.dayCount = 0
		bucket.dayReset = now.Add(24 * time.Hour)
		// Reset persisted daily counter
		bucket.sentToday = 0
		rl.saveUserSentToday(user, 0)
	}

	return rl.checkUserBucket(user, bucket)
}

func (rl *RateLimiter) checkUserBucket(user string, bucket *userBucket) Result {
	if rl.config.UserPerMinute > 0 && bucket.minuteCount >= rl.config.UserPerMinute {
		retrySecs := int(time.Until(bucket.minuteReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 1
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("User rate limit exceeded: %d/min", rl.config.UserPerMinute),
			RetryAfter: retrySecs,
		}
	}

	if rl.config.UserPerHour > 0 && bucket.hourCount >= rl.config.UserPerHour {
		retrySecs := int(time.Until(bucket.hourReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 60
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("User rate limit exceeded: %d/hour", rl.config.UserPerHour),
			RetryAfter: retrySecs,
		}
	}

	// Check daily quota (persisted)
	if rl.config.UserPerDay > 0 && bucket.sentToday >= int64(rl.config.UserPerDay) {
		retrySecs := int(time.Until(bucket.dayReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 3600
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Daily sending quota exceeded: %d/day", rl.config.UserPerDay),
			RetryAfter: retrySecs,
		}
	}

	// Increment counters
	bucket.minuteCount++
	bucket.hourCount++
	bucket.dayCount++
	bucket.sentToday++
	rl.saveUserSentToday(user, bucket.sentToday)

	return Result{Allowed: true}
}

// CheckRecipients checks if too many recipients for a user
func (rl *RateLimiter) CheckRecipients(user string, count int) Result {
	if rl.config.UserMaxRecipients > 0 && count > rl.config.UserMaxRecipients {
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Too many recipients: %d (max: %d)", count, rl.config.UserMaxRecipients),
			RetryAfter: 0,
		}
	}
	return Result{Allowed: true}
}

// CheckConnection checks if a new connection is allowed from an IP
func (rl *RateLimiter) CheckConnection(ip string) Result {
	rl.connMu.Lock()
	defer rl.connMu.Unlock()

	now := time.Now()
	counter, exists := rl.connLimits[ip]
	if !exists {
		counter = &connCounter{until: now.Add(time.Minute)}
		rl.connLimits[ip] = counter
		counter.count = 1
		return Result{Allowed: true}
	}

	// Reset if window expired
	if now.After(counter.until) {
		counter.count = 1
		counter.until = now.Add(time.Minute)
		return Result{Allowed: true}
	}

	// Check limit
	if rl.config.IPConnections > 0 && counter.count >= rl.config.IPConnections {
		retrySecs := int(counter.until.Sub(now).Seconds())
		if retrySecs < 1 {
			retrySecs = 10
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Too many connections: %d (max: %d)", counter.count, rl.config.IPConnections),
			RetryAfter: retrySecs,
		}
	}

	counter.count++
	return Result{Allowed: true}
}

// ReleaseConnection releases a connection slot when session ends
func (rl *RateLimiter) ReleaseConnection(ip string) {
	rl.connMu.Lock()
	defer rl.connMu.Unlock()

	counter, exists := rl.connLimits[ip]
	if exists && counter.count > 0 {
		counter.count--
	}
}

// GetIPStats returns current rate limit stats for an IP
func (rl *RateLimiter) GetIPStats(ip string) map[string]any {
	rl.ipMu.RLock()
	defer rl.ipMu.RUnlock()

	stats := make(map[string]any)
	if bucket, exists := rl.ipCounters[ip]; exists {
		stats["minute_count"] = bucket.minuteCount
		stats["hour_count"] = bucket.hourCount
		stats["day_count"] = bucket.dayCount
		stats["minute_reset"] = bucket.minuteReset
		stats["hour_reset"] = bucket.hourReset
		stats["day_reset"] = bucket.dayReset
	} else {
		stats["minute_count"] = 0
		stats["hour_count"] = 0
		stats["day_count"] = 0
	}
	return stats
}

// GetUserStats returns current rate limit stats for a user
func (rl *RateLimiter) GetUserStats(user string) map[string]any {
	rl.userMu.RLock()
	defer rl.userMu.RUnlock()

	stats := make(map[string]any)
	if bucket, exists := rl.userCounters[user]; exists {
		stats["minute_count"] = bucket.minuteCount
		stats["hour_count"] = bucket.hourCount
		stats["day_count"] = bucket.dayCount
		stats["sent_today"] = bucket.sentToday
		stats["daily_limit"] = rl.config.UserPerDay
		stats["minute_reset"] = bucket.minuteReset
		stats["hour_reset"] = bucket.hourReset
		stats["day_reset"] = bucket.dayReset
	} else {
		stats["minute_count"] = 0
		stats["hour_count"] = 0
		stats["day_count"] = 0
		stats["sent_today"] = 0
		stats["daily_limit"] = rl.config.UserPerDay
	}
	return stats
}

// cleanupLoop periodically cleans up expired entries
func (rl *RateLimiter) cleanupLoop() {
	interval := rl.config.CleanupInterval
	if interval <= 0 {
		interval = 5 * time.Minute // Default cleanup interval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// Stop cleanly shuts down the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
	})
}

func (rl *RateLimiter) cleanup() {
	now := time.Now()

	// Cleanup IP counters
	rl.ipMu.Lock()
	for ip, bucket := range rl.ipCounters {
		if now.After(bucket.dayReset) && now.After(bucket.hourReset.Add(time.Hour)) {
			delete(rl.ipCounters, ip)
		}
	}
	rl.ipMu.Unlock()

	// Cleanup connection counters
	rl.connMu.Lock()
	for ip, counter := range rl.connLimits {
		if now.After(counter.until) && counter.count == 0 {
			delete(rl.connLimits, ip)
		}
	}
	rl.connMu.Unlock()
}

// bbolt persistence for user daily quotas

func (rl *RateLimiter) loadUserSentToday(user string) int64 {
	if rl.bolt == nil {
		return 0
	}
	var count int64
	rl.bolt.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ratelimit_users"))
		if bucket == nil {
			return nil
		}
		key := []byte(user + ":sent_today")
		if v := bucket.Get(key); len(v) == 8 {
			count = int64(binary.BigEndian.Uint64(v))
		}
		return nil
	})
	return count
}

func (rl *RateLimiter) saveUserSentToday(user string, count int64) {
	if rl.bolt == nil {
		return
	}
	rl.bolt.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ratelimit_users"))
		if bucket == nil {
			return nil
		}
		key := []byte(user + ":sent_today")
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(count))
		return bucket.Put(key, buf[:])
	})
}
