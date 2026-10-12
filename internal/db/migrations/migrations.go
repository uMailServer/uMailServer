package migrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// ErrSchemaTooNew is returned when the database records a migration this
// binary does not know about: it was written by a newer release. Running
// (or opening) it with this binary could corrupt data, and migrations never
// downgrade, so the caller must upgrade the binary instead (F6620).
var ErrSchemaTooNew = errors.New("database schema is newer than this binary supports")

// unknownApplied returns applied versions absent from the registry, sorted.
func (m *Migrator) unknownApplied(applied map[string]migrationRecord) []string {
	known := make(map[string]struct{}, len(m.registry.migrations))
	for _, mg := range m.registry.migrations {
		known[mg.Version] = struct{}{}
	}
	var unknown []string
	for v := range applied {
		if _, ok := known[v]; !ok {
			unknown = append(unknown, v)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func tooNewError(unknown []string) error {
	return fmt.Errorf("%w: unknown applied migration(s) %v; upgrade uMailServer", ErrSchemaTooNew, unknown)
}

// CheckCompatible returns ErrSchemaTooNew (wrapped) if the database holds
// migrations unknown to the registry. It is read-only and tolerates a
// database that has never been migrated.
func (m *Migrator) CheckCompatible() error {
	applied, err := m.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}
	if unknown := m.unknownApplied(applied); len(unknown) > 0 {
		return tooNewError(unknown)
	}
	return nil
}

// Migrate runs all pending migrations. Every pending migration and its
// version record run inside ONE write transaction: any failure rolls the
// whole batch back and leaves the database unchanged (F6621). Already
// applied versions are skipped, so re-running is idempotent.
func (m *Migrator) Migrate() error {
	// F7334: an empty or duplicated version would be silently skipped (the
	// second copy looks "already applied"), losing that migration.
	seen := make(map[string]struct{}, len(m.registry.migrations))
	for _, mg := range m.registry.migrations {
		if mg.Version == "" {
			return errors.New("migration with empty version")
		}
		if _, dup := seen[mg.Version]; dup {
			return fmt.Errorf("duplicate migration version %q", mg.Version)
		}
		seen[mg.Version] = struct{}{}
	}
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(migrationBucket))
		if err != nil {
			return fmt.Errorf("failed to create migration bucket: %w", err)
		}

		applied := make(map[string]migrationRecord)
		if err := bucket.ForEach(func(k, v []byte) error {
			var record migrationRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			applied[string(k)] = record
			return nil
		}); err != nil {
			return fmt.Errorf("failed to get applied migrations: %w", err)
		}
		if unknown := m.unknownApplied(applied); len(unknown) > 0 {
			return tooNewError(unknown)
		}

		for _, migration := range m.registry.migrations {
			if _, ok := applied[migration.Version]; ok {
				continue
			}
			if err := applyMigration(tx, bucket, migration); err != nil {
				return fmt.Errorf("failed to run migration %s: %w", migration.Version, err)
			}
			applied[migration.Version] = migrationRecord{Version: migration.Version}
		}
		return nil
	})
}

func applyMigration(tx *bbolt.Tx, bucket *bbolt.Bucket, migration Migration) error {
	if migration.Up == nil {
		return fmt.Errorf("migration %s has no up function", migration.Version)
	}
	if err := migration.Up(tx); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	data, err := json.Marshal(migrationRecord{Version: migration.Version, AppliedAt: time.Now()})
	if err != nil {
		return err
	}
	return bucket.Put([]byte(migration.Version), data)
}

// Rollback rolls back the last applied migration. The choice of the migration,
// its Down function and the removal of its record all happen inside ONE write
// transaction (F7333): choosing it from an earlier read let two concurrent
// rollbacks pick the same version and run its Down twice.
func (m *Migrator) Rollback() error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(migrationBucket))
		applied := make(map[string]migrationRecord)
		if bucket != nil {
			if err := bucket.ForEach(func(k, v []byte) error {
				var record migrationRecord
				if err := json.Unmarshal(v, &record); err != nil {
					return err
				}
				applied[string(k)] = record
				return nil
			}); err != nil {
				return fmt.Errorf("failed to get applied migrations: %w", err)
			}
		}
		if len(applied) == 0 {
			return fmt.Errorf("no migrations to rollback")
		}
		if unknown := m.unknownApplied(applied); len(unknown) > 0 {
			return tooNewError(unknown)
		}

		// Last applied = highest in registry order (F6621: AppliedAt ties
		// inside one batch made timestamp ordering nondeterministic).
		var migration *Migration
		for i := range m.registry.migrations {
			if _, ok := applied[m.registry.migrations[i].Version]; ok {
				migration = &m.registry.migrations[i]
			}
		}
		if migration == nil {
			return fmt.Errorf("no applied migration found in registry")
		}
		if migration.Down == nil {
			return fmt.Errorf("migration %s has no rollback", migration.Version)
		}
		if err := migration.Down(tx); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		return bucket.Delete([]byte(migration.Version))
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
