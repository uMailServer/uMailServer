package api

// Regression tests for the webmail send defect: handleMailSend validated
// recipients, stored a Sent copy, and responded "Email sent successfully"
// without ever submitting the message to the outbound delivery queue —
// recipients never received anything. The queue manager is wired into the
// API server in production (internal/server/server_api.go SetQueueManager)
// and docs/ARCHITECTURE.md documents the endpoint as "Send email", so a send
// must enqueue the message for its envelope recipients (To + Cc + Bcc; Bcc
// stays out of the headers). Direct MailHandler use without a wired queue
// keeps the legacy store-to-Sent-only behavior (existing handler tests rely
// on it).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func webmailSendServer(t *testing.T) (*Server, *queue.Manager) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	mailDB, err := storage.OpenDatabase(filepath.Join(dir, "mail.db"))
	if err != nil {
		t.Fatalf("mail db open: %v", err)
	}
	t.Cleanup(func() { mailDB.Close() })

	msgStore, err := storage.NewMessageStore(filepath.Join(dir, "messages"))
	if err != nil {
		t.Fatalf("message store: %v", err)
	}
	qm := queue.NewManager(database, store.NewMaildirStore(filepath.Join(dir, "maildir")), filepath.Join(dir, "queue"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Not started: queued entries stay Pending and observable.

	if err := database.CreateDomain(&db.DomainData{Name: "sendtest.invalid", IsActive: true}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	fixtureEmail := "sender@sendtest.invalid"
	fixturePassword := strings.Repeat("fixture", 3)
	hash, err := bcrypt.GenerateFromPassword([]byte(fixturePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{
		Email: fixtureEmail, LocalPart: "sender", Domain: "sendtest.invalid",
		PasswordHash: string(hash), IsActive: true,
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}

	srv := NewServer(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		JWTSecret:   strings.Repeat("webmailsendtest", 2),
		TokenExpiry: time.Hour,
	})
	srv.SetMailDB(mailDB)
	srv.SetMsgStore(msgStore)
	srv.SetQueueManager(qm) // order-independent: re-wires the mail handler
	return srv, qm
}

func TestWebmailSendQueuesForDelivery(t *testing.T) {
	srv, qm := webmailSendServer(t)

	login := func() string {
		body, _ := json.Marshal(map[string]string{
			"email": "sender@sendtest.invalid", "password": strings.Repeat("fixture", 3),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("CONTROL FAILED (harness): login = %d (body %q)", rr.Code, rr.Body.String())
		}
		var resp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.Token == "" {
			t.Fatalf("CONTROL FAILED (harness): no token: %v", err)
		}
		return resp.Token
	}

	send := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		return rr
	}

	token := login()

	sendBody := `{"to":["dest@remote.example"],"cc":["cc@remote.example"],"bcc":["bcc@remote.example"],"subject":"queued","body":"hello"}`
	if rr := send(token, sendBody); rr.Code != http.StatusOK {
		t.Fatalf("send = %d (body %q), want 200", rr.Code, rr.Body.String())
	}

	stats, err := qm.GetStats()
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}
	if stats.Pending < 3 {
		t.Fatalf("FAIL: send claimed success but queued %d message(s), want >= 3 (To+Cc+Bcc)", stats.Pending)
	}

	// Control: the Sent copy is still stored for the composing user.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mail/inbox?folder=sent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("CONTROL FAILED (harness): sent list = %d", rr.Code)
	}
	var sent struct {
		Emails []map[string]any `json:"emails"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &sent); err != nil {
		t.Fatalf("CONTROL FAILED (harness): sent decode: %v", err)
	}
	if len(sent.Emails) < 1 {
		t.Fatalf("CONTROL FAILED (harness): sent copy missing after send")
	}
}

// The legacy direct-handler path (no queue wired) must keep working: store
// to Sent, respond success, and not panic on the nil queue.
func TestWebmailSendWithoutQueueKeepsLegacyBehavior(t *testing.T) {
	h := NewMailHandler()
	body := `{"to":["dest@remote.example"],"subject":"legacy","body":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), "user", "legacy@sendtest.invalid"))
	rr := httptest.NewRecorder()
	h.handleMailSend(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("legacy nil-queue send = %d (body %q), want 200", rr.Code, rr.Body.String())
	}
}
