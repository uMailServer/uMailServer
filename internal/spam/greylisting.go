package spam

import (
	"net"
	"strings"
	"sync"
	"time"
)

// Greylisting implements greylisting for spam prevention
type Greylisting struct {
	mu       sync.RWMutex
	triplets map[string]*greylistEntry
	config   GreylistConfig

	// Cleanup ticker
	ticker *time.Ticker
	stop   chan struct{}
}

// GreylistEntry holds greylisting data for a triplet
type greylistEntry struct {
	FirstSeen  time.Time
	RetryAfter time.Time
	Passed     bool
	Count      int
}

// GreylistConfig holds greylisting configuration
type GreylistConfig struct {
	Enabled        bool
	Delay          time.Duration
	Expiry         time.Duration
	WhitelistPass  int // Number of passes before whitelisting
}

// DefaultGreylistConfig returns default configuration
func DefaultGreylistConfig() GreylistConfig {
	return GreylistConfig{
		Enabled:       true,
		Delay:         5 * time.Minute,
		Expiry:        4 * time.Hour * 7, // 28 hours
		WhitelistPass: 5,
	}
}

// NewGreylisting creates a new greylisting instance
func NewGreylisting(config GreylistConfig) *Greylisting {
	g := &Greylisting{
		triplets: make(map[string]*greylistEntry),
		config:   config,
		stop:     make(chan struct{}),
	}

	// Start cleanup goroutine
	if config.Enabled {
		g.ticker = time.NewTicker(5 * time.Minute)
		go g.cleanup()
	}

	return g
}

// Close stops the greylisting cleanup
func (g *Greylisting) Close() {
	if g.ticker != nil {
		g.ticker.Stop()
		close(g.stop)
	}
}

// Check checks if a message should be greylisted
// Returns: allowed (bool), retryAfter (time.Duration), error
func (g *Greylisting) Check(senderIP net.IP, sender, recipient string) (bool, time.Duration, error) {
	if !g.config.Enabled {
		return true, 0, nil
	}

	triplet := g.makeTriplet(senderIP, sender, recipient)

	g.mu.Lock()
	defer g.mu.Unlock()

	entry, exists := g.triplets[triplet]
	now := time.Now()

	if !exists {
		// First time seeing this triplet - greylist
		g.triplets[triplet] = &greylistEntry{
			FirstSeen:  now,
			RetryAfter: now.Add(g.config.Delay),
			Passed:     false,
			Count:      1,
		}
		return false, g.config.Delay, nil
	}

	entry.Count++

	// Check if already passed
	if entry.Passed {
		return true, 0, nil
	}

	// Check if enough time has passed
	if now.After(entry.RetryAfter) {
		entry.Passed = true
		return true, 0, nil
	}

	// Still greylisted
	retryAfter := entry.RetryAfter.Sub(now)
	return false, retryAfter, nil
}

// IsWhitelisted checks if a triplet is whitelisted
func (g *Greylisting) IsWhitelisted(senderIP net.IP, sender, recipient string) bool {
	if !g.config.Enabled {
		return false
	}

	triplet := g.makeTriplet(senderIP, sender, recipient)

	g.mu.RLock()
	defer g.mu.RUnlock()

	entry, exists := g.triplets[triplet]
	if !exists {
		return false
	}

	return entry.Passed && entry.Count >= g.config.WhitelistPass
}

// makeTriplet creates a triplet key
func (g *Greylisting) makeTriplet(senderIP net.IP, sender, recipient string) string {
	// Normalize sender IP to string
	ip := senderIP.String()

	// Simple concatenation - in production, use a proper hash
	// Normalize email addresses to lowercase
	sender = strings.ToLower(sender)
	recipient = strings.ToLower(recipient)
	return ip + "|" + sender + "|" + recipient
}

// cleanup removes expired entries
func (g *Greylisting) cleanup() {
	for {
		select {
		case <-g.ticker.C:
			g.doCleanup()
		case <-g.stop:
			return
		}
	}
}

// doCleanup removes expired entries
func (g *Greylisting) doCleanup() {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	expired := make([]string, 0)

	for triplet, entry := range g.triplets {
		// Remove entries that haven't been seen in a while
		// Keep whitelisted entries longer
		expiry := g.config.Expiry
		if entry.Passed {
			expiry = g.config.Expiry * 7 // Keep whitelisted entries for a week
		}

		if now.Sub(entry.FirstSeen) > expiry {
			expired = append(expired, triplet)
		}
	}

	for _, triplet := range expired {
		delete(g.triplets, triplet)
	}
}

// GetStats returns greylisting statistics
func (g *Greylisting) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	whitelisted := 0
	pending := 0

	for _, entry := range g.triplets {
		if entry.Passed {
			whitelisted++
		} else {
			pending++
		}
	}

	return map[string]interface{}{
		"total_triplets": len(g.triplets),
		"whitelisted":    whitelisted,
		"pending":        pending,
	}
}

// Reset clears all entries
func (g *Greylisting) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.triplets = make(map[string]*greylistEntry)
}
