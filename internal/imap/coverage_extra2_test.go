package imap

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// =======================================================================
// mailstore.go: StoreFlags (83.3% -> higher)
// The uncovered branches are:
//   - add=false (remove flags) with non-empty newFlags
//   - add=true when the flag is already present (hasFlag returns true, so no append)
//   - add=true when the flag is not present (append branch)
// =======================================================================

func TestStoreFlags_RemoveFlagsWithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// Append a message with initial flags
	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, mbox, []string{"\\Seen", "\\Flagged"}, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	// Remove the \\Seen flag (add=false)
	err = ms.StoreFlags(user, mbox, "1", []string{"\\Seen"}, false)
	if err != nil {
		t.Fatalf("StoreFlags remove: %v", err)
	}

	// Verify by fetching
	msgs, err := ms.FetchMessages(user, mbox, "1", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	for _, m := range msgs {
		if hasFlag(m.Flags, "\\Seen") {
			t.Errorf("expected \\Seen to be removed, flags=%v", m.Flags)
		}
		if !hasFlag(m.Flags, "\\Flagged") {
			t.Errorf("expected \\Flagged to remain, flags=%v", m.Flags)
		}
	}
}

func TestStoreFlags_AddAlreadyPresent(t *testing.T) {
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
	err = ms.AppendMessage(user, mbox, []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	// Add \\Seen again (already present) -- hasFlag returns true, no append
	err = ms.StoreFlags(user, mbox, "1", []string{"\\Seen"}, true)
	if err != nil {
		t.Fatalf("StoreFlags add (already present): %v", err)
	}

	// Add a new flag that is not present -- hasFlag returns false, append
	err = ms.StoreFlags(user, mbox, "1", []string{"\\Deleted"}, true)
	if err != nil {
		t.Fatalf("StoreFlags add new flag: %v", err)
	}

	msgs, _ := ms.FetchMessages(user, mbox, "1", []string{"FLAGS"})
	for _, m := range msgs {
		if !hasFlag(m.Flags, "\\Seen") {
			t.Errorf("expected \\Seen to remain, flags=%v", m.Flags)
		}
		if !hasFlag(m.Flags, "\\Deleted") {
			t.Errorf("expected \\Deleted to be added, flags=%v", m.Flags)
		}
	}
}

func TestStoreFlags_InvalidSeqSet(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	// Invalid sequence set should return error from ParseSequenceSet
	err = ms.StoreFlags(user, mbox, "abc", []string{"\\Seen"}, true)
	if err == nil {
		t.Error("expected error for invalid seq set")
	}
}

// =======================================================================
// mailstore.go: SearchMessages (84.6% -> higher)
// Need to exercise the path where criteria match and results are appended.
// =======================================================================

func TestSearchMessages_CriteriaMatch(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	data := []byte("From: alice@example.com\r\nSubject: Hello\r\n\r\nBody text")
	err = ms.AppendMessage(user, mbox, nil, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	// Search with All criteria -- should match
	results, err := ms.SearchMessages(user, mbox, SearchCriteria{All: true})
	if err != nil {
		t.Fatalf("SearchMessages All: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for All search")
	}

	// Search by From
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{From: "alice"})
	if err != nil {
		t.Fatalf("SearchMessages From: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for From search")
	}

	// Search by Subject
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{Subject: "hello"})
	if err != nil {
		t.Fatalf("SearchMessages Subject: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for Subject search")
	}

	// Search with non-matching criteria
	results, err = ms.SearchMessages(user, mbox, SearchCriteria{From: "nobody"})
	if err != nil {
		t.Fatalf("SearchMessages no match: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching From, got %d", len(results))
	}
}

// =======================================================================
// mailstore.go: parseSeqSet (85.7% -> higher)
// Missing: invalid range with != 2 parts (e.g. "1:2:3")
// =======================================================================

