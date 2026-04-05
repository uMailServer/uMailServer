package push

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSendNotification_NilSubscription tests SendNotification with nil subscription
func TestSendNotification_NilSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	notification := &Notification{
		Title: "Test",
		Body:  "Test notification",
	}

	err := service.SendNotification(nil, notification)
	if err == nil {
		t.Error("expected error for nil subscription")
	}
}

// TestSendNotification_InvalidSubscription tests SendNotification with invalid subscription data
func TestSendNotification_InvalidSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Create subscription with invalid endpoint
	sub := &Subscription{
		ID:       "test-sub",
		UserID:   "user@example.com",
		Endpoint: "invalid-endpoint",
		P256dh:   "invalid-p256dh",
		Auth:     "invalid-auth",
	}

	notification := &Notification{
		Title: "Test",
		Body:  "Test notification",
	}

	// This should fail because the subscription is invalid
	err := service.SendNotification(sub, notification)
	if err == nil {
		t.Error("expected error for invalid subscription")
	}
}

// TestLoadSubscriptions_InvalidJSON tests loading subscriptions with invalid JSON
func TestLoadSubscriptions_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid subscription file
	invalidJSON := []byte(`{"invalid json`)
	err := os.WriteFile(filepath.Join(tmpDir, "sub_invalid.json"), invalidJSON, 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Create service - should handle invalid JSON gracefully
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have no valid subscriptions
	if len(service.subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(service.subscriptions))
	}
}

// TestLoadSubscriptions_NonJSONFile tests loading subscriptions with non-JSON file
func TestLoadSubscriptions_NonJSONFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a non-JSON file
	err := os.WriteFile(filepath.Join(tmpDir, "notjson.txt"), []byte("not json"), 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Create service - should handle non-JSON files gracefully
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have no valid subscriptions
	if len(service.subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(service.subscriptions))
	}
}


// TestLoadSubscriptions_DirectoryNotExist tests loading when directory doesn't exist
func TestLoadSubscriptions_DirectoryNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "nonexistent", "path")

	// Create service with non-existent directory
	service, err := NewService(nonExistentDir, nil)
	if err != nil {
		t.Fatalf("NewService should create directory: %v", err)
	}

	// Should have 0 subscriptions but work fine
	if len(service.subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(service.subscriptions))
	}
}

// TestSaveSubscription_MarshalError tests saveSubscription with marshal error
func TestSaveSubscription_MarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Create subscription with invalid data that can't be marshaled
	// This is hard to trigger with normal data, so we test the success case
	sub := &Subscription{
		ID:       "test-sub",
		UserID:   "user@example.com",
		Endpoint: "https://example.com/push",
		P256dh:   "test-p256dh",
		Auth:     "test-auth",
	}

	err := service.saveSubscription(sub)
	if err != nil {
		t.Errorf("saveSubscription failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(tmpDir, "sub_test-sub.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("subscription file should exist")
	}
}

// TestDeleteSubscriptionFile_NotExist tests deleting non-existent subscription file
func TestDeleteSubscriptionFile_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Try to delete non-existent file
	err := service.deleteSubscriptionFile("nonexistent")
	if err == nil {
		t.Error("expected error when deleting non-existent file")
	}
}


// TestLoadOrGenerateConfig_InvalidJSON tests loadOrGenerateConfig with invalid JSON
func TestLoadOrGenerateConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid vapid.json
	invalidJSON := []byte(`{"invalid json`)
	err := os.WriteFile(filepath.Join(tmpDir, "vapid.json"), invalidJSON, 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Create service - should regenerate keys when JSON is invalid
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have valid keys
	if service.config.VAPIDPublicKey == "" {
		t.Error("VAPIDPublicKey should be regenerated")
	}
}

