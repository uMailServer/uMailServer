package alert

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled != false {
		t.Error("expected Enabled=false by default")
	}
	if cfg.DiskThreshold != 85.0 {
		t.Errorf("expected DiskThreshold=85, got %f", cfg.DiskThreshold)
	}
	if cfg.MemoryThreshold != 90.0 {
		t.Errorf("expected MemoryThreshold=90, got %f", cfg.MemoryThreshold)
	}
	if cfg.ErrorThreshold != 5.0 {
		t.Errorf("expected ErrorThreshold=5, got %f", cfg.ErrorThreshold)
	}
	if cfg.TLSWarningDays != 7 {
		t.Errorf("expected TLSWarningDays=7, got %d", cfg.TLSWarningDays)
	}
	if cfg.QueueThreshold != 1000 {
		t.Errorf("expected QueueThreshold=1000, got %d", cfg.QueueThreshold)
	}
	if cfg.MinInterval != 5*time.Minute {
		t.Errorf("expected MinInterval=5m, got %v", cfg.MinInterval)
	}
	if cfg.MaxAlerts != 100 {
		t.Errorf("expected MaxAlerts=100, got %d", cfg.MaxAlerts)
	}
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	mgr := NewManager(cfg, nil)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if !mgr.IsEnabled() {
		t.Error("expected manager to be enabled")
	}
}

func TestManager_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = tt.enabled
			mgr := NewManager(cfg, nil)

			if mgr.IsEnabled() != tt.expected {
				t.Errorf("expected IsEnabled()=%v, got %v", tt.expected, mgr.IsEnabled())
			}
		})
	}
}

func TestManager_Send_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	mgr := NewManager(cfg, nil)

	err := mgr.Send("test", SeverityInfo, "test message", nil)
	if err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}
}

func TestManager_Send_RateLimiting(t *testing.T) {
	// Create test server
	var receivedAlerts []Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		receivedAlerts = append(receivedAlerts, alert)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.MinInterval = 100 * time.Millisecond
	cfg.MaxAlerts = 10

	mgr := NewManager(cfg, nil)

	// Send first alert - should succeed
	err := mgr.Send("test_alert", SeverityWarning, "first alert", nil)
	if err != nil {
		t.Errorf("first send failed: %v", err)
	}

	// Send same alert immediately - should be rate limited
	err = mgr.Send("test_alert", SeverityWarning, "second alert", nil)
	if err != nil {
		t.Errorf("second send should not error (silently dropped), got %v", err)
	}

	// Wait for rate limit to expire
	time.Sleep(150 * time.Millisecond)

	// Send again - should succeed
	err = mgr.Send("test_alert", SeverityWarning, "third alert", nil)
	if err != nil {
		t.Errorf("third send after rate limit failed: %v", err)
	}

	// Should have received 2 alerts (first and third)
	if len(receivedAlerts) != 2 {
		t.Errorf("expected 2 received alerts, got %d", len(receivedAlerts))
	}
}

func TestManager_Send_MaxAlertsPerHour(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.MaxAlerts = 2

	mgr := NewManager(cfg, nil)

	// Send max alerts
	for i := 0; i < 2; i++ {
		err := mgr.Send(string(rune('a'+i)), SeverityWarning, "alert", nil)
		if err != nil {
			t.Errorf("send %d failed: %v", i, err)
		}
	}

	// Third alert should be dropped due to hourly limit
	// (different name so not rate limited by MinInterval)
	cfg2 := DefaultConfig()
	cfg2.Enabled = true
	cfg2.WebhookURL = server.URL
	cfg2.MaxAlerts = 2
	mgr2 := NewManager(cfg2, nil)

	// Reset internal state manually for this test
	mgr2.hourlyCount = 2
	mgr2.hourStart = time.Now()

	// Try to send another - should fail silently
	err := mgr2.Send("c", SeverityWarning, "third alert", nil)
	if err != nil {
		t.Errorf("third send should not error (silently dropped), got %v", err)
	}
}

func TestManager_sendWebhook(t *testing.T) {
	var receivedAlert Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Error("expected custom header")
		}

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedAlert)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.WebhookURL = server.URL
	cfg.WebhookHeaders = map[string]string{
		"X-Custom-Header": "custom-value",
	}

	mgr := NewManager(cfg, nil)

	alert := Alert{
		ID:        "test-123",
		Name:      "test_alert",
		Severity:  SeverityWarning,
		Message:   "test message",
		Timestamp: time.Now(),
	}

	err := mgr.sendWebhook(alert)
	if err != nil {
		t.Errorf("sendWebhook failed: %v", err)
	}

	if receivedAlert.Name != "test_alert" {
		t.Errorf("expected name 'test_alert', got %s", receivedAlert.Name)
	}
	if receivedAlert.Severity != SeverityWarning {
		t.Errorf("expected severity 'warning', got %s", receivedAlert.Severity)
	}
}