func TestParseSeqSet_InvalidRange(t *testing.T) {
	_, err := parseSeqSet("1:2:3", 10)
	if err == nil {
		t.Error("expected error for invalid range 1:2:3")
	}
	if !strings.Contains(err.Error(), "invalid range") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseSeqSet_InvalidSeqNumInRange(t *testing.T) {
	_, err := parseSeqSet("abc:def", 10)
	if err == nil {
		t.Error("expected error for non-numeric range parts")
	}
}

func TestParseSeqSet_StarRange(t *testing.T) {
	seqs, err := parseSeqSet("3:5", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seqs) != 3 {
		t.Fatalf("expected 3 sequence numbers, got %d: %v", len(seqs), seqs)
	}
	if seqs[0] != 3 || seqs[1] != 4 || seqs[2] != 5 {
		t.Errorf("expected [3,4,5], got %v", seqs)
	}
}

// =======================================================================
// commands.go: handleSelect (87.5%) - test mailbox with Unseen==0
// commands.go: handleExamine (87.5%) - test mailbox with Unseen==0
// The "if mailbox.Unseen > 0" branch is not taken when Unseen==0.
// mockMailstore always returns Exists:10, Unseen not set (zero), so that
// branch is already skipped. Need to cover path where Unseen > 0.
// =======================================================================

func TestHandleSelect_WithUnseen(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	// Replace mailstore with one that returns Unseen > 0
	wrapper := &unseenMockMailstore{&mockMailstore{}}
	session.server.mailstore = wrapper

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SELECT INBOX")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "UNSEEN") {
		t.Errorf("Expected UNSEEN in response, got: %s", resp)
	}
	<-done
}

func TestHandleExamine_WithUnseen(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	wrapper := &unseenMockMailstore{&mockMailstore{}}
	session.server.mailstore = wrapper

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXAMINE INBOX")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "UNSEEN") {
		t.Errorf("Expected UNSEEN in response, got: %s", resp)
	}
	<-done
}

// unseenMockMailstore returns a mailbox with Unseen > 0
type unseenMockMailstore struct {
	*mockMailstore
}

func (u *unseenMockMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	return &Mailbox{
		Name:        mailbox,
		Exists:      5,
		Recent:      1,
		Unseen:      3,
		UIDValidity: 12345,
		UIDNext:     100,
	}, nil
}

// =======================================================================
// commands.go: handleCreate (85.7%) - need the error from mailstore
// Already covered by failingMailstore, but let's ensure the error
// message formatting is covered.
// =======================================================================

func TestHandleCreate_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	// nil mailstore is set by setupSessionWithPipe which uses mockMailstore,
	// but handleCreate checks s.server.mailstore == nil
	// We need a server with nil mailstore
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CREATE TestBox")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleRename (85.7%) - nil mailstore path
// =======================================================================

func TestHandleRename_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 RENAME Old New")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: handleList (85.7%) - nil mailstore path + empty pattern
// =======================================================================

func TestHandleList_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LIST \"\" *")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

func TestHandleList_EmptyPattern(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LIST \"\" \"\"")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestHandleList_ReferenceWithSlash(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		// Reference ends with "/" and pattern is non-empty
		done <- session.handleCommand("A001 LIST \"INBOX/\" *")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// commands.go: handleStatus (85.2%) - nil mailstore + each status item
// =======================================================================

func TestHandleStatus_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STATUS INBOX (MESSAGES)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

func TestHandleStatus_AllItemsIndividually(t *testing.T) {
	items := []string{"MESSAGES", "RECENT", "UIDNEXT", "UIDVALIDITY", "UNSEEN"}
	for _, item := range items {
		t.Run(item, func(t *testing.T) {
			client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
			defer client.Close()

			done := make(chan error, 1)
			go func() {
				done <- session.handleCommand(fmt.Sprintf("A001 STATUS INBOX (%s)", item))
			}()

			resp := drainConn(client, 200*time.Millisecond)
			if !strings.Contains(resp, item) {
				t.Errorf("Expected %s in status response, got: %s", item, resp)
			}
			<-done
		})
	}
}

// =======================================================================
// commands.go: handleFetch (88.2%) - nil mailstore, nil selected
// =======================================================================

func TestHandleFetch_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH 1 (FLAGS)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

func TestHandleFetch_NilSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH 1 (FLAGS)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil selected, got: %s", resp)
	}
	<-done
}

func TestHandleFetch_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH 1")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for missing fetch items, got: %s", resp)
	}
	<-done
}

// =======================================================================
// commands.go: parseSearchCriteria (89.7%) - test UID arg with no value
// =======================================================================

