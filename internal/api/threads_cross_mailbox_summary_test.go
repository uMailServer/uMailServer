package api

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func seedSharedMailboxThread(t *testing.T) (*Server, *storage.Database, string, string) {
	t.Helper()
	s, database, user, id := startThreadsTestServer(t)
	if err := database.CreateMailbox(user, "Sent"); err != nil {
		t.Fatal(err)
	}
	if err := database.StoreMessageMetadata(user, "Sent", 2, &storage.MessageMetadata{UID: 2, MessageID: "sent-message", ThreadID: id}); err != nil {
		t.Fatal(err)
	}
	thread, err := database.GetThread(user, id)
	if err != nil {
		t.Fatal(err)
	}
	thread.MessageCount, thread.UnreadCount = 2, 2
	if err := database.UpdateThread(user, thread); err != nil {
		t.Fatal(err)
	}
	return s, database, user, id
}

func TestThreadSummarySingleMailbox(t *testing.T) {
	s, database, user, id := startThreadsTestServer(t)
	if err := s.markThreadAsRead(user, "INBOX", id); err != nil {
		t.Fatal(err)
	}
	thread, err := database.GetThread(user, id)
	if err != nil || thread.UnreadCount != 0 {
		t.Fatalf("single-mailbox mark control failed: %v %v", thread, err)
	}
	if err := s.deleteThread(user, "INBOX", id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetThread(user, id); err == nil {
		t.Fatal("single-mailbox delete control failed")
	}
	t.Log("CONTROL EXPECTED: single-mailbox read/deletion correct ACTUAL: correct")
}

func TestThreadSummaryAcrossMailboxes(t *testing.T) {
	t.Run("mark", func(t *testing.T) {
		s, database, user, id := seedSharedMailboxThread(t)
		if err := s.markThreadAsRead(user, "INBOX", id); err != nil {
			t.Fatal(err)
		}
		thread, err := database.GetThread(user, id)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("EXPECTED: unread=1 ACTUAL: unread=%d", thread.UnreadCount)
		if thread.UnreadCount != 1 {
			t.Error("DEFECT F4743: unread Sent message omitted from global summary")
		}
	})
	t.Run("delete", func(t *testing.T) {
		s, database, user, id := seedSharedMailboxThread(t)
		if err := s.deleteThread(user, "INBOX", id); err != nil {
			t.Fatal(err)
		}
		if messages, err := database.GetThreadMessages(user, "Sent", id); err != nil || len(messages) != 1 {
			t.Fatalf("unaffected Sent message control failed: %v %v", messages, err)
		}
		thread, err := database.GetThread(user, id)
		t.Logf("EXPECTED: surviving summary count=1, unread=1 ACTUAL: summary=%v error=%v", thread, err)
		if err != nil || thread.MessageCount != 1 || thread.UnreadCount != 1 {
			t.Error("DEFECT F4743: surviving Sent thread summary deleted or stale")
		}
	})
}

func TestThreadSummaryRepeatedMailboxOperations(t *testing.T) {
	s, database, user, id := seedSharedMailboxThread(t)
	for i := 0; i < 2; i++ {
		if err := s.markThreadAsRead(user, "INBOX", id); err != nil {
			t.Fatal(err)
		}
		thread, err := database.GetThread(user, id)
		if err != nil || thread.MessageCount != 2 || thread.UnreadCount != 1 {
			t.Fatalf("repeat mark: %v %v", thread, err)
		}
	}
	if err := s.deleteThread(user, "INBOX", id); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteThread(user, "INBOX", id); err != nil {
		t.Fatal(err)
	}
	thread, err := database.GetThread(user, id)
	if err != nil || thread.MessageCount != 1 || thread.UnreadCount != 1 {
		t.Fatalf("repeat delete: %v %v", thread, err)
	}
	if err := s.markThreadAsRead(user, "Sent", id); err != nil {
		t.Fatal(err)
	}
	thread, err = database.GetThread(user, id)
	if err != nil || thread.UnreadCount != 0 {
		t.Fatalf("all read: %v %v", thread, err)
	}
	if err := s.deleteThread(user, "Sent", id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetThread(user, id); err == nil {
		t.Fatal("empty summary not removed")
	}
}
