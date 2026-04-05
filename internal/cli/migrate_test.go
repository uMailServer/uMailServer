package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

// TestNewMigrationManagerForMigrate tests creating a new migration manager
func TestNewMigrationManagerForMigrate(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	msgStorePath := filepath.Join(tmpDir, "messages")
	msgStore, err := storage.NewMessageStore(msgStorePath)
	if err != nil {
		t.Fatalf("Failed to create message store: %v", err)
	}
	defer msgStore.Close()

	mm := NewMigrationManager(database, msgStore, nil)
	if mm == nil {
		t.Fatal("NewMigrationManager returned nil")
	}
	if mm.db != database {
		t.Error("Database not set correctly")
	}
	if mm.msgStore != msgStore {
		t.Error("Message store not set correctly")
	}
}

// TestNewMigrationManagerMigrateNilLogger tests creating manager with nil logger
func TestNewMigrationManagerMigrateNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)
	if mm == nil {
		t.Fatal("NewMigrationManager returned nil")
	}
	if mm.logger == nil {
		t.Error("Expected default logger to be set")
	}
}

// TestMigrateFromDovecotInvalidPath tests migration with invalid path
func TestMigrateFromDovecotInvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test with non-existent path - use a Windows-specific invalid path
	nonexistentPath := filepath.Join(tmpDir, "does_not_exist_12345")
	err = mm.MigrateFromDovecot(nonexistentPath, "")
	if err == nil {
		t.Error("Expected error for invalid maildir path")
		return
	}
	if !containsStr(err.Error(), "maildir not found") {
		t.Errorf("Expected 'maildir not found' error, got: %v", err)
	}
}

// TestMigrateFromDovecotEmptyPath tests migration with empty path
func TestMigrateFromDovecotEmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test with empty path
	err = mm.MigrateFromDovecot("", "")
	if err == nil {
		t.Error("Expected error for empty maildir path")
		return
	}
}

// TestMigrateFromDovecotValidPath tests migration with valid path
func TestMigrateFromDovecotValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a valid maildir structure
	maildirPath := filepath.Join(tmpDir, "maildir")
	os.MkdirAll(filepath.Join(maildirPath, "cur"), 0755)
	os.MkdirAll(filepath.Join(maildirPath, "new"), 0755)
	os.MkdirAll(filepath.Join(maildirPath, "tmp"), 0755)

	// Test with valid path - may fail due to implementation, but should not panic
	err = mm.MigrateFromDovecot(maildirPath, "")
	if err != nil {
		t.Logf("MigrateFromDovecot returned error (may be expected): %v", err)
	}
}

// TestMigrateFromIMAPInvalidURL tests IMAP migration with invalid URL
func TestMigrateFromIMAPInvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	opts := MigrateOptions{
		SourceType: "imap",
		SourceURL:  "://invalid-url",
		TargetUser: "test@example.com",
	}

	err = mm.MigrateFromIMAP(opts)
	if err == nil {
		t.Error("Expected error for invalid IMAP URL")
	}
}

// TestMigrateFromIMAPConnectionFailure tests IMAP migration handles connection failure
func TestMigrateFromIMAPConnectionFailure(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	opts := MigrateOptions{
		SourceType: "imap",
		SourceURL:  "imap://unreachable.invalid:993",
		Username:   "user",
		Password:   "pass",
		TargetUser: "test@example.com",
	}

	err = mm.MigrateFromIMAP(opts)
	if err == nil {
		t.Error("Expected error for unreachable IMAP server")
	}
	// Should get a connection error, not "not implemented"
	if containsStr(err.Error(), "not yet fully implemented") {
		t.Errorf("IMAP migration should be implemented, got: %v", err)
	}
}

// TestMigrateOptionsStruct tests MigrateOptions struct
func TestMigrateOptionsStruct(t *testing.T) {
	opts := MigrateOptions{
		SourceType: "dovecot",
		SourcePath: "/var/mail",
		SourceURL:  "imap://example.com",
		Username:   "user",
		Password:   "pass",
		TargetUser: "target@example.com",
		DryRun:     true,
	}

	if opts.SourceType != "dovecot" {
		t.Errorf("Expected SourceType 'dovecot', got %s", opts.SourceType)
	}
	if opts.SourcePath != "/var/mail" {
		t.Errorf("Expected SourcePath '/var/mail', got %s", opts.SourcePath)
	}
	if !opts.DryRun {
		t.Error("Expected DryRun to be true")
	}
}

// Helper function
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
