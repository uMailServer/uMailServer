package migrations

import (
	"os"
	"testing"

	"go.etcd.io/bbolt"
	boltErr "go.etcd.io/bbolt/errors"
)

func setupTestDB(t *testing.T) *bbolt.DB {
	t.Helper()
	tmpFile := t.TempDir() + "/test.db"
	db, err := bbolt.Open(tmpFile, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(tmpFile)
	})
	return db
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.migrations) != 0 {
		t.Error("new registry should have no migrations")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	m := Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			return nil
		},
	}

	r.Register(m)

	if len(r.migrations) != 1 {
		t.Errorf("expected 1 migration, got %d", len(r.migrations))
	}

	if r.migrations[0].Version != "001" {
		t.Errorf("expected version 001, got %s", r.migrations[0].Version)
	}
}

func TestNewMigrator(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	m := NewMigrator(db, registry)
	if m == nil {
		t.Fatal("NewMigrator returned nil")
	}
	if m.db != db {
		t.Error("migrator db mismatch")
	}
	if m.registry != registry {
		t.Error("migrator registry mismatch")
	}
}

func TestMigrator_Migrate(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	// Add a test migration
	migrationRan := false
	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			migrationRan = true
			return nil
		},
	})

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	if !migrationRan {
		t.Error("migration was not run")
	}

	// Running again should not run the migration
	migrationRan = false
	err = m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed on second run: %v", err)
	}

	if migrationRan {
		t.Error("migration ran twice")
	}
}

func TestMigrator_MigrateMultiple(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	runOrder := []string{}

	registry.Register(Migration{
		Version:     "001",
		Description: "First migration",
		Up: func(tx *bbolt.Tx) error {
			runOrder = append(runOrder, "001")
			return nil
		},
	})

	registry.Register(Migration{
		Version:     "002",
		Description: "Second migration",
		Up: func(tx *bbolt.Tx) error {
			runOrder = append(runOrder, "002")
			return nil
		},
	})

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	if len(runOrder) != 2 {
		t.Fatalf("expected 2 migrations to run, got %d", len(runOrder))
	}

	if runOrder[0] != "001" || runOrder[1] != "002" {
		t.Errorf("wrong run order: %v", runOrder)
	}
}

func TestMigrator_MigrateWithError(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Failing migration",
		Up: func(tx *bbolt.Tx) error {
			return boltErr.ErrDatabaseNotOpen
		},
	})

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err == nil {
		t.Error("expected error from failing migration")
	}
}

func TestMigrator_Status(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			return nil
		},
	})

	m := NewMigrator(db, registry)

	// Before migration
	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.Applied != 0 {
		t.Errorf("expected 0 applied, got %d", status.Applied)
	}

	if status.Pending != 1 {
		t.Errorf("expected 1 pending, got %d", status.Pending)
	}

	if status.Total != 1 {
		t.Errorf("expected 1 total, got %d", status.Total)
	}

	// After migration
	err = m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	status, err = m.Status()
	if err != nil {
		t.Fatalf("Status failed after migrate: %v", err)
	}

	if status.Applied != 1 {
		t.Errorf("expected 1 applied after migrate, got %d", status.Applied)
	}

	if status.Pending != 0 {
		t.Errorf("expected 0 pending after migrate, got %d", status.Pending)
	}
}

func TestMigrator_Rollback(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	downRan := false

	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			return nil
		},
		Down: func(tx *bbolt.Tx) error {
			downRan = true
			return nil
		},
	})

	m := NewMigrator(db, registry)

	// Apply migration first
	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Rollback
	err = m.Rollback()
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if !downRan {
		t.Error("down migration was not run")
	}
}

func TestMigrator_RollbackNoMigrations(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	m := NewMigrator(db, registry)

	err := m.Rollback()
	if err == nil {
		t.Error("expected error when rolling back with no migrations")
	}
}

func TestMigrator_RollbackNoDown(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration without down",
		Up: func(tx *bbolt.Tx) error {
			return nil
		},
		// No Down function
	})

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	err = m.Rollback()
	if err == nil {
		t.Error("expected error when migration has no down function")
	}
}

func TestInitMigrations(t *testing.T) {
	r := NewRegistry()
	InitMigrations(r)

	if len(r.migrations) == 0 {
		t.Error("InitMigrations should register migrations")
	}

	// Check that 001 is registered
	found := false
	for _, m := range r.migrations {
		if m.Version == "001" {
			found = true
			break
		}
	}

	if !found {
		t.Error("migration 001 should be registered")
	}
}