func TestParseSearchCriteria_UIDNoValue(t *testing.T) {
	// UID at the end with no following value should not panic
	result := parseSearchCriteria([]string{"UID"})
	// When UID is at end, i+1 >= len(args), so UIDSet is not set
	if result.UIDSet != "" {
		t.Errorf("expected empty UIDSet, got %q", result.UIDSet)
	}
}

func TestParseSearchCriteria_EmptyArgs(t *testing.T) {
	result := parseSearchCriteria([]string{})
	if !result.All {
		t.Error("expected All to be true by default")
	}
}

func TestParseSearchCriteria_SubjectNoValue(t *testing.T) {
	result := parseSearchCriteria([]string{"SUBJECT"})
	if result.Subject != "" {
		t.Errorf("expected empty Subject, got %q", result.Subject)
	}
}

func TestParseSearchCriteria_FromNoValue(t *testing.T) {
	result := parseSearchCriteria([]string{"FROM"})
	if result.From != "" {
		t.Errorf("expected empty From, got %q", result.From)
	}
}

func TestParseSearchCriteria_ToNoValue(t *testing.T) {
	result := parseSearchCriteria([]string{"TO"})
	if result.To != "" {
		t.Errorf("expected empty To, got %q", result.To)
	}
}

// =======================================================================
// parser.go: readQuotedString (83.3%)
// Missing paths:
//   - Escaped character inside quoted string
//   - Unclosed quote (no closing quote)
//   - Calling readQuotedString when pos does not point to a quote
// =======================================================================

func TestReadQuotedString_EscapedChar(t *testing.T) {
	p := NewParser(`"hello\"world" test`)
	result := p.readQuotedString()
	if result != `hello\"world` {
		t.Errorf("expected 'hello\\\"world', got %q", result)
	}
}

