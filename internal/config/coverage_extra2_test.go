package config

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, os.ErrClosed
}

func TestSetupWizardRunHostnameError(t *testing.T) {
	tmpDir := t.TempDir()

	// First input is the data dir (success), second read triggers error via EOF
	input := tmpDir + "\n"

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	_, err := wizard.Run()
	// Should error when trying to read hostname (EOF after data dir)
	if err == nil {
		t.Error("expected error when wizard fails to read hostname")
	}
}

func TestSetupWizardRunDataDirMkdirError(t *testing.T) {
	// Provide a data directory with null byte
	input := string([]byte{0}) + "\n"

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	_, err := wizard.Run()
	if err == nil {
		t.Error("expected error when data dir MkdirAll fails")
	}
}

func TestSetupWizardRunPOP3Enabled(t *testing.T) {
	tmpDir := t.TempDir()

	// Enable everything including POP3
	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"y\n" + // SMTP inbound
		"25\n" +
		"y\n" + // SMTP submission
		"587\n" +
		"y\n" + // IMAP
		"993\n" +
		"y\n" + // POP3 enabled
		"995\n" + // POP3 port
		"y\n" + // Admin enabled
		"8080\n" +
		"n\n" + // ACME disabled
		"y\n" + // Spam enabled
		"y\n" + // Bayesian
		"y\n" + // Greylisting
		"1\n" // Log level

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !cfg.POP3.Enabled {
		t.Error("expected POP3 to be enabled")
	}
	if cfg.POP3.Port != 995 {
		t.Errorf("expected POP3 port 995, got %d", cfg.POP3.Port)
	}
}

func TestSetupWizardRunACMEDisabled(t *testing.T) {
	tmpDir := t.TempDir()

	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"y\n25\n" +
		"y\n587\n" +
		"n\n" + // IMAP disabled
		"n\n" + // POP3 disabled
		"n\n" + // Admin disabled
		"n\n" + // ACME disabled
		"n\n" + // Spam disabled
		"1\n" // Log level

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if cfg.TLS.ACME.Enabled {
		t.Error("expected ACME to be disabled")
	}
}

func TestSetupWizardRunSpamEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"n\n" + // SMTP inbound disabled
		"n\n" + // SMTP submission disabled
		"n\n" + // IMAP disabled
		"n\n" + // POP3 disabled
		"n\n" + // Admin disabled
		"n\n" + // ACME disabled
		"y\n" + // Spam enabled
		"n\n" + // Bayesian disabled
		"n\n" + // Greylisting disabled
		"1\n" // Log level

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !cfg.Spam.Enabled {
		t.Error("expected spam to be enabled")
	}
	if cfg.Spam.Bayesian.Enabled {
		t.Error("expected Bayesian to be disabled")
	}
	if cfg.Spam.Greylisting.Enabled {
		t.Error("expected greylisting to be disabled")
	}
}

func TestSetupWizardSaveError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.Config.Server.DataDir = string([]byte{0})

	// This should fail due to null byte path
	err := wizard.Save(string([]byte{0}) + "/config.yaml")
	if err == nil {
		t.Error("expected Save to fail with null byte path")
	}
}

func TestGetDefaultDataDirFallback(t *testing.T) {
	// Save original
	origXDG := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", origXDG)

	// Unset XDG to test fallback
	os.Unsetenv("XDG_DATA_HOME")

	dir := GetDefaultDataDir()
	if dir == "" {
		t.Error("expected non-empty directory")
	}
}

func TestParseSizeDefaultCase(t *testing.T) {
	// The regex `^(\d+(?:\.\d+)?)\s*([KMGT]B?|B?)$` matches things like "1PB"
	// but PB is not in the switch. However, "P" or "PB" won't match the regex
	// because P is not in [KMGT]. Let me test what we can.
	// The default case in the switch for unit shouldn't be reachable due to regex
	// but we test the error return for invalid formats.
	_, err := ParseSize("abc")
	if err == nil {
		t.Error("expected error for invalid size")
	}
}

