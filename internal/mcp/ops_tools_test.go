package mcp

// The queue, diagnostics, status and reload tools answered with fixed text
// ("retry requested", "Status: OK") and did nothing. They now act through
// the backends the server installs, and say so when one is missing.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/cli"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/health"
)

type fakeQueue struct {
	retried []string
	flushed int
}

func (q *fakeQueue) RetryEntry(id string) error { q.retried = append(q.retried, id); return nil }
func (q *fakeQueue) FlushQueue() error          { q.flushed++; return nil }

type fakeDiag struct{}

func (fakeDiag) CheckDNS(domain string) ([]cli.DNSCheckResult, error) {
	return []cli.DNSCheckResult{{RecordType: "MX", RecordName: domain, Status: "fail", Message: "no MX record", Expected: "mx." + domain}}, nil
}
func (fakeDiag) CheckTLS(host string) (*cli.TLSCheckResult, error) {
	return &cli.TLSCheckResult{Valid: true, Version: "TLS 1.3", Expiry: "2027-01-01"}, nil
}

func opsServer(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/mcp.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewServer(database)
}

// callOps calls a tool and returns the HTTP status, the result text and
// whether the result is an error.
func callOps(t *testing.T, s *Server, name string, args map[string]interface{}, admin bool) (int, string, bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": name, "arguments": args}})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	if admin {
		req = req.WithContext(context.WithValue(req.Context(), adminCtxKeyVal, true))
	}
	rr := httptest.NewRecorder()
	s.HandleHTTP(rr, req)
	var resp struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	text := ""
	for _, c := range resp.Result.Content {
		text += c.Text
	}
	return rr.Code, text, resp.Result.IsError
}

func TestMCPQueueToolsAct(t *testing.T) {
	s := opsServer(t)
	now := time.Now()
	for _, e := range []*db.QueueEntry{
		{ID: "q-old", From: "a@ex.com", To: []string{"x@far.test"}, Status: "failed", RetryCount: 5, LastError: "550 no such user", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "q-new", From: "b@ex.com", To: []string{"y@far.test", "z@far.test"}, Status: "pending", CreatedAt: now.Add(-time.Hour), NextRetry: now.Add(time.Minute)},
		{ID: "q-done", From: "c@ex.com", To: []string{"w@far.test"}, Status: "delivered", CreatedAt: now},
	} {
		if err := s.db.Enqueue(e); err != nil {
			t.Fatal(err)
		}
	}

	_, text, isErr := callOps(t, s, "get_queue_status", map[string]interface{}{}, true)
	for _, want := range []string{"failed: 1", "pending: 1", "delivered: 1", "q-old [failed] from <a@ex.com>", "550 no such user", "q-new [pending]", "2 recipient(s)"} {
		if isErr || !strings.Contains(text, want) {
			t.Errorf("queue status lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "q-done") || strings.Index(text, "q-old") > strings.Index(text, "q-new") {
		t.Errorf("queue status should list waiting entries oldest first:\n%s", text)
	}
	if _, text, _ := callOps(t, s, "get_queue_status", map[string]interface{}{"limit": 1}, true); strings.Contains(text, "q-new") || !strings.Contains(text, "Oldest 1 of 2") {
		t.Errorf("limit not applied:\n%s", text)
	}

	// Without a queue the tools say so instead of claiming success.
	if _, text, isErr := callOps(t, s, "flush_queue", map[string]interface{}{}, true); !isErr || !strings.Contains(text, "not available") {
		t.Errorf("flush without a queue: isError=%v %q", isErr, text)
	}
	q := &fakeQueue{}
	s.SetQueue(q)
	if _, text, isErr := callOps(t, s, "retry_queue_item", map[string]interface{}{"id": "q-old"}, true); isErr || len(q.retried) != 1 || q.retried[0] != "q-old" {
		t.Errorf("retry: isError=%v %q retried=%v", isErr, text, q.retried)
	}
	if _, text, isErr := callOps(t, s, "retry_queue_item", map[string]interface{}{"id": "nope"}, true); !isErr || !strings.Contains(text, "not found") || len(q.retried) != 1 {
		t.Errorf("retry of a missing entry: isError=%v %q", isErr, text)
	}
	if _, _, isErr := callOps(t, s, "flush_queue", map[string]interface{}{}, true); isErr || q.flushed != 1 {
		t.Errorf("flush: isError=%v flushed=%d", isErr, q.flushed)
	}
}

func TestMCPOpsToolsRequireAdmin(t *testing.T) {
	s := opsServer(t)
	s.SetQueue(&fakeQueue{})
	s.SetDiagnostics(fakeDiag{})
	for _, tool := range []string{"get_queue_status", "retry_queue_item", "check_dns", "check_tls"} {
		if code, _, _ := callOps(t, s, tool, map[string]interface{}{"id": "x", "domain": "example.com"}, false); code != http.StatusForbidden {
			t.Errorf("%s without admin: status %d, want 403", tool, code)
		}
	}
}

func TestMCPDiagnosticsStatusAndReload(t *testing.T) {
	s := opsServer(t)
	for _, tool := range []string{"check_dns", "get_system_status", "reload_config"} {
		if _, text, isErr := callOps(t, s, tool, map[string]interface{}{"domain": "example.com"}, true); !isErr || !strings.Contains(text, "not available") {
			t.Errorf("%s without a backend: isError=%v %q", tool, isErr, text)
		}
	}

	s.SetDiagnostics(fakeDiag{})
	if _, text, isErr := callOps(t, s, "check_dns", map[string]interface{}{"domain": "Example.com"}, true); isErr ||
		!strings.Contains(text, "MX example.com: FAIL (no MX record)") || !strings.Contains(text, "expected: mx.example.com") {
		t.Errorf("check_dns: %q", text)
	}
	if _, text, _ := callOps(t, s, "check_tls", map[string]interface{}{"domain": "example.com"}, true); !strings.Contains(text, "Valid: true") || !strings.Contains(text, "TLS 1.3") {
		t.Errorf("check_tls: %q", text)
	}

	s.SetHealthCheck(func(context.Context) health.Report {
		return health.Report{Status: health.StatusDegraded, Checks: []health.Check{{Name: "disk", Status: health.StatusDegraded, Message: "92% used"}}}
	})
	if _, text, _ := callOps(t, s, "get_system_status", nil, false); !strings.Contains(text, "Status: degraded") || !strings.Contains(text, "disk: degraded (92% used)") {
		t.Errorf("get_system_status: %q", text)
	}

	reloads := 0
	s.SetTLSReload(func() error { reloads++; return nil })
	if _, text, isErr := callOps(t, s, "reload_config", nil, true); isErr || reloads != 1 || !strings.Contains(text, "restart") {
		t.Errorf("reload_config: isError=%v reloads=%d %q", isErr, reloads, text)
	}
	s.SetTLSReload(func() error { return errors.New("bad key") })
	if _, text, isErr := callOps(t, s, "reload_config", nil, true); !isErr || !strings.Contains(text, "bad key") {
		t.Errorf("failed reload reported as success: %q", text)
	}
}
