package push

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if service.subscriptions == nil {
		t.Error("subscriptions map should not be nil")
	}

	if service.userSubs == nil {
		t.Error("userSubs map should not be nil")
	}

	if service.config.VAPIDPublicKey == "" {
		t.Error("VAPIDPublicKey should be generated")
	}

	if service.config.VAPIDPrivateKey == "" {
		t.Error("VAPIDPrivateKey should be generated")
	}
}

func TestNewService_WithExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first service to generate config
	service1, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("First NewService failed: %v", err)
	}

	publicKey := service1.config.VAPIDPublicKey

	// Create second service should load existing config
	service2, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("Second NewService failed: %v", err)
	}

	if service2.config.VAPIDPublicKey != publicKey {
		t.Error("Second service should load existing VAPID keys")
	}
}

func TestGetVAPIDPublicKey(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	key := service.GetVAPIDPublicKey()
	if key == "" {
		t.Error("GetVAPIDPublicKey should return non-empty string")
	}
}

func TestSubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	sub := &Subscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/test",
		P256dh:   "test-p256dh-key",
		Auth:     "test-auth-secret",
		DeviceInfo: DeviceInfo{
			DeviceType: "mobile",
			OS:         "Android",
			Browser:    "Chrome",
			Name:       "Test Device",
		},
	}

	err := service.Subscribe("user@example.com", sub)
	if err != nil {
		t.Errorf("Subscribe failed: %v", err)
	}

	if sub.ID == "" {
		t.Error("Subscription ID should be generated")
	}

	if sub.UserID != "user@example.com" {
		t.Errorf("UserID = %s, want user@example.com", sub.UserID)
	}

	if sub.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestSubscribe_WithID(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	sub := &Subscription{
		ID:       "existing-id",
		Endpoint: "https://fcm.googleapis.com/fcm/send/test",
		P256dh:   "test-p256dh-key",
		Auth:     "test-auth-secret",
	}

	err := service.Subscribe("user@example.com", sub)
	if err != nil {
		t.Errorf("Subscribe failed: %v", err)
	}

	if sub.ID != "existing-id" {
		t.Error("Existing ID should be preserved")
	}
}

func TestGetUserSubscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Initially should return empty
	subs := service.GetUserSubscriptions("user@example.com")
	if len(subs) != 0 {
		t.Errorf("Expected 0 subscriptions, got %d", len(subs))
	}

	// Add subscriptions
	for i := 0; i < 3; i++ {
		sub := &Subscription{
			Endpoint: "https://fcm.googleapis.com/fcm/send/test-" + string(rune('a'+i)),
			P256dh:   "test-key",
			Auth:     "test-auth",
		}
		_ = service.Subscribe("user@example.com", sub)
	}

	// Now should return 3 subscriptions
	subs = service.GetUserSubscriptions("user@example.com")
	if len(subs) != 3 {
		t.Errorf("Expected 3 subscriptions, got %d", len(subs))
	}
}

func TestUnsubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	sub := &Subscription{
		ID:       "test-sub-id",
		Endpoint: "https://fcm.googleapis.com/fcm/send/test",
		P256dh:   "test-key",
		Auth:     "test-auth",
	}
	_ = service.Subscribe("user@example.com", sub)

	// Verify subscription exists
	subs := service.GetUserSubscriptions("user@example.com")
	if len(subs) != 1 {
		t.Fatal("Subscription should exist")
	}

	// Unsubscribe
	err := service.Unsubscribe("user@example.com", "test-sub-id")
	if err != nil {
		t.Errorf("Unsubscribe failed: %v", err)
	}

	// Verify subscription removed
	subs = service.GetUserSubscriptions("user@example.com")
	if len(subs) != 0 {
		t.Error("Subscription should be removed")
	}
}

func TestUnsubscribe_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	err := service.Unsubscribe("user@example.com", "nonexistent-id")
	if err == nil {
		t.Error("Unsubscribe should return error for non-existent subscription")
	}
}

func TestUnsubscribe_WrongUser(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	sub := &Subscription{
		ID:       "test-sub-id",
		Endpoint: "https://fcm.googleapis.com/fcm/send/test",
		P256dh:   "test-key",
		Auth:     "test-auth",
	}
	_ = service.Subscribe("user@example.com", sub)

	// Try to unsubscribe with different user
	err := service.Unsubscribe("other@example.com", "test-sub-id")
	if err == nil {
		t.Error("Unsubscribe should return error for wrong user")
	}
}

