package migrations

import (
	"fmt"
	"testing"

	"go.etcd.io/bbolt"
)

// TestInitMigrations_Coverage tests InitMigrations
func TestInitMigrations_Coverage(t *testing.T) {
	registry := NewRegistry()
	InitMigrations(registry)

	// Should have registered migrations
	if len(registry.migrations) == 0 {
		t.Error("InitMigrations should register migrations")
	}
}

// TestMigrate_WithInitMigrations tests Migrate with InitMigrations
func TestMigrate_WithInitMigrations(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	InitMigrations(registry)

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Check status after migration
	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.Applied == 0 {
		t.Error("should have applied migrations")
	}
}


// TestMigrate_AlreadyApplied tests Migrate when migrations already applied
func TestMigrate_AlreadyApplied(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	callCount := 0
	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			callCount++
			return nil
		},
	})

	m := NewMigrator(db, registry)

	// First migrate
	err := m.Migrate()
	if err != nil {
		t.Fatalf("First migrate failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second migrate - should not call Up again
	err = m.Migrate()
	if err != nil {
		t.Fatalf("Second migrate failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected still 1 call, got %d", callCount)
	}
}

// TestRollback_NoMigrations tests Rollback when no migrations applied
func TestRollback_NoMigrations(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	m := NewMigrator(db, registry)

	// Try to rollback without migrating first
	err := m.Rollback()
	if err == nil {
		t.Error("expected error when rolling back with no migrations")
	}
}

// TestStatus_NoMigrations tests Status when no migrations
func TestStatus_NoMigrations(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	m := NewMigrator(db, registry)

	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.Applied != 0 {
		t.Errorf("expected 0 applied migrations, got %d", status.Applied)
	}

	if status.Pending != 0 {
		t.Errorf("expected 0 pending migrations, got %d", status.Pending)
	}
}

// TestRunMigration_UpError tests runMigration with Up error
func TestRunMigration_UpError(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			return fmt.Errorf("up error")
		},
	})

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err == nil {
		t.Error("expected error from Up")
	}
}

// TestMigrate_EmptyRegistry tests Migrate with empty registry
func TestMigrate_EmptyRegistry(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()
	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
}

// TestRunMigration_NilUp tests runMigration with nil Up function
func TestRunMigration_NilUp(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Nil up migration",
		Up:         nil,
		Down:       nil,
	})

	m := NewMigrator(db, registry)

	err := m.Migrate()
	if err == nil {
		t.Error("expected error when migration Up is nil")
	}
}

// TestRollback_WithMigration tests Rollback after migration
func TestRollback_WithMigration(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			return nil
		},
		Down: func(tx *bbolt.Tx) error {
			return nil
		},
	})

	m := NewMigrator(db, registry)

	// Apply migration
	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Rollback
	err = m.Rollback()
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
}

// TestRollback_LastMigrationError tests Rollback when last migration Down returns error
func TestRollback_LastMigrationError(t *testing.T) {
	db := setupTestDB(t)
	registry := NewRegistry()

	registry.Register(Migration{
		Version:     "001",
		Description: "Test migration",
		Up: func(tx *bbolt.Tx) error {
			return nil
		},
		Down: func(tx *bbolt.Tx) error {
			return fmt.Errorf("rollback failed intentionally")
		},
	})

	m := NewMigrator(db, registry)

	// Apply migration
	err := m.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Rollback should fail
	err = m.Rollback()
	if err == nil {
		t.Error("expected error when Rollback Down fails")
	}
}