func TestManager_sendWebhook_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.WebhookURL = server.URL

	mgr := NewManager(cfg, nil)

	alert := Alert{
		Name:      "test_alert",
		Severity:  SeverityWarning,
		Timestamp: time.Now(),
	}

	err := mgr.sendWebhook(alert)
	if err == nil {
		t.Error("expected error for 500 status, got nil")
	}
}

func TestManager_sendEmail(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SMTPServer = "invalid.smtp.server"
	cfg.SMTPPort = 587
	cfg.FromAddress = "test@example.com"
	cfg.ToAddresses = []string{"alert@example.com"}

	mgr := NewManager(cfg, nil)

	alert := Alert{
		Name:      "test_alert",
		Severity:  SeverityCritical,
		Message:   "test message",
		Details:   map[string]interface{}{"key": "value"},
		Timestamp: time.Now(),
	}

	// Should fail due to invalid SMTP server, but shouldn't panic
	err := mgr.sendEmail(alert)
	if err == nil {
		t.Error("expected error for invalid SMTP server")
	}
}

func TestManager_CheckDiskSpace(t *testing.T) {
	var receivedAlert *Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		receivedAlert = &alert
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.DiskThreshold = 85.0
	cfg.MinInterval = 0 // Disable rate limiting for this test

	mgr := NewManager(cfg, nil)

	// Check below threshold - no alert
	mgr.CheckDiskSpace(50.0, "/data")
	if receivedAlert != nil {
		t.Error("expected no alert below threshold")
	}

	// Check at warning level (75% = 85-10) - should alert
	mgr.CheckDiskSpace(75.0, "/data")
	if receivedAlert == nil {
		t.Fatal("expected warning alert")
	}
	if receivedAlert.Name != "disk_space_warning" {
		t.Errorf("expected name 'disk_space_warning', got %s", receivedAlert.Name)
	}
	if receivedAlert.Severity != SeverityWarning {
		t.Errorf("expected severity 'warning', got %s", receivedAlert.Severity)
	}

	// Reset for critical test
	receivedAlert = nil
	cfg2 := DefaultConfig()
	cfg2.Enabled = true
	cfg2.WebhookURL = server.URL
	cfg2.DiskThreshold = 85.0
	cfg2.MinInterval = 0
	mgr2 := NewManager(cfg2, nil)

	// Check above threshold - critical alert
	mgr2.CheckDiskSpace(90.0, "/data")
	if receivedAlert == nil {
		t.Fatal("expected critical alert")
	}
	if receivedAlert.Name != "disk_space_critical" {
		t.Errorf("expected name 'disk_space_critical', got %s", receivedAlert.Name)
	}
	if receivedAlert.Severity != SeverityCritical {
		t.Errorf("expected severity 'critical', got %s", receivedAlert.Severity)
	}
}

func TestManager_CheckMemory(t *testing.T) {
	var receivedAlert *Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		receivedAlert = &alert
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.MemoryThreshold = 90.0
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)

	// Check below threshold - no alert
	mgr.CheckMemory(50.0, 512, 1024)
	if receivedAlert != nil {
		t.Error("expected no alert below threshold")
	}

	// Check above threshold - critical alert
	mgr.CheckMemory(95.0, 972, 1024)
	if receivedAlert == nil {
		t.Fatal("expected critical alert")
	}
	if receivedAlert.Name != "memory_critical" {
		t.Errorf("expected name 'memory_critical', got %s", receivedAlert.Name)
	}
}

func TestManager_CheckErrorRate(t *testing.T) {
	var receivedAlert *Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		receivedAlert = &alert
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.ErrorThreshold = 5.0
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)

	// Check below threshold - no alert
	mgr.CheckErrorRate(2.0, "5m")
	if receivedAlert != nil {
		t.Error("expected no alert below threshold")
	}

	// Check above threshold - warning alert
	mgr.CheckErrorRate(10.0, "5m")
	if receivedAlert == nil {
		t.Fatal("expected warning alert")
	}
	if receivedAlert.Name != "high_error_rate" {
		t.Errorf("expected name 'high_error_rate', got %s", receivedAlert.Name)
	}
}