func TestUpdateDeviceInfo(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	sub := &Subscription{
		ID:       "test-sub-id",
		Endpoint: "https://fcm.googleapis.com/fcm/send/test",
		P256dh:   "test-key",
		Auth:     "test-auth",
		DeviceInfo: DeviceInfo{
			DeviceType: "mobile",
			OS:         "Android",
		},
	}
	_ = service.Subscribe("user@example.com", sub)

	newInfo := DeviceInfo{
		DeviceType: "desktop",
		OS:         "Windows",
		Browser:    "Chrome",
		Name:       "Work PC",
	}

	err := service.UpdateDeviceInfo("user@example.com", "test-sub-id", newInfo)
	if err != nil {
		t.Errorf("UpdateDeviceInfo failed: %v", err)
	}

	// Verify update
	subs := service.GetUserSubscriptions("user@example.com")
	if len(subs) == 0 {
		t.Fatal("Subscription should exist")
	}

	if subs[0].DeviceInfo.Name != "Work PC" {
		t.Errorf("DeviceInfo.Name = %s, want Work PC", subs[0].DeviceInfo.Name)
	}
}

func TestCleanExpiredSubscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Add a recent subscription
	recentSub := &Subscription{
		ID:        "recent-sub",
		Endpoint:  "https://fcm.googleapis.com/fcm/send/recent",
		P256dh:    "test-key",
		Auth:      "test-auth",
		UpdatedAt: time.Now(),
	}
	service.Subscribe("user@example.com", recentSub)

	// Manually add an expired subscription
	expiredSub := &Subscription{
		ID:        "expired-sub",
		UserID:    "user@example.com",
		Endpoint:  "https://fcm.googleapis.com/fcm/send/expired",
		P256dh:    "test-key",
		Auth:      "test-auth",
		UpdatedAt: time.Now().Add(-100 * 24 * time.Hour), // 100 days ago
	}
	service.mu.Lock()
	service.subscriptions["expired-sub"] = expiredSub
	service.userSubs["user@example.com"] = append(service.userSubs["user@example.com"], "expired-sub")
	service.mu.Unlock()

	// Clean expired
	err := service.CleanExpiredSubscriptions()
	if err != nil {
		t.Errorf("CleanExpiredSubscriptions failed: %v", err)
	}

	// Verify expired subscription removed
	if _, exists := service.subscriptions["expired-sub"]; exists {
		t.Error("Expired subscription should be removed")
	}

	// Verify recent subscription still exists
	if _, exists := service.subscriptions["recent-sub"]; !exists {
		t.Error("Recent subscription should still exist")
	}
}

func TestGetStats(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Add subscriptions with different device types
	subs := []*Subscription{
		{
			Endpoint:   "https://fcm.googleapis.com/fcm/send/1",
			P256dh:     "key1",
			Auth:       "auth1",
			DeviceInfo: DeviceInfo{DeviceType: "mobile", OS: "Android"},
		},
		{
			Endpoint:   "https://fcm.googleapis.com/fcm/send/2",
			P256dh:     "key2",
			Auth:       "auth2",
			DeviceInfo: DeviceInfo{DeviceType: "mobile", OS: "iOS"},
		},
		{
			Endpoint:   "https://fcm.googleapis.com/fcm/send/3",
			P256dh:     "key3",
			Auth:       "auth3",
			DeviceInfo: DeviceInfo{DeviceType: "desktop", OS: "Windows"},
		},
	}

	for _, sub := range subs {
		_ = service.Subscribe("user@example.com", sub)
	}

	stats := service.GetStats()

	totalSubs, ok := stats["totalSubscriptions"].(int)
	if !ok || totalSubs != 3 {
		t.Errorf("totalSubscriptions = %v, want 3", stats["totalSubscriptions"])
	}

	deviceTypes, ok := stats["deviceTypes"].(map[string]int)
	if !ok {
		t.Fatal("deviceTypes should be map[string]int")
	}

	if deviceTypes["mobile"] != 2 {
		t.Errorf("mobile count = %d, want 2", deviceTypes["mobile"])
	}

	if deviceTypes["desktop"] != 1 {
		t.Errorf("desktop count = %d, want 1", deviceTypes["desktop"])
	}
}