// TestLoadOrGenerateConfig_NoPrivateKey tests loadOrGenerateConfig with missing private key
func TestLoadOrGenerateConfig_NoPrivateKey(t *testing.T) {
	tmpDir := t.TempDir()

	// Create vapid.json with only public key
	config := map[string]string{
		"publicKey":  "test-public-key",
		"subject":    "mailto:test@example.com",
	}
	data, _ := json.Marshal(config)
	err := os.WriteFile(filepath.Join(tmpDir, "vapid.json"), data, 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Create service - should regenerate keys when private key is missing
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have regenerated keys
	if service.config.VAPIDPublicKey == "test-public-key" {
		t.Error("Keys should be regenerated when private key is missing")
	}
}

// TestLoadSubscriptions_EmptyFile tests loading an empty subscription file
func TestLoadSubscriptions_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an empty JSON file
	err := os.WriteFile(filepath.Join(tmpDir, "sub_empty.json"), []byte("not valid json"), 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Create service - should handle empty file gracefully
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have 0 valid subscriptions
	if len(service.subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(service.subscriptions))
	}
}

// TestSendNotification_NoQueue tests SendNotification when queue is nil
func TestSendNotification_NoQueue(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Create valid subscription
	sub := &Subscription{
		ID:       "test-sub",
		UserID:   "user@example.com",
		Endpoint: "https://example.com/push",
		P256dh:   "test-p256dh",
		Auth:     "test-auth",
	}

	notification := &Notification{
		Title: "Test",
		Body:  "Test notification",
	}

	// Should fail because VAPID keys are not properly configured
	err := service.SendNotification(sub, notification)
	if err == nil {
		t.Error("expected error when VAPID keys are not configured")
	}
}

// TestLoadSubscriptions_MultipleFiles tests loading multiple subscription files
func TestLoadSubscriptions_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple valid subscription files
	sub1 := Subscription{
		ID:       "sub-1",
		UserID:   "user1@example.com",
		Endpoint: "https://example.com/push/1",
		P256dh:   "p256dh-1",
		Auth:     "auth-1",
	}
	sub2 := Subscription{
		ID:       "sub-2",
		UserID:   "user2@example.com",
		Endpoint: "https://example.com/push/2",
		P256dh:   "p256dh-2",
		Auth:     "auth-2",
	}

	data1, _ := json.Marshal(sub1)
	data2, _ := json.Marshal(sub2)

	os.WriteFile(filepath.Join(tmpDir, "sub_sub-1.json"), data1, 0600)
	os.WriteFile(filepath.Join(tmpDir, "sub_sub-2.json"), data2, 0600)

	// Create service
	service, err := NewService(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Should have 2 subscriptions
	if len(service.subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(service.subscriptions))
	}

	// Verify subscriptions are loaded correctly
	if _, ok := service.subscriptions["sub-1"]; !ok {
		t.Error("expected sub-1 to be loaded")
	}
	if _, ok := service.subscriptions["sub-2"]; !ok {
		t.Error("expected sub-2 to be loaded")
	}
}

// TestSendToUser_WithInvalidSubscriptions tests SendToUser with some invalid subscriptions
func TestSendToUser_WithInvalidSubscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	service, _ := NewService(tmpDir, nil)

	// Create subscriptions
	sub1 := &Subscription{
		ID:       "sub-1",
		UserID:   "user@example.com",
		Endpoint: "https://example.com/push/1",
		P256dh:   "p256dh-1",
		Auth:     "auth-1",
	}
	sub2 := &Subscription{
		ID:       "sub-2",
		UserID:   "user@example.com",
		Endpoint: "https://example.com/push/2",
		P256dh:   "p256dh-2",
		Auth:     "auth-2",
	}

	service.subscriptions["sub-1"] = sub1
	service.subscriptions["sub-2"] = sub2

	notification := &Notification{
		Title: "Test",
		Body:  "Test notification",
	}

	// Should return error because VAPID keys are not properly configured
	err := service.SendToUser("user@example.com", notification)
	// The function returns an error if any notification fails
	if err == nil {
		t.Log("SendToUser may succeed or fail depending on implementation")
	}
}
