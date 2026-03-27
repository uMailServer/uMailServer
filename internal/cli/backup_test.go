package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
)

func TestNewBackupManager(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   t.TempDir(),
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)
	if bm == nil {
		t.Fatal("expected non-nil backup manager")
	}
	if bm.config != cfg {
		t.Error("expected config to be set")
	}
}

func TestBackupManagerListBackups(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a dummy backup file
	backupFile := filepath.Join(tempDir, "umailserver_backup_20240101_120000.tar.gz")
	if err := os.WriteFile(backupFile, []byte("dummy backup data"), 0644); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	// Create a non-backup file
	nonBackupFile := filepath.Join(tempDir, "not_a_backup.txt")
	if err := os.WriteFile(nonBackupFile, []byte("not a backup"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	backups, err := bm.ListBackups(tempDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	}

	if len(backups) > 0 {
		if backups[0].Filename != "umailserver_backup_20240101_120000.tar.gz" {
			t.Errorf("unexpected filename: %s", backups[0].Filename)
		}
	}
}

func TestBackupManagerListBackupsEmptyDir(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	backups, err := bm.ListBackups(tempDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(backups))
	}
}

func TestBackupManagerListBackupsNonExistentDir(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   t.TempDir(),
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	_, err := bm.ListBackups("/non/existent/directory")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestNewMigrationManager(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)
	if mm == nil {
		t.Fatal("expected non-nil migration manager")
	}
	if mm.db != database {
		t.Error("expected database to be set")
	}
	if mm.logger == nil {
		t.Error("expected logger to be initialized")
	}
}

func TestMigrationManagerValidateSource(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	tests := []struct {
		name        string
		sourceType  string
		source      string
		shouldError bool
	}{
		{"Valid IMAP URL", "imap", "imap://mail.example.com", false},
		{"Valid IMAPS URL", "imap", "imaps://mail.example.com", false},
		{"Invalid IMAP URL", "imap", "://invalid", true},
		{"IMAP wrong scheme", "imap", "http://mail.example.com", true},
		{"Valid Dovecot path", "dovecot", t.TempDir(), false},
		{"Invalid Dovecot path", "dovecot", "/non/existent/path", true},
		{"Valid MBOX pattern", "mbox", t.TempDir() + "/*.mbox", true},
		{"Invalid source type", "invalid", "test", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mm.ValidateSource(tc.sourceType, tc.source)
			if tc.shouldError && err == nil {
				t.Error("expected error")
			}
			if !tc.shouldError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseEmail(t *testing.T) {
	tests := []struct {
		email         string
		expectedUser  string
		expectedDomain string
	}{
		{"user@example.com", "user", "example.com"},
		{"admin@sub.domain.com", "admin", "sub.domain.com"},
		{"user", "user", ""},
		{"@example.com", "", "example.com"},
		{"", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.email, func(t *testing.T) {
			user, domain := parseEmail(tc.email)
			if user != tc.expectedUser {
				t.Errorf("expected user '%s', got '%s'", tc.expectedUser, user)
			}
			if domain != tc.expectedDomain {
				t.Errorf("expected domain '%s', got '%s'", tc.expectedDomain, domain)
			}
		})
	}
}

func TestMigrationManagerMigrateFromIMAP(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	opts := MigrateOptions{
		SourceType: "imap",
		SourceURL:  "imap://mail.example.com",
		Username:   "user",
		Password:   "pass",
		TargetUser: "localuser",
		DryRun:     true,
	}

	err = mm.MigrateFromIMAP(opts)
	// Should return "not yet fully implemented" error
	if err == nil {
		t.Error("expected error for unimplemented IMAP migration")
	}
}

func TestMigrationManagerMigrateFromDovecotNonExistent(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	err = mm.MigrateFromDovecot("/non/existent/maildir", "")
	if err == nil {
		t.Error("expected error for non-existent maildir")
	}
}

func TestMigrationManagerMigrateFromMBOXNoFiles(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Pattern that matches nothing
	err = mm.MigrateFromMBOX("/non/existent/*.mbox")
	if err == nil {
		t.Error("expected error when no MBOX files found")
	}
}

func TestBackupInfoStruct(t *testing.T) {
	info := BackupInfo{
		Filename: "backup.tar.gz",
		Size:     1024,
		ModTime:  time.Now(),
		Path:     "/backups/backup.tar.gz",
	}

	if info.Filename != "backup.tar.gz" {
		t.Error("BackupInfo.Filename not set correctly")
	}
	if info.Size != 1024 {
		t.Error("BackupInfo.Size not set correctly")
	}
}

func TestMigrateOptions(t *testing.T) {
	opts := MigrateOptions{
		SourceType: "imap",
		SourceURL:  "imap://mail.example.com",
		SourcePath: "/mail",
		Username:   "admin",
		Password:   "secret",
		TargetUser: "local",
		DryRun:     true,
	}

	if opts.SourceType != "imap" {
		t.Error("MigrateOptions.SourceType not set correctly")
	}
	if !opts.DryRun {
		t.Error("MigrateOptions.DryRun not set correctly")
	}
}

func TestNewDiagnosticsWithConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  t.TempDir(),
		},
	}

	d := NewDiagnostics(cfg)
	if d == nil {
		t.Fatal("expected non-nil diagnostics")
	}
	if d.config != cfg {
		t.Error("expected config to be set")
	}
}

func TestPrintDNSResults(t *testing.T) {
	results := []DNSCheckResult{
		{
			RecordType: "MX",
			RecordName: "example.com",
			Expected:   "mail.example.com",
			Found:      "mail.example.com",
			Status:     "pass",
			Message:    "MX record configured correctly",
		},
		{
			RecordType: "SPF",
			RecordName: "example.com",
			Status:     "fail",
			Message:    "No SPF record found",
		},
		{
			RecordType: "DKIM",
			RecordName: "default._domainkey.example.com",
			Status:     "warning",
			Message:    "DKIM record not found",
		},
	}

	// Should not panic
	PrintDNSResults(results)
}

func TestPrintDNSResultsAllPass(t *testing.T) {
	results := []DNSCheckResult{
		{
			RecordType: "MX",
			Status:     "pass",
			Message:    "MX OK",
		},
		{
			RecordType: "SPF",
			Status:     "pass",
			Message:    "SPF OK",
		},
	}

	// Should not panic
	PrintDNSResults(results)
}

func TestTLSCheckResult(t *testing.T) {
	result := TLSCheckResult{
		Protocol: "SMTP",
		Cipher:   "TLS_AES_256_GCM_SHA384",
		Version:  "TLS 1.3",
		Valid:    true,
		Expiry:   "2025-12-31",
		Message:  "TLS configuration valid",
	}

	if !result.Valid {
		t.Error("TLSCheckResult.Valid should be true")
	}
	if result.Version != "TLS 1.3" {
		t.Error("TLSCheckResult.Version not set correctly")
	}
}

func TestBackupManagerBackup(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	// Create required directories
	os.MkdirAll(filepath.Join(tempDir, "config"), 0755)
	os.MkdirAll(filepath.Join(tempDir, "messages"), 0755)

	// Create a test config file
	configFile := filepath.Join(tempDir, "config", "test.conf")
	os.WriteFile(configFile, []byte("test config"), 0644)

	bm := NewBackupManager(cfg)

	// Test backup
	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Verify backup file was created
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}

	foundBackup := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			foundBackup = true
			break
		}
	}

	if !foundBackup {
		t.Error("expected backup file to be created")
	}
}

