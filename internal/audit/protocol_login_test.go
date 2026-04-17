package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readEvents drains the audit log file and returns the parsed events.
func readEvents(t *testing.T, path string) []Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var out []Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("parse audit line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

func TestLogProtocolLogin_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewLogger(path, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer l.Close()

	l.LogProtocolLogin("imap", "user@example.com", "10.0.0.1", true, "")

	evts := readEvents(t, path)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	e := evts[0]
	if e.Type != LoginSuccess {
		t.Errorf("Type = %q", e.Type)
	}
	if e.Service != "imap" {
		t.Errorf("Service = %q", e.Service)
	}
	if !e.Success {
		t.Error("Success = false")
	}
	if e.User != "user@example.com" || e.IP != "10.0.0.1" {
		t.Errorf("user/ip: %+v", e)
	}
	if e.Details != nil {
		t.Errorf("expected no details on success, got %v", e.Details)
	}
}

func TestLogProtocolLogin_FailureWithReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, _ := NewLogger(path, 1, 1, 1)
	defer l.Close()

	l.LogProtocolLogin("smtp", "evil@example.com", "1.2.3.4", false, "invalid_credentials")

	evts := readEvents(t, path)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	e := evts[0]
	if e.Type != LoginFailure {
		t.Errorf("Type = %q", e.Type)
	}
	if e.Service != "smtp" {
		t.Errorf("Service = %q", e.Service)
	}
	if e.Success {
		t.Error("Success = true")
	}
	if e.Details["reason"] != "invalid_credentials" {
		t.Errorf("reason: %v", e.Details)
	}
}

func TestLogProtocolLogin_FailureNoReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, _ := NewLogger(path, 1, 1, 1)
	defer l.Close()

	l.LogProtocolLogin("pop3", "u", "1.1.1.1", false, "")

	evts := readEvents(t, path)
	if evts[0].Details != nil {
		t.Errorf("expected no details when reason is empty, got %v", evts[0].Details)
	}
}

func TestLogProtocolLogin_DisabledLoggerNoop(t *testing.T) {
	// NewLogger with empty path returns a disabled logger; calling helpers
	// must be a silent no-op (no panic, no error surfacing).
	l, err := NewLogger("", 0, 0, 0)
	if err != nil {
		t.Fatalf("NewLogger empty: %v", err)
	}
	l.LogProtocolLogin("imap", "u", "1.1.1.1", true, "")
}