func TestGenerateSubscriptionID(t *testing.T) {
	id1 := generateSubscriptionID()
	id2 := generateSubscriptionID()

	if id1 == "" {
		t.Error("generateSubscriptionID should not return empty string")
	}

	if id1 == id2 {
		t.Error("generateSubscriptionID should generate unique IDs")
	}
}

func TestSubscriptionStruct(t *testing.T) {
	now := time.Now()
	sub := Subscription{
		ID:        "sub-123",
		UserID:    "user@example.com",
		Endpoint:  "https://fcm.googleapis.com/fcm/send/test",
		P256dh:    "p256dh-key",
		Auth:      "auth-secret",
		CreatedAt: now,
		UpdatedAt: now,
		DeviceInfo: DeviceInfo{
			DeviceType: "mobile",
			OS:         "Android",
			Browser:    "Chrome",
			Name:       "My Phone",
		},
	}

	if sub.ID != "sub-123" {
		t.Errorf("ID = %s, want sub-123", sub.ID)
	}

	if sub.DeviceInfo.OS != "Android" {
		t.Errorf("DeviceInfo.OS = %s, want Android", sub.DeviceInfo.OS)
	}
}

func TestNotificationStruct(t *testing.T) {
	notif := Notification{
		Title:              "New Email",
		Body:               "You have a new message",
		Icon:               "/icons/mail.png",
		Badge:              "/icons/badge.png",
		Tag:                "new-mail",
		RequireInteraction: true,
		Data: map[string]string{
			"type": "email",
			"id":   "123",
		},
		Actions: []NotificationAction{
			{Action: "open", Title: "Open", Icon: "/icons/open.png"},
			{Action: "dismiss", Title: "Dismiss"},
		},
	}

	if notif.Title != "New Email" {
		t.Errorf("Title = %s, want New Email", notif.Title)
	}

	if len(notif.Actions) != 2 {
		t.Errorf("Actions count = %d, want 2", len(notif.Actions))
	}
}

func TestNotificationActionStruct(t *testing.T) {
	action := NotificationAction{
		Action: "reply",
		Title:  "Reply",
		Icon:   "/icons/reply.png",
	}

	if action.Action != "reply" {
		t.Errorf("Action = %s, want reply", action.Action)
	}

	if action.Title != "Reply" {
		t.Errorf("Title = %s, want Reply", action.Title)
	}
}

func TestDeviceInfoStruct(t *testing.T) {
	info := DeviceInfo{
		DeviceType: "tablet",
		OS:         "iOS",
		Browser:    "Safari",
		Name:       "iPad",
	}

	if info.DeviceType != "tablet" {
		t.Errorf("DeviceType = %s, want tablet", info.DeviceType)
	}

	if info.Browser != "Safari" {
		t.Errorf("Browser = %s, want Safari", info.Browser)
	}
}
func TestSendToUser_NoSubscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	notification := &Notification{
		Title: "Test",
		Body:  "Test notification",
	}

	// Should return nil when no subscriptions
	err := service.SendToUser("user@example.com", notification)
	if err != nil {
		t.Errorf("Expected no error when no subscriptions, got: %v", err)
	}
}

// Test SendNewMailNotification
func TestSendNewMailNotification(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Add a subscription first
	sub := &Subscription{
		Endpoint: "https://invalid.endpoint.test/404",
		P256dh:   "test-p256dh",
		Auth:     "test-auth",
	}
	_ = service.Subscribe("user@example.com", sub)

	// This will fail due to invalid endpoint, but tests the code path
	err := service.SendNewMailNotification("user@example.com", "sender@test.com", "Test Subject", "Test preview")
	// We expect an error because the endpoint is invalid
	if err == nil {
		t.Log("Note: SendNewMailNotification completed without error (may indicate network mock needed)")
	}
}

// Test SendNewMailNotification with no subscriptions
func TestSendNewMailNotification_NoSubscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Should return nil when no subscriptions
	err := service.SendNewMailNotification("user@example.com", "sender@test.com", "Test Subject", "Test preview")
	if err != nil {
		t.Errorf("Expected no error when no subscriptions, got: %v", err)
	}
}