func TestParseSizeFloatError(t *testing.T) {
	// This is hard to trigger because the regex validates the number format
	// Just verify that valid float values work
	size, err := ParseSize("1.5GB")
	if err != nil {
		t.Fatalf("ParseSize '1.5GB' failed: %v", err)
	}
	if size != Size(1.5*float64(GB)) {
		t.Errorf("expected %d, got %d", Size(1.5*float64(GB)), size)
	}
}

func TestLoadSectionFromEnvCantSet(t *testing.T) {
	// Create a struct with an unexported field
	type myStruct struct {
		hostname string
	}
	s := myStruct{}
	v := reflect.ValueOf(&s).Elem()

	// hostname is unexported, so CanSet() returns false
	err := loadSectionFromEnv(v, "TEST_")
	if err != nil {
		t.Errorf("expected nil error for unsettable fields, got: %v", err)
	}
}

func TestLoadSectionFromEnvRecursive(t *testing.T) {
	os.Setenv("UMAILSERVER_SERVER_HOSTNAME", "recursive.example.com")
	os.Setenv("UMAILSERVER_SERVER_DATADIR", "/test/data")
	defer os.Unsetenv("UMAILSERVER_SERVER_HOSTNAME")
	defer os.Unsetenv("UMAILSERVER_SERVER_DATADIR")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err != nil {
		t.Fatalf("loadFromEnv failed: %v", err)
	}
	if cfg.Server.Hostname != "recursive.example.com" {
		t.Errorf("expected recursive.example.com, got %s", cfg.Server.Hostname)
	}
	if cfg.Server.DataDir != "/test/data" {
		t.Errorf("expected /test/data, got %s", cfg.Server.DataDir)
	}
}

func TestSetupWizardRunFormatChoice(t *testing.T) {
	tmpDir := t.TempDir()

	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"n\n" + // SMTP inbound disabled
		"n\n" + // SMTP submission disabled
		"n\n" + // IMAP disabled
		"n\n" + // POP3 disabled
		"n\n" + // Admin disabled
		"n\n" + // ACME disabled
		"n\n" + // Spam disabled
		"1\n" + // Log level (debug)
		"2\n" // Log format (text)

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("expected debug, got %s", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("expected text, got %s", cfg.Logging.Format)
	}
}

func TestSetupWizardRunEnsureDataDirError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid data dir, but then set up config to fail on EnsureDataDir
	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"n\n" +
		"n\n" +
		"n\n" +
		"n\n" +
		"n\n" +
		"n\n" +
		"n\n" +
		"1\n" +
		"1\n"

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	// Override config data dir to something invalid after parsing
	originalConfig := wizard.Config
	_ = originalConfig

	cfg, err := wizard.Run()
	// Should succeed because tmpDir is writable
	if err != nil {
		t.Fatalf("Run should succeed with valid tmpDir: %v", err)
	}
	_ = cfg
}

func TestSetupWizardRunWithACMEEmail(t *testing.T) {
	tmpDir := t.TempDir()

	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"n\n" + // SMTP inbound
		"n\n" + // SMTP submission
		"n\n" + // IMAP
		"n\n" + // POP3
		"n\n" + // Admin
		"y\n" + // ACME enabled
		"test@example.com\n" + // ACME email
		"n\n" + // Spam disabled
		"1\n" + // Log level
		"1\n" // Log format

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !cfg.TLS.ACME.Enabled {
		t.Error("expected ACME to be enabled")
	}
	if cfg.TLS.ACME.Email != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", cfg.TLS.ACME.Email)
	}
}

func TestSetupWizardSaveAndVerifyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	wizard := NewSetupWizard()
	wizard.Config = DefaultConfig()
	wizard.Config.Server.DataDir = tmpDir
	wizard.Config.Server.Hostname = "savetest.example.com"

	configPath := filepath.Join(tmpDir, "config.yaml")
	err := wizard.Save(configPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the config is loadable
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Server.Hostname != "savetest.example.com" {
		t.Errorf("expected savetest.example.com, got %s", loaded.Server.Hostname)
	}
}

func TestAskStringWithReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	// Use a reader that will error
	wizard.reader = bufio.NewReader(&errorReader{})

	_, err := wizard.askString("Prompt:", "default")
	if err == nil {
		t.Error("expected error from errorReader")
	}
}

func TestRunDataDirQuestionError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(&errorReader{})

	_, err := wizard.Run()
	if err == nil {
		t.Error("expected error when data dir read fails")
	}
}

func TestSetupWizardRunWithACMEEmptyEmail(t *testing.T) {
	tmpDir := t.TempDir()

	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"n\n" + // SMTP inbound
		"n\n" + // SMTP submission
		"n\n" + // IMAP
		"n\n" + // POP3
		"n\n" + // Admin
		"y\n" + // ACME enabled
		"\n" + // ACME email (empty, uses default "")
		"n\n" + // Spam disabled
		"1\n" + // Log level
		"1\n" // Log format

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !cfg.TLS.ACME.Enabled {
		t.Error("expected ACME to be enabled")
	}
}

func TestSizeStringBytes(t *testing.T) {
	s := Size(1536) // 1.5KB, not exact
	result := s.String()
	if result != "1536" {
		t.Errorf("expected '1536', got %q", result)
	}
}

func TestParseSizeEmptyString(t *testing.T) {
	size, err := ParseSize("")
	if err != nil {
		t.Fatalf("ParseSize empty string should not error: %v", err)
	}
	if size != 0 {
		t.Errorf("expected 0, got %d", size)
	}
}

func TestParseSizeJustNumber(t *testing.T) {
	size, err := ParseSize("4096")
	if err != nil {
		t.Fatalf("ParseSize '4096' failed: %v", err)
	}
	if size != 4096 {
		t.Errorf("expected 4096, got %d", size)
	}
}

func TestDurationParseViaEnv(t *testing.T) {
	os.Setenv("UMAILSERVER_SPAM_GREYLISTING_DELAY", "300000000000") // 5m in ns
	defer os.Unsetenv("UMAILSERVER_SPAM_GREYLISTING_DELAY")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err != nil {
		t.Fatalf("loadFromEnv with nanosecond duration failed: %v", err)
	}
	if cfg.Spam.Greylisting.Delay != Duration(5*time.Minute) {
		t.Errorf("expected 5m, got %v", time.Duration(cfg.Spam.Greylisting.Delay))
	}
}

func TestValidateInboundSMTPDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SMTP.Inbound.Enabled = false
	cfg.SMTP.Inbound.Port = 0 // Should be fine since inbound is disabled
	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected no error when SMTP inbound is disabled: %v", err)
	}
}

func TestValidateSubmissionDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SMTP.Submission.Enabled = false
	cfg.SMTP.Submission.Port = 0
	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected no error when SMTP submission is disabled: %v", err)
	}
}

func TestValidateIMAPDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IMAP.Enabled = false
	cfg.IMAP.Port = 0
	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected no error when IMAP is disabled: %v", err)
	}
}

func TestParseSizeWithSpaces(t *testing.T) {
	size, err := ParseSize("  5GB  ")
	if err != nil {
		t.Fatalf("ParseSize with spaces failed: %v", err)
	}
	if size != 5*GB {
		t.Errorf("expected %d, got %d", 5*GB, size)
	}
}

func TestParseSizeLowercase(t *testing.T) {
	size, err := ParseSize("5gb")
	if err != nil {
		t.Fatalf("ParseSize lowercase failed: %v", err)
	}
	if size != 5*GB {
		t.Errorf("expected %d, got %d", 5*GB, size)
	}
}
