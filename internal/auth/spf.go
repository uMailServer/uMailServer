package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
)

// SPFResult represents the result of an SPF check
type SPFResult int

const (
	SPFNone      SPFResult = iota // No SPF record found
	SPFNeutral                    // Neutral result
	SPFPass                       // SPF check passed
	SPFFail                       // SPF check failed (hard fail)
	SPFSoftFail                   // SPF check failed (soft fail)
	SPFTempError                  // Temporary error
	SPFPermError                  // Permanent error
)

func (r SPFResult) String() string {
	switch r {
	case SPFNone:
		return "none"
	case SPFNeutral:
		return "neutral"
	case SPFPass:
		return "pass"
	case SPFFail:
		return "fail"
	case SPFSoftFail:
		return "softfail"
	case SPFTempError:
		return "temperror"
	case SPFPermError:
		return "permerror"
	default:
		return "unknown"
	}
}

// SPFChecker performs SPF verification
type SPFChecker struct {
	resolver DNSResolver
	cache    *spfCache
}

// DNSResolver interface for DNS lookups
type DNSResolver interface {
	LookupTXT(ctx context.Context, domain string) ([]string, error)
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
	LookupMX(ctx context.Context, domain string) ([]*net.MX, error)
}

// spfCache caches SPF lookup results with bounded size
type spfCache struct {
	records     map[string]*cacheEntry
	mu          sync.RWMutex
	maxSize     int
	nextCleanup time.Time
}

type cacheEntry struct {
	record    string
	expiresAt time.Time
}

// defaultSPFMaxCacheSize is the maximum number of domains to cache
const defaultSPFMaxCacheSize = 10000

// NewSPFChecker creates a new SPF checker
func NewSPFChecker(resolver DNSResolver) *SPFChecker {
	return &SPFChecker{
		resolver: resolver,
		cache: &spfCache{
			records:     make(map[string]*cacheEntry),
			maxSize:     defaultSPFMaxCacheSize,
			nextCleanup: time.Now().Add(1 * time.Minute),
		},
	}
}

// CheckSPF evaluates SPF for the given sender IP and domain
func (c *SPFChecker) CheckSPF(ctx context.Context, ip net.IP, domain string, sender string) (SPFResult, string) {
	// Check cache first
	if record, ok := c.cache.get(domain); ok {
		metrics.Get().SPFCacheHit()
		return c.evaluate(ctx, ip, domain, sender, record, 0, 0)
	}

	metrics.Get().SPFCacheMiss()

	// Look up SPF record
	record, err := c.lookupSPF(ctx, domain)
	if err != nil {
		if isTemporaryError(err) {
			return SPFTempError, "DNS lookup failed"
		}
		return SPFNone, "No SPF record found"
	}

	// Cache the record
	c.cache.set(domain, record, 5*time.Minute)

	return c.evaluate(ctx, ip, domain, sender, record, 0, 0)
}

// lookupSPF looks up the SPF record for a domain
func (c *SPFChecker) lookupSPF(ctx context.Context, domain string) (string, error) {
	txtRecords, err := c.resolver.LookupTXT(ctx, domain)
	if err != nil {
		return "", err
	}

	for _, record := range txtRecords {
		if strings.HasPrefix(record, "v=spf1") {
			return record, nil
		}
	}

	return "", fmt.Errorf("no SPF record found")
}

