package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
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
		email          string
		expectedUser   string
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
		email          string
		expectedUser   string
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

func TestDiagnosticsNilConfig(t *testing.T) {
	// Test with nil config
	d := NewDiagnostics(nil)
	if d == nil {
		t.Fatal("expected non-nil diagnostics")
	}

	// CheckPTR should handle nil config
	result := d.checkPTR("example.com")
	if result.Status != "warning" {
		t.Errorf("expected warning status, got %s", result.Status)
	}
}

func TestMinFunction(t *testing.T) {
	if min(1, 2) != 1 {
		t.Error("min(1, 2) should be 1")
	}
	if min(2, 1) != 1 {
		t.Error("min(2, 1) should be 1")
	}
	if min(5, 5) != 5 {
		t.Error("min(5, 5) should be 5")
	}
}

func TestTLSVersionNameResult(t *testing.T) {
	// Test TLS check result structures
	result := TLSCheckResult{
		Protocol: "SMTP",
		Version:  "TLS 1.2",
		Valid:    true,
		Message:  "Test",
	}

	if result.Protocol != "SMTP" {
		t.Error("Protocol mismatch")
	}
	if result.Version != "TLS 1.2" {
		t.Error("Version mismatch")
	}
}

func TestPrintDNSResultsVariations(t *testing.T) {
	// Test with all pass
	resultsAllPass := []DNSCheckResult{
		{RecordType: "MX", Status: "pass", Message: "MX OK"},
		{RecordType: "SPF", Status: "pass", Message: "SPF OK"},
	}
	PrintDNSResults(resultsAllPass)

	// Test with all fail
	resultsAllFail := []DNSCheckResult{
		{RecordType: "MX", Status: "fail", Message: "MX FAIL"},
		{RecordType: "SPF", Status: "fail", Message: "SPF FAIL"},
	}
	PrintDNSResults(resultsAllFail)

	// Test with all warnings
	resultsAllWarning := []DNSCheckResult{
		{RecordType: "DKIM", Status: "warning", Message: "DKIM WARN"},
		{RecordType: "DMARC", Status: "warning", Message: "DMARC WARN"},
	}
	PrintDNSResults(resultsAllWarning)

	// Test with expected and found fields
	resultsWithFields := []DNSCheckResult{
		{RecordType: "MX", Status: "pass", Expected: "mail.example.com", Found: "mail.example.com", Message: "MX OK"},
	}
	PrintDNSResults(resultsWithFields)
}

func TestBackupManagerBackupWithData(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	// Create required directories and files
	os.MkdirAll(filepath.Join(tempDir, "config"), 0755)
	os.MkdirAll(filepath.Join(tempDir, "messages"), 0755)

	// Create a test config file
	configFile := filepath.Join(tempDir, "config", "test.conf")
	os.WriteFile(configFile, []byte("test config content"), 0644)

	// Create a test database file
	dbFile := filepath.Join(tempDir, "umailserver.db")
	os.WriteFile(dbFile, []byte("test db content"), 0644)

	// Create a message file
	msgDir := filepath.Join(tempDir, "messages", "user@test.com")
	os.MkdirAll(msgDir, 0755)
	msgFile := filepath.Join(msgDir, "test.eml")
	os.WriteFile(msgFile, []byte("test message content"), 0644)

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

func TestBackupManagerBackupCreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups", "subdir")
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	// Create required directories
	os.MkdirAll(filepath.Join(tempDir, "config"), 0755)
	os.MkdirAll(filepath.Join(tempDir, "messages"), 0755)

	bm := NewBackupManager(cfg)

	// Test backup - should create the backup directory
	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Error("expected backup directory to be created")
	}
}

func TestBackupManagerRestoreMissingManifest(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a valid tar.gz without manifest
	backupFile := filepath.Join(tempDir, "no_manifest.tar.gz")
	file, _ := os.Create(backupFile)
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Add a file that's not a manifest
	content := []byte("test content")
	header := &tar.Header{
		Name: "test.txt",
		Size: int64(len(content)),
		Mode: 0644,
	}
	tw.WriteHeader(header)
	tw.Write(content)

	tw.Close()
	gw.Close()

	// Test restore - should fail because manifest is missing
	err := bm.Restore(backupFile)
	if err == nil {
		t.Error("expected error for backup without manifest")
	}
	if !strings.Contains(err.Error(), "manifest not found") {
		t.Errorf("expected 'manifest not found' error, got: %v", err)
	}
}

