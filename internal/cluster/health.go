// Package cluster provides Redis-based clustering features
package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisHealthMonitor implements HealthMonitor using Redis
type RedisHealthMonitor struct {
	client     *redis.Client
	instanceID string
	ttl        time.Duration
}

// NewRedisHealthMonitor creates a new Redis health monitor
func NewRedisHealthMonitor(redisURL, instanceID string) (*RedisHealthMonitor, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisHealthMonitor{
		client:     client,
		instanceID: instanceID,
		ttl:        60 * time.Second, // Consider offline if no heartbeat for 60s
	}, nil
}

// HealthElectionKey is the leader-election key whose holder is reported as
// leader in instance health.
const HealthElectionKey = "server"

// healthKey returns the Redis key for instance health
func healthKey(instanceID string) string {
	return fmt.Sprintf("health:%s", instanceID)
}

// instancesKey returns the Redis key for instance set
func instancesKey() string {
	return "cluster:instances"
}

// RecordHeartbeat records a heartbeat for this instance
func (h *RedisHealthMonitor) RecordHeartbeat(ctx context.Context) error {
	now := time.Now().Unix()

	// Report real leadership (F7043): it was hard-coded false, so no
	// instance ever appeared as leader in /cluster status.
	isLeader := false
	if cur, err := h.client.Get(ctx, leaderKey(HealthElectionKey)).Result(); err == nil {
		isLeader = cur == h.instanceID
	} else if !errors.Is(err, redis.Nil) {
		return err
	}

	// Store heartbeat with timestamp
	err := h.client.HSet(ctx, healthKey(h.instanceID), map[string]interface{}{
		"instance_id": h.instanceID,
		"last_beat":   now,
		"is_leader":   isLeader,
	}).Err()
	if err != nil {
		return err
	}

	// Add to instances set with expiry
	err = h.client.SAdd(ctx, instancesKey(), h.instanceID).Err()
	if err != nil {
		return err
	}

	// Expire the instance key after 2x ttl to handle crashes
	return h.client.Expire(ctx, healthKey(h.instanceID), h.ttl*2).Err()
}

// GetInstanceHealth returns health status of all instances
func (h *RedisHealthMonitor) GetInstanceHealth(ctx context.Context) ([]InstanceHealth, error) {
	// Get all instance IDs
	instanceIDs, err := h.client.SMembers(ctx, instancesKey()).Result()
	if err != nil {
		return nil, err
	}

	// Leadership comes from the live leader key, not the is_leader flag cached
	// at each instance's last heartbeat: after a failover or lease expiry the
	// cached flags are stale and would show the old leader (or two) (F7380).
	currentLeader := ""
	if cur, err := h.client.Get(ctx, leaderKey(HealthElectionKey)).Result(); err == nil {
		currentLeader = cur
	} else if !errors.Is(err, redis.Nil) {
		return nil, err
	}

	var health []InstanceHealth
	now := time.Now().Unix()
	cutoff := now - int64(h.ttl.Seconds())

	for _, id := range instanceIDs {
		data, err := h.client.HGetAll(ctx, healthKey(id)).Result()
		if err != nil {
			return nil, err
		}

		if len(data) == 0 {
			// Instance key expired, remove from set
			h.client.SRem(ctx, instancesKey(), id)
			continue
		}

		lastBeat := int64(0)
		if v, ok := data["last_beat"]; ok {
			fmt.Sscanf(v, "%d", &lastBeat)
		}

		status := "healthy"
		if lastBeat < cutoff {
			status = "offline"
		}

		health = append(health, InstanceHealth{
			InstanceID: id,
			LastBeat:   time.Unix(lastBeat, 0),
			IsLeader:   id == currentLeader,
			Status:     status,
		})
	}

	return health, nil
}

// IsHealthy returns true if the instance has a recent heartbeat
func (h *RedisHealthMonitor) IsHealthy(ctx context.Context) (bool, error) {
	data, err := h.client.HGetAll(ctx, healthKey(h.instanceID)).Result()
	if err != nil {
		return false, err
	}

	if len(data) == 0 {
		return false, nil
	}

	lastBeat := int64(0)
	if v, ok := data["last_beat"]; ok {
		fmt.Sscanf(v, "%d", &lastBeat)
	}

	cutoff := time.Now().Add(-h.ttl).Unix()
	return lastBeat >= cutoff, nil
}

// Close closes the Redis connection
func (h *RedisHealthMonitor) Close() error {
	return h.client.Close()
}

// ClusterManager orchestrates all cluster features
type ClusterManager struct {
	config  *Config
	session SessionStore
	leader  LeaderElection
	lock    DistributedLock
	health  HealthMonitor
}

// NewClusterManager creates a new cluster manager
func NewClusterManager(config *Config, redisURL string) (*ClusterManager, error) {
	session, err := NewRedisSessionStore(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create session store: %w", err)
	}

	leader, err := NewRedisLeaderElection(redisURL, config.InstanceID, config.LeaseTimeout)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to create leader election: %w", err)
	}

	lock, err := NewRedisDistributedLock(redisURL)
	if err != nil {
		session.Close()
		leader.Close()
		return nil, fmt.Errorf("failed to create distributed lock: %w", err)
	}

	health, err := NewRedisHealthMonitor(redisURL, config.InstanceID)
	if err != nil {
		session.Close()
		leader.Close()
		lock.Close()
		return nil, fmt.Errorf("failed to create health monitor: %w", err)
	}

	return &ClusterManager{
		config:  config,
		session: session,
		leader:  leader,
		lock:    lock,
		health:  health,
	}, nil
}

// SessionStore returns the session store
func (c *ClusterManager) SessionStore() SessionStore {
	return c.session
}

// LeaderElection returns the leader election
func (c *ClusterManager) LeaderElection() LeaderElection {
	return c.leader
}

// DistributedLock returns the distributed lock
func (c *ClusterManager) DistributedLock() DistributedLock {
	return c.lock
}

// HealthMonitor returns the health monitor
func (c *ClusterManager) HealthMonitor() HealthMonitor {
	return c.health
}

// Close closes all cluster resources
func (c *ClusterManager) Close() error {
	var errs []error
	if c.session != nil {
		errs = append(errs, c.session.Close())
	}
	if c.leader != nil {
		errs = append(errs, c.leader.Close())
	}
	if c.lock != nil {
		errs = append(errs, c.lock.Close())
	}
	if c.health != nil {
		errs = append(errs, c.health.Close())
	}
	return errors.Join(errs...)
}
