package server

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/alert"
	"github.com/umailserver/umailserver/internal/config"
)

// TestStartAlertChecker_NoAlertManager tests starting checker without alert manager
func TestStartAlertChecker_NoAlertManager(t *testing.T) {
	srv := helperServer(t)

	// Should not panic and should not block
	srv.startAlertChecker()
}

// TestStartAlertChecker_DisabledAlertManager tests starting checker with disabled alert manager
func TestStartAlertChecker_DisabledAlertManager(t *testing.T) {
	srv := helperServer(t)
	alertMgr := alert.NewManager(alert.Config{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	srv.alertMgr = alertMgr

	// Should not panic and should not block
	srv.startAlertChecker()
}

// TestCheckAlerts_NoAlertManager tests checking alerts without alert manager
func TestCheckAlerts_NoAlertManager(t *testing.T) {
	srv := helperServer(t)

	// Should not panic
	srv.checkAlerts()
}

// TestCheckAlerts_DisabledAlertManager tests checking alerts with disabled alert manager
func TestCheckAlerts_DisabledAlertManager(t *testing.T) {
	srv := helperServer(t)
	alertMgr := alert.NewManager(alert.Config{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	srv.alertMgr = alertMgr

	// Should not panic
	srv.checkAlerts()
}

// TestCheckAlerts_WithEnabledManager tests checking alerts with enabled manager
func TestCheckAlerts_WithEnabledManager(t *testing.T) {
	srv := helperServer(t)
	alertMgr := alert.NewManager(alert.Config{
		Enabled:        true,
		QueueThreshold: 100,
		TLSWarningDays: 30,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	srv.alertMgr = alertMgr

	// Should not panic
	srv.checkAlerts()
}

// TestCheckAlerts_NoQueueManager tests checking alerts without queue manager
func TestCheckAlerts_NoQueueManager(t *testing.T) {
	srv := helperServer(t)
	alertMgr := alert.NewManager(alert.Config{
		Enabled:        true,
		QueueThreshold: 100,
		TLSWarningDays: 30,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	srv.alertMgr = alertMgr
	srv.queue = nil

	// Should not panic
	srv.checkAlerts()
}

// TestCheckAlerts_NoTLSManager tests checking alerts without TLS manager
func TestCheckAlerts_NoTLSManager(t *testing.T) {
	srv := helperServer(t)
	alertMgr := alert.NewManager(alert.Config{
		Enabled:        true,
		QueueThreshold: 100,
		TLSWarningDays: 30,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	srv.alertMgr = alertMgr
	srv.tlsManager = nil

	// Should not panic
	srv.checkAlerts()
}

// TestBuildAlertConfig_DefaultsBackfill verifies that zero-valued fields in
// config.AlertConfig are replaced with alert.DefaultConfig() values when the
// server constructs the alert manager.
func TestBuildAlertConfig_DefaultsBackfill(t *testing.T) {
	cfg := config.AlertConfig{Enabled: true, WebhookURL: "https://hooks.example.com/x"}
	out := buildAlertConfig(cfg)

	defaults := alert.DefaultConfig()
	if out.MinInterval != defaults.MinInterval {
		t.Errorf("MinInterval = %v, want %v", out.MinInterval, defaults.MinInterval)
	}
	if out.MaxAlerts != defaults.MaxAlerts {
		t.Errorf("MaxAlerts = %d, want %d", out.MaxAlerts, defaults.MaxAlerts)
	}
	if out.DiskThreshold != defaults.DiskThreshold {
		t.Errorf("DiskThreshold = %v, want %v", out.DiskThreshold, defaults.DiskThreshold)
	}
	if out.MemoryThreshold != defaults.MemoryThreshold {
		t.Errorf("MemoryThreshold = %v, want %v", out.MemoryThreshold, defaults.MemoryThreshold)
	}
	if out.ErrorThreshold != defaults.ErrorThreshold {
		t.Errorf("ErrorThreshold = %v, want %v", out.ErrorThreshold, defaults.ErrorThreshold)
	}
	if out.TLSWarningDays != defaults.TLSWarningDays {
		t.Errorf("TLSWarningDays = %d, want %d", out.TLSWarningDays, defaults.TLSWarningDays)
	}
	if out.QueueThreshold != defaults.QueueThreshold {
		t.Errorf("QueueThreshold = %d, want %d", out.QueueThreshold, defaults.QueueThreshold)
	}
}

// TestBuildAlertConfig_ExplicitValuesPreserved verifies user-supplied values
// are passed through unchanged (not overridden by defaults).
func TestBuildAlertConfig_ExplicitValuesPreserved(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:         true,
		WebhookURL:      "https://hooks.example.com/x",
		WebhookHeaders:  map[string]string{"X-Token": "abc"},
		WebhookTemplate: "{{.Name}}",
		SMTPServer:      "smtp.example.com",
		SMTPPort:        587,
		SMTPUsername:    "alert@example.com",
		SMTPPassword:    "s3cret",
		FromAddress:     "alert@example.com",
		ToAddresses:     []string{"oncall@example.com"},
		UseTLS:          true,
		MinInterval:     config.Duration(30 * time.Second),
		MaxAlerts:       42,
		DiskThreshold:   70,
		MemoryThreshold: 80,
		ErrorThreshold:  1.5,
		TLSWarningDays:  21,
		QueueThreshold:  250,
	}
	out := buildAlertConfig(cfg)

	if !out.Enabled || out.WebhookURL != cfg.WebhookURL {
		t.Errorf("basic fields not copied: %+v", out)
	}
	if string(out.SMTPPassword) != "s3cret" {
		t.Errorf("SMTPPassword conversion broken: %q", string(out.SMTPPassword))
	}
	if out.MinInterval != 30*time.Second {
		t.Errorf("MinInterval = %v, want 30s", out.MinInterval)
	}
	if out.MaxAlerts != 42 || out.QueueThreshold != 250 || out.TLSWarningDays != 21 {
		t.Errorf("ints not preserved: %+v", out)
	}
	if out.DiskThreshold != 70 || out.MemoryThreshold != 80 || out.ErrorThreshold != 1.5 {
		t.Errorf("thresholds not preserved: %+v", out)
	}
	if out.WebhookHeaders["X-Token"] != "abc" {
		t.Errorf("WebhookHeaders not preserved: %#v", out.WebhookHeaders)
	}
	if len(out.ToAddresses) != 1 || out.ToAddresses[0] != "oncall@example.com" {
		t.Errorf("ToAddresses not preserved: %v", out.ToAddresses)
	}
}
