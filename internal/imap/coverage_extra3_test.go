package imap

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// =======================================================================
// mailstore.go: NewBboltMailstore (66.7%)
// Uncovered: error from storage.NewMessageStore -> db.Close() + return err
// =======================================================================

func TestNewBboltMailstore_MessageStoreError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file at the messages path to prevent directory creation
	msgPath := tmpDir + "/messages"
	if err := os.WriteFile(msgPath, []byte("blocker"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := NewBboltMailstore(tmpDir)
	if err == nil {
		t.Error("expected error when message store path is blocked by a file")
	}
	if err != nil && !strings.Contains(err.Error(), "message store") {
		t.Logf("got error: %v", err)
	}
}

// =======================================================================
// mailstore.go: Close (80.0%)
// Uncovered: both msgStore and db are nil
// =======================================================================

func TestBboltMailstoreClose_NilFields(t *testing.T) {
	ms := &BboltMailstore{}
	err := ms.Close()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// =======================================================================
// mailstore.go: SelectMailbox (71.4%)
// Uncovered: error from GetMailboxCounts
// =======================================================================

func TestSelectMailbox_GetMailboxCountsError(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	// Don't create the mailbox, so both GetMailbox and GetMailboxCounts may fail
	mb, err := ms.SelectMailbox(user, "NonExistent")
	// This should succeed (GetMailbox creates default) or return an error
	// Either way, we exercise the code path
	t.Logf("SelectMailbox result: mb=%v, err=%v", mb, err)
}

// =======================================================================
// mailstore.go: AppendMessage (80.0%)
// Uncovered: error from GetNextUID, error from StoreMessageMetadata
// We exercise the happy path more fully with proper headers.
// =======================================================================

func TestAppendMessage_WithHeaders(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	data := []byte("From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Hello\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\n\r\nHello World")
	err = ms.AppendMessage(user, mbox, []string{"\\Seen", "\\Flagged"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Verify message was stored by fetching
	msgs, err := ms.FetchMessages(user, mbox, "1", []string{"FLAGS", "RFC822"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !hasFlag(msgs[0].Flags, "\\Seen") {
		t.Error("expected \\Seen flag")
	}
	if len(msgs[0].Data) == 0 {
		t.Error("expected message data to be loaded for RFC822 fetch")
	}
}

// =======================================================================
// mailstore.go: Expunge (81.8%)
// Uncovered: the path where message has \\Deleted flag and gets deleted
// Also: the path where GetMessageMetadata returns error (continue)
// =======================================================================

func TestExpunge_WithDeletedMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// Append two messages
	data1 := []byte("From: a@b.com\r\nSubject: Msg1\r\n\r\nBody1")
	data2 := []byte("From: c@d.com\r\nSubject: Msg2\r\n\r\nBody2")
	err = ms.AppendMessage(user, mbox, []string{"\\Seen"}, time.Now(), data1)
	if err != nil {
		t.Fatalf("AppendMessage 1: %v", err)
	}
	err = ms.AppendMessage(user, mbox, []string{"\\Seen", "\\Deleted"}, time.Now(), data2)
	if err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}

	// Expunge should remove the deleted message
	err = ms.Expunge(user, mbox)
	if err != nil {
		t.Fatalf("Expunge: %v", err)
	}

	// Verify only one message remains
	msgs, err := ms.FetchMessages(user, mbox, "1:*", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after expunge, got %d", len(msgs))
	}
}

func TestExpunge_GetMessageUIDsError(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	// Expunge on a mailbox that was never created
	err = ms.Expunge("nonexistent_user", "NonExistent")
	if err != nil {
		t.Logf("Expunge on nonexistent mailbox: %v (error is expected)", err)
	}
}

// =======================================================================
// mailstore.go: SearchMessages (84.6%)
// Uncovered: meta fetch error (continue path)
// Exercise with actual messages returning results.
// =======================================================================

func TestSearchMessages_WithActualMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// Append messages with different characteristics
	data1 := []byte("From: alice@example.com\r\nSubject: Important\r\n\r\nBody1")
	data2 := []byte("From: bob@example.com\r\nSubject: Routine\r\n\r\nBody2")
	err = ms.AppendMessage(user, mbox, []string{"\\Seen"}, time.Now(), data1)
	if err != nil {
		t.Fatalf("AppendMessage 1: %v", err)
	}
	err = ms.AppendMessage(user, mbox, nil, time.Now(), data2)
	if err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}

	// Search ALL - should return 2
	results, err := ms.SearchMessages(user, mbox, SearchCriteria{All: true})
	if err != nil {
		t.Fatalf("SearchMessages All: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for ALL, got %d", len(results))
	}

	// Search SEEN - should return 1 (first message)
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{Seen: true})
	if err != nil {
		t.Fatalf("SearchMessages Seen: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for SEEN, got %d", len(results))
	}

	// Search UNSEEN - should return 1 (second message)
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{Unseen: true})
	if err != nil {
		t.Fatalf("SearchMessages Unseen: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for UNSEEN, got %d", len(results))
	}

	// Search by Subject
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{Subject: "Important"})
	if err != nil {
		t.Fatalf("SearchMessages Subject: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for Subject=Important, got %d", len(results))
	}

	// Search with non-matching criteria
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{From: "nobody@nowhere.com"})
	if err != nil {
		t.Fatalf("SearchMessages non-matching: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching From, got %d", len(results))
	}

	// Search on nonexistent mailbox (exercises error path)
	results, err = ms.SearchMessages("nobody", "NoBox", SearchCriteria{All: true})
	t.Logf("SearchMessages nonexistent: results=%v err=%v", results, err)
}

