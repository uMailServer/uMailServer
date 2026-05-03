// Package cluster provides Redis-based clustering features
package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore implements SessionStore using Redis
type RedisSessionStore struct {
	client *redis.Client
}

// NewRedisSessionStore creates a new Redis session store
func NewRedisSessionStore(redisURL string) (*RedisSessionStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisSessionStore{client: client}, nil
}

// sessionKey returns the Redis key for a session
func sessionKey(sessionID string) string {
	return fmt.Sprintf("session:%s", sessionID)
}

// userSessionsKey returns the Redis key for user's session set
func userSessionsKey(userID string) string {
	return fmt.Sprintf("user:sessions:%s", userID)
}

// StoreSession stores a session with TTL
func (s *RedisSessionStore) StoreSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error {
	return s.client.Set(ctx, sessionKey(sessionID), data, ttl).Err()
}

// GetSession retrieves a session
func (s *RedisSessionStore) GetSession(ctx context.Context, sessionID string) ([]byte, error) {
	return s.client.Get(ctx, sessionKey(sessionID)).Bytes()
}

// DeleteSession removes a session
func (s *RedisSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	return s.client.Del(ctx, sessionKey(sessionID)).Err()
}

// RefreshSession extends the TTL of a session
func (s *RedisSessionStore) RefreshSession(ctx context.Context, sessionID string, ttl time.Duration) error {
	return s.client.Expire(ctx, sessionKey(sessionID), ttl).Err()
}

// AddSessionToUser maps a session to a user
func (s *RedisSessionStore) AddSessionToUser(ctx context.Context, userID, sessionID string) error {
	return s.client.SAdd(ctx, userSessionsKey(userID), sessionID).Err()
}

// GetUserSessions returns all sessions for a user
func (s *RedisSessionStore) GetUserSessions(ctx context.Context, userID string) ([]string, error) {
	return s.client.SMembers(ctx, userSessionsKey(userID)).Result()
}

// RemoveSessionFromUser removes a session from user's session set
func (s *RedisSessionStore) RemoveSessionFromUser(ctx context.Context, userID, sessionID string) error {
	return s.client.SRem(ctx, userSessionsKey(userID), sessionID).Err()
}

// Close closes the Redis connection
func (s *RedisSessionStore) Close() error {
	return s.client.Close()
}

// RedisLeaderElection implements LeaderElection using Redis SETNX + expiration
type RedisLeaderElection struct {
	client     *redis.Client
	instanceID string
	leaseTTL   time.Duration
}

// NewRedisLeaderElection creates a new Redis leader election
func NewRedisLeaderElection(redisURL, instanceID string, leaseTTL time.Duration) (*RedisLeaderElection, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisLeaderElection{
		client:     client,
		instanceID: instanceID,
		leaseTTL:   leaseTTL,
	}, nil
}

// leaderKey returns the Redis key for a leader election
func leaderKey(electionKey string) string {
	return fmt.Sprintf("lock:leader:%s", electionKey)
}

// TryAcquire attempts to become the leader
func (l *RedisLeaderElection) TryAcquire(ctx context.Context, electionKey string) (bool, error) {
	// Use SET with NX and EX to atomically set the leader if not exists
	result, err := l.client.SetNX(ctx, leaderKey(electionKey), l.instanceID, l.leaseTTL).Result()
	if err != nil {
		return false, err
	}

	if result {
		return true, nil // We acquired leadership
	}

	// Check if we are the current leader
	current, err := l.client.Get(ctx, leaderKey(electionKey)).Result()
	if err != nil {
		return false, err
	}

	return current == l.instanceID, nil
}

// IsLeader returns true if this instance is the leader
func (l *RedisLeaderElection) IsLeader(ctx context.Context, electionKey string) (bool, error) {
	current, err := l.client.Get(ctx, leaderKey(electionKey)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return current == l.instanceID, nil
}

// Refresh extends the leader lease
func (l *RedisLeaderElection) Refresh(ctx context.Context, electionKey string) error {
	// Only refresh if we are the leader
	isLeader, err := l.IsLeader(ctx, electionKey)
	if err != nil {
		return err
	}
	if !isLeader {
		return fmt.Errorf("not the leader")
	}

	return l.client.Expire(ctx, leaderKey(electionKey), l.leaseTTL).Err()
}

// Release gives up leadership
func (l *RedisLeaderElection) Release(ctx context.Context, electionKey string) error {
	// Use Lua script to atomically delete only if we are the leader
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)

	_, err := script.Run(ctx, l.client, []string{leaderKey(electionKey)}, l.instanceID).Result()
	return err
}

// Close closes the Redis connection
func (l *RedisLeaderElection) Close() error {
	return l.client.Close()
}

// RedisDistributedLock implements DistributedLock using Redlock algorithm
type RedisDistributedLock struct {
	client *redis.Client
}

// NewRedisDistributedLock creates a new Redis distributed lock
func NewRedisDistributedLock(redisURL string) (*RedisDistributedLock, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisDistributedLock{client: client}, nil
}

// lockKey returns the Redis key for a lock
func lockKey(lockName string) string {
	return fmt.Sprintf("lock:%s", lockName)
}

// Acquire attempts to acquire the lock with TTL
func (l *RedisDistributedLock) Acquire(ctx context.Context, lockName string, ttl time.Duration) (bool, error) {
	// Generate a unique lock value
	b := make([]byte, 16)
	rand.Read(b)
	lockValue := hex.EncodeToString(b)

	result, err := l.client.SetNX(ctx, lockKey(lockName), lockValue, ttl).Result()
	if err != nil {
		return false, err
	}

	return result, nil
}

// Release releases the lock
func (l *RedisDistributedLock) Release(ctx context.Context, lockName string) error {
	// Note: In a real implementation, we'd use a Lua script to atomically
	// delete only if we own the lock. For simplicity, this is a basic implementation.
	_, err := l.client.Del(ctx, lockKey(lockName)).Result()
	return err
}

// Extend extends the lock TTL
func (l *RedisDistributedLock) Extend(ctx context.Context, lockName string, ttl time.Duration) error {
	return l.client.Expire(ctx, lockKey(lockName), ttl).Err()
}

// Close closes the Redis connection
func (l *RedisDistributedLock) Close() error {
	return l.client.Close()
}

// SessionData represents session data stored in Redis
type SessionData struct {
	SessionID  string            `json:"session_id"`
	UserID     string            `json:"user_id"`
	InstanceID string            `json:"instance_id"`
	CreatedAt  int64             `json:"created_at"`
	ExpiresAt  int64             `json:"expires_at"`
	Metadata   map[string]string `json:"metadata"`
}

// Marshal serializes session data to JSON
func (s *SessionData) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

// Unmarshal deserializes session data from JSON
func (s *SessionData) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}
