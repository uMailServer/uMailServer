package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
	"golang.org/x/crypto/bcrypt"
)

// F7640: webmail/REST sends take the same local/relay routing as SMTP
// submission. Local mailboxes are delivered directly; only other recipients
// are queued for outbound relay.
func r290Server(t *testing.T) (*Server, string) {
	t.Helper()
	srv, pw := newJWTRotationTestServer(t, "", false, false)
	hash, _ := bcrypt.GenerateFromPassword([]byte("x"), 4)
	if err := srv.database.CreateAccount(&db.AccountData{Email: "bob@test.com", LocalPart: "bob", Domain: "test.com", PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	// production wiring creates the queue in Start; no workers here, so no
	// network is ever touched.
	srv.queue = queue.NewManager(srv.database, nil, filepath.Join(t.TempDir(), "queue"), srv.logger)
	if err := srv.startAPI(); err != nil {
		t.Fatal(err)
	}
	return srv, pw
}

func r290Send(t *testing.T, srv *Server, tok string, to []string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"to": to, "subject": "hi", "body": "hello"})
	return jwtRotDo(srv.apiServer, http.MethodPost, "/api/v1/mail/send", tok, body).Code
}

func r290QueuedTo(t *testing.T, srv *Server) []string {
	t.Helper()
	pend, _ := srv.queue.GetPendingEntries()
	var out []string
	for _, e := range pend {
		out = append(out, e.To...)
	}
	return out
}

func r290Inbox(t *testing.T, srv *Server, user string) int {
	t.Helper()
	uids, _ := srv.storageDB.GetMessageUIDs(user, "INBOX")
	return len(uids)
}

func TestR290LocalRecipientDeliveredDirectly(t *testing.T) {
	srv, pw := r290Server(t)
	tok := jwtRotLogin(t, srv, pw)
	if code := r290Send(t, srv, tok, []string{"bob@test.com"}); code != http.StatusOK {
		t.Fatalf("send status %d", code)
	}
	if n := r290Inbox(t, srv, "bob@test.com"); n != 1 {
		t.Fatalf("bob INBOX = %d messages, want 1", n)
	}
	if q := r290QueuedTo(t, srv); len(q) != 0 {
		t.Fatalf("local recipient was queued for relay: %v", q)
	}
}

func TestR290MixedLocalAndRemote(t *testing.T) {
	srv, pw := r290Server(t)
	tok := jwtRotLogin(t, srv, pw)
	if code := r290Send(t, srv, tok, []string{"bob@test.com", "carol@remote.example", "bob@test.com"}); code != http.StatusOK {
		t.Fatalf("send status %d", code)
	}
	if n := r290Inbox(t, srv, "bob@test.com"); n != 1 {
		t.Fatalf("bob INBOX = %d, want 1 (duplicate recipient served once)", n)
	}
	if q := r290QueuedTo(t, srv); len(q) != 1 || q[0] != "carol@remote.example" {
		t.Fatalf("queued = %v, want only the remote recipient", q)
	}
}

func TestR290BccLocalRecipientGetsMailWithoutHeader(t *testing.T) {
	srv, pw := r290Server(t)
	tok := jwtRotLogin(t, srv, pw)
	body, _ := json.Marshal(map[string]interface{}{"to": []string{"alice@test.com"}, "bcc": []string{"bob@test.com"}, "subject": "s", "body": "b"})
	if rec := jwtRotDo(srv.apiServer, http.MethodPost, "/api/v1/mail/send", tok, body); rec.Code != http.StatusOK {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	uids, _ := srv.storageDB.GetMessageUIDs("bob@test.com", "INBOX")
	if len(uids) != 1 {
		t.Fatalf("bcc recipient INBOX = %d, want 1", len(uids))
	}
	meta, err := srv.storageDB.GetMessageMetadata("bob@test.com", "INBOX", uids[0])
	if err != nil || meta == nil {
		t.Fatalf("metadata: %v", err)
	}
	raw, err := srv.msgStore.ReadMessage("bob@test.com", meta.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("\r\nbcc:")) {
		t.Fatalf("delivered message exposes a Bcc header:\n%s", raw)
	}
}

func TestR290UnknownLocalRecipientFailsImmediately(t *testing.T) {
	srv, pw := r290Server(t)
	tok := jwtRotLogin(t, srv, pw)
	if code := r290Send(t, srv, tok, []string{"ghost@test.com"}); code != http.StatusInternalServerError {
		t.Fatalf("send to a missing local mailbox: status %d, want 500", code)
	}
	if q := r290QueuedTo(t, srv); len(q) != 0 {
		t.Fatalf("missing local mailbox was queued: %v", q)
	}
}
