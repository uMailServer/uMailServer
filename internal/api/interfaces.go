package api

import (
	"io/fs"

	"github.com/umailserver/umailserver/internal/push"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/ratelimit"
	"github.com/umailserver/umailserver/internal/vacation"
)

// QueueManager interface for queue operations
type QueueManager interface {
	GetStats() (*queue.QueueStats, error)
	RetryEntry(id string) error
	DeleteEntry(id string) error
}

// PushService interface for push notifications
type PushService interface {
	Subscribe(userID string, sub *push.Subscription) error
	Unsubscribe(userID, subscriptionID string) error
	SendNotification(userID string, notif *push.Notification) error
	GetVAPIDPublicKey() string
}

// VacationManager interface for vacation auto-reply
type VacationManager interface {
	GetConfig(userID string) (*vacation.Config, error)
	SetConfig(userID string, cfg *vacation.Config) error
	DeleteConfig(userID string) error
	ListActive() ([]string, error)
}

// FilterManager interface for email filters
type FilterManager interface {
	GetUserFilters(userID string) ([]*EmailFilter, error)
	GetFilter(userID, filterID string) (*EmailFilter, error)
	SaveFilter(filter *EmailFilter) error
	DeleteFilter(userID, filterID string) error
	ReorderFilters(userID string, filterIDs []string) error
}

// RateLimitManager interface for rate limiting
type RateLimitManager interface {
	GetConfig() *ratelimit.Config
	SetConfig(cfg *ratelimit.Config)
	GetIPStats(ip string) map[string]any
	GetUserStats(user string) map[string]any
}

// FileSystem interface for embed.FS abstraction
type FileSystem interface {
	Open(name string) (fs.File, error)
	ReadFile(name string) ([]byte, error)
	Exists(name string) bool
}
