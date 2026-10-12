package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/db"
)

// F7370: every admin mutation on domains, aliases and the queue must leave an
// audit entry naming the acting admin (only account mutations were audited).
func TestR260AdminMutationsAreAudited(t *testing.T) {
	s, database := r124Server(t)
	logPath := t.TempDir() + "/audit.log"
	al, err := audit.NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.auditLogger = al

	do := func(h http.HandlerFunc, method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), "user", "root@example.com"))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec.Code
	}
	if c := do(s.handleDomains, http.MethodPost, "/api/v1/domains", `{"name":"example.com"}`); c != 201 {
		t.Fatalf("create domain %d", c)
	}
	if c := do(s.handleDomainDetail, http.MethodPut, "/api/v1/domains/example.com", `{"max_accounts":5}`); c != 200 {
		t.Fatalf("update domain %d", c)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "bob@example.com", Domain: "example.com", LocalPart: "bob", PasswordHash: "x", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if c := do(s.handleAliases, http.MethodPost, "/api/v1/aliases", `{"alias":"info@example.com","target":"bob@example.com"}`); c != 201 {
		t.Fatalf("create alias %d", c)
	}
	if c := do(s.handleAliasDetail, http.MethodPut, "/api/v1/aliases/info@example.com", `{"is_active":false}`); c != 200 {
		t.Fatalf("update alias %d", c)
	}
	if c := do(s.handleAliasDetail, http.MethodDelete, "/api/v1/aliases/info@example.com", ``); c != 204 {
		t.Fatalf("delete alias %d", c)
	}
	if err := database.Enqueue(&db.QueueEntry{ID: "q1", From: "a@example.com", To: []string{"z@remote.test"}, Status: "failed", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if c := do(s.handleQueueDetail, http.MethodPost, "/api/v1/queue/q1", ``); c != 200 {
		t.Fatalf("retry %d", c)
	}
	if c := do(s.handleQueueDetail, http.MethodDelete, "/api/v1/queue/q1", ``); c != 204 {
		t.Fatalf("drop %d", c)
	}
	if err := database.DeleteAccount("example.com", "bob"); err != nil {
		t.Fatal(err)
	}
	if c := do(s.handleDomainDetail, http.MethodDelete, "/api/v1/domains/example.com", ``); c != 204 {
		t.Fatalf("delete domain %d", c)
	}
	al.Close()
	data, _ := os.ReadFile(logPath)
	log := string(data)
	for _, action := range []string{"domain_create", "domain_update", "domain_delete", "alias_create", "alias_update", "alias_delete", "queue_retry", "queue_drop"} {
		if !strings.Contains(log, `"action":"`+action+`"`) {
			t.Errorf("no audit entry for %s", action)
		}
	}
	if !strings.Contains(log, "root@example.com") {
		t.Errorf("actor missing from audit log")
	}
}