// evaluate evaluates an SPF record
func (c *SPFChecker) evaluate(ctx context.Context, ip net.IP, domain, sender, record string, lookups, voidLookups int) (SPFResult, string) {
	// RFC 7208: Maximum 10 DNS lookups
	if lookups >= 10 {
		return SPFPermError, "Too many DNS lookups"
	}

	// RFC 7208: Maximum 2 void lookups
	if voidLookups >= 2 {
		return SPFPermError, "Too many void lookups"
	}

	// Parse mechanisms
	mechanisms := parseSPF(record)

	// Check for redirect modifier (must be at end and only processed if no match)
	var redirect string
	if len(mechanisms) > 0 {
		lastMech := mechanisms[len(mechanisms)-1]
		if lastMech.typ == "redirect" {
			redirect = lastMech.value
			mechanisms = mechanisms[:len(mechanisms)-1]
		}
	}

	// Default result
	result := SPFNeutral
	var explanation string
	var matched bool

	// Evaluate each mechanism
	for _, m := range mechanisms {
		// Check lookup limit before evaluation
		if lookups >= 10 {
			return SPFPermError, "Too many DNS lookups"
		}

		match, void, lookupCount, err := c.evaluateMechanism(ctx, ip, domain, sender, m, lookups, voidLookups)
		if err != nil {
			if isTemporaryError(err) {
				return SPFTempError, err.Error()
			}
			return SPFPermError, err.Error()
		}

		if void {
			voidLookups++
		}
		lookups += lookupCount

		if match {
			result = m.qualifier
			explanation = m.String()
			matched = true
			break
		}
	}

	// Handle redirect if no mechanism matched
	if !matched && redirect != "" {
		record, err := c.lookupSPF(ctx, redirect)
		if err != nil {
			if isTemporaryError(err) {
				return SPFTempError, "DNS lookup failed"
			}
			return SPFPermError, "Invalid redirect"
		}
		// Redirect counts as one lookup
		if lookups+1 >= 10 {
			return SPFPermError, "Too many DNS lookups"
		}
		return c.evaluate(ctx, ip, redirect, sender, record, lookups+1, voidLookups)
	}

	return result, explanation
}

// evaluateMechanism evaluates a single SPF mechanism
// Returns: match, isVoid, lookupCount, error
func (c *SPFChecker) evaluateMechanism(ctx context.Context, ip net.IP, domain, sender string, m spfMechanism, lookups, voidLookups int) (bool, bool, int, error) {
	switch m.typ {
	case "all":
		return true, false, 0, nil

	case "ip4":
		return c.evaluateIP4(ip, m.value), false, 0, nil

	case "ip6":
		return c.evaluateIP6(ip, m.value), false, 0, nil

	case "a":
		match, void, err := c.evaluateA(ctx, ip, m.value, domain, lookups, voidLookups)
		return match, void, 1, err

	case "mx":
		match, void, err := c.evaluateMX(ctx, ip, m.value, domain, lookups, voidLookups)
		return match, void, 1, err

	case "ptr":
		// PTR is discouraged in SPF, return false
		return false, false, 0, nil

	case "exists":
		match, void, err := c.evaluateExists(ctx, m.value, lookups, voidLookups)
		return match, void, 1, err

	case "include":
		return c.evaluateInclude(ctx, ip, m.value, sender, lookups, voidLookups)

	default:
		return false, false, 0, nil
	}
}

// evaluateIP4 checks if IP matches an IPv4 range
func (c *SPFChecker) evaluateIP4(ip net.IP, value string) bool {
	if ip.To4() == nil {
		return false
	}

	_, ipNet, err := net.ParseCIDR(value)
	if err != nil {
		// Try parsing as single IP
		targetIP := net.ParseIP(value)
		if targetIP == nil {
			return false
		}
		return ip.Equal(targetIP)
	}

	return ipNet.Contains(ip)
}

// evaluateIP6 checks if IP matches an IPv6 range
func (c *SPFChecker) evaluateIP6(ip net.IP, value string) bool {
	if ip.To4() != nil {
		return false
	}

	_, ipNet, err := net.ParseCIDR(value)
	if err != nil {
		// Try parsing as single IP
		targetIP := net.ParseIP(value)
		if targetIP == nil {
			return false
		}
		return ip.Equal(targetIP)
	}

	return ipNet.Contains(ip)
}