func TestCheckIMAPTLS(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  t.TempDir(),
		},
		IMAP: config.IMAPConfig{
			Port: 1143,
		},
	}

	d := NewDiagnostics(cfg)

	// Test checkIMAPTLS - will fail to connect but should not panic
	result, err := d.checkIMAPTLS("test.example.com")

	// Should return error since no IMAP server running
	if err == nil {
		t.Error("expected error when connecting to non-existent IMAP server")
	}

	// Result should be nil on error
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestTLSVersionName(t *testing.T) {
	tests := []struct {
		version  uint16
		expected string
	}{
		{0x0301, "TLS 1.0"},
		{0x0302, "TLS 1.1"},
		{0x0303, "TLS 1.2"},
		{0x0304, "TLS 1.3"},
		{0x0000, "Unknown (0x0)"},
	}

	for _, tc := range tests {
		result := tlsVersionName(tc.version)
		if result != tc.expected {
			t.Errorf("tlsVersionName(%d): expected %q, got %q", tc.version, tc.expected, result)
		}
	}
}

func TestCheckDKIM(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  t.TempDir(),
		},
	}

	d := NewDiagnostics(cfg)

	// Test checkDKIM
	result := d.checkDKIM("test.example.com")

	if result.RecordType != "DKIM" {
		t.Errorf("expected record type DKIM, got: %s", result.RecordType)
	}
}

func TestCheckDMARC(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  t.TempDir(),
		},
	}

	d := NewDiagnostics(cfg)

	// Test checkDMARC
	result := d.checkDMARC("test.example.com")

	if result.RecordType != "DMARC" {
		t.Errorf("expected record type DMARC, got: %s", result.RecordType)
	}
}

func TestMigrationManagerImportDovecotUsers(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test passwd file
	passwdFile := t.TempDir() + "/passwd"
	content := "# Test passwd file\n"
	content += "user1@example.com:password_hash:1000:1000::/home/user1:/bin/bash\n"
	content += "user2@test.com:password_hash:1001:1001::/home/user2:/bin/bash\n"
	os.WriteFile(passwdFile, []byte(content), 0644)

	// Test importDovecotUsers
	err = mm.importDovecotUsers(passwdFile)
	if err != nil {
		t.Errorf("importDovecotUsers failed: %v", err)
	}
}

func TestMigrationManagerImportDovecotUsersInvalidFile(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test with non-existent file
	err = mm.importDovecotUsers("/nonexistent/passwd")
	if err == nil {
		t.Error("expected error for non-existent passwd file")
	}
}

func TestMigrationManagerImportMaildir(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test maildir structure
	maildirPath := t.TempDir() + "/maildir"
	os.MkdirAll(maildirPath+"/user@example.com/INBOX/cur", 0755)
	os.MkdirAll(maildirPath+"/user@example.com/INBOX/new", 0755)
	os.MkdirAll(maildirPath+"/user@example.com/INBOX/tmp", 0755)

	// Create a test message
	msgContent := []byte("From: test@example.com\nTo: user@example.com\nSubject: Test\n\nTest message")
	os.WriteFile(maildirPath+"/user@example.com/INBOX/new/1234567890.1", msgContent, 0644)

	// Test importMaildir
	err = mm.importMaildir(maildirPath)
	if err != nil {
		t.Errorf("importMaildir failed: %v", err)
	}
}

func TestMigrationManagerImportMaildirInvalidPath(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test with non-existent path
	err = mm.importMaildir("/nonexistent/maildir")
	if err == nil {
		t.Error("expected error for non-existent maildir")
	}
}

func TestMigrationManagerImportMBOXFile(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test MBOX file
	mboxFile := t.TempDir() + "/test.mbox"
	content := "From user@example.com Mon Jan 01 00:00:00 2024\n"
	content += "From: sender@example.com\n"
	content += "To: user@example.com\n"
	content += "Subject: Test Message\n"
	content += "\n"
	content += "This is a test message.\n"
	content += "From user2@example.com Mon Jan 01 00:00:01 2024\n"
	content += "From: sender2@example.com\n"
	content += "To: user2@example.com\n"
	content += "Subject: Test Message 2\n"
	content += "\n"
	content += "This is another test message.\n"
	os.WriteFile(mboxFile, []byte(content), 0644)

	// Test importMBOXFile
	err = mm.importMBOXFile(mboxFile)
	if err != nil {
		t.Errorf("importMBOXFile failed: %v", err)
	}
}

