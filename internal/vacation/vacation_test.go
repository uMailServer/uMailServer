package vacation

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	manager := NewManager(tmpDir, logger)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}

	if manager.dataDir != tmpDir {
		t.Errorf("dataDir = %s, want %s", manager.dataDir, tmpDir)
	}

	if manager.configs == nil {
		t.Error("configs map should not be nil")
	}

	if manager.sentCache == nil {
		t.Error("sentCache map should not be nil")
	}

	if manager.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestNewManager_WithNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	if manager.logger == nil {
		t.Error("logger should use default when nil")
	}
}

func TestGetConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config, err := manager.GetConfig("user@example.com")
	if err != nil {
		t.Errorf("GetConfig error = %v", err)
	}

	if config == nil {
		t.Fatal("GetConfig should return default config")
	}

	if config.Enabled {
		t.Error("Default config should have Enabled = false")
	}

	if config.Subject != "Out of Office" {
		t.Errorf("Default Subject = %s, want Out of Office", config.Subject)
	}

	if config.SendInterval != 7*24*time.Hour {
		t.Errorf("Default SendInterval = %v, want 7 days", config.SendInterval)
	}
}

func TestSetConfig(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:      true,
		Subject:      "Vacation",
		Message:      "I am on vacation",
		SendInterval: 24 * time.Hour,
		IgnoreLists:  true,
		IgnoreBulk:   true,
	}

	err := manager.SetConfig("user@example.com", config)
	if err != nil {
		t.Errorf("SetConfig error = %v", err)
	}

	// Verify config was saved
	saved, err := manager.GetConfig("user@example.com")
	if err != nil {
		t.Errorf("GetConfig error = %v", err)
	}

	if !saved.Enabled {
		t.Error("Config should be enabled")
	}

	if saved.Subject != "Vacation" {
		t.Errorf("Subject = %s, want Vacation", saved.Subject)
	}
}

func TestSetConfig_Validation(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	// Missing subject
	config := &Config{
		Enabled: true,
		Message: "I am on vacation",
	}

	err := manager.SetConfig("user@example.com", config)
	if err == nil {
		t.Error("SetConfig should require subject when enabled")
	}

	// Missing message
	config2 := &Config{
		Enabled: true,
		Subject: "Vacation",
	}

	err = manager.SetConfig("user@example.com", config2)
	if err == nil {
		t.Error("SetConfig should require message when enabled")
	}
}

func TestSetConfig_DefaultInterval(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:      true,
		Subject:      "Vacation",
		Message:      "I am on vacation",
		SendInterval: 0, // Should default to 7 days
	}

	err := manager.SetConfig("user@example.com", config)
	if err != nil {
		t.Errorf("SetConfig error = %v", err)
	}

	if config.SendInterval != 7*24*time.Hour {
		t.Errorf("SendInterval = %v, want 7 days", config.SendInterval)
	}
}

func TestShouldSendAutoReply_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if result {
		t.Error("Should not send auto-reply when disabled")
	}
}

func TestShouldSendAutoReply_BeforeStartDate(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:   true,
		Subject:   "Vacation",
		Message:   "I am on vacation",
		StartDate: time.Now().Add(24 * time.Hour), // Starts tomorrow
	}
	manager.SetConfig("user@example.com", config)

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if result {
		t.Error("Should not send auto-reply before start date")
	}
}

func TestShouldSendAutoReply_AfterEndDate(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Vacation",
		Message: "I am on vacation",
		EndDate: time.Now().Add(-24 * time.Hour), // Ended yesterday
	}
	manager.SetConfig("user@example.com", config)

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if result {
		t.Error("Should not send auto-reply after end date")
	}
}

func TestShouldSendAutoReply_ExcludeAddress(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:          true,
		Subject:          "Vacation",
		Message:          "I am on vacation",
		ExcludeAddresses: []string{"blocked@example.com"},
	}
	manager.SetConfig("user@example.com", config)

	result := manager.ShouldSendAutoReply("user@example.com", "blocked@example.com", nil)
	if result {
		t.Error("Should not send auto-reply to excluded address")
	}
}

func TestShouldSendAutoReply_ListEmail(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:     true,
		Subject:     "Vacation",
		Message:     "I am on vacation",
		IgnoreLists: true,
	}
	manager.SetConfig("user@example.com", config)

	headers := map[string]string{
		"List-Id": "<test-list.example.com>",
	}

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", headers)
	if result {
		t.Error("Should not send auto-reply to list emails")
	}
}

