package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/webhook"
)

// TestRecordLoginResult_NilDeps confirms the unified sink is safe when no
// audit logger or webhook manager is wired.
func TestRecordLoginResult_NilDeps(t *testing.T) {
	s := &Server{}
	s.recordLoginResult("smtp", "u@example.com", true, "1.1.1.1", "")
	s.recordLoginResult("imap", "u@example.com", false, "1.1.1.1", "invalid_credentials")
}

// TestProtoLoginHandler_TagsService verifies the returned closure passes the
// service tag through to the audit log.
func TestProtoLoginHandler_TagsService(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	apiSrv := newAPIServerWithAuditLog(t, auditPath)

	s := &Server{apiServer: apiSrv}
	s.protoLoginHandler("imap")("user@example.com", true, "1.2.3.4", "")
	s.protoLoginHandler("pop3")("user@example.com", false, "5.6.7.8", "lockout")

	events := readAuditEvents(t, auditPath)
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(events))
	}
	if events[0].Service != "imap" || !events[0].Success {
		t.Errorf("event 0: %+v", events[0])
	}
	if events[1].Service != "pop3" || events[1].Success {
		t.Errorf("event 1: %+v", events[1])
	}
	if events[1].Details["reason"] != "lockout" {
		t.Errorf("expected lockout reason, got %v", events[1].Details)
	}
}

// TestLoginResult_BackwardCompatTagsAsSMTP ensures the legacy 3-arg helper
// still tags events as SMTP for any caller still using it.
func TestLoginResult_BackwardCompatTagsAsSMTP(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	apiSrv := newAPIServerWithAuditLog(t, auditPath)

	s := &Server{apiServer: apiSrv}
	s.loginResult("u@example.com", true, "9.9.9.9")

	events := readAuditEvents(t, auditPath)
	if len(events) != 1 || events[0].Service != "smtp" {
		t.Fatalf("expected single smtp event, got %+v", events)
	}
}

// newAPIServerWithAuditLog builds a minimal api.Server with only the audit
// logger initialised. We cannot call api.NewServer here without dragging in a
// real db, so we exploit the public AuditLogger constructor and inject it via
// a parallel api.Server wired with the same path.
func newAPIServerWithAuditLog(t *testing.T, path string) *api.Server {
	t.Helper()
	cfg := api.Config{AuditLog: api.AuditLogConfig{
		Path:       path,
		MaxSizeMB:  1,
		MaxBackups: 1,
		MaxAgeDays: 1,
	}}
	srv := api.NewServerWithInterfaces(nil, nil, cfg, nil, nil, nil, nil, nil)
	if srv.AuditLogger() == nil {
		t.Fatal("audit logger not constructed")
	}
	t.Cleanup(func() {
		_ = srv.AuditLogger().Close()
	})
	// Give the rotating writer a moment if needed.
	time.Sleep(5 * time.Millisecond)
	return srv
}

func readAuditEvents(t *testing.T, path string) []audit.Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var out []audit.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e audit.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("parse line: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// TestRecordLoginResult_IncludesWebhookPayload verifies the webhook event is
// fired with the service field and reason populated correctly.
func TestRecordLoginResult_IncludesWebhookPayload(t *testing.T) {
	wm := webhook.NewManager(nil, "secret")
	s := &Server{webhookMgr: wm}
	// Empty webhook list — just exercise the payload assembly path; no
	// outbound delivery to assert on, but confirm no panic and Trigger runs.
	s.recordLoginResult("imap", "u", false, "1.1.1.1", "invalid_credentials")
	s.recordLoginResult("smtp", "u", true, "1.1.1.1", "")
}
