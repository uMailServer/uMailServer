// Package storage provides data storage for the mail server
package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

// BackupJob represents a scheduled backup job stored in the database
type BackupJob struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Target       string     `json:"target"`
	Schedule     string     `json:"schedule"`
	Retention    int        `json:"retention_days"`
	Enabled      bool       `json:"enabled"`
	LastRun      *time.Time `json:"last_run,omitempty"`
	NextRun      *time.Time `json:"next_run,omitempty"`
	Destinations string     `json:"destinations"`
	Options      string     `json:"options"`
	Status       string     `json:"status"`
	LastError    string     `json:"last_error,omitempty"`
}

// BackupManifest represents metadata about a stored backup
type BackupManifest struct {
	ID             string    `json:"id"`
	Filename       string    `json:"filename"`
	Size           int64     `json:"size"`
	CreatedAt      time.Time `json:"created_at"`
	Type           string    `json:"type"`
	Target         string    `json:"target"`
	Checksum       string    `json:"checksum"`
	Encrypted      bool      `json:"encrypted"`
	RetentionUntil time.Time `json:"retention_until"`
	Destination    string    `json:"destination"`
	Path           string    `json:"path"`
	Metadata       string    `json:"metadata"`
}

var (
	backupJobsBucket      = []byte("backup_jobs")
	backupManifestsBucket = []byte("backup_manifests")
)

// CreateBackupJob stores a backup job
func (db *Database) CreateBackupJob(job *BackupJob) error {
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		bkt, err := tx.CreateBucketIfNotExists(backupJobsBucket)
		if err != nil {
			return err
		}
		data, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return bkt.Put([]byte(job.ID), data)
	})
}

// GetBackupJob retrieves a backup job by ID
func (db *Database) GetBackupJob(id string) (*BackupJob, error) {
	var job BackupJob
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(backupJobsBucket)
		if bkt == nil {
			return fmt.Errorf("backup job not found")
		}
		data := bkt.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("backup job not found")
		}
		return json.Unmarshal(data, &job)
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// UpdateBackupJob updates an existing backup job
func (db *Database) UpdateBackupJob(job *BackupJob) error {
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		bkt, err := tx.CreateBucketIfNotExists(backupJobsBucket)
		if err != nil {
			return err
		}
		data, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return bkt.Put([]byte(job.ID), data)
	})
}

// DeleteBackupJob deletes a backup job
func (db *Database) DeleteBackupJob(id string) error {
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(backupJobsBucket)
		if bkt == nil {
			return nil
		}
		return bkt.Delete([]byte(id))
	})
}

// ListBackupJobs returns all backup jobs, optionally filtered by enabled status
func (db *Database) ListBackupJobs(enabledOnly bool) ([]BackupJob, error) {
	var jobs []BackupJob
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(backupJobsBucket)
		if bkt == nil {
			return nil
		}
		return bkt.ForEach(func(k, v []byte) error {
			var job BackupJob
			if err := json.Unmarshal(v, &job); err != nil {
				return nil
			}
			if enabledOnly && !job.Enabled {
				return nil
			}
			jobs = append(jobs, job)
			return nil
		})
	})
	return jobs, err
}

// CreateBackupManifest stores a backup manifest
func (db *Database) CreateBackupManifest(manifest *BackupManifest) error {
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		bkt, err := tx.CreateBucketIfNotExists(backupManifestsBucket)
		if err != nil {
			return err
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		return bkt.Put([]byte(manifest.ID), data)
	})
}

// GetBackupManifest retrieves a backup manifest by ID
func (db *Database) GetBackupManifest(id string) (*BackupManifest, error) {
	var manifest BackupManifest
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(backupManifestsBucket)
		if bkt == nil {
			return fmt.Errorf("backup manifest not found")
		}
		data := bkt.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("backup manifest not found")
		}
		return json.Unmarshal(data, &manifest)
	})
	if err != nil {
		return nil, err
	}
	return &manifest, nil
}

// DeleteBackupManifest deletes a backup manifest
func (db *Database) DeleteBackupManifest(id string) error {
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(backupManifestsBucket)
		if bkt == nil {
			return nil
		}
		return bkt.Delete([]byte(id))
	})
}

// ListBackupManifests returns all backup manifests, optionally filtered by target
func (db *Database) ListBackupManifests(target string) ([]BackupManifest, error) {
	var manifests []BackupManifest
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(backupManifestsBucket)
		if bkt == nil {
			return nil
		}
		return bkt.ForEach(func(k, v []byte) error {
			var manifest BackupManifest
			if err := json.Unmarshal(v, &manifest); err != nil {
				return nil
			}
			if target != "" && manifest.Target != target {
				return nil
			}
			manifests = append(manifests, manifest)
			return nil
		})
	})
	return manifests, err
}