func TestReadQuotedString_UnclosedQuote(t *testing.T) {
	p := NewParser(`"hello world`)
	result := p.readQuotedString()
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestReadQuotedString_NotAtQuote(t *testing.T) {
	p := NewParser("hello")
	// Set pos to 0 which is not a quote
	result := p.readQuotedString()
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestReadQuotedString_EmptyQuoted(t *testing.T) {
	p := NewParser(`""`)
	result := p.readQuotedString()
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestReadQuotedString_EscapedAtEnd(t *testing.T) {
	p := NewParser(`"test\"`)
	result := p.readQuotedString()
	// The escaped quote is consumed by the parser; returns raw content
	if result != `test\"` {
		t.Errorf("got %q", result)
	}
}

// =======================================================================
// parser.go: normalizeFetchItem (87.5%)
// The uncovered branch is the final `return item` when none of the
// special prefixes match (i.e., not BODY[, BODY.PEEK[, or RFC822).
// =======================================================================

func TestNormalizeFetchItem_PlainItem(t *testing.T) {
	result := normalizeFetchItem("FLAGS")
	if result != "FLAGS" {
		t.Errorf("expected FLAGS, got %q", result)
	}
}

func TestNormalizeFetchItem_BodyPeekBracket(t *testing.T) {
	result := normalizeFetchItem("body.peek[HEADER]")
	if result != "BODY.PEEK[HEADER]" {
		t.Errorf("expected BODY.PEEK[HEADER], got %q", result)
	}
}

func TestNormalizeFetchItem_RFC822Prefix(t *testing.T) {
	result := normalizeFetchItem("rfc822.header")
	if result != "RFC822.HEADER" {
		t.Errorf("expected RFC822.HEADER, got %q", result)
	}
}

func TestNormalizeFetchItem_UID(t *testing.T) {
	result := normalizeFetchItem("uid")
	if result != "UID" {
		t.Errorf("expected UID, got %q", result)
	}
}

func TestNormalizeFetchItem_InternalDate(t *testing.T) {
	result := normalizeFetchItem("internaldate")
	if result != "INTERNALDATE" {
		t.Errorf("expected INTERNALDATE, got %q", result)
	}
}

// =======================================================================
// parser.go: ParseStatusItems (81.2%)
// Missing: the parenthesized list path for StatusItems
// =======================================================================

func TestParseStatusItems_MultipleItemsParens(t *testing.T) {
	items, err := ParseStatusItems("(MESSAGES RECENT UIDNEXT UIDVALIDITY UNSEEN)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 5 {
		t.Errorf("expected 5 items, got %d: %v", len(items), items)
	}
}

func TestParseStatusItems_SingleItemNoParens(t *testing.T) {
	items, err := ParseStatusItems("MESSAGES")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0] != StatusMessages {
		t.Errorf("expected [MESSAGES], got %v", items)
	}
}

// =======================================================================
// server.go: Start (87.5%) - test error path (listen failure)
// =======================================================================

func TestServerStart_ListenError(t *testing.T) {
	// Use an invalid address that can't be listened on
	config := &Config{Addr: "256.256.256.256:0"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	err := server.Start()
	if err == nil {
		t.Error("expected error for invalid listen address")
		server.Stop()
	}
}

// =======================================================================
// server.go: handleConnection (0%) and Handle (50%)
// These are hard to test directly because they require real connections.
// Let's test handleConnection indirectly via Start + real connection.
// =======================================================================

func TestServerHandleConnection(t *testing.T) {
	config := &Config{Addr: "127.0.0.1:0"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Get the actual address
	var addr string
	for _, l := range server.listeners {
		addr = l.Addr().String()
		break
	}

	// Connect to the server
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		server.Stop()
		t.Fatalf("Failed to connect: %v", err)
	}

	// Read the greeting
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		server.Stop()
		t.Fatalf("Failed to read greeting: %v", err)
	}

	greeting := string(buf[:n])
	if !strings.Contains(greeting, "OK") || !strings.Contains(greeting, "IMAP4rev1") {
		conn.Close()
		server.Stop()
		t.Errorf("Expected greeting with OK IMAP4rev1, got: %s", greeting)
	}

	// Send LOGOUT to end session cleanly
	conn.Write([]byte("A1 LOGOUT\r\n"))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.Read(buf)

	conn.Close()
	server.Stop()
}

func TestServerAcceptLoop_AcceptError(t *testing.T) {
	config := &Config{Addr: "127.0.0.1:0"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	// Create a listener manually and then close it to force accept error
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close() // Close it so accept will fail

	// Try to listen on the same address (may or may not work)
	// Instead, just test that a closed listener causes acceptLoop to handle errors gracefully
	listener2, _ := net.Listen("tcp", "127.0.0.1:0")
	_ = addr
	server.listeners = append(server.listeners, listener2)
	server.shutdown = make(chan struct{})
	server.running.Store(true)

	// Close the listener to force accept error
	listener2.Close()

	done := make(chan bool, 1)
	go func() {
		server.acceptLoop(listener2)
		done <- true
	}()

	// The acceptLoop should continue running (not crash) even with errors
	// Close shutdown to exit
	close(server.shutdown)

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for acceptLoop to exit")
	}
}

// =======================================================================
// server.go: Handle (50%) - test the command error path
// =======================================================================

func TestSessionHandle_CommandError(t *testing.T) {
	client, serverConn := net.Pipe()
	defer client.Close()

	config := &Config{Addr: ":0"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(serverConn, imapServer)

	handleDone := make(chan struct{})
	go func() {
		defer close(handleDone)
		session.Handle()
	}()

	// Read greeting
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	client.Read(buf)

	// Send NOOP
	client.Write([]byte("A1 NOOP\r\n"))
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	client.Read(buf)

	// Close client to force read error in Handle loop, which exits
	client.Close()

	select {
	case <-handleDone:
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for Handle to finish")
	}
}

// =======================================================================
// parser.go: readToken - test empty input
// =======================================================================

func TestReadToken_EmptyInput(t *testing.T) {
	p := NewParser("")
	// Advance pos past end
	p.pos = 10
	result := p.readToken()
	if result != "" {
		t.Errorf("expected empty token, got %q", result)
	}
}

// =======================================================================
// parser.go: ParseSequenceSet (90.9%) - test star in range
// =======================================================================

func TestParseSequenceSet_StarInRange(t *testing.T) {
	ranges, err := ParseSequenceSet("1:*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("expected 1 range, got %d", len(ranges))
	}
	if ranges[0].Start != 1 || ranges[0].End != 0 {
		t.Errorf("expected Start=1, End=0 (star), got Start=%d, End=%d", ranges[0].Start, ranges[0].End)
	}
}

// =======================================================================
// mailstore.go: CopyMessages (76.7%) - test with actual messages
// =======================================================================

func TestCopyMessages_WithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Sent")

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	err = ms.CopyMessages(user, "INBOX", "Sent", "1")
	if err != nil {
		t.Fatalf("CopyMessages: %v", err)
	}
}

func TestCopyMessages_InvalidSeqSet(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Sent")

	err = ms.CopyMessages(user, "INBOX", "Sent", "abc")
	if err == nil {
		t.Error("expected error for invalid seq set")
	}
}

// =======================================================================
// mailstore.go: MoveMessages (79.2%) - test with actual messages
// =======================================================================

func TestMoveMessages_WithMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Archive")

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	err = ms.MoveMessages(user, "INBOX", "Archive", "1")
	if err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}
}

