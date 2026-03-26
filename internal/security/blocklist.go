package security

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// Blocklist manages IP and account blocking
type Blocklist struct {
	mu        sync.RWMutex
	blocks    map[string]*BlockEntry // key -> entry
	db        *bbolt.DB
	bucket    []byte
}

// BlockEntry represents a blocked entry
type BlockEntry struct {
	Key       string    `json:"key"`
	Type      string    `json:"type"` // ip, account, domain
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Permanent bool      `json:"permanent"`
}

// NewBlocklist creates a new blocklist
func NewBlocklist(db *bbolt.DB) (*Blocklist, error) {
	// Ensure bucket exists
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("blocklist"))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create blocklist bucket: %w", err)
	}

	bl := &Blocklist{
		blocks: make(map[string]*BlockEntry),
		db:     db,
		bucket: []byte("blocklist"),
	}

	// Load existing blocks from database
	if err := bl.loadFromDB(); err != nil {
		return nil, err
	}

	return bl, nil
}

// loadFromDB loads blocked entries from database
func (bl *Blocklist) loadFromDB() error {
	return bl.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bl.bucket)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var entry BlockEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}

			// Skip expired entries
			if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
				continue
			}

			bl.blocks[string(k)] = &entry
		}

		return nil
	})
}

// Add adds a permanent block
func (bl *Blocklist) Add(key, blockType, reason string) error {
	return bl.addBlock(key, blockType, reason, nil, true)
}

// AddTemporary adds a temporary block
func (bl *Blocklist) AddTemporary(key string, duration time.Duration, reason string) error {
	expiresAt := time.Now().Add(duration)
	return bl.addBlock(key, "ip", reason, &expiresAt, false)
}

// addBlock adds a block entry
func (bl *Blocklist) addBlock(key, blockType, reason string, expiresAt *time.Time, permanent bool) error {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	entry := &BlockEntry{
		Key:       key,
		Type:      blockType,
		Reason:    reason,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
		Permanent: permanent,
	}

	// Store in memory
	bl.blocks[key] = entry

	// Store in database
	return bl.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bl.bucket)
		if b == nil {
			return fmt.Errorf("blocklist bucket not found")
		}

		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}

		return b.Put([]byte(key), data)
	})
}

// Remove removes a block
func (bl *Blocklist) Remove(key string) error {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	delete(bl.blocks, key)

	return bl.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bl.bucket)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// IsBlocked checks if a key is blocked
func (bl *Blocklist) IsBlocked(key string) bool {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	// Check exact match
	if entry, exists := bl.blocks[key]; exists {
		if entry.Permanent {
			return true
		}
		if entry.ExpiresAt != nil && time.Now().Before(*entry.ExpiresAt) {
			return true
		}
	}

	// Check CIDR ranges for IPs
	if net.ParseIP(key) != nil {
		for k, entry := range bl.blocks {
			if entry.Type != "ip" {
				continue
			}
			if strings.Contains(k, "/") {
				// It's a CIDR range
				if isIPInCIDR(key, k) {
					return true
				}
			}
		}
	}

	return false
}

// IsIPBlocked checks if an IP is blocked
func (bl *Blocklist) IsIPBlocked(ip string) bool {
	return bl.IsBlocked(ip)
}

// IsAccountBlocked checks if an account is blocked
func (bl *Blocklist) IsAccountBlocked(email string) bool {
	return bl.IsBlocked("account:" + email)
}

// GetEntry returns a block entry
func (bl *Blocklist) GetEntry(key string) *BlockEntry {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return bl.blocks[key]
}

// List returns all blocked entries
func (bl *Blocklist) List() []*BlockEntry {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	entries := make([]*BlockEntry, 0, len(bl.blocks))
	for _, entry := range bl.blocks {
		// Skip expired entries
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

// ListByType returns blocked entries of a specific type
func (bl *Blocklist) ListByType(blockType string) []*BlockEntry {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	entries := make([]*BlockEntry, 0)
	for _, entry := range bl.blocks {
		if entry.Type != blockType {
			continue
		}
		// Skip expired entries
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

// Cleanup removes expired entries
func (bl *Blocklist) Cleanup() error {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for key, entry := range bl.blocks {
		if entry.ExpiresAt != nil && now.After(*entry.ExpiresAt) {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		delete(bl.blocks, key)
	}

	// Also clean up database
	return bl.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bl.bucket)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var entry BlockEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}

			if entry.ExpiresAt != nil && now.After(*entry.ExpiresAt) {
				b.Delete(k)
			}
		}

		return nil
	})
}

// StartCleanup starts a background cleanup goroutine
func (bl *Blocklist) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			bl.Cleanup()
		}
	}()
}

// GetStats returns blocklist statistics
func (bl *Blocklist) GetStats() map[string]interface{} {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	ipBlocks := 0
	accountBlocks := 0
	temporary := 0
	permanent := 0

	for _, entry := range bl.blocks {
		// Skip expired
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			continue
		}

		switch entry.Type {
		case "ip":
			ipBlocks++
		case "account":
			accountBlocks++
		}

		if entry.Permanent {
			permanent++
		} else {
			temporary++
		}
	}

	return map[string]interface{}{
		"total":          ipBlocks + accountBlocks,
		"ip_blocks":      ipBlocks,
		"account_blocks": accountBlocks,
		"temporary":      temporary,
		"permanent":      permanent,
	}
}

// isIPInCIDR checks if an IP is in a CIDR range
func isIPInCIDR(ip, cidr string) bool {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	return ipnet.Contains(parsedIP)
}