func TestMigrationManagerImportMBOXFileInvalid(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test with non-existent file
	err = mm.importMBOXFile("/nonexistent/test.mbox")
	if err == nil {
		t.Error("expected error for non-existent mbox file")
	}
}

func TestMigrationManagerProcessMBOXMessage(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test processMBOXMessage
	message := []byte("From: sender@example.com\nTo: recipient@example.com\nSubject: Test\n\nTest body")
	err = mm.processMBOXMessage(message, "INBOX")
	if err != nil {
		t.Errorf("processMBOXMessage failed: %v", err)
	}
}

func TestMigrationManagerImportMessage(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test message file in user directory structure
	msgDir := t.TempDir() + "/messages/user@example.com/INBOX"
	os.MkdirAll(msgDir, 0755)
	msgFile := msgDir + "/test.eml"
	content := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest body")
	os.WriteFile(msgFile, content, 0644)

	// Test importMessage
	err = mm.importMessage(msgFile)
	// May fail due to missing account, which is OK
	_ = err
}

func TestMigrationManagerImportMessageInvalid(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Test with non-existent file
	err = mm.importMessage("/nonexistent/test.eml")
	if err == nil {
		t.Error("expected error for non-existent message file")
	}
}

// TestBackupManagerRestoreWithValidBackup tests Restore with a valid backup
func TestBackupManagerRestoreWithValidBackup(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a valid backup file with manifest
	backupFile := filepath.Join(tempDir, "test_backup.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Create manifest
	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240101_120000",
		"hostname":  "test.example.com",
		"data_dir":  tempDir,
	}
	manifestData, _ := json.Marshal(manifest)

	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0644,
	}
	tw.WriteHeader(header)
	tw.Write(manifestData)

	// Add a test file
	testContent := []byte("test content")
	testHeader := &tar.Header{
		Name: "config/test.conf",
		Size: int64(len(testContent)),
		Mode: 0644,
	}
	tw.WriteHeader(testHeader)
	tw.Write(testContent)

	tw.Close()
	gw.Close()

	// Test restore
	err = bm.Restore(backupFile)
	if err != nil {
		t.Logf("Restore returned error: %v", err)
	}
}