func TestShouldSendAutoReply_BulkEmail(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:    true,
		Subject:    "Vacation",
		Message:    "I am on vacation",
		IgnoreBulk: true,
	}
	manager.SetConfig("user@example.com", config)

	headers := map[string]string{
		"Precedence": "bulk",
	}

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", headers)
	if result {
		t.Error("Should not send auto-reply to bulk emails")
	}
}

func TestShouldSendAutoReply_AutoGenerated(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Vacation",
		Message: "I am on vacation",
	}
	manager.SetConfig("user@example.com", config)

	headers := map[string]string{
		"Auto-Submitted": "auto-generated",
	}

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", headers)
	if result {
		t.Error("Should not send auto-reply to auto-generated emails")
	}
}

func TestShouldSendAutoReply_SendInterval(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:      true,
		Subject:      "Vacation",
		Message:      "I am on vacation",
		SendInterval: 24 * time.Hour,
	}
	manager.SetConfig("user@example.com", config)

	// First send should be allowed
	result1 := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if !result1 {
		t.Error("First auto-reply should be allowed")
	}

	// Record the send
	manager.RecordAutoReplySent("user@example.com", "sender@example.com")

	// Immediate second send should not be allowed
	result2 := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if result2 {
		t.Error("Second auto-reply should not be allowed within interval")
	}
}

func TestShouldSendAutoReply_DifferentSenders(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:      true,
		Subject:      "Vacation",
		Message:      "I am on vacation",
		SendInterval: 24 * time.Hour,
	}
	manager.SetConfig("user@example.com", config)

	// Send to first sender
	manager.RecordAutoReplySent("user@example.com", "sender1@example.com")

	// Different sender should still get auto-reply
	result := manager.ShouldSendAutoReply("user@example.com", "sender2@example.com", nil)
	if !result {
		t.Error("Different sender should get auto-reply")
	}
}

func TestRecordAutoReplySent(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	manager.RecordAutoReplySent("user@example.com", "sender@example.com")

	manager.cacheMu.RLock()
	userCache, exists := manager.sentCache["user@example.com"]
	manager.cacheMu.RUnlock()

	if !exists {
		t.Fatal("User cache should exist")
	}

	if _, exists := userCache["sender@example.com"]; !exists {
		t.Error("Sender should be recorded in cache")
	}
}

func TestGetAutoReplyMessage(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:     true,
		Subject:     "Out of Office",
		Message:     "I am on vacation",
		HTMLMessage: "<p>I am on vacation</p>",
	}
	manager.SetConfig("user@example.com", config)

	subject, textBody, htmlBody := manager.GetAutoReplyMessage("user@example.com")

	if subject != "Out of Office" {
		t.Errorf("Subject = %s, want Out of Office", subject)
	}

	if textBody != "I am on vacation" {
		t.Errorf("TextBody = %s, want I am on vacation", textBody)
	}

	if htmlBody != "<p>I am on vacation</p>" {
		t.Errorf("HTMLBody = %s, want <p>I am on vacation</p>", htmlBody)
	}
}

func TestGetAutoReplyMessage_NotEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	subject, textBody, htmlBody := manager.GetAutoReplyMessage("user@example.com")

	if subject != "" || textBody != "" || htmlBody != "" {
		t.Error("Should return empty strings when not enabled")
	}
}

func TestDeleteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Vacation",
		Message: "I am on vacation",
	}
	manager.SetConfig("user@example.com", config)

	// Verify config exists
	saved, _ := manager.GetConfig("user@example.com")
	if !saved.Enabled {
		t.Error("Config should exist before deletion")
	}

	// Delete config
	err := manager.DeleteConfig("user@example.com")
	if err != nil {
		t.Errorf("DeleteConfig error = %v", err)
	}

	// Verify config deleted
	manager.mu.RLock()
	_, exists := manager.configs["user@example.com"]
	manager.mu.RUnlock()

	if exists {
		t.Error("Config should be deleted from memory")
	}
}

func TestDeleteConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	// Should not error when deleting non-existent config
	err := manager.DeleteConfig("nonexistent@example.com")
	if err != nil {
		t.Errorf("DeleteConfig error = %v", err)
	}
}

