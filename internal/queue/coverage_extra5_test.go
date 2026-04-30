package queue

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// --- sendSuccessDSN tests ---

func TestManager_SendSuccessDSN_ZeroRet(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: dataDir + "/msg",
		Ret:         0, // DSNRetFull - wants original message
	}

	// sendSuccessDSN is called internally when message is delivered
	// We just verify the function exists and doesn't panic
	mgr.sendSuccessDSN(entry)
}

// --- createFallbackBounce tests ---

func TestManager_CreateFallbackBounce(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		LastError: "550 No such user",
		CreatedAt: testTime(),
	}

	originalMsg := []byte("From: sender\r\nTo: recipient\r\nSubject: Test\r\n\r\nBody")

	result := m.createFallbackBounce(entry, originalMsg)
	if len(result) == 0 {
		t.Fatal("Expected non-empty bounce message")
	}
}

func TestManager_CreateFallbackBounce_EmptyOriginal(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		LastError: "Connection timeout",
		CreatedAt: testTime(),
	}

	result := m.createFallbackBounce(entry, nil)
	if len(result) == 0 {
		t.Fatal("Expected non-empty bounce message")
	}
}

func TestManager_CreateFallbackBounce_LongError(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		LastError: "451 4.4.2 DNS zone timeout exceeded - Please try again later",
		CreatedAt: testTime(),
	}

	result := m.createFallbackBounce(entry, []byte("original"))
	if len(result) == 0 {
		t.Fatal("Expected non-empty bounce message")
	}
}

func TestManager_CreateFallbackBounce_MultipleRecipients(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:      "sender@example.com",
		To:        []string{"recipient1@example.com", "recipient2@example.com"},
		LastError: "550 mailbox full",
		CreatedAt: testTime(),
	}

	result := m.createFallbackBounce(entry, nil)
	if len(result) == 0 {
		t.Fatal("Expected non-empty bounce message")
	}
}

func TestManager_CreateFallbackBounce_NoError(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		LastError: "",
		CreatedAt: testTime(),
	}

	result := m.createFallbackBounce(entry, nil)
	if len(result) == 0 {
		t.Fatal("Expected non-empty bounce message")
	}
}

// --- generateBounce coverage ---

func TestManager_GenerateBounce_RetFull(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: dataDir + "/msg",
		Ret:         0, // DSNRetFull
		LastError:   "550 mailbox full",
		CreatedAt:   testTime(),
	}

	m.generateBounce(entry)
}

func TestManager_GenerateBounce_RetHeaders(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: dataDir + "/msg",
		Ret:         1, // DSNRetHeaders
		LastError:   "550 user unknown",
		CreatedAt:   testTime(),
	}

	m.generateBounce(entry)
}

func TestManager_GenerateBounce_RetNotFound(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	entry := &db.QueueEntry{
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: dataDir + "/msg",
		Ret:         999, // Invalid - treated as no original
		LastError:   "550 unknown error",
		CreatedAt:   testTime(),
	}

	m.generateBounce(entry)
}

// --- SetWebhookTrigger ---

type mockWebhookTrigger struct {
	triggered bool
}

func (m *mockWebhookTrigger) Trigger(eventType string, data interface{}) {
	m.triggered = true
}

func TestManager_SetWebhookTrigger(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	wh := &mockWebhookTrigger{}
	m.SetWebhookTrigger(wh)

	if m.webhook == nil {
		t.Fatal("webhook not set")
	}

	m.webhook.Trigger("test", nil)
	if !wh.triggered {
		t.Error("webhook was not triggered")
	}
}

// --- getStats ---

func TestManager_GetStats_VariousStatuses(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	m := NewManager(database, nil, dataDir, nil)

	// Enqueue entries with different statuses
	statuses := []string{"pending", "sending", "failed", "delivered", "bounced"}
	for _, status := range statuses {
		entry := &db.QueueEntry{
			ID:        "msg-" + status,
			From:      "sender@example.com",
			To:        []string{"recipient@example.com"},
			Status:    status,
			CreatedAt: time.Now(),
			NextRetry: time.Now(),
		}
		_ = database.Enqueue(entry)
	}

	stats, err := m.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Total != 5 {
		t.Errorf("expected total=5, got %d", stats.Total)
	}
	if stats.Pending != 1 {
		t.Errorf("expected pending=1, got %d", stats.Pending)
	}
	if stats.Sending != 1 {
		t.Errorf("expected sending=1, got %d", stats.Sending)
	}
	if stats.Failed != 1 {
		t.Errorf("expected failed=1, got %d", stats.Failed)
	}
	if stats.Delivered != 1 {
		t.Errorf("expected delivered=1, got %d", stats.Delivered)
	}
	if stats.Bounced != 1 {
		t.Errorf("expected bounced=1, got %d", stats.Bounced)
	}
}

// --- ParseMDNAddress ---

func TestParseMDNAddress_Empty(t *testing.T) {
	_, err := ParseMDNAddress("")
	if err == nil {
		t.Error("expected error for empty address")
	}
}

func TestParseMDNAddress_InvalidWithAngle(t *testing.T) {
	result, err := ParseMDNAddress("invalid <broken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Original != "invalid <broken" {
		t.Errorf("expected original to be preserved, got %s", result.Original)
	}
}

func TestParseMDNAddress_InvalidWithoutAngle(t *testing.T) {
	result, err := ParseMDNAddress("not-an-email")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Original != "not-an-email" {
		t.Errorf("expected original to be preserved, got %s", result.Original)
	}
}

// --- helper ---

func testTime() time.Time {
	t, _ := time.Parse(time.RFC1123Z, "Mon, 01 Jan 2024 12:00:00 +0000")
	return t
}