// evaluateA checks if IP matches an A record
func (c *SPFChecker) evaluateA(ctx context.Context, ip net.IP, value, domain string, lookups, voidLookups int) (bool, bool, error) {
	host := value
	if host == "" {
		host = domain
	}

	ips, err := c.resolver.LookupIP(ctx, host)
	if err != nil {
		if isTemporaryError(err) {
			return false, false, err
		}
		return false, true, nil // Void lookup
	}

	if len(ips) == 0 {
		return false, true, nil // Void lookup
	}

	for _, targetIP := range ips {
		if ip.Equal(targetIP) {
			return true, false, nil
		}
	}

	return false, false, nil
}

// evaluateMX checks if IP matches an MX record
func (c *SPFChecker) evaluateMX(ctx context.Context, ip net.IP, value, domain string, lookups, voidLookups int) (bool, bool, error) {
	mxDomain := value
	if mxDomain == "" {
		mxDomain = domain
	}

	mxRecords, err := c.resolver.LookupMX(ctx, mxDomain)
	if err != nil {
		if isTemporaryError(err) {
			return false, false, err
		}
		return false, true, nil // Void lookup
	}

	if len(mxRecords) == 0 {
		return false, true, nil // Void lookup
	}

	// Check IP of each MX host
	for _, mx := range mxRecords {
		mxIPs, err := c.resolver.LookupIP(ctx, mx.Host)
		if err != nil {
			if isTemporaryError(err) {
				return false, false, err
			}
			continue
		}

		for _, targetIP := range mxIPs {
			if ip.Equal(targetIP) {
				return true, false, nil
			}
		}
	}

	return false, false, nil
}

// evaluateExists checks if a domain exists
func (c *SPFChecker) evaluateExists(ctx context.Context, value string, lookups, voidLookups int) (bool, bool, error) {
	_, err := c.resolver.LookupIP(ctx, value)
	if err != nil {
		if isTemporaryError(err) {
			return false, false, err
		}
		return false, true, nil // Void lookup
	}

	return true, false, nil
}

// evaluateInclude includes another domain's SPF record
func (c *SPFChecker) evaluateInclude(ctx context.Context, ip net.IP, domain, sender string, lookups, voidLookups int) (bool, bool, int, error) {
	record, err := c.lookupSPF(ctx, domain)
	if err != nil {
		if isTemporaryError(err) {
			return false, false, 1, err
		}
		return false, true, 1, nil // Void lookup
	}

	result, explanation := c.evaluate(ctx, ip, domain, sender, record, lookups+1, voidLookups)

	// Propagate permanent errors from nested evaluation
	if result == SPFPermError {
		return false, false, 0, errors.New(explanation)
	}
	if result == SPFTempError {
		return false, false, 0, errors.New("DNS lookup failed")
	}

	// Include returns true only if the included SPF passes
	return result == SPFPass, false, 0, nil
}

// spfMechanism represents an SPF mechanism
type spfMechanism struct {
	qualifier SPFResult
	typ       string
	value     string
}

func (m spfMechanism) String() string {
	prefix := ""
	switch m.qualifier {
	case SPFPass:
		prefix = "+"
	case SPFNeutral:
		prefix = "?"
	case SPFFail:
		prefix = "-"
	case SPFSoftFail:
		prefix = "~"
	}

	if m.value != "" {
		return fmt.Sprintf("%s%s:%s", prefix, m.typ, m.value)
	}
	return prefix + m.typ
}

// parseSPF parses an SPF record into mechanisms
func parseSPF(record string) []spfMechanism {
	var mechanisms []spfMechanism

	parts := strings.Fields(record)
	for i, part := range parts {
		// Skip version
		if i == 0 && part == "v=spf1" {
			continue
		}

		mechanism := parseMechanism(part)
		if mechanism.typ != "" {
			mechanisms = append(mechanisms, mechanism)
		}
	}

	return mechanisms
}