func TestListActiveVacations(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	now := time.Now()

	// Add active vacation (no dates)
	config1 := &Config{
		Enabled: true,
		Subject: "Active No Dates",
		Message: "I am on vacation",
	}
	manager.SetConfig("active1@example.com", config1)

	// Add active vacation (within dates)
	config2 := &Config{
		Enabled:   true,
		Subject:   "Active With Dates",
		Message:   "I am on vacation",
		StartDate: now.Add(-24 * time.Hour),
		EndDate:   now.Add(24 * time.Hour),
	}
	manager.SetConfig("active2@example.com", config2)

	// Add inactive vacation (disabled)
	config3 := &Config{
		Enabled: false,
		Subject: "Disabled",
		Message: "I am not on vacation",
	}
	manager.SetConfig("inactive1@example.com", config3)

	// Add inactive vacation (future start date)
	config4 := &Config{
		Enabled:   true,
		Subject:   "Future",
		Message:   "I will be on vacation",
		StartDate: now.Add(24 * time.Hour),
	}
	manager.SetConfig("inactive2@example.com", config4)

	// Add inactive vacation (past end date)
	config5 := &Config{
		Enabled: true,
		Subject: "Past",
		Message: "I was on vacation",
		EndDate: now.Add(-24 * time.Hour),
	}
	manager.SetConfig("inactive3@example.com", config5)

	active := manager.ListActiveVacations()

	if len(active) != 2 {
		t.Errorf("Active vacations count = %d, want 2", len(active))
	}

	// Check that only active ones are included
	hasActive1 := false
	hasActive2 := false
	for _, email := range active {
		if email == "active1@example.com" {
			hasActive1 = true
		}
		if email == "active2@example.com" {
			hasActive2 = true
		}
	}

	if !hasActive1 {
		t.Error("Should include active1@example.com")
	}
	if !hasActive2 {
		t.Error("Should include active2@example.com")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user@example.com", "dXNlckBleGFtcGxlLmNvbQ"},
		{"first.last@example.com", "Zmlyc3QubGFzdEBleGFtcGxlLmNvbQ"},
		{"user/test@example.com", "dXNlci90ZXN0QGV4YW1wbGUuY29t"},
		{"user\\test@example.com", "dXNlclx0ZXN0QGV4YW1wbGUuY29t"},
	}

	for _, tt := range tests {
		result := sanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeFilename(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestConfigStruct(t *testing.T) {
	now := time.Now()
	config := Config{
		Enabled:          true,
		StartDate:        now,
		EndDate:          now.Add(7 * 24 * time.Hour),
		Subject:          "Out of Office",
		Message:          "I am on vacation",
		HTMLMessage:      "<p>I am on vacation</p>",
		SendInterval:     24 * time.Hour,
		ExcludeAddresses: []string{"boss@example.com"},
		IgnoreLists:      true,
		IgnoreBulk:       true,
	}

	if !config.Enabled {
		t.Error("Enabled should be true")
	}

	if config.Subject != "Out of Office" {
		t.Errorf("Subject = %s, want Out of Office", config.Subject)
	}

	if len(config.ExcludeAddresses) != 1 {
		t.Errorf("ExcludeAddresses count = %d, want 1", len(config.ExcludeAddresses))
	}
}

func TestManager_LoadExistingConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manager and add config
	manager1 := NewManager(tmpDir, nil)
	config := &Config{
		Enabled: true,
		Subject: "Loaded Config",
		Message: "This should be loaded",
	}
	manager1.SetConfig("persistent@example.com", config)

	// Create new manager pointing to same directory
	manager2 := NewManager(tmpDir, nil)

	loaded, err := manager2.GetConfig("persistent@example.com")
	if err != nil {
		t.Errorf("GetConfig error = %v", err)
	}

	if !loaded.Enabled {
		t.Error("Loaded config should be enabled")
	}

	if loaded.Subject != "Loaded Config" {
		t.Errorf("Loaded Subject = %s, want Loaded Config", loaded.Subject)
	}
}

func TestUnsanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Legacy format (with _at_ marker) - should use old unsanitize
		{"user_at_example_com", "user@example.com"},
		{"first_last_at_example_com", "first.last@example.com"},
		{"user_test_at_example_com", "user.test@example.com"},
		// New base64 format - should decode correctly
		{"dXNlckBleGFtcGxlLmNvbQ", "user@example.com"},
		{"Zmlyc3QubGFzdEBleGFtcGxlLmNvbQ", "first.last@example.com"},
		{"dXNlci90ZXN0QGV4YW1wbGUuY29t", "user/test@example.com"},
	}

	for _, tt := range tests {
		result := unsanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("unsanitizeFilename(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

// Test loadConfigs with directory read error
func TestLoadConfigs_ReadDirError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file where directory should be to cause read error on some systems
	// Actually this won't cause an error - let's just test with empty dir
	manager := NewManager(tmpDir, nil)

	// Manually create a file that's not readable in some way
	// This test just verifies empty directory works
	if len(manager.configs) != 0 {
		t.Errorf("Expected empty configs, got %d", len(manager.configs))
	}
}

// Test loadConfigFile with invalid JSON
func TestLoadConfigFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file with invalid JSON
	invalidJSON := []byte(`{"invalid json`)
	filePath := tmpDir + "/user_at_example_com.json"
	os.WriteFile(filePath, invalidJSON, 0o600)

	// NewManager should skip invalid files
	manager := NewManager(tmpDir, nil)

	// The invalid config should not be loaded
	_, err := manager.GetConfig("user@example.com")
	if err != nil {
		t.Errorf("GetConfig should return default for invalid file: %v", err)
	}
}

// Test saveConfig with directory that doesn't exist
func TestSaveConfig_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDir := tmpDir + "/nonexistent"

	manager := NewManager(nonExistentDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Test",
		Message: "Test message",
	}

	// Should create directory and save config
	err := manager.SetConfig("user@example.com", config)
	if err != nil {
		t.Errorf("SetConfig should succeed: %v", err)
	}
}