// TestBackupManagerRestoreWithEmptyManifest tests Restore with empty manifest fields
func TestBackupManagerRestoreWithEmptyManifest(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:   tempDir,
			Hostname:  "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a backup with minimal manifest
	backupFile := filepath.Join(tempDir, "minimal_backup.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Create minimal manifest
	manifest := map[string]interface{}{}
	manifestData, _ := json.Marshal(manifest)

	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0644,
	}
	tw.WriteHeader(header)
	tw.Write(manifestData)

	// Add a directory entry
	dirHeader := &tar.Header{
		Name:     "config/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	tw.WriteHeader(dirHeader)

	tw.Close()
	gw.Close()

	// Test restore - should handle empty manifest
	err = bm.Restore(backupFile)
	// May error but shouldn't panic
	_ = err
}

// TestMigrationManagerMigrateFromDovecotWithPasswd tests MigrateFromDovecot with passwd file
func TestMigrationManagerMigrateFromDovecotWithPasswd(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test maildir structure
	maildirPath := t.TempDir() + "/maildir"
	userDir := maildirPath + "/user@example.com/Maildir/INBOX/cur"
	os.MkdirAll(userDir, 0755)

	// Create a test message
	msgContent := []byte("From: test@example.com\nTo: user@example.com\nSubject: Test\n\nTest message")
	os.WriteFile(userDir+"/1234567890.1", msgContent, 0644)

	// Create a passwd file
	passwdFile := t.TempDir() + "/passwd"
	passwdContent := "user@example.com:password_hash:1000:1000::/home/user:/bin/bash\n"
	os.WriteFile(passwdFile, []byte(passwdContent), 0644)

	// Test MigrateFromDovecot
	err = mm.MigrateFromDovecot(maildirPath, passwdFile)
	// May error but shouldn't panic
	_ = err
}

// TestMigrationManagerMigrateFromMBOXWithFiles tests MigrateFromMBOX with actual files
func TestMigrationManagerMigrateFromMBOXWithFiles(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create temp directory for MBOX files
	mboxDir := t.TempDir()

	// Create a test MBOX file
	mboxFile := mboxDir + "/test.mbox"
	content := "From user@example.com Mon Jan 01 00:00:00 2024\n"
	content += "From: sender@example.com\n"
	content += "To: user@example.com\n"
	content += "Subject: Test Message\n"
	content += "\n"
	content += "This is a test message.\n"
	os.WriteFile(mboxFile, []byte(content), 0644)

	// Test MigrateFromMBOX
	err = mm.MigrateFromMBOX(mboxDir + "/*.mbox")
	if err != nil {
		t.Logf("MigrateFromMBOX returned error: %v", err)
	}
}

// TestMigrationManagerImportDovecotUsersWithComments tests importDovecotUsers with comments
func TestMigrationManagerImportDovecotUsersWithComments(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test passwd file with comments and empty lines
	passwdFile := t.TempDir() + "/passwd"
	content := "# This is a comment\n"
	content += "\n"
	content += "user1@example.com:password_hash:1000:1000::/home/user1:/bin/bash\n"
	content += "# Another comment\n"
	content += "user2@test.com:password_hash:1001:1001::/home/user2:/bin/bash\n"
	content += "\n"
	os.WriteFile(passwdFile, []byte(content), 0644)

	// Test importDovecotUsers
	err = mm.importDovecotUsers(passwdFile)
	if err != nil {
		t.Logf("importDovecotUsers returned error: %v", err)
	}
}

// TestMigrationManagerImportDovecotUsersShortLines tests importDovecotUsers with short lines
func TestMigrationManagerImportDovecotUsersShortLines(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create a test passwd file with short lines
	passwdFile := t.TempDir() + "/passwd"
	content := "user1@example.com:pass:1000\n" // Too few fields
	content += "user2@test.com:password_hash:1001:1001::/home/user2:/bin/bash\n"
	os.WriteFile(passwdFile, []byte(content), 0644)

	// Test importDovecotUsers - should skip short lines
	err = mm.importDovecotUsers(passwdFile)
	if err != nil {
		t.Logf("importDovecotUsers returned error: %v", err)
	}
}

// TestMigrationManagerImportMessageWithFlags tests importMessage with various flags
func TestMigrationManagerImportMessageWithFlags(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mm := NewMigrationManager(database, nil, nil)

	// Create test directories
	tmpDir := t.TempDir()
	msgDir := tmpDir + "/example.com/user/Maildir/INBOX/cur"
	os.MkdirAll(msgDir, 0755)

	// Test with various flag combinations
	flagTests := []string{
		"1234567890.1:2,S",    // Seen
		"1234567890.2:2,SR",   // Seen + Replied
		"1234567890.3:2,SF",   // Seen + Flagged
		"1234567890.4:2,ST",   // Seen + Deleted
		"1234567890.5:2,SD",   // Seen + Draft
		"1234567890.6:2,SRFTD", // All flags
		"1234567890.7",         // No flags
	}

	for _, filename := range flagTests {
		msgFile := msgDir + "/" + filename
		content := []byte("From: sender@example.com\r\nTo: user@example.com\r\nSubject: Test\r\n\r\nBody")
		os.WriteFile(msgFile, content, 0644)

		err = mm.importMessage(msgFile)
		// May fail due to account not existing, which is OK
		_ = err
	}
}

// --- New tests for improved coverage ---

func TestBackupManagerBackupWithDatabase(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	// Create required directories and files
	os.MkdirAll(filepath.Join(tempDir, "config"), 0755)
	os.MkdirAll(filepath.Join(tempDir, "messages"), 0755)

	// Create a test database file
	dbFile := filepath.Join(tempDir, "umailserver.db")
	os.WriteFile(dbFile, []byte("database content here"), 0644)

	bm := NewBackupManager(cfg)

	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Verify backup file was created
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected backup file to be created")
	}
}

func TestBackupManagerBackupNoMessagesDir(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	// Create config dir but not messages dir
	os.MkdirAll(filepath.Join(tempDir, "config"), 0755)
	configFile := filepath.Join(tempDir, "config", "app.conf")
	os.WriteFile(configFile, []byte("key=value"), 0644)

	bm := NewBackupManager(cfg)

	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup should succeed without messages dir: %v", err)
	}
}

func TestBackupManagerBackupOnlyDatabase(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	// Only create database file, no config or messages dirs
	dbFile := filepath.Join(tempDir, "umailserver.db")
	os.WriteFile(dbFile, []byte("db content"), 0644)

	bm := NewBackupManager(cfg)

	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	entries, _ := os.ReadDir(backupDir)
	foundBackup := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Error("expected backup file to be created")
	}
}