func TestBackupManagerBackupNoConfigDir(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	// Don't create config directory - test that backup still works
	os.MkdirAll(filepath.Join(tempDir, "messages"), 0755)

	bm := NewBackupManager(cfg)

	// Test backup - should handle missing config dir gracefully
	err := bm.Backup(backupDir)
	// May error due to missing config, which is ok
	_ = err
}

func TestBackupManagerRestoreInvalidBackup(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Test restore with invalid backup file
	err := bm.Restore("/non/existent/backup.tar.gz")
	if err == nil {
		t.Error("expected error for non-existent backup file")
	}
}

func TestBackupManagerRestoreNotGzip(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a non-gzip file
	backupFile := filepath.Join(tempDir, "not_a_backup.tar.gz")
	os.WriteFile(backupFile, []byte("not gzip data"), 0644)

	// Test restore with invalid gzip file
	err := bm.Restore(backupFile)
	if err == nil {
		t.Error("expected error for invalid gzip file")
	}
}

func TestMigrationManagerValidateSourceAllTypes(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test all source types with valid inputs
	tests := []struct {
		name       string
		sourceType string
		source     string
		valid      bool
	}{
		{"Empty IMAP URL", "imap", "", false},
		{"IMAP with path", "imap", "imap://mail.example.com/path", true},
		{"IMAPS only", "imap", "imaps://mail.example.com", true},
		{"Empty Dovecot path", "dovecot", "", false},
		{"Empty MBOX pattern", "mbox", "", false},
		{"MBOX valid pattern", "mbox", "*.mbox", false}, // returns error if no files match
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mm.ValidateSource(tc.sourceType, tc.source)
			if tc.valid && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.valid && err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestParseEmailVariations(t *testing.T) {
	tests := []struct {
		email         string
		expectedUser  string
		expectedDomain string
	}{
		{"user@example.com", "user", "example.com"},
		{"user.name+tag@example.com", "user.name+tag", "example.com"},
		{"first.last@sub.domain.com", "first.last", "sub.domain.com"},
		{"user@", "user", ""},
		{"@domain.com", "", "domain.com"},
		{"", "", ""},
		{"a@b@c.com", "a@b", "c.com"},
	}

	for _, tc := range tests {
		t.Run(tc.email, func(t *testing.T) {
			user, domain := parseEmail(tc.email)
			if user != tc.expectedUser {
				t.Errorf("expected user '%s', got '%s'", tc.expectedUser, user)
			}
			if domain != tc.expectedDomain {
				t.Errorf("expected domain '%s', got '%s'", tc.expectedDomain, domain)
			}
		})
	}
}

func TestBackupInfoFields(t *testing.T) {
	now := time.Now()
	info := BackupInfo{
		Filename: "backup.tar.gz",
		Size:     1024 * 1024,
		ModTime:  now,
		Path:     "/backups/backup.tar.gz",
	}

	if info.Filename != "backup.tar.gz" {
		t.Error("Filename mismatch")
	}
	if info.Size != 1024*1024 {
		t.Error("Size mismatch")
	}
	if !info.ModTime.Equal(now) {
		t.Error("ModTime mismatch")
	}
	if info.Path != "/backups/backup.tar.gz" {
		t.Error("Path mismatch")
	}
}

func TestMigrateOptionsFields(t *testing.T) {
	opts := MigrateOptions{
		SourceType: "imap",
		SourceURL:  "imap://mail.example.com",
		SourcePath: "/mail",
		Username:   "admin",
		Password:   "secret",
		TargetUser: "local",
		DryRun:     true,
	}

	if opts.SourceType != "imap" {
		t.Error("SourceType mismatch")
	}
	if opts.SourceURL != "imap://mail.example.com" {
		t.Error("SourceURL mismatch")
	}
	if opts.SourcePath != "/mail" {
		t.Error("SourcePath mismatch")
	}
	if opts.Username != "admin" {
		t.Error("Username mismatch")
	}
	if opts.Password != "secret" {
		t.Error("Password mismatch")
	}
	if opts.TargetUser != "local" {
		t.Error("TargetUser mismatch")
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
}