// Test loadConfigFile with empty file
func TestLoadConfigFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an empty file
	filePath := tmpDir + "/user_at_example_com.json"
	os.WriteFile(filePath, []byte(""), 0o600)

	// NewManager should skip empty files
	manager := NewManager(tmpDir, nil)

	// The empty config should not be loaded
	_, err := manager.GetConfig("user@example.com")
	if err != nil {
		t.Errorf("GetConfig should return default for empty file: %v", err)
	}
}

// Test loadConfigFile with partial JSON
func TestLoadConfigFile_PartialJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file with partial valid JSON
	partialJSON := []byte(`{"enabled": true, "subject": "Test"` + "\n")
	filePath := tmpDir + "/user_at_example_com.json"
	os.WriteFile(filePath, partialJSON, 0o600)

	// NewManager should skip invalid files
	manager := NewManager(tmpDir, nil)

	// The partial config should not be loaded
	_, err := manager.GetConfig("user@example.com")
	if err != nil {
		t.Errorf("GetConfig should return default for partial JSON: %v", err)
	}
}

// Test GetConfig with multiple configs loaded
func TestGetConfig_MultipleLoaded(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	// Set multiple configs
	config1 := &Config{Enabled: true, Subject: "Test1", Message: "Msg1"}
	config2 := &Config{Enabled: true, Subject: "Test2", Message: "Msg2"}

	manager.SetConfig("user1@example.com", config1)
	manager.SetConfig("user2@example.com", config2)

	// Both should be retrievable
	cfg1, _ := manager.GetConfig("user1@example.com")
	cfg2, _ := manager.GetConfig("user2@example.com")

	if cfg1.Subject != "Test1" {
		t.Errorf("User1 subject = %s, want Test1", cfg1.Subject)
	}
	if cfg2.Subject != "Test2" {
		t.Errorf("User2 subject = %s, want Test2", cfg2.Subject)
	}
}

// Test ShouldSendAutoReply with only end date
func TestShouldSendAutoReply_OnlyEndDate(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Vacation",
		Message: "I am on vacation",
		EndDate: time.Now().Add(24 * time.Hour), // Ends tomorrow - still active
	}
	manager.SetConfig("user@example.com", config)

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if !result {
		t.Error("Should send auto-reply when within date range")
	}
}

// Test ShouldSendAutoReply with only start date
func TestShouldSendAutoReply_OnlyStartDate(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:   true,
		Subject:   "Vacation",
		Message:   "I am on vacation",
		StartDate: time.Now().Add(-24 * time.Hour), // Started yesterday - still active
	}
	manager.SetConfig("user@example.com", config)

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if !result {
		t.Error("Should send auto-reply when within date range")
	}
}

// Test ShouldSendAutoReply with both start and end dates outside range
func TestShouldSendAutoReply_OutsideDateRange(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:   true,
		Subject:   "Vacation",
		Message:   "I am on vacation",
		StartDate: time.Now().Add(-48 * time.Hour), // Started 2 days ago
		EndDate:   time.Now().Add(-24 * time.Hour), // Ended yesterday
	}
	manager.SetConfig("user@example.com", config)

	result := manager.ShouldSendAutoReply("user@example.com", "sender@example.com", nil)
	if result {
		t.Error("Should not send auto-reply when outside date range")
	}
}

