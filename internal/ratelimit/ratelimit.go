package ratelimit

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
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
	globalBucket *globalBucket
	globalMu     sync.RWMutex
	stopCh       chan struct{}
	stopOnce     sync.Once
}

// globalBucket tracks global rate limit state
type globalBucket struct {
	minuteCount int64
	minuteReset time.Time
	hourCount   int64
	hourReset   time.Time
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
	now := time.Now()
	rl := &RateLimiter{
		bolt:         bolt,
		config:       cfg,
		ipCounters:   make(map[string]*ipBucket),
		userCounters: make(map[string]*userBucket),
		connLimits:   make(map[string]*connCounter),
		globalBucket: &globalBucket{
			minuteReset: now.Add(time.Minute),
			hourReset:   now.Add(time.Hour),
		},
		stopCh: make(chan struct{}),
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

// configLocked returns the current config for reading.
//
// rl.config is swapped by SetConfig under configMu, so every reader has to take
// configMu to obtain the pointer. SetConfig installs a freshly built *Config and
// never mutates one in place, so once the pointer has been read under the lock
// the value behind it is immutable and safe to read without holding the lock.
func (rl *RateLimiter) configLocked() *Config {
	rl.configMu.RLock()
	defer rl.configMu.RUnlock()
	return rl.config
}

// ipKey groups addresses into the unit an IP limit applies to: one IPv4
// address, or one IPv6 /64 (the smallest allocation a single subscriber gets),
// so rotating the interface ID cannot bypass the limit (F5106). IPv4-mapped
// IPv6 addresses share the IPv4 bucket. Unparseable input is used verbatim.
func ipKey(ip string) string {
	addr := net.ParseIP(ip)
	if addr == nil {
		return ip
	}
	if v4 := addr.To4(); v4 != nil {
		return v4.String()
	}
	return addr.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// CheckIP checks rate limits for an IP address (inbound)
func (rl *RateLimiter) CheckIP(ip string) Result {
	rl.ipMu.Lock()
	defer rl.ipMu.Unlock()

	bucket := rl.ipBucketLocked(ipKey(ip), time.Now())
	result := rl.ipLimit(bucket)
	if result.Allowed {
		bucket.commit()
	}
	return result
}

// ipBucketLocked returns the bucket for key with expired windows reset,
// creating it if needed. The caller holds ipMu.
func (rl *RateLimiter) ipBucketLocked(key string, now time.Time) *ipBucket {
	bucket, exists := rl.ipCounters[key]
	if !exists {
		bucket = &ipBucket{
			minuteReset: now.Add(time.Minute),
			hourReset:   now.Add(time.Hour),
			dayReset:    now.Add(24 * time.Hour),
		}
		rl.ipCounters[key] = bucket
		return bucket
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
	return bucket
}

// ipLimit reports whether one more message fits in bucket without counting it.
func (rl *RateLimiter) ipLimit(bucket *ipBucket) Result {
	cfg := rl.configLocked()
	if cfg.IPPerMinute > 0 && bucket.minuteCount >= cfg.IPPerMinute {
		retrySecs := int(time.Until(bucket.minuteReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 1
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("IP rate limit exceeded: %d/min", cfg.IPPerMinute),
			RetryAfter: retrySecs,
		}
	}

	if cfg.IPPerHour > 0 && bucket.hourCount >= cfg.IPPerHour {
		retrySecs := int(time.Until(bucket.hourReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 60
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("IP rate limit exceeded: %d/hour", cfg.IPPerHour),
			RetryAfter: retrySecs,
		}
	}

	if cfg.IPPerDay > 0 && bucket.dayCount >= cfg.IPPerDay {
		retrySecs := int(time.Until(bucket.dayReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 3600
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("IP rate limit exceeded: %d/day", cfg.IPPerDay),
			RetryAfter: retrySecs,
		}
	}

	return Result{Allowed: true}
}

func (b *ipBucket) commit() {
	b.minuteCount++
	b.hourCount++
	b.dayCount++
}

// CheckUser checks rate limits for an authenticated user (outbound sending)
func (rl *RateLimiter) CheckUser(user string) Result {
	rl.userMu.Lock()
	defer rl.userMu.Unlock()

	bucket := rl.userBucketLocked(user, time.Now())
	result := rl.userLimit(bucket)
	if result.Allowed {
		rl.commitUser(user, bucket)
	}
	return result
}

// userBucketLocked returns the user's bucket with expired windows reset,
// creating it (and restoring the persisted daily quota) if needed. The
// caller holds userMu.
func (rl *RateLimiter) userBucketLocked(user string, now time.Time) *userBucket {
	bucket, exists := rl.userCounters[user]
	if !exists {
		bucket = &userBucket{
			minuteReset: now.Add(time.Minute),
			hourReset:   now.Add(time.Hour),
			dayReset:    now.Add(24 * time.Hour),
		}
		// Load persisted sentToday from bbolt. The persisted window end is
		// restored too, so a restart neither restarts the 24h window for a
		// spent quota nor carries yesterday's count into a new day (F5105).
		if rl.bolt != nil {
			sent, reset := rl.loadUserSentToday(user)
			switch {
			case reset.IsZero(): // legacy record without a window end
				bucket.sentToday = sent
			case now.Before(reset):
				bucket.sentToday = sent
				bucket.dayReset = reset
			}
		}
		rl.userCounters[user] = bucket
		return bucket
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
		rl.saveUserSentToday(user, 0, bucket.dayReset)
	}
	return bucket
}

// userLimit reports whether one more message fits in bucket without counting it.
func (rl *RateLimiter) userLimit(bucket *userBucket) Result {
	cfg := rl.configLocked()
	if cfg.UserPerMinute > 0 && bucket.minuteCount >= cfg.UserPerMinute {
		retrySecs := int(time.Until(bucket.minuteReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 1
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("User rate limit exceeded: %d/min", cfg.UserPerMinute),
			RetryAfter: retrySecs,
		}
	}

	if cfg.UserPerHour > 0 && bucket.hourCount >= cfg.UserPerHour {
		retrySecs := int(time.Until(bucket.hourReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 60
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("User rate limit exceeded: %d/hour", cfg.UserPerHour),
			RetryAfter: retrySecs,
		}
	}

	// Check daily quota (persisted)
	if cfg.UserPerDay > 0 && bucket.sentToday >= int64(cfg.UserPerDay) {
		retrySecs := int(time.Until(bucket.dayReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 3600
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Daily sending quota exceeded: %d/day", cfg.UserPerDay),
			RetryAfter: retrySecs,
		}
	}

	return Result{Allowed: true}
}

// commitUser counts one message against the user's bucket and persists the
// daily quota. The caller holds userMu.
func (rl *RateLimiter) commitUser(user string, bucket *userBucket) {
	bucket.minuteCount++
	bucket.hourCount++
	bucket.dayCount++
	bucket.sentToday++
	rl.saveUserSentToday(user, bucket.sentToday, bucket.dayReset)
}

// CheckGlobal checks global rate limits across all users/connections
func (rl *RateLimiter) CheckGlobal() Result {
	rl.globalMu.Lock()
	defer rl.globalMu.Unlock()

	rl.resetGlobalLocked(time.Now())
	result := rl.globalLimit()
	if result.Allowed {
		rl.commitGlobal()
	}
	return result
}

// resetGlobalLocked resets expired global windows. The caller holds globalMu.
func (rl *RateLimiter) resetGlobalLocked(now time.Time) {
	if now.After(rl.globalBucket.minuteReset) {
		rl.globalBucket.minuteCount = 0
		rl.globalBucket.minuteReset = now.Add(time.Minute)
	}
	if now.After(rl.globalBucket.hourReset) {
		rl.globalBucket.hourCount = 0
		rl.globalBucket.hourReset = now.Add(time.Hour)
	}
}

// globalLimit reports whether one more message fits globally without
// counting it. The caller holds globalMu.
func (rl *RateLimiter) globalLimit() Result {
	cfg := rl.configLocked()
	if cfg.GlobalPerMinute > 0 && rl.globalBucket.minuteCount >= int64(cfg.GlobalPerMinute) {
		retrySecs := int(time.Until(rl.globalBucket.minuteReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 1
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Global rate limit exceeded: %d/min", cfg.GlobalPerMinute),
			RetryAfter: retrySecs,
		}
	}

	if cfg.GlobalPerHour > 0 && rl.globalBucket.hourCount >= int64(cfg.GlobalPerHour) {
		retrySecs := int(time.Until(rl.globalBucket.hourReset).Seconds())
		if retrySecs < 1 {
			retrySecs = 60
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Global rate limit exceeded: %d/hour", cfg.GlobalPerHour),
			RetryAfter: retrySecs,
		}
	}

	return Result{Allowed: true}
}

// commitGlobal counts one message globally. The caller holds globalMu.
func (rl *RateLimiter) commitGlobal() {
	rl.globalBucket.minuteCount++
	rl.globalBucket.hourCount++
}

// CheckMessage checks the per-user (when user is non-empty), per-IP and
// global limits for one message and counts it against all of them only when
// every limit allows it. A message refused by one limit therefore spends
// nothing from the others, in particular not the user's persisted daily
// quota (F5205). Limits are evaluated in the order user, IP, global and the
// first refusal is returned.
func (rl *RateLimiter) CheckMessage(user, ip string) Result {
	now := time.Now()

	// Lock order: userMu → ipMu → globalMu (configMu is taken innermost by
	// the limit helpers). No other path holds two of these at once.
	var ub *userBucket
	if user != "" {
		rl.userMu.Lock()
		defer rl.userMu.Unlock()
		ub = rl.userBucketLocked(user, now)
		if result := rl.userLimit(ub); !result.Allowed {
			return result
		}
	}

	if result := rl.checkIPAndGlobal(ipKey(ip), now); !result.Allowed {
		return result
	}

	if ub != nil {
		rl.commitUser(user, ub)
	}
	return Result{Allowed: true}
}

// checkIPAndGlobal checks the IP and global limits together and counts the
// message against both only when both allow it.
func (rl *RateLimiter) checkIPAndGlobal(key string, now time.Time) Result {
	rl.ipMu.Lock()
	defer rl.ipMu.Unlock()
	rl.globalMu.Lock()
	defer rl.globalMu.Unlock()

	ib := rl.ipBucketLocked(key, now)
	if result := rl.ipLimit(ib); !result.Allowed {
		return result
	}
	rl.resetGlobalLocked(now)
	if result := rl.globalLimit(); !result.Allowed {
		return result
	}
	ib.commit()
	rl.commitGlobal()
	return Result{Allowed: true}
}

// CheckRecipients checks if too many recipients for a user
func (rl *RateLimiter) CheckRecipients(user string, count int) Result {
	cfg := rl.configLocked()
	if cfg.UserMaxRecipients > 0 && count > cfg.UserMaxRecipients {
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Too many recipients: %d (max: %d)", count, cfg.UserMaxRecipients),
			RetryAfter: 0,
		}
	}
	return Result{Allowed: true}
}

// CheckConnection checks if a new connection is allowed from an IP
func (rl *RateLimiter) CheckConnection(ip string) Result {
	ip = ipKey(ip)
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

	// Active connection slots remain occupied until ReleaseConnection.
	if counter.count == 0 && now.After(counter.until) {
		counter.count = 1
		counter.until = now.Add(time.Minute)
		return Result{Allowed: true}
	}

	// Check limit
	cfg := rl.configLocked()
	if cfg.IPConnections > 0 && counter.count >= cfg.IPConnections {
		retrySecs := int(counter.until.Sub(now).Seconds())
		if retrySecs < 1 {
			retrySecs = 10
		}
		return Result{
			Allowed:    false,
			Reason:     fmt.Sprintf("Too many connections: %d (max: %d)", counter.count, cfg.IPConnections),
			RetryAfter: retrySecs,
		}
	}

	counter.count++
	return Result{Allowed: true}
}

// ReleaseConnection releases a connection slot when session ends
func (rl *RateLimiter) ReleaseConnection(ip string) {
	ip = ipKey(ip)
	rl.connMu.Lock()
	defer rl.connMu.Unlock()

	counter, exists := rl.connLimits[ip]
	if exists && counter.count > 0 {
		counter.count--
	}
}

// GetIPStats returns current rate limit stats for an IP
func (rl *RateLimiter) GetIPStats(ip string) map[string]any {
	ip = ipKey(ip)
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
	cfg := rl.configLocked()

	stats := make(map[string]any)
	if bucket, exists := rl.userCounters[user]; exists {
		stats["minute_count"] = bucket.minuteCount
		stats["hour_count"] = bucket.hourCount
		stats["day_count"] = bucket.dayCount
		stats["sent_today"] = bucket.sentToday
		stats["daily_limit"] = cfg.UserPerDay
		stats["minute_reset"] = bucket.minuteReset
		stats["hour_reset"] = bucket.hourReset
		stats["day_reset"] = bucket.dayReset
	} else {
		stats["minute_count"] = 0
		stats["hour_count"] = 0
		stats["day_count"] = 0
		stats["sent_today"] = 0
		stats["daily_limit"] = cfg.UserPerDay
	}
	return stats
}

// cleanupLoop periodically cleans up expired entries
func (rl *RateLimiter) cleanupLoop() {
	// Take configMu briefly to read CleanupInterval under the same lock
	// that SetConfig holds when swapping rl.config. Without this, the
	// background goroutine started by New() races with concurrent
	// SetConfig calls (caught by `go test -race`).
	rl.configMu.RLock()
	interval := rl.config.CleanupInterval
	rl.configMu.RUnlock()
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

func (rl *RateLimiter) loadUserSentToday(user string) (int64, time.Time) {
	if rl.bolt == nil {
		return 0, time.Time{}
	}
	var count int64
	var reset time.Time
	_ = rl.bolt.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ratelimit_users"))
		if bucket == nil {
			return nil
		}
		key := []byte(user + ":sent_today")
		if v := bucket.Get(key); len(v) == 8 {
			c := binary.BigEndian.Uint64(v)
			if c > uint64(math.MaxInt64) {
				c = uint64(math.MaxInt64)
			}
			count = int64(c)
		}
		if v := bucket.Get([]byte(user + ":day_reset")); len(v) == 8 {
			// #nosec G115 -- written by saveUserSentToday from UnixNano
			reset = time.Unix(0, int64(binary.BigEndian.Uint64(v)))
		}
		return nil
	})
	return count, reset
}

// saveUserSentToday persists the daily count and, when known, the end of the
// daily window it belongs to (F5105).
func (rl *RateLimiter) saveUserSentToday(user string, count int64, reset time.Time) {
	if rl.bolt == nil {
		return
	}
	_ = rl.bolt.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ratelimit_users"))
		if bucket == nil {
			return nil
		}
		key := []byte(user + ":sent_today")
		var buf [8]byte
		if count < 0 {
			count = 0
		}
		// #nosec G115 -- count is validated non-negative above
		binary.BigEndian.PutUint64(buf[:], uint64(count))
		if err := bucket.Put(key, buf[:]); err != nil {
			return err
		}
		if reset.IsZero() {
			return nil
		}
		var resetBuf [8]byte
		// #nosec G115 -- UnixNano of a current wall-clock time is positive
		binary.BigEndian.PutUint64(resetBuf[:], uint64(reset.UnixNano()))
		return bucket.Put([]byte(user+":day_reset"), resetBuf[:])
	})
}