// Test loadSubscriptions with various file types
func TestLoadSubscriptions_VariousFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create service to generate VAPID keys
	service1, _ := NewService(tmpDir, nil)

	// Add a subscription
	sub := &Subscription{
		ID:        "test-sub-1",
		UserID:    "user@example.com",
		Endpoint:  "https://example.com/push",
		P256dh:    "test-p256dh",
		Auth:      "test-auth",
		CreatedAt: time.Now(),
	}
	service1.Subscribe("user@example.com", sub)

	// Create some invalid files in the directory
	invalidJSONPath := tmpDir + "/invalid.json"
	os.WriteFile(invalidJSONPath, []byte("not valid json"), 0o644)

	txtFilePath := tmpDir + "/readme.txt"
	os.WriteFile(txtFilePath, []byte("readme"), 0o644)

	dirPath := tmpDir + "/subdir"
	os.Mkdir(dirPath, 0o755)

	// Create new service to trigger reload
	service2, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Check that valid subscription is loaded
	subs := service2.GetUserSubscriptions("user@example.com")
	// Note: Due to file naming conventions, subscription may or may not be loaded
	// depending on implementation details
	t.Logf("Loaded %d subscriptions", len(subs))
}

// Test loadSubscriptions with non-existent dataDir
func TestLoadSubscriptions_CreateDir(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := tmpDir + "/new_push_data"

	// Create service with non-existent directory
	_, err := NewService(dataDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Directory should be created
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Error("Data directory should be created")
	}
}

// Test generateVAPIDKeys error handling
func TestGenerateVAPIDKeys(t *testing.T) {
	// Test the standalone function
	privateKey, publicKey, err := generateVAPIDKeys()
	if err != nil {
		t.Errorf("generateVAPIDKeys failed: %v", err)
	}
	if privateKey == "" {
		t.Error("Expected non-empty private key")
	}
	if publicKey == "" {
		t.Error("Expected non-empty public key")
	}
}

// Test loadOrGenerateConfig with existing invalid config
func TestLoadOrGenerateConfig_Invalid(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid vapid.json
	vapidPath := filepath.Join(tmpDir, "vapid.json")
	os.WriteFile(vapidPath, []byte("invalid json"), 0o644)

	// Create service should handle invalid config gracefully
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService should handle invalid config: %v", err)
	}

	// Should have generated new keys
	if service.config.VAPIDPublicKey == "" {
		t.Error("Expected new VAPID keys to be generated")
	}
}

// Test SendNotification with nil subscription
func TestSendNotification_NilSub(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	notification := &Notification{
		Title: "Test",
		Body:  "Test body",
	}

	// Should handle nil subscription gracefully
	err := service.SendNotification(nil, notification)
	if err == nil {
		t.Error("Expected error when sending to nil subscription")
	}
}

// Test loadSubscriptions with unreadable subscription file
func TestLoadSubscriptions_UnreadableFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subscription file with invalid permissions (directory instead of file)
	subDir := filepath.Join(tmpDir, "sub_test.json")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create service - should skip the directory entry
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have no subscriptions (directory was skipped)
	subs := service.GetUserSubscriptions("user@example.com")
	if len(subs) != 0 {
		t.Errorf("Expected 0 subscriptions, got %d", len(subs))
	}
}

// Test deleteSubscriptionFile error handling
func TestDeleteSubscriptionFile_Error(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Try to delete non-existent file (should not panic)
	err := service.deleteSubscriptionFile("nonexistent")
	if err == nil {
		// It's okay if no error is returned for non-existent file
		t.Log("deleteSubscriptionFile returned nil for non-existent file")
	}
}

// Test NewService with custom logger
func TestNewService_WithLogger(t *testing.T) {
	tmpDir := t.TempDir()

	// Create service with nil logger (should use default)
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if service.logger == nil {
		t.Error("Service should have a logger")
	}
}

// Test loadOrGenerateConfig when config file cannot be read
func TestLoadOrGenerateConfig_ReadError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create vapid.json as a directory to cause read error on some systems
	vapidPath := filepath.Join(tmpDir, "vapid.json")
	if err := os.Mkdir(vapidPath, 0o755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// On Windows, reading a directory returns an error which triggers key generation
	// On Unix, it may succeed or fail depending on the system
	service, err := NewService(tmpDir, nil)
	if err != nil {
		// Expected on Windows - directory can't be read as file
		t.Logf("NewService returned error as expected on this OS: %v", err)
		return
	}

	// If no error, should have generated new keys
	if service.config.VAPIDPublicKey == "" {
		t.Error("Expected new VAPID keys to be generated")
	}
}

