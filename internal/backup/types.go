// Package backup provides backup and restore functionality for uMailServer
package backup

import (
	"time"
)

// BackupType represents the type of backup
type BackupType string

const (
	BackupTypeFull    BackupType = "full"
	BackupTypeUser    BackupType = "per-user"
	BackupTypeMailbox BackupType = "per-mailbox"
)

// BackupDestinationType represents where backup data is stored
type BackupDestinationType string

const (
	DestinationLocal BackupDestinationType = "local"
	DestinationS3    BackupDestinationType = "s3"
	DestinationSFTP  BackupDestinationType = "sftp"
)

// RestoreMode defines how restore should behave
type RestoreMode string

const (
	RestoreModeOverwrite RestoreMode = "overwrite"
	RestoreModeMerge     RestoreMode = "merge"
	RestoreModeDifferent RestoreMode = "different-user"
)

// BackupJob represents a scheduled backup job stored in database
type BackupJob struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Type         BackupType          `json:"type"`
	Target       string              `json:"target"`         // user or mailbox for per-user/per-mailbox backups, empty for full
	Schedule     string              `json:"schedule"`       // cron expression
	Retention    int                 `json:"retention_days"` // days to keep backups
	Enabled      bool                `json:"enabled"`
	LastRun      *time.Time          `json:"last_run,omitempty"`
	NextRun      *time.Time          `json:"next_run,omitempty"`
	Destinations []BackupDestination `json:"destinations"`
	Options      BackupOptions       `json:"options"`
	Status       string              `json:"status"` // idle, running, failed
	LastError    string              `json:"last_error,omitempty"`
}

// BackupDestination represents where a backup should be stored
type BackupDestination struct {
	Type   BackupDestinationType `json:"type"`
	Path   string                `json:"path"`   // local path or s3 bucket
	Region string                `json:"region"` // AWS region for S3
}

// BackupOptions contains optional backup settings
type BackupOptions struct {
	Encrypted bool   `json:"encrypted"`
	Password  string `json:"-"` // Not stored in JSON
	Compress  bool   `json:"compress"`
}

// RestoreOptions contains settings for restore operation
type RestoreOptions struct {
	Mode       RestoreMode `json:"mode"`
	TargetUser string      `json:"target_user"` // For different-user restore
	TargetPath string      `json:"target_path"` // Custom restore path
	DateFrom   *time.Time  `json:"date_from"`   // Selective restore start date
	DateTo     *time.Time  `json:"date_to"`     // Selective restore end date
	Mailboxes  []string    `json:"mailboxes"`   // Specific mailboxes to restore
	Overwrite  bool        `json:"overwrite"`   // Overwrite existing files
	VerifyOnly bool        `json:"verify_only"` // Only verify, don't restore
}

// BackupInfo represents a backup file on disk
type BackupInfo struct {
	Filename string
	Size     int64
	ModTime  time.Time
	Path     string
}

// BackupManifest represents metadata about a backup
type BackupManifest struct {
	ID             string            `json:"id"`
	Filename       string            `json:"filename"`
	Size           int64             `json:"size"`
	CreatedAt      time.Time         `json:"created_at"`
	Type           BackupType        `json:"type"`
	Target         string            `json:"target"` // user or mailbox name
	Checksum       string            `json:"checksum"`
	Encrypted      bool              `json:"encrypted"`
	RetentionUntil time.Time         `json:"retention_until"`
	Destination    string            `json:"destination"`
	Path           string            `json:"path"`
	Metadata       map[string]string `json:"metadata"`
}