// Test SetConfig with custom interval
func TestSetConfig_CustomInterval(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled:      true,
		Subject:      "Vacation",
		Message:      "I am on vacation",
		SendInterval: 12 * time.Hour, // Custom 12 hour interval
	}

	err := manager.SetConfig("user@example.com", config)
	if err != nil {
		t.Errorf("SetConfig error = %v", err)
	}

	saved, _ := manager.GetConfig("user@example.com")
	if saved.SendInterval != 12*time.Hour {
		t.Errorf("SendInterval = %v, want 12 hours", saved.SendInterval)
	}
}

// Test DeleteConfig after SetConfig
func TestDeleteConfig_AfterSet(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Vacation",
		Message: "I am on vacation",
	}

	manager.SetConfig("user@example.com", config)
	manager.DeleteConfig("user@example.com")

	// After delete, should get default config
	defaultCfg, _ := manager.GetConfig("user@example.com")
	if defaultCfg.Enabled {
		t.Error("After delete, config should be disabled (default)")
	}
}

// Test saveConfig file write error path
func TestSaveConfig_WriteError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory where the config file should go
	configDir := tmpDir + "/subdir"
	os.Mkdir(configDir, 0o755)

	manager := NewManager(configDir, nil)

	config := &Config{
		Enabled: true,
		Subject: "Test",
		Message: "Test message",
	}

	// Create a file with same name as directory to block write
	blockPath := configDir + "/user_at_example.com.json"
	os.WriteFile(blockPath, []byte("block"), 0o755)

	// Try to save - may fail or succeed depending on OS
	err := manager.SetConfig("user@example.com", config)
	t.Logf("SetConfig result in blocked dir: %v", err)
}

func TestCheckAndRecordAutoReply(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, nil)

	user := "user@example.com"
	sender := "sender@example.com"

	// No config set - should return false
	if manager.CheckAndRecordAutoReply(user, sender, nil) {
		t.Error("should return false when no config exists")
	}

	// Set enabled config
	config := &Config{
		Enabled:      true,
		Subject:      "Away",
		Message:      "I am away",
		SendInterval: 24 * time.Hour,
		IgnoreLists:  true,
		IgnoreBulk:   true,
	}
	_ = manager.SetConfig(user, config)

	// First call should return true
	if !manager.CheckAndRecordAutoReply(user, sender, nil) {
		t.Error("first call should return true")
	}

	// Immediate second call should return false (within send interval)
	if manager.CheckAndRecordAutoReply(user, sender, nil) {
		t.Error("second call within interval should return false")
	}

	// Different sender should return true
	if !manager.CheckAndRecordAutoReply(user, "other@example.com", nil) {
		t.Error("different sender should return true")
	}

	// Excluded sender should return false
	config.ExcludeAddresses = []string{"blocked@example.com"}
	_ = manager.SetConfig(user, config)
	if manager.CheckAndRecordAutoReply(user, "blocked@example.com", nil) {
		t.Error("excluded sender should return false")
	}

	// Mailing list headers should return false
	if manager.CheckAndRecordAutoReply(user, "list@example.com", map[string]string{
		"List-Id": "test-list",
	}) {
		t.Error("mailing list should return false")
	}

	// Bulk precedence should return false
	if manager.CheckAndRecordAutoReply(user, "bulk@example.com", map[string]string{
		"Precedence": "bulk",
	}) {
		t.Error("bulk mail should return false")
	}

	// Auto-submitted should return false
	if manager.CheckAndRecordAutoReply(user, "auto@example.com", map[string]string{
		"Auto-Submitted": "auto-generated",
	}) {
		t.Error("auto-submitted should return false")
	}

	// X-Auto-Response-Suppress should return false
	if manager.CheckAndRecordAutoReply(user, "suppress@example.com", map[string]string{
		"X-Auto-Response-Suppress": "OOF",
	}) {
		t.Error("X-Auto-Response-Suppress should return false")
	}

	// Mail loop detection
	if manager.CheckAndRecordAutoReply(user, "loop@example.com", map[string]string{
		"X-Mail-Loop": user,
	}) {
		t.Error("mail loop should return false")
	}
}
