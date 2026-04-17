package smtp

import (
	"log/slog"
	"testing"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/sieve"
)

// TestAuthDMARCStage_SetReporter tests setting the DMARC reporter
func TestAuthDMARCStage_SetReporter(t *testing.T) {
	// Create a mock DMARC evaluator
	evaluator := auth.NewDMARCEvaluator(nil)
	stage := NewAuthDMARCStage(evaluator, slog.Default())

	reporter := auth.NewDMARCReporter(nil, nil, auth.DMARCReporterConfig{})

	// Set reporter should not panic
	stage.SetReporter(reporter)

	if stage.reporter != reporter {
		t.Error("reporter not set correctly")
	}
}

// TestAuthDMARCStage_SetReporter_Nil tests setting nil reporter
func TestAuthDMARCStage_SetReporter_Nil(t *testing.T) {
	evaluator := auth.NewDMARCEvaluator(nil)
	stage := NewAuthDMARCStage(evaluator, slog.Default())

	// Set nil reporter should not panic
	stage.SetReporter(nil)

	if stage.reporter != nil {
		t.Error("expected nil reporter")
	}
}

// TestServer_SetLoginResultHandler tests setting the login result handler
func TestServer_SetLoginResultHandler(t *testing.T) {
	server := &Server{}

	handler := func(username string, success bool, ip, reason string) {}
	server.SetLoginResultHandler(handler)

	if server.onLoginResult == nil {
		t.Error("login result handler not set")
	}
}

// TestServer_SetLoginResultHandler_Nil tests setting nil handler
func TestServer_SetLoginResultHandler_Nil(t *testing.T) {
	server := &Server{}

	server.SetLoginResultHandler(nil)

	if server.onLoginResult != nil {
		t.Error("expected nil handler")
	}
}

// TestServer_SetTracingProvider tests setting the tracing provider
func TestServer_SetTracingProvider(t *testing.T) {
	server := &Server{}

	// Set tracing provider should not panic (even with nil)
	server.SetTracingProvider(nil)
}

// TestSieveStage_SetVacationHandler tests setting the vacation handler
func TestSieveStage_SetVacationHandler(t *testing.T) {
	stage := &SieveStage{}

	handler := func(sender, recipient string, vacation sieve.VacationAction) {}
	stage.SetVacationHandler(handler)

	if stage.vacationHandler == nil {
		t.Error("vacation handler not set")
	}
}

// TestSieveStage_SetVacationHandler_Nil tests setting nil handler
func TestSieveStage_SetVacationHandler_Nil(t *testing.T) {
	stage := &SieveStage{}

	stage.SetVacationHandler(nil)

	if stage.vacationHandler != nil {
		t.Error("expected nil handler")
	}
}