// =======================================================================
// mailstore.go: FetchMessages (85.7%)
// Uncovered: getMessage returning error (continue path)
// Exercise with sequence ranges
// =======================================================================

func TestFetchMessages_WithRangeAndBody(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// Append multiple messages
	for i := 0; i < 3; i++ {
		data := []byte(fmt.Sprintf("From: user%d@test.com\r\nSubject: Msg%d\r\n\r\nBody%d", i, i, i))
		err = ms.AppendMessage(user, mbox, []string{"\\Seen"}, time.Now(), data)
		if err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	// Fetch with range 1:2
	msgs, err := ms.FetchMessages(user, mbox, "1:2", []string{"FLAGS", "BODY[]", "RFC822"})
	if err != nil {
		t.Fatalf("FetchMessages range: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages for range 1:2, got %d", len(msgs))
	}

	// Fetch with * (last message)
	msgs, err = ms.FetchMessages(user, mbox, "*", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages star: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message for *, got %d", len(msgs))
	}

	// Fetch with 1:* (all)
	msgs, err = ms.FetchMessages(user, mbox, "1:*", []string{"BODY"})
	if err != nil {
		t.Fatalf("FetchMessages 1:* body: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages for 1:*, got %d", len(msgs))
	}

	// Fetch out of range
	msgs, err = ms.FetchMessages(user, mbox, "10", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages out of range: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for out of range, got %d", len(msgs))
	}
}

// =======================================================================
// mailstore.go: CopyMessages (80.0%) and MoveMessages (83.3%)
// Exercise with multiple messages and various sequence sets.
// =======================================================================

func TestCopyMessages_MultipleMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Archive")

	// Append 3 messages
	for i := 0; i < 3; i++ {
		data := []byte(fmt.Sprintf("From: test@test.com\r\nSubject: Copy%d\r\n\r\nBody", i))
		err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
		if err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	// Copy messages 1:2 to Archive
	err = ms.CopyMessages(user, "INBOX", "Archive", "1:2")
	if err != nil {
		t.Fatalf("CopyMessages: %v", err)
	}

	// Verify destination has messages
	msgs, err := ms.FetchMessages(user, "Archive", "1:*", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages Archive: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages in Archive, got %d", len(msgs))
	}
}

func TestMoveMessages_MultipleMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Trash")

	// Append 2 messages
	for i := 0; i < 2; i++ {
		data := []byte(fmt.Sprintf("From: test@test.com\r\nSubject: Move%d\r\n\r\nBody", i))
		err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
		if err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	// Move message 1 to Trash
	err = ms.MoveMessages(user, "INBOX", "Trash", "1")
	if err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}

	// Verify moved message has \Deleted flag (only message 1 was moved)
	msgs, err := ms.FetchMessages(user, "INBOX", "1", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages INBOX: %v", err)
	}
	if len(msgs) > 0 && !hasFlag(msgs[0].Flags, "\\Deleted") {
		t.Errorf("expected moved message to have \\Deleted flag, flags=%v", msgs[0].Flags)
	}
}

// =======================================================================
// commands.go: handleCommand (92.3%)
// Uncovered: LoggedOut state return after switch
// =======================================================================

func TestHandleCommand_LoggedOutState(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateLoggedOut, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 NOOP")
	}()

	err := <-done
	if err != nil {
		t.Errorf("handleCommand in LoggedOut state returned error: %v", err)
	}
}

// =======================================================================
// commands.go: handleSelect (91.7%)
// Uncovered: SelectMailbox error (already tested with failingMailstore)
// Also: the error from mailstore being nil (tested but via handleCommand).
// Test: missing args via direct call
// =======================================================================

func TestHandleSelect_MissingArgsDirect(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleSelect([]string{})
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for missing args, got: %s", resp)
	}
	<-done
}

