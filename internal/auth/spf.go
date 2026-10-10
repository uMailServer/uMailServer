package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
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
	ttl         time.Duration
}

type cacheEntry struct {
	record    string
	expiresAt time.Time
}

// defaultSPFMaxCacheSize is the maximum number of domains to cache
const defaultSPFMaxCacheSize = 10000

// defaultSPFCacheTTL is the default TTL for cached SPF records
const defaultSPFCacheTTL = 5 * time.Minute

// NewSPFChecker creates a new SPF checker
func NewSPFChecker(resolver DNSResolver) *SPFChecker {
	return &SPFChecker{
		resolver: resolver,
		cache: &spfCache{
			records:     make(map[string]*cacheEntry),
			maxSize:     defaultSPFMaxCacheSize,
			nextCleanup: time.Now().Add(1 * time.Minute),
			ttl:         defaultSPFCacheTTL,
		},
	}
}

// SetCacheTTL sets the TTL for cached SPF records. Values <= 0 are ignored
// and the default (5 minutes) is retained.
func (c *SPFChecker) SetCacheTTL(d time.Duration) {
	if d <= 0 {
		return
	}
	c.cache.mu.Lock()
	c.cache.ttl = d
	c.cache.mu.Unlock()
}

// CheckSPF evaluates SPF for the given sender IP and domain
func (c *SPFChecker) CheckSPF(ctx context.Context, ip net.IP, domain string, sender string) (SPFResult, string) {
	// Check cache first
	if record, ok := c.cache.get(domain); ok {
		metrics.Get().SPFCacheHit()
		return c.evaluate(ctx, ip, domain, sender, record, &spfLimits{})
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

	// Cache the record using the configured TTL
	c.cache.mu.RLock()
	ttl := c.cache.ttl
	c.cache.mu.RUnlock()
	if ttl <= 0 {
		ttl = defaultSPFCacheTTL
	}
	c.cache.set(domain, record, ttl)

	return c.evaluate(ctx, ip, domain, sender, record, &spfLimits{})
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

// spfMaxDNSTerms and spfMaxVoidLookups are the RFC 7208 §4.6.4 limits.
const (
	spfMaxDNSTerms    = 10
	spfMaxVoidLookups = 2
)

// spfLimits carries the DNS-term and void-lookup counters across the whole
// check_host() evaluation, including include and redirect recursion
// (F4885: nested counts used to be discarded).
type spfLimits struct {
	lookups int
	voids   int
}

// spfIsDNSTerm reports whether a term counts toward the 10-term limit
// (RFC 7208 §4.6.4: include, a, mx, ptr, exists and redirect).
func spfIsDNSTerm(typ string) bool {
	switch typ {
	case "include", "a", "mx", "ptr", "exists", "redirect":
		return true
	}
	return false
}

// evaluate evaluates an SPF record
func (c *SPFChecker) evaluate(ctx context.Context, ip net.IP, domain, sender, record string, lim *spfLimits) (SPFResult, string) {
	// Parse mechanisms
	terms := parseSPF(record)

	// F4888: modifiers may appear anywhere in the record; redirect is applied
	// only after no mechanism matched.
	var redirect string
	mechanisms := terms[:0:0]
	for _, m := range terms {
		if m.typ == "redirect" {
			if redirect == "" {
				redirect = m.value
			}
			continue
		}
		mechanisms = append(mechanisms, m)
	}

	// Default result
	result := SPFNeutral
	var explanation string
	var matched bool

	// Evaluate each mechanism
	for _, m := range mechanisms {
		// F4887: only DNS terms count, and only the 11th exceeds the limit.
		if spfIsDNSTerm(m.typ) {
			lim.lookups++
			if lim.lookups > spfMaxDNSTerms {
				return SPFPermError, "Too many DNS lookups"
			}
		}

		match, void, err := c.evaluateMechanism(ctx, ip, domain, sender, m, lim)
		if err != nil {
			if isTemporaryError(err) {
				return SPFTempError, err.Error()
			}
			return SPFPermError, err.Error()
		}

		if void {
			lim.voids++
			if lim.voids > spfMaxVoidLookups {
				return SPFPermError, "Too many void lookups"
			}
		}

		if match {
			result = m.qualifier
			explanation = m.String()
			matched = true
			break
		}
	}

	// Handle redirect if no mechanism matched
	if !matched && redirect != "" {
		// Redirect counts as one DNS term
		lim.lookups++
		if lim.lookups > spfMaxDNSTerms {
			return SPFPermError, "Too many DNS lookups"
		}
		record, err := c.lookupSPF(ctx, redirect)
		if err != nil {
			if isTemporaryError(err) {
				return SPFTempError, "DNS lookup failed"
			}
			return SPFPermError, "Invalid redirect"
		}
		return c.evaluate(ctx, ip, redirect, sender, record, lim)
	}

	return result, explanation
}

// evaluateMechanism evaluates a single SPF mechanism
// Returns: match, isVoid, error
func (c *SPFChecker) evaluateMechanism(ctx context.Context, ip net.IP, domain, sender string, m spfMechanism, lim *spfLimits) (bool, bool, error) {
	switch m.typ {
	case "all":
		return true, false, nil

	case "ip4":
		return c.evaluateIP4(ip, m.value), false, nil

	case "ip6":
		return c.evaluateIP6(ip, m.value), false, nil

	case "a":
		return c.evaluateA(ctx, ip, m.value, domain, lim.lookups, lim.voids)

	case "mx":
		return c.evaluateMX(ctx, ip, m.value, domain, lim.lookups, lim.voids)

	case "ptr":
		// PTR is discouraged in SPF, return false
		return false, false, nil

	case "exists":
		return c.evaluateExists(ctx, m.value, lim.lookups, lim.voids)

	case "include":
		return c.evaluateInclude(ctx, ip, m.value, sender, lim)

	default:
		return false, false, nil
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
	host, v4len, v6len, err := spfSplitDualCIDR(value)
	if err != nil {
		return false, false, err
	}
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
		if spfIPInCIDR(ip, targetIP, v4len, v6len) {
			return true, false, nil
		}
	}

	return false, false, nil
}

// spfSplitDualCIDR splits an a/mx domain-spec from its optional
// dual-cidr-length ("host/24//64", "/24", "//64"; RFC 7208 §5.6). Lengths
// default to 32 and 128 (F5312).
func spfSplitDualCIDR(value string) (string, int, int, error) {
	v4len, v6len := 32, 128
	if i := strings.Index(value, "//"); i >= 0 {
		n, err := strconv.Atoi(value[i+2:])
		if err != nil || n < 0 || n > 128 {
			return "", 0, 0, fmt.Errorf("invalid ip6-cidr-length in %q", value)
		}
		v6len = n
		value = value[:i]
	}
	if i := strings.LastIndex(value, "/"); i >= 0 {
		n, err := strconv.Atoi(value[i+1:])
		if err != nil || n < 0 || n > 32 {
			return "", 0, 0, fmt.Errorf("invalid ip4-cidr-length in %q", value)
		}
		v4len = n
		value = value[:i]
	}
	return value, v4len, v6len, nil
}

// spfIPInCIDR reports whether client lies in target's network of the
// address family's prefix length (F5312).
func spfIPInCIDR(client, target net.IP, v4len, v6len int) bool {
	if c4, t4 := client.To4(), target.To4(); c4 != nil || t4 != nil {
		if c4 == nil || t4 == nil {
			return false
		}
		m := net.CIDRMask(v4len, 32)
		return c4.Mask(m).Equal(t4.Mask(m))
	}
	m := net.CIDRMask(v6len, 128)
	return client.Mask(m).Equal(target.Mask(m))
}

// evaluateMX checks if IP matches an MX record
func (c *SPFChecker) evaluateMX(ctx context.Context, ip net.IP, value, domain string, lookups, voidLookups int) (bool, bool, error) {
	mxDomain, v4len, v6len, err := spfSplitDualCIDR(value)
	if err != nil {
		return false, false, err
	}
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
			if spfIPInCIDR(ip, targetIP, v4len, v6len) {
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
func (c *SPFChecker) evaluateInclude(ctx context.Context, ip net.IP, domain, sender string, lim *spfLimits) (bool, bool, error) {
	record, err := c.lookupSPF(ctx, domain)
	if err != nil {
		if isTemporaryError(err) {
			return false, false, err
		}
		// F4886: RFC 7208 §5.2 — an included domain without an SPF record
		// ("none") is a permerror, not a non-match.
		return false, false, fmt.Errorf("include target %s has no SPF record", domain)
	}

	// F4885: nested lookups and voids accumulate in the shared counters.
	result, explanation := c.evaluate(ctx, ip, domain, sender, record, lim)

	// Propagate permanent errors from nested evaluation
	if result == SPFPermError {
		return false, false, errors.New(explanation)
	}
	if result == SPFTempError {
		return false, false, errors.New("DNS lookup failed")
	}

	// Include returns true only if the included SPF passes
	return result == SPFPass, false, nil
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
	} else if idx := strings.Index(part, "/"); idx > 0 && (part[:idx] == "a" || part[:idx] == "mx") {
		// F5312: "a/24", "mx//64" — dual-cidr-length without a domain-spec.
		m.typ = part[:idx]
		m.value = part[idx:]
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
	// Check for net.Error with Timeout() method
	var netErr net.Error
	if errors.As(err, &netErr) {
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