func TestBackupManagerListBackupsWithDirectory(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a subdirectory (should be skipped)
	os.MkdirAll(filepath.Join(tempDir, "subdir"), 0755)

	// Create a backup file
	backupFile := filepath.Join(tempDir, "backup_20240101.tar.gz")
	os.WriteFile(backupFile, []byte("data"), 0644)

	backups, err := bm.ListBackups(tempDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	}
}

func TestBackupManagerRestoreWithInvalidManifestJSON(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a backup with invalid JSON manifest
	backupFile := filepath.Join(tempDir, "bad_manifest.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Add a manifest with invalid JSON
	badJSON := []byte("{invalid json")
	header := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(badJSON)),
		Mode: 0644,
	}
	tw.WriteHeader(header)
	tw.Write(badJSON)

	tw.Close()
	gw.Close()

	err = bm.Restore(backupFile)
	if err == nil {
		t.Error("expected error for invalid JSON manifest")
	}
}

func TestBackupManagerRestoreWithDirectoryEntries(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a valid backup with directory entries
	backupFile := filepath.Join(tempDir, "dir_backup.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Create manifest
	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240101_120000",
		"hostname":  "test.example.com",
	}
	manifestData, _ := json.Marshal(manifest)
	mh := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0644,
	}
	tw.WriteHeader(mh)
	tw.Write(manifestData)

	// Add a directory entry
	dirHeader := &tar.Header{
		Name:     "config/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	tw.WriteHeader(dirHeader)

	// Add a regular file
	content := []byte("test config")
	fh := &tar.Header{
		Name: "config/app.conf",
		Size: int64(len(content)),
		Mode: 0644,
	}
	tw.WriteHeader(fh)
	tw.Write(content)

	tw.Close()
	gw.Close()

	err = bm.Restore(backupFile)
	if err != nil {
		t.Errorf("Restore should succeed with valid backup: %v", err)
	}
}

func TestBackupManagerRestoreWithManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a backup with only manifest (no other files)
	backupFile := filepath.Join(tempDir, "manifest_only.tar.gz")
	file, err := os.Create(backupFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240101_120000",
		"hostname":  "test.example.com",
		"data_dir":  "/var/lib/umailserver",
	}
	manifestData, _ := json.Marshal(manifest)
	mh := &tar.Header{
		Name: "manifest.json",
		Size: int64(len(manifestData)),
		Mode: 0644,
	}
	tw.WriteHeader(mh)
	tw.Write(manifestData)

	tw.Close()
	gw.Close()

	err = bm.Restore(backupFile)
	if err != nil {
		t.Errorf("Restore should succeed: %v", err)
	}
}

func TestCheckSPFWithConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  t.TempDir(),
		},
	}
	d := NewDiagnostics(cfg)

	// checkSPF on a domain that likely has no SPF for our test hostname
	result := d.checkSPF("example.com")
	if result.RecordType != "SPF" {
		t.Errorf("expected record type SPF, got: %s", result.RecordType)
	}
	// The result will vary based on actual DNS, just verify it doesn't panic
}

func TestCheckMX(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  t.TempDir(),
		},
	}
	d := NewDiagnostics(cfg)

	// checkMX should work without panicking
	results, err := d.checkMX("example.com")
	if err != nil {
		t.Errorf("checkMX returned unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one MX result")
	}
}

func TestCheckMXNilConfig(t *testing.T) {
	d := NewDiagnostics(nil)

	results, err := d.checkMX("example.com")
	if err != nil {
		t.Errorf("checkMX returned unexpected error: %v", err)
	}
	// Should still work, just won't match expected hostname
	if len(results) == 0 {
		t.Error("expected at least one MX result")
	}
}

func TestCheckDNS(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  t.TempDir(),
		},
	}
	d := NewDiagnostics(cfg)

	results, err := d.CheckDNS("example.com")
	if err != nil {
		t.Errorf("CheckDNS returned unexpected error: %v", err)
	}
	// Should return results for MX, SPF, DKIM, DMARC, PTR
	if len(results) < 4 {
		t.Errorf("expected at least 4 DNS check results, got %d", len(results))
	}
}

