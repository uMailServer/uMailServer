package server

// Regression test for the SMTP delivery threading defect: deliverLocal
// (internal/server/server_handlers.go) built MessageMetadata WITHOUT ThreadID,
// while the IMAP APPEND path assigns one (internal/imap/mailstore.go,
// GetOrCreateThreadID + the Thread aggregate row). SMTP-delivered mail — the
// primary path — was therefore invisible to JMAP Thread/get and the REST
// /api/v1/threads API, and never grouped by subject or references.
// The fix mirrors the IMAP convention at the delivery site: parse
// In-Reply-To/References, GetOrCreateThreadID, set meta.ThreadID/IsThreadRoot,
// and refresh the best-effort Thread aggregate row.

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

func startThreadingDeliveryServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	accountsDB, err := db.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatalf("open accounts db: %v", err)
	}
	t.Cleanup(func() { accountsDB.Close() })

	domain := &db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}
	if err := accountsDB.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	account := &db.AccountData{
		Email: "alice@test.com", LocalPart: "alice", Domain: "test.com",
		PasswordHash: "test-hash", IsActive: true,
	}
	if err := accountsDB.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	stDB, err := storage.OpenDatabase(filepath.Join(dir, "storage.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { stDB.Close() })
	mStore, err := storage.NewMessageStore(filepath.Join(dir, "messages"))
	if err != nil {
		t.Fatalf("open message store: %v", err)
	}
	t.Cleanup(func() { mStore.Close() })

	return &Server{database: accountsDB, storageDB: stDB, msgStore: mStore, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

const threadedMailOne = "From: Bob <bob@ext.com>\r\nTo: alice@test.com\r\nSubject: Project planning\r\nMessage-ID: <m1@ext.com>\r\n\r\nfirst body\r\n"

const threadedMailReply = "From: Carol <carol@ext.com>\r\nTo: alice@test.com\r\nSubject: Re: Project planning\r\nMessage-ID: <m2@ext.com>\r\nIn-Reply-To: <m1@ext.com>\r\nReferences: <m1@ext.com>\r\n\r\nreply body\r\n"

func TestSMTPDeliveryAssignsThreadIdentity(t *testing.T) {
	s := startThreadingDeliveryServer(t)

	// Control sanity: a plain local delivery must succeed.
	if err := s.deliverLocal("alice", "test.com", "bob@ext.com", []byte(threadedMailOne)); err != nil {
		t.Fatalf("CONTROL FAILED (harness): deliverLocal: %v", err)
	}

	meta, err := s.storageDB.GetMessageMetadata("alice@test.com", "INBOX", 1)
	if err != nil {
		t.Fatalf("GetMessageMetadata: %v", err)
	}
	if meta.ThreadID == "" {
		t.Fatalf("FAIL: SMTP-delivered message has no ThreadID — primary-path mail is invisible to Thread/get and the REST threads API")
	}

	// A reply must group into the same thread.
	if err := s.deliverLocal("alice", "test.com", "carol@ext.com", []byte(threadedMailReply)); err != nil {
		t.Fatalf("deliverLocal (reply): %v", err)
	}
	msgs, err := s.storageDB.GetThreadMessages("alice@test.com", "INBOX", meta.ThreadID)
	if err != nil {
		t.Fatalf("GetThreadMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("FAIL: thread grouping broken — thread %s holds %d messages, want 2 (reply did not group)", meta.ThreadID, len(msgs))
	}

	// The aggregate row must exist for the REST threads API.
	threads, err := s.storageDB.GetThreads("alice@test.com", 50, 0)
	if err != nil {
		t.Fatalf("GetThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("FAIL: REST threads list has %d threads, want 1 — SMTP-delivered mail never created a Thread row", len(threads))
	}
	if threads[0].MessageCount != 2 || threads[0].UnreadCount != 2 {
		t.Fatalf("FAIL: thread aggregate MessageCount=%d UnreadCount=%d, want 2/2", threads[0].MessageCount, threads[0].UnreadCount)
	}
}

func TestSMTPDeliverySubjectGroupsSameThread(t *testing.T) {
	s := startThreadingDeliveryServer(t)

	// Two same-subject replies with no explicit references: the subject
	// fallback must group them (mirrors the IMAP path's GetOrCreateThreadID
	// normalization).
	mail := strings.Replace(threadedMailOne, "From: Bob <bob@ext.com>", "From: Dan <dan@ext.com>", 1)
	if err := s.deliverLocal("alice", "test.com", "dan@ext.com", []byte(mail)); err != nil {
		t.Fatalf("deliverLocal (1): %v", err)
	}
	if err := s.deliverLocal("alice", "test.com", "dan@ext.com", []byte(mail)); err != nil {
		t.Fatalf("deliverLocal (2): %v", err)
	}

	threads, err := s.storageDB.GetThreads("alice@test.com", 50, 0)
	if err != nil {
		t.Fatalf("GetThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].MessageCount != 2 {
		t.Fatalf("FAIL: same-subject deliveries produced %d threads (MessageCount %d), want 1 thread with 2 messages", len(threads), func() int {
			if len(threads) == 1 {
				return threads[0].MessageCount
			}
			return -1
		}())
	}
}