// parseMechanism parses a single SPF mechanism
func parseMechanism(part string) spfMechanism {
	m := spfMechanism{qualifier: SPFPass}

	// Check for qualifier
	if len(part) > 0 {
		switch part[0] {
		case '+':
			m.qualifier = SPFPass
			part = part[1:]
		case '-':
			m.qualifier = SPFFail
			part = part[1:]
		case '~':
			m.qualifier = SPFSoftFail
			part = part[1:]
		case '?':
			m.qualifier = SPFNeutral
			part = part[1:]
		}
	}

	// Parse mechanism type and value
	// Handle redirect=domain (uses = separator)
	if strings.HasPrefix(part, "redirect=") {
		m.typ = "redirect"
		m.value = part[9:] // After "redirect="
		return m
	}

	// Handle exp=domain (explanation modifier)
	if strings.HasPrefix(part, "exp=") {
		m.typ = "exp"
		m.value = part[4:] // After "exp="
		return m
	}

	if idx := strings.Index(part, ":"); idx > 0 {
		m.typ = part[:idx]
		m.value = part[idx+1:]
	} else if strings.HasPrefix(part, "ip4:") || strings.HasPrefix(part, "ip6:") {
		// Handle ip4: and ip6: without explicit split
		m.typ = part[:3]
		m.value = part[4:]
	} else {
		m.typ = part
	}

	return m
}

// Cache methods

func (c *spfCache) get(domain string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.records[domain]
	if !ok {
		return "", false
	}

	if time.Now().After(entry.expiresAt) {
		// Note: we don't delete here to avoid write lock in read path
		// The cleanup will happen on next set() or get() after expiry check
		return "", false
	}

	return entry.record, true
}

func (c *spfCache) set(domain, record string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict old entries
	if len(c.records) >= c.maxSize {
		c.evictOldest()
	}

	c.records[domain] = &cacheEntry{
		record:    record,
		expiresAt: time.Now().Add(ttl),
	}

	// Periodic cleanup of expired entries
	if time.Now().After(c.nextCleanup) {
		c.cleanupExpired()
		c.nextCleanup = time.Now().Add(1 * time.Minute)
	}
}

// evictOldest removes the oldest entries when cache is full
func (c *spfCache) evictOldest() {
	// Remove 10% of entries (oldest first by expiry time)
	targetSize := c.maxSize * 9 / 10
	now := time.Now()

	// Collect entries with their expiry times
	type entryWithExpiry struct {
		domain    string
		expiresAt time.Time
	}

	var entries []entryWithExpiry
	for domain, entry := range c.records {
		entries = append(entries, entryWithExpiry{domain, entry.expiresAt})
	}

	// Sort by expiry time (oldest first)
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].expiresAt.After(entries[j].expiresAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove oldest entries until we're at target size
	for i := 0; i < len(entries) && len(c.records) > targetSize; i++ {
		delete(c.records, entries[i].domain)
	}

	// If we still need to free space, also remove expired entries
	c.cleanupExpiredLocked(now)
}

// cleanupExpired removes all expired entries
func (c *spfCache) cleanupExpired() {
	c.cleanupExpiredLocked(time.Now())
}

// cleanupExpiredLocked removes expired entries (caller must hold lock)
func (c *spfCache) cleanupExpiredLocked(now time.Time) {
	for domain, entry := range c.records {
		if now.After(entry.expiresAt) {
			delete(c.records, domain)
		}
	}
}

// isTemporaryError checks if an error is temporary.
// Uses proper net.Error type assertion per RFC 7208.
func isTemporaryError(err error) bool {
	if err == nil {
		return false
	}
	// Check for net.Error with Temporary() method
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Temporary() {
			return true
		}
		// Timeout implies temporary
		if netErr.Timeout() {
			return true
		}
	}
	// Check for context cancellation/deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Fallback: string-based matching for errors that don't implement net.Error
	// (e.g., errors from mock resolvers in tests, or third-party libraries)
	errMsg := err.Error()
	return strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "temporary")
}