func TestManager_CheckTLSCertificate(t *testing.T) {
	var alerts []Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		alerts = append(alerts, alert)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.TLSWarningDays = 7
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)

	// Check with plenty of time - no alert
	mgr.CheckTLSCertificate("example.com", 30)
	if len(alerts) > 0 {
		t.Error("expected no alert with 30 days remaining")
	}

	// Check near expiry - warning alert
	mgr.CheckTLSCertificate("example.com", 5)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Name != "tls_certificate_expiring" {
		t.Errorf("expected name 'tls_certificate_expiring', got %s", alerts[0].Name)
	}

	// Reset for expired test
	alerts = nil
	cfg2 := DefaultConfig()
	cfg2.Enabled = true
	cfg2.WebhookURL = server.URL
	cfg2.MinInterval = 0
	mgr2 := NewManager(cfg2, nil)

	// Check expired - critical alert
	mgr2.CheckTLSCertificate("example.com", -1)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Name != "tls_certificate_expired" {
		t.Errorf("expected name 'tls_certificate_expired', got %s", alerts[0].Name)
	}
	if alerts[0].Severity != SeverityCritical {
		t.Errorf("expected severity 'critical', got %s", alerts[0].Severity)
	}
}

func TestManager_CheckQueueBacklog(t *testing.T) {
	var receivedAlert *Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		receivedAlert = &alert
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.QueueThreshold = 1000
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)

	// Check below threshold - no alert
	mgr.CheckQueueBacklog(500)
	if receivedAlert != nil {
		t.Error("expected no alert below threshold")
	}

	// Check above threshold - warning alert
	mgr.CheckQueueBacklog(1500)
	if receivedAlert == nil {
		t.Fatal("expected warning alert")
	}
	if receivedAlert.Name != "queue_backlog" {
		t.Errorf("expected name 'queue_backlog', got %s", receivedAlert.Name)
	}
}

func TestManager_Info_Warn_Critical(t *testing.T) {
	var alerts []Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var alert Alert
		json.Unmarshal(body, &alert)
		alerts = append(alerts, alert)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)

	// Test Info
	err := mgr.Info("info_test", "info message", nil)
	if err != nil {
		t.Errorf("Info failed: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != SeverityInfo {
		t.Error("expected info alert")
	}

	// Test Warn
	err = mgr.Warn("warn_test", "warn message", nil)
	if err != nil {
		t.Errorf("Warn failed: %v", err)
	}
	if len(alerts) != 2 || alerts[1].Severity != SeverityWarning {
		t.Error("expected warning alert")
	}

	// Test Critical
	err = mgr.Critical("critical_test", "critical message", nil)
	if err != nil {
		t.Errorf("Critical failed: %v", err)
	}
	if len(alerts) != 3 || alerts[2].Severity != SeverityCritical {
		t.Error("expected critical alert")
	}
}

func TestManager_GetStats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxAlerts = 50

	mgr := NewManager(cfg, nil)

	// Send an alert to populate stats
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg.WebhookURL = server.URL
	cfg.MinInterval = 0
	mgr = NewManager(cfg, nil)
	mgr.Send("test", SeverityInfo, "test", nil)

	stats := mgr.GetStats()

	if stats["enabled"] != true {
		t.Error("expected enabled=true")
	}
	if stats["max_alerts"] != 50 {
		t.Errorf("expected max_alerts=50, got %v", stats["max_alerts"])
	}
}

func TestAlert_Serialization(t *testing.T) {
	alert := Alert{
		ID:        "test-123",
		Name:      "test_alert",
		Severity:  SeverityWarning,
		Message:   "test message",
		Details:   map[string]interface{}{"key": "value"},
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(alert)
	if err != nil {
		t.Fatalf("failed to marshal alert: %v", err)
	}

	var decoded Alert
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("failed to unmarshal alert: %v", err)
	}

	if decoded.Name != alert.Name {
		t.Errorf("expected name %s, got %s", alert.Name, decoded.Name)
	}
	if decoded.Severity != alert.Severity {
		t.Errorf("expected severity %s, got %s", alert.Severity, decoded.Severity)
	}
}

func TestGenerateAlertID(t *testing.T) {
	id1 := generateAlertID()
	time.Sleep(time.Millisecond) // Ensure different timestamp
	id2 := generateAlertID()

	if id1 == "" {
		t.Error("generateAlertID returned empty string")
	}
	if id1 == id2 {
		t.Error("generateAlertID should generate unique IDs")
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = server.URL
	cfg.MinInterval = 0

	mgr := NewManager(cfg, nil)

	// Concurrent sends
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			mgr.Send(string(rune('a'+id)), SeverityWarning, "concurrent alert", nil)
		}(i)
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and stats should be consistent
	stats := mgr.GetStats()
	if _, ok := stats["hourly_count"]; !ok {
		t.Error("expected hourly_count in stats")
	}
}
