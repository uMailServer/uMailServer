// Package cluster provides Redis-based clustering features for uMailServer
// including session distribution, leader election, and distributed locking.
package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Config holds cluster configuration
type Config struct {
	RedisURL     string
	InstanceID   string
	LeaseTimeout time.Duration
	LockTimeout  time.Duration
}

// NewConfig creates a default cluster config
func NewConfig(redisURL, instanceID string) *Config {
	return &Config{
		RedisURL:     redisURL,
		InstanceID:   instanceID,
		LeaseTimeout: 15 * time.Second,
		LockTimeout:  30 * time.Second,
	}
}

// SessionStore interface for distributed session management
type SessionStore interface {
	// StoreSession stores a session with TTL
	StoreSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error
	// GetSession retrieves a session
	GetSession(ctx context.Context, sessionID string) ([]byte, error)
	// DeleteSession removes a session
	DeleteSession(ctx context.Context, sessionID string) error
	// RefreshSession extends the TTL of a session
	RefreshSession(ctx context.Context, sessionID string, ttl time.Duration) error
	// AddSessionToUser maps a session to a user
	AddSessionToUser(ctx context.Context, userID, sessionID string) error
	// GetUserSessions returns all sessions for a user
	GetUserSessions(ctx context.Context, userID string) ([]string, error)
	// RemoveSessionFromUser removes a session from user's session set
	RemoveSessionFromUser(ctx context.Context, userID, sessionID string) error
	// Close closes the session store
	Close() error
}

// LeaderElection handles leader election using Redis
type LeaderElection interface {
	// TryAcquire attempts to become the leader
	TryAcquire(ctx context.Context, key string) (bool, error)
	// IsLeader returns true if this instance is the leader
	IsLeader(ctx context.Context, key string) (bool, error)
	// Refresh extends the leader lease
	Refresh(ctx context.Context, key string) error
	// Release gives up leadership
	Release(ctx context.Context, key string) error
	// Close closes the leader election
	Close() error
}

// DistributedLock provides distributed mutex using Redis
type DistributedLock interface {
	// Acquire attempts to acquire the lock
	Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Release releases the lock
	Release(ctx context.Context, key string) error
	// Extend extends the lock TTL
	Extend(ctx context.Context, key string, ttl time.Duration) error
	// Close closes the lock
	Close() error
}

// HealthMonitor monitors cluster health
type HealthMonitor interface {
	// RecordHeartbeat records a heartbeat for this instance
	RecordHeartbeat(ctx context.Context) error
	// GetInstanceHealth returns health status of all instances
	GetInstanceHealth(ctx context.Context) ([]InstanceHealth, error)
	// IsHealthy returns true if the instance is healthy
	IsHealthy(ctx context.Context) (bool, error)
	// Close closes the health monitor
	Close() error
}

// InstanceHealth represents health status of a cluster instance
type InstanceHealth struct {
	InstanceID  string    `json:"instance_id"`
	LastBeat    time.Time `json:"last_heartbeat"`
	IsLeader    bool      `json:"is_leader"`
	Connections int       `json:"connections"`
	Status      string    `json:"status"` // healthy, degraded, offline
}

// GenerateInstanceID generates a unique instance ID
func GenerateInstanceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
