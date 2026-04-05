package migrations

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

// Migration represents a database migration
type Migration struct {
	Version     string
	Description string
	Up          func(*bbolt.Tx) error
	Down        func(*bbolt.Tx) error
}

// migrationRecord tracks applied migrations
type migrationRecord struct {
	Version   string    `json:"version"`
	AppliedAt time.Time `json:"applied_at"`
}

const migrationBucket = "__migrations"

// Registry holds all registered migrations
type Registry struct {
	migrations []Migration
}

// NewRegistry creates a new migration registry
func NewRegistry() *Registry {
	return &Registry{
		migrations: make([]Migration, 0),
	}
}

// Register adds a migration to the registry
func (r *Registry) Register(m Migration) {
	r.migrations = append(r.migrations, m)
}

// Migrator handles running migrations
type Migrator struct {
	db       *bbolt.DB
	registry *Registry
}

// NewMigrator creates a new migrator
func NewMigrator(db *bbolt.DB, registry *Registry) *Migrator {
	return &Migrator{
		db:       db,
		registry: registry,
	}
}

// Migrate runs all pending migrations
func (m *Migrator) Migrate() error {
	// Ensure migration bucket exists
	if err := m.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(migrationBucket))
		return err
	}); err != nil {
		return fmt.Errorf("failed to create migration bucket: %w", err)
	}

	// Get applied migrations
	applied, err := m.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Run pending migrations
	for _, migration := range m.registry.migrations {
		if _, ok := applied[migration.Version]; ok {
			continue // Already applied
		}

		if err := m.runMigration(migration); err != nil {
			return fmt.Errorf("failed to run migration %s: %w", migration.Version, err)
		}
	}

	return nil
}

// Rollback rolls back the last migration
func (m *Migrator) Rollback() error {
	// Get applied migrations
	applied, err := m.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	if len(applied) == 0 {
		return fmt.Errorf("no migrations to rollback")
	}

	// Find last applied migration
	var lastVersion string
	var lastTime time.Time
	for version, record := range applied {
		if record.AppliedAt.After(lastTime) {
			lastVersion = version
			lastTime = record.AppliedAt
		}
	}

	// Find migration
	var migration *Migration
	for i := range m.registry.migrations {
		if m.registry.migrations[i].Version == lastVersion {
			migration = &m.registry.migrations[i]
			break
		}
	}

	if migration == nil {
		return fmt.Errorf("migration %s not found in registry", lastVersion)
	}

	// Run rollback
	return m.db.Update(func(tx *bbolt.Tx) error {
		if migration.Down == nil {
			return fmt.Errorf("migration %s has no rollback", lastVersion)
		}

		if err := migration.Down(tx); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}

		// Remove migration record
		bucket := tx.Bucket([]byte(migrationBucket))
		return bucket.Delete([]byte(lastVersion))
	})
}

// Status returns current migration status
func (m *Migrator) Status() (*Status, error) {
	applied, err := m.getAppliedMigrations()
	if err != nil {
		return nil, err
	}

	pending := 0
	for _, migration := range m.registry.migrations {
		if _, ok := applied[migration.Version]; !ok {
			pending++
		}
	}

	return &Status{
		Applied: len(applied),
		Pending: pending,
		Total:   len(m.registry.migrations),
	}, nil
}

// Status holds migration status
type Status struct {
	Applied int
	Pending int
	Total   int
}

func (m *Migrator) getAppliedMigrations() (map[string]migrationRecord, error) {
	applied := make(map[string]migrationRecord)

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(migrationBucket))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, v []byte) error {
			var record migrationRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			applied[string(k)] = record
			return nil
		})
	})

	return applied, err
}

func (m *Migrator) runMigration(migration Migration) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		if migration.Up == nil {
			return fmt.Errorf("migration %s has no up function", migration.Version)
		}

		if err := migration.Up(tx); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		// Record migration
		record := migrationRecord{
			Version:   migration.Version,
			AppliedAt: time.Now(),
		}

		data, err := json.Marshal(record)
		if err != nil {
			return err
		}

		bucket := tx.Bucket([]byte(migrationBucket))
		return bucket.Put([]byte(migration.Version), data)
	})
}

// InitMigrations registers all built-in migrations
func InitMigrations(registry *Registry) {
	// Migration: 001_initial - Initial schema (placeholder)
	// This would contain the initial database schema creation
	// Since the schema is already managed by db.go, this is a no-op migration
	registry.Register(Migration{
		Version:     "001",
		Description: "Initial schema",
		Up: func(tx *bbolt.Tx) error {
			// Schema is already managed by db.go
			// This migration just marks the initial state
			return nil
		},
		Down: func(tx *bbolt.Tx) error {
			// Cannot rollback initial migration
			return fmt.Errorf("cannot rollback initial migration")
		},
	})

	// Migration: 002_add_account_indexes - Add indexes for account lookups
	registry.Register(Migration{
		Version:     "002",
		Description: "Add account indexes",
		Up: func(tx *bbolt.Tx) error {
			// Migration already applied in current schema
			return nil
		},
		Down: func(tx *bbolt.Tx) error {
			return nil
		},
	})
}
