package server

import (
	"log/slog"
	"os"
	"testing"

	"github.com/umailserver/umailserver/internal/alert"
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