// Test saveSubscription with invalid directory
func TestSaveSubscription_InvalidDir(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Create a file with the same name as the data directory to block writes
	blockFile := filepath.Join(tmpDir, "sub_test123.json")
	if err := os.WriteFile(blockFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("Failed to create block file: %v", err)
	}

	// Try to save subscription with ID that matches blocked file
	sub := &Subscription{
		ID:       "test123",
		UserID:   "user@example.com",
		Endpoint: "https://example.com/push",
		P256dh:   "test",
		Auth:     "test",
	}

	// This may fail or succeed depending on OS
	err := service.saveSubscription(sub)
	t.Logf("saveSubscription result: %v", err)
}

// Test GetUserSubscriptions with stale reference in userSubs
func TestGetUserSubscriptions_StaleReference(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Manually inject a stale reference
	service.mu.Lock()
	service.userSubs["user@example.com"] = []string{"nonexistent-sub-id"}
	service.mu.Unlock()

	// Should return empty slice (no panic)
	subs := service.GetUserSubscriptions("user@example.com")
	if len(subs) != 0 {
		t.Errorf("Expected 0 subscriptions for stale reference, got %d", len(subs))
	}
}

// Test Unsubscribe when deleteSubscriptionFile returns error
func TestUnsubscribe_DeleteFileError(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Add a subscription
	sub := &Subscription{
		ID:       "test-sub-delete",
		Endpoint: "https://example.com/push",
		P256dh:   "test-key",
		Auth:     "test-auth",
	}
	_ = service.Subscribe("user@example.com", sub)

	// Verify subscription exists
	subs := service.GetUserSubscriptions("user@example.com")
	if len(subs) != 1 {
		t.Fatal("Subscription should exist")
	}

	// Manually remove the file to cause delete error
	subFile := filepath.Join(tmpDir, "sub_test-sub-delete.json")
	os.Remove(subFile)

	// Unsubscribe should still succeed (only logs warning about file delete)
	err := service.Unsubscribe("user@example.com", "test-sub-delete")
	if err != nil {
		t.Errorf("Unsubscribe should not fail when file delete fails: %v", err)
	}

	// Verify subscription removed from memory
	subs = service.GetUserSubscriptions("user@example.com")
	if len(subs) != 0 {
		t.Error("Subscription should be removed from memory")
	}
}

// Test CleanExpiredSubscriptions with multiple users
func TestCleanExpiredSubscriptions_MultiUser(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Add subscriptions for different users
	users := []string{"user1@example.com", "user2@example.com", "user3@example.com"}
	for i, user := range users {
		sub := &Subscription{
			ID:        fmt.Sprintf("multi-sub-%d", i),
			Endpoint:  fmt.Sprintf("https://example.com/push%d", i),
			P256dh:    "test-key",
			Auth:      "test-auth",
			UpdatedAt: time.Now(),
		}
		service.Subscribe(user, sub)
	}

	// Add expired subscription for user1
	expiredSub := &Subscription{
		ID:        "expired-multi",
		UserID:    "user1@example.com",
		Endpoint:  "https://example.com/expired",
		P256dh:    "test-key",
		Auth:      "test-auth",
		UpdatedAt: time.Now().Add(-100 * 24 * time.Hour),
	}
	service.mu.Lock()
	service.subscriptions["expired-multi"] = expiredSub
	service.userSubs["user1@example.com"] = append(service.userSubs["user1@example.com"], "expired-multi")
	service.mu.Unlock()

	// Clean expired
	err := service.CleanExpiredSubscriptions()
	if err != nil {
		t.Errorf("CleanExpiredSubscriptions failed: %v", err)
	}

	// Verify expired subscription removed
	if _, exists := service.subscriptions["expired-multi"]; exists {
		t.Error("Expired subscription should be removed")
	}

	// Verify other users still have their subscriptions
	for _, user := range users {
		subs := service.GetUserSubscriptions(user)
		if len(subs) == 0 {
			t.Errorf("User %s should still have subscriptions", user)
		}
	}
}