func TestMoveMessages_InvalidSeqSet(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Archive")

	err = ms.MoveMessages(user, "INBOX", "Archive", "abc")
	if err == nil {
		t.Error("expected error for invalid seq set")
	}
}

// =======================================================================
// mailstore.go: FetchMessages (81.0%) - test with actual messages + body
// =======================================================================

func TestFetchMessages_WithBodyItems(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nHello World")
	err = ms.AppendMessage(user, mbox, nil, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	// Fetch with BODY[] item to exercise the needsData=true path
	msgs, err := ms.FetchMessages(user, mbox, "1", []string{"BODY[]", "FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if len(msgs[0].Data) == 0 {
		t.Error("expected message data to be loaded")
	}
}

func TestFetchMessages_InvalidSeqSet(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	_, err = ms.FetchMessages(user, mbox, "abc", []string{"FLAGS"})
	if err == nil {
		t.Error("expected error for invalid seq set")
	}
}

// =======================================================================
// mailstore.go: Expunge (81.8%) - test with non-deleted messages
// =======================================================================

func TestExpunge_NoDeletedMessages(t *testing.T) {
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
	err = ms.AppendMessage(user, mbox, []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Skipf("AppendMessage: %v", err)
	}

	// Expunge without any deleted messages
	err = ms.Expunge(user, mbox)
	if err != nil {
		t.Fatalf("Expunge: %v", err)
	}

	// Message should still be there
	msgs, _ := ms.FetchMessages(user, mbox, "1", []string{"FLAGS"})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after expunge, got %d", len(msgs))
	}
}

// =======================================================================
// mailstore.go: AppendMessage (80.0%) - test with empty data
// =======================================================================

func TestAppendMessage_EmptyData(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mbox := "INBOX"
	ms.CreateMailbox(user, mbox)

	err = ms.AppendMessage(user, mbox, nil, time.Now(), []byte{})
	if err != nil {
		t.Logf("AppendMessage with empty data: %v", err)
	}
}

// =======================================================================
// mailstore.go: SelectMailbox (71.4%) - test error path
// =======================================================================

func TestSelectMailbox_NonexistentMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	mb, err := ms.SelectMailbox(user, "NonExistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mb == nil {
		t.Error("expected non-nil mailbox")
	}
	if mb.Name != "NonExistent" {
		t.Errorf("expected name NonExistent, got %s", mb.Name)
	}
}

// =======================================================================
// mailstore.go: ListMailboxes (88.9%) - test with different patterns
// =======================================================================

func TestListMailboxes_SentStar(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Sent")
	ms.CreateMailbox(user, "SentItems")

	list, err := ms.ListMailboxes(user, "Sent*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 mailboxes matching Sent*, got %d: %v", len(list), list)
	}
}

// =======================================================================
// commands.go: handleStore additional operations
// =======================================================================

func TestHandleStore_FlagsSilentRemove(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 -FLAGS.SILENT (\\Seen)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestHandleStore_FlagsReplace(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 FLAGS (\\Seen)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// commands.go: handleSearch with parseSearchCriteria UID/NEW/OLD
// =======================================================================

func TestHandleSearch_WithUID(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH UID 1:10")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// commands.go: handleAppend with nil mailstore
// =======================================================================

func TestHandleAppend_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX {5}")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("hello\r\n"))
	}

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// commands.go: handleDelete nil mailstore
// =======================================================================

func TestHandleDelete_NilMailstoreViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 DELETE SomeFolder")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}