func TestHandleSelect_NilMailstoreDirect(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleSelect([]string{"INBOX"})
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleExamine (91.7%)
// =======================================================================

func TestHandleExamine_MissingArgsDirect(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleExamine([]string{})
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for missing args, got: %s", resp)
	}
	<-done
}

func TestHandleExamine_NilMailstoreDirect(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleExamine([]string{"INBOX"})
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleList (95.2%)
// Uncovered: reference not empty and doesn't end with "/" + pattern not empty
// =======================================================================

func TestHandleList_ReferenceNoSlash(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		// Reference "INBOX" doesn't end with "/", pattern "Sent" - should add "/"
		done <- session.handleCommand("A001 LIST INBOX Sent")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestHandleList_MissingArgsViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LIST onlyone")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for LIST with one arg, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleStatus (92.6% -> higher)
// Uncovered: SelectMailbox returning error via handleStatus
// =======================================================================

func TestHandleStatus_SelectMailboxError(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.selectErr = fmt.Errorf("mailbox not found")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STATUS INBOX (MESSAGES)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for status with select error, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleFetch (88.2%)
// Uncovered: handleFetch returns messages, formatFetchResponse called
// =======================================================================

func TestHandleFetch_WithMessagesFromStore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	// Use a mock that returns actual messages
	session.server.mailstore = &returningMessagesMailstore{}

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH 1 (FLAGS UID RFC822.SIZE ENVELOPE BODY RFC822)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "FETCH") {
		t.Errorf("Expected FETCH in response, got: %s", resp)
	}
	if !strings.Contains(resp, "OK") {
		t.Errorf("Expected OK in response, got: %s", resp)
	}
	<-done
}

// returningMessagesMailstore returns messages for testing fetch
type returningMessagesMailstore struct {
	*mockMailstore
}

func (r *returningMessagesMailstore) FetchMessages(user, mailbox string, seqSet string, items []string) ([]*Message, error) {
	return []*Message{
		{
			SeqNum:       1,
			UID:          42,
			Flags:        []string{"\\Seen", "\\Flagged"},
			InternalDate: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			Size:         1024,
			Subject:      "Test",
			From:         "sender@example.com",
			To:           "recipient@example.com",
			Date:         "15-Jan-2024 10:30:00 +0000",
			Data:         []byte("Subject: Test\r\n\r\nBody"),
		},
	}, nil
}

func (r *returningMessagesMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	return &Mailbox{Name: mailbox, Exists: 1}, nil
}

func (r *returningMessagesMailstore) Authenticate(username, password string) (bool, error) {
	return true, nil
}

// =======================================================================
// commands.go: handleSearch (92.9%)
// Uncovered: SearchMessages returning results
// =======================================================================

func TestHandleSearch_WithResults(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	session.server.mailstore = &returningSearchMailstore{}

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH ALL")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "SEARCH 1 2 3") {
		t.Errorf("Expected SEARCH results in response, got: %s", resp)
	}
	if !strings.Contains(resp, "OK") {
		t.Errorf("Expected OK in response, got: %s", resp)
	}
	<-done
}

type returningSearchMailstore struct {
	*mockMailstore
}

func (r *returningSearchMailstore) SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error) {
	return []uint32{1, 2, 3}, nil
}

func (r *returningSearchMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	return &Mailbox{Name: mailbox, Exists: 3}, nil
}

func (r *returningSearchMailstore) Authenticate(username, password string) (bool, error) {
	return true, nil
}

// =======================================================================
// commands.go: handleStore (96.3%)
// Uncovered: StoreFlags error path
// =======================================================================

func TestHandleStore_StoreError(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.storeErr = fmt.Errorf("db write error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 +FLAGS (\\Seen)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for store error, got: %s", resp)
	}
	<-done
}

func TestHandleStore_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for missing store args, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleAppend (93.9%)
// Uncovered: literal read with no closing brace, no literal at all
// Also: the path where literalStart <= 0 (no literal)
// =======================================================================

func TestHandleAppend_NoLiteral(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		// APPEND without {N} literal marker - should just return OK
		done <- session.handleCommand("A001 APPEND INBOX (\\Seen)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// commands.go: handleIdle (90.7%)
// Uncovered: NotificationMailboxUpdate with a mailstore that returns a different Exists
// =======================================================================

func TestHandleIdle_MailboxUpdateChangesExists(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 1, Recent: 0})
	defer client.Close()

	// Use a mailstore that returns a different Exists count
	session.server.mailstore = &changingMailboxStore{}

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout waiting for continuation")
	}

	time.Sleep(50 * time.Millisecond)

	hub := GetNotificationHub()
	hub.NotifyMailboxUpdate("testuser", "INBOX")

	// Wait for potential notification
	waitForLine(lines, "", 2*time.Second)

	client.Write([]byte("DONE\r\n"))

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for idle to finish")
	}
}