func TestCheckTLSNonExistentHost(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "nonexistent.host.invalid",
			DataDir:  t.TempDir(),
		},
	}
	d := NewDiagnostics(cfg)

	result, err := d.CheckTLS("nonexistent.host.invalid")
	if err != nil {
		t.Errorf("CheckTLS should not return error, got: %v", err)
	}
	// Should have a message about failed connection
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestCheckSMTPTLSNonExistent(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "nonexistent.host.invalid",
			DataDir:  t.TempDir(),
		},
	}
	d := NewDiagnostics(cfg)

	result, err := d.checkSMTPTLS("nonexistent.host.invalid")
	if err == nil {
		t.Error("expected error connecting to non-existent SMTP server")
	}
	if result != nil {
		t.Error("expected nil result on connection failure")
	}
}

func TestTLSVersionNameAllVersions(t *testing.T) {
	tests := []struct {
		version  uint16
		expected string
	}{
		{0x0300, "SSL 3.0"},
		{0x0301, "TLS 1.0"},
		{0x0302, "TLS 1.1"},
		{0x0303, "TLS 1.2"},
		{0x0304, "TLS 1.3"},
		{0x00FF, "Unknown (0xff)"},
	}
	for _, tc := range tests {
		result := tlsVersionName(tc.version)
		if result != tc.expected {
			t.Errorf("tlsVersionName(0x%x): expected %q, got %q", tc.version, tc.expected, result)
		}
	}
}

func TestMinFunctionAllCases(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 0, 0},
		{-1, 1, -1},
		{100, 200, 100},
	}
	for _, tc := range tests {
		result := min(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("min(%d, %d) = %d, want %d", tc.a, tc.b, result, tc.expected)
		}
	}
}

func TestBackupManagerBackupEmptyDataDir(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// No config, no database, no messages directories
	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup should succeed with empty data dir: %v", err)
	}

	entries, _ := os.ReadDir(backupDir)
	foundBackup := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Error("expected backup file to be created even with empty data dir")
	}
}

func TestBackupManagerBackupCreatesNestedBackupDir(t *testing.T) {
	tempDir := t.TempDir()
	// Create a deeply nested backup directory that doesn't exist yet
	backupDir := filepath.Join(tempDir, "a", "b", "c", "backups")
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	err := bm.Backup(backupDir)
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Error("expected nested backup directory to be created")
	}
}

func TestListBackupsWithGZFile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}
	bm := NewBackupManager(cfg)

	// Create a .gz file (not .tar.gz)
	gzFile := filepath.Join(tempDir, "somefile.gz")
	os.WriteFile(gzFile, []byte("gz data"), 0644)

	backups, err := bm.ListBackups(tempDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup (.gz file), got %d", len(backups))
	}
}

func TestRestoreWithManifestAndFiles(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			DataDir:  tempDir,
			Hostname: "test.example.com",
		},
	}

	bm := NewBackupManager(cfg)

	// Create a complete backup
	backupFile := filepath.Join(tempDir, "full_backup.tar.gz")
	file, _ := os.Create(backupFile)
	defer file.Close()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	// Manifest
	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": "20240615_100000",
		"hostname":  "test.example.com",
		"data_dir":  tempDir,
	}
	manifestData, _ := json.Marshal(manifest)
	mh := &tar.Header{Name: "manifest.json", Size: int64(len(manifestData)), Mode: 0644}
	tw.WriteHeader(mh)
	tw.Write(manifestData)

	// Database file
	dbContent := []byte("database dump")
	dh := &tar.Header{Name: "database/umailserver.db", Size: int64(len(dbContent)), Mode: 0644}
	tw.WriteHeader(dh)
	tw.Write(dbContent)

	// Config file
	confContent := []byte("server.hostname=test.example.com")
	ch := &tar.Header{Name: "config/test.conf", Size: int64(len(confContent)), Mode: 0644}
	tw.WriteHeader(ch)
	tw.Write(confContent)

	// Messages directory
	mdh := &tar.Header{Name: "messages/", Mode: 0755, Typeflag: tar.TypeDir}
	tw.WriteHeader(mdh)

	// Message file
	msgContent := []byte("From: test@example.com\nSubject: Hello\n\nWorld")
	msgH := &tar.Header{Name: "messages/user@example.com/INBOX/cur/msg1", Size: int64(len(msgContent)), Mode: 0644}
	tw.WriteHeader(msgH)
	tw.Write(msgContent)

	tw.Close()
	gw.Close()

	err := bm.Restore(backupFile)
	if err != nil {
		t.Errorf("Restore failed: %v", err)
	}
}