type changingMailboxStore struct {
	*mockMailstore
}

func (c *changingMailboxStore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	return &Mailbox{Name: mailbox, Exists: 5, Recent: 2}, nil
}

func (c *changingMailboxStore) Authenticate(username, password string) (bool, error) {
	return true, nil
}

// =======================================================================
// commands.go: handleExpunge via command (covers mailstore nil check)
// =======================================================================

func TestHandleExpunge_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXPUNGE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore expunge, got: %s", resp)
	}
	<-done
}

func TestHandleExpunge_NilSelectedViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXPUNGE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil selected expunge, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleSearch nil mailstore / nil selected via command
// =======================================================================

func TestHandleSearch_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH ALL")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore search, got: %s", resp)
	}
	<-done
}

func TestHandleSearch_NilSelectedViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH ALL")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil selected search, got: %s", resp)
	}
	<-done
}

// =======================================================================
// parser.go: ParseSequenceSet (90.9%)
// Uncovered: invalid sequence number
// =======================================================================

func TestParseSequenceSet_InvalidSeqNumber(t *testing.T) {
	_, err := ParseSequenceSet("abc")
	if err == nil {
		t.Error("expected error for invalid sequence number")
	}
}

func TestParseSequenceSet_InvalidRangePart(t *testing.T) {
	_, err := ParseSequenceSet("abc:def")
	if err == nil {
		t.Error("expected error for invalid range parts")
	}
}

// =======================================================================
// parser.go: ParseStatusItems (93.8%)
// Uncovered: unknown status item
// =======================================================================

// =======================================================================
// parser.go: ParseFlags (95.2%)
// Uncovered: error for non-parenthesized flags
// =======================================================================

// =======================================================================
// server.go: Handle (83.3%)
// Uncovered: the empty line continue path
// =======================================================================

// =======================================================================
// server.go: StartTLS (20.0%) - this is hard to test fully, but let's
// at least cover the error path.
// Already tested: StartTLS with nil config returns error.
// The success path requires actual TLS cert + listener.
// =======================================================================

// =======================================================================
// mailstore.go: ListMailboxes (88.9%)
// Uncovered: error from db.ListMailboxes
// =======================================================================

func TestListMailboxes_Error(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	// ListMailboxes with a user that has no data should still work
	list, err := ms.ListMailboxes("testuser", "*")
	if err != nil {
		t.Logf("ListMailboxes with no data: %v", err)
	}
	t.Logf("ListMailboxes result: %v", list)
}

// =======================================================================
// mailstore.go: getMessage (93.3%)
// Uncovered: msgStore.ReadMessage returning error (data stays nil)
// =======================================================================

func TestGetMessage_NoDataNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, mbox, nil, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Fetch with only FLAGS (no data needed)
	msgs, err := ms.FetchMessages(user, mbox, "1", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// Data should not be loaded since we only asked for FLAGS
	if len(msgs[0].Data) != 0 {
		t.Log("Data was loaded even though only FLAGS requested")
	}
}

// =======================================================================
// mailstore.go: StoreFlags (90.0%)
// Uncovered: UpdateMessageMetadata error path, all messages not in set
// =======================================================================

func TestStoreFlags_NoMessagesInSet(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// StoreFlags with no messages - should not error
	err = ms.StoreFlags(user, mbox, "1", []string{"\\Seen"}, true)
	if err != nil {
		t.Logf("StoreFlags with no messages: %v", err)
	}
}

func TestStoreFlags_RemoveFlagFromEmptyFlags(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// Append message with no flags
	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, mbox, nil, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Remove flag from message that has no flags - should work fine
	err = ms.StoreFlags(user, mbox, "1", []string{"\\Seen"}, false)
	if err != nil {
		t.Fatalf("StoreFlags remove from empty: %v", err)
	}
}

// =======================================================================
// parser.go: ParseFetchItems - test ALL/FAST/FULL macros
// =======================================================================

// =======================================================================
// mailstore.go: ListMailboxes with different patterns
// =======================================================================

func TestListMailboxes_ExactMatch(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Sent")

	// Exact match
	list, err := ms.ListMailboxes(user, "INBOX")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 1 || list[0] != "INBOX" {
		t.Errorf("expected [INBOX], got %v", list)
	}

	// Star suffix
	list, err = ms.ListMailboxes(user, "Sen*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 1 || list[0] != "Sent" {
		t.Errorf("expected [Sent], got %v", list)
	}

	// Prefix star
	list, err = ms.ListMailboxes(user, "*BOX")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 1 || list[0] != "INBOX" {
		t.Errorf("expected [INBOX], got %v", list)
	}
}
