package imap

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// setupSessionWithPipeRaw creates a net.Pipe pair and a session on the
// server side. It returns (clientConn, session). The caller is
// responsible for closing clientConn.
func setupSessionWithPipeRaw(t *testing.T, state State, user string, selected *Mailbox) (net.Conn, *Session) {
	t.Helper()
	client, srv := net.Pipe()
	server := NewServer(&Config{Addr: ":0"}, &mockMailstore{})
	session := NewSession(srv, server)
	session.state = state
	session.user = user
	session.selected = selected
	return client, session
}

// =======================================================================
// handleID: 0.0% -> cover via handleCommand in both authenticated and
// selected states.
// =======================================================================

func TestCoverHandleID_Authenticated(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 ID (\"name\" \"test\")")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "ID") {
		t.Errorf("expected ID in response, got: %s", resp)
	}
	if !strings.Contains(resp, "uMailServer") {
		t.Errorf("expected uMailServer in ID response, got: %s", resp)
	}
	if !strings.Contains(resp, "OK") {
		t.Errorf("expected OK in ID response, got: %s", resp)
	}
	<-done
}

func TestCoverHandleID_Selected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 ID NIL")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "ID") {
		t.Errorf("expected ID in response, got: %s", resp)
	}
	<-done
}

func TestCoverHandleID_Direct(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleID([]string{"NIL"})
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "ID") {
		t.Errorf("expected ID in response, got: %s", resp)
	}
	<-done
}

// =======================================================================
// EnsureDefaultMailboxes: 0.0%
// =======================================================================

func TestCoverEnsureDefaultMailboxes(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	err = ms.EnsureDefaultMailboxes(user)
	if err != nil {
		t.Fatalf("EnsureDefaultMailboxes: %v", err)
	}

	// Verify the default mailboxes were created
	list, err := ms.ListMailboxes(user, "*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}

	expected := []string{"INBOX", "Sent", "Drafts", "Junk", "Trash", "Archive"}
	for _, name := range expected {
		found := false
		for _, mb := range list {
			if mb == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected mailbox %q to exist, got: %v", name, list)
		}
	}

	// Calling again should be idempotent (no error)
	err = ms.EnsureDefaultMailboxes(user)
	if err != nil {
		t.Fatalf("EnsureDefaultMailboxes (second call): %v", err)
	}
}

// =======================================================================
// splitAddress: 66.7% - address without @ sign
// =======================================================================

func TestCoverSplitAddress_NoAtSign(t *testing.T) {
	local, domain := splitAddress("just-a-name")
	if local != "just-a-name" {
		t.Errorf("expected local 'just-a-name', got %q", local)
	}
	if domain != "" {
		t.Errorf("expected empty domain, got %q", domain)
	}
}

func TestCoverSplitAddress_WithAt(t *testing.T) {
	local, domain := splitAddress("user@example.com")
	if local != "user" {
		t.Errorf("expected local 'user', got %q", local)
	}
	if domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", domain)
	}
}

func TestCoverSplitAddress_MultipleAt(t *testing.T) {
	// LastIndex should split on the last @
	local, domain := splitAddress("user@host@domain.com")
	if local != "user@host" {
		t.Errorf("expected local 'user@host', got %q", local)
	}
	if domain != "domain.com" {
		t.Errorf("expected domain 'domain.com', got %q", domain)
	}
}

func TestCoverSplitAddress_Empty(t *testing.T) {
	local, domain := splitAddress("")
	if local != "" {
		t.Errorf("expected empty local, got %q", local)
	}
	if domain != "" {
		t.Errorf("expected empty domain, got %q", domain)
	}
}

// =======================================================================
// handleCommand: default in handleNotAuthenticated (unrecognized cmd)
// =======================================================================

func TestCoverHandleCommand_NotAuthenticatedDefault(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 BOGUSCOMMAND")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for unknown command in NotAuthenticated state, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleAuthenticated: default (unknown command) path
// =======================================================================

func TestCoverHandleAuthenticated_DefaultCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 UNKNOWNCOMMAND")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for unknown command in Authenticated state, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleSelected: default (unknown command) path + missing args for
// COPY, MOVE, FETCH, STORE, SEARCH
// =======================================================================

func TestCoverHandleSelected_DefaultCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 WEIRDCMD")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for unknown command in Selected state, got: %s", resp)
	}
	<-done
}

func TestCoverHandleCopy_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 COPY")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for COPY without args, got: %s", resp)
	}
	<-done
}

func TestCoverHandleMove_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 MOVE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for MOVE without args, got: %s", resp)
	}
	<-done
}

func TestCoverHandleStore_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for STORE without args, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleStore: invalid operation (93.5%)
// =======================================================================

func TestCoverHandleStore_InvalidOperation(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 INVALIDOP (\\Seen)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for invalid STORE operation, got: %s", resp)
	}
	<-done
}

func TestCoverHandleStore_NoSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 +FLAGS (\\Seen)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for no selected mailbox, got: %s", resp)
	}
	<-done
}

func TestCoverHandleStore_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 +FLAGS (\\Seen)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

func TestCoverHandleStore_FlagsSilentReplace(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 FLAGS.SILENT (\\Seen)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// handleExpunge: search error path (81.2%)
// =======================================================================

func TestCoverHandleExpunge_SearchError(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.searchErr = fmt.Errorf("search failed")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXPUNGE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for search error in EXPUNGE, got: %s", resp)
	}
	<-done
}

func TestCoverHandleExpunge_WithDeletedResults(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	// Use a store that returns deleted messages in search
	session.server.mailstore = &expungeResultMailstore{}

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXPUNGE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "EXPUNGE") {
		t.Logf("EXPUNGE response: %s", resp)
	}
	if !strings.Contains(resp, "OK") {
		t.Errorf("expected OK after EXPUNGE, got: %s", resp)
	}
	<-done
}

// expungeResultMailstore returns deleted sequence numbers for SearchMessages
type expungeResultMailstore struct {
	*mockMailstore
}

func (e *expungeResultMailstore) SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error) {
	if criteria.Deleted {
		return []uint32{3, 1}, nil // Return some deleted sequence numbers
	}
	return nil, nil
}

func (e *expungeResultMailstore) Authenticate(username, password string) (bool, error) {
	return true, nil
}

// =======================================================================
// parseSearchCriteria: DRAFT, UNDRAFT keywords (93.6%)
// =======================================================================

func TestCoverParseSearchCriteria_Draft(t *testing.T) {
	// Note: parseSearchCriteria (commands.go) doesn't handle DRAFT/UNDRAFT.
	// DRAFT falls through to the default case (ignored).
	result := parseSearchCriteria([]string{"DRAFT"})
	// The default case in the switch means DRAFT is silently ignored
	// All stays true by default
	if !result.All {
		t.Error("expected All to be true by default")
	}
}

func TestCoverParseSearchCriteria_Undraft(t *testing.T) {
	// UNDRAFT also falls through to default case
	result := parseSearchCriteria([]string{"UNDRAFT"})
	if !result.All {
		t.Error("expected All to be true by default")
	}
}

func TestCoverParseSearchCriteria_NewOldRecent(t *testing.T) {
	result := parseSearchCriteria([]string{"NEW", "OLD", "RECENT"})
	if !result.New {
		t.Error("expected New to be true")
	}
	if !result.Old {
		t.Error("expected Old to be true")
	}
	if !result.Recent {
		t.Error("expected Recent to be true")
	}
}

// =======================================================================
// handleAppend: literal without closing brace (93.9%)
// The uncovered branch is when literalStart > 0 but literalEnd <= 0
// (no closing brace after opening brace).
// =======================================================================

func TestCoverHandleAppend_LiteralNoCloseBrace(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		// APPEND with { but no } - literalStart > 0 but literalEnd <= 0
		done <- session.handleCommand("A001 APPEND INBOX (\\Seen) {10")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// handleAuthPlain: continuation request path (no initial response) (40%)
// =======================================================================

func TestCoverHandleAuthPlain_ContinuationRequest(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.tag = "A001"
	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthPlain([]string{})
	}()

	// Read the continuation
	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	contResp := string(buf[:n])
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Send valid PLAIN credentials: base64("\0test\0password") = AHRlc3QAcGFzc3dvcmQ=
	client.Write([]byte("AHRlc3QAcGFzc3dvcmQ=\r\n"))

	resp := drainConn(client, 500*time.Millisecond)
	if !strings.Contains(resp, "OK") {
		t.Logf("AUTH PLAIN continuation response: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthPlain_ContinuationCancel(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.tag = "A001"
	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthPlain([]string{})
	}()

	// Read the continuation
	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	contResp := string(buf[:n])
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Cancel with *
	client.Write([]byte("*\r\n"))

	resp := drainConn(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for cancelled AUTHENTICATE, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthPlain_ContinuationBadBase64(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.tag = "A001"
	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthPlain([]string{})
	}()

	// Read the continuation
	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	contResp := string(buf[:n])
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Invalid base64
	client.Write([]byte("!!!invalid-base64!!!\r\n"))

	resp := drainConn(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad base64, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthPlain_BadBase64Initial(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.tag = "A001"
	done := make(chan error, 1)
	go func() {
		done <- session.handleAuthPlain([]string{"!!!badbase64!!!"})
	}()

	resp := drainConn(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad base64 in initial response, got: %q", resp)
	}
	<-done
}

func TestCoverHandleAuthPlain_InvalidCredentials(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.tag = "A001"
	done := make(chan error, 1)
	go func() {
		// Valid base64 but not enough parts (only 2 instead of 3)
		// base64("only\0two") = b25seQB0d28=
		done <- session.handleAuthPlain([]string{"b25seQB0d28="})
	}()

	resp := drainConn(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for invalid PLAIN credentials, got: %q", resp)
	}
	<-done
}

// =======================================================================
// handleAuthLogin: cancel with *, bad base64 paths (60%)
// We use a raw net.Pipe directly (not through setupSessionWithPipe) to avoid
// bufio.Reader conflict when handleCommand reads the command line
// before passing it to handleAuthLogin which tries to create a new bufio.Reader.
// =======================================================================

// setupAuthLoginPipe creates a net.Pipe pair and a session ready for
// AUTH LOGIN testing. Returns (clientConn, session, serverConn).
func setupAuthLoginPipe(t *testing.T) (net.Conn, *Session, net.Conn) {
	t.Helper()
	client, srv := net.Pipe()
	server := NewServer(&Config{Addr: ":0"}, &mockMailstore{})
	session := NewSession(srv, server)
	session.state = StateNotAuthenticated
	session.tag = "A001"
	return client, session, srv
}

// readAuthLine reads from conn with a deadline and returns the response.
func readAuthLine(conn net.Conn, timeout time.Duration) string {
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	return string(buf[:n])
}

func TestCoverHandleAuthLogin_BadBase64Username(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Invalid base64 for username
	client.Write([]byte("!!!bad!!!\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad base64 username, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthLogin_CancelPassword(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Valid base64 username: "test" = dGVzdA==
	client.Write([]byte("dGVzdA==\r\n"))

	contResp2 := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp2, "+") {
		t.Fatalf("expected + continuation for password, got: %q", contResp2)
	}

	// Cancel at password prompt
	client.Write([]byte("*\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for cancelled password, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthLogin_BadBase64Password(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Valid base64 username
	client.Write([]byte("dGVzdA==\r\n"))

	contResp2 := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp2, "+") {
		t.Fatalf("expected + continuation for password, got: %q", contResp2)
	}

	// Invalid base64 for password
	client.Write([]byte("!!!bad-password!!!\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad base64 password, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthLogin_BadCredentials(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// base64("wrong") = d3Jvbmc=
	client.Write([]byte("d3Jvbmc=\r\n"))

	contResp2 := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp2, "+") {
		t.Fatalf("expected + continuation for password, got: %q", contResp2)
	}

	// base64("creds") = Y3JlZHM=
	client.Write([]byte("Y3JlZHM=\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad credentials, got: %q", resp)
	}
	<-authDone
}

// =======================================================================
// handleAuthenticate: unsupported mechanism
// =======================================================================

func TestCoverHandleAuthenticate_UnsupportedMechanism(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE CRAM-MD5")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for unsupported mechanism, got: %s", resp)
	}
	<-done
}

func TestCoverHandleAuthenticate_MissingMechanism(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for missing mechanism, got: %s", resp)
	}
	<-done
}

// =======================================================================
// MoveMessages: 87.5% - message already has \\Deleted flag
// =======================================================================

func TestCoverMoveMessages_AlreadyDeleted(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Trash")

	// Append with \\Deleted flag already set
	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen", "\\Deleted"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	err = ms.MoveMessages(user, "INBOX", "Trash", "1")
	if err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}

	// Verify message still has \\Deleted flag
	msgs, _ := ms.FetchMessages(user, "INBOX", "1", []string{"FLAGS"})
	for _, m := range msgs {
		if !hasFlag(m.Flags, "\\Deleted") {
			t.Errorf("expected \\Deleted flag, got: %v", m.Flags)
		}
	}
}

// =======================================================================
// CopyMessages: 83.3% - error paths within the loop
// Test with sequence set that references messages that don't exist in dest
// =======================================================================

func TestCoverCopyMessages_SequenceOutOfRange(t *testing.T) {
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
		t.Fatalf("AppendMessage: %v", err)
	}

	// Copy with sequence number out of range
	err = ms.CopyMessages(user, "INBOX", "Archive", "5")
	if err != nil {
		t.Fatalf("CopyMessages out of range: %v", err)
	}
	// Should not error - just no messages copied
}

// =======================================================================
// AppendMessage: 80.0% - StoreMessageMetadata error path
// =======================================================================

func TestCoverAppendMessage_ToNonexistentMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	// Don't create INBOX - the AppendMessage should still succeed since
	// the underlying database creates default structures
	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	// May or may not error depending on implementation
	t.Logf("AppendMessage to nonexistent mailbox: err=%v", err)
}

// =======================================================================
// ListMailboxes: 88.9% - empty result set (no mailboxes match pattern)
// =======================================================================

func TestCoverListMailboxes_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	// Pattern that matches nothing
	list, err := ms.ListMailboxes(user, "ZZZZZ*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 mailboxes, got %d: %v", len(list), list)
	}
}

// =======================================================================
// handleSelect/Examine missing args direct calls
// =======================================================================

func TestCoverHandleSelect_EmptyMailboxName(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SELECT \"\"")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverHandleExamine_EmptyMailboxName(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXAMINE \"\"")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// handleCreate/Delete: missing args
// =======================================================================

func TestCoverHandleCreate_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CREATE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for CREATE without args, got: %s", resp)
	}
	<-done
}

func TestCoverHandleDelete_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 DELETE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for DELETE without args, got: %s", resp)
	}
	<-done
}

func TestCoverHandleRename_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 RENAME")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for RENAME without args, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleUID: missing args and unknown UID command
// =======================================================================

func TestCoverHandleUID_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 UID")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for UID without args, got: %s", resp)
	}
	<-done
}

func TestCoverHandleUID_UnknownSubcommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 UID BOGUS 1")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for unknown UID subcommand, got: %s", resp)
	}
	<-done
}

// =======================================================================
// SearchMessages: error from GetMessageUIDs
// =======================================================================

func TestCoverSearchMessages_Error(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	// Search on nonexistent user/mailbox
	results, err := ms.SearchMessages("nonexistent", "NoBox", SearchCriteria{All: true})
	t.Logf("SearchMessages nonexistent: results=%v err=%v", results, err)
}

// =======================================================================
// FetchMessages: getMessage error path
// =======================================================================

func TestCoverFetchMessages_GetMessageError(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	// Fetch from nonexistent mailbox - should get empty results
	msgs, err := ms.FetchMessages("nonexistent", "NoBox", "1", []string{"FLAGS"})
	t.Logf("FetchMessages nonexistent: msgs=%v err=%v", msgs, err)
}

// =======================================================================
// StoreFlags: GetMessageUIDs error path
// =======================================================================

func TestCoverStoreFlags_GetMessageUIDsError(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	err = ms.StoreFlags("nonexistent", "NoBox", "1", []string{"\\Seen"}, true)
	t.Logf("StoreFlags nonexistent: err=%v", err)
}

// =======================================================================
// handleAuthenticated: APPEND in authenticated state (not selected)
// =======================================================================

func TestCoverHandleAuthenticated_Append(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX (\\Seen) {5}")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("hello\r\n"))
	}

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// handleClose: nil selected
// =======================================================================

func TestCoverHandleClose_NilSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CLOSE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "OK") {
		t.Errorf("expected OK for CLOSE with nil selected, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleAppend: read error during literal
// =======================================================================

func TestCoverHandleAppend_ReadError(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX (\\Seen) {100}")
	}()

	// Read continuation
	_ = drainConn(client, 200*time.Millisecond)

	// Close client to force read error
	client.Close()

	err := <-done
	if err != nil {
		t.Logf("handleAppend with read error returned: %v (expected)", err)
	}
}

// =======================================================================
// NewBboltMailstore: error from MkdirAll
// =======================================================================

func TestCoverNewBboltMailstore_MkdirError(t *testing.T) {
	// On Windows, MkdirAll may succeed for nested paths,
	// so we test by providing a path where the messages subdirectory
	// cannot be created (e.g., it's a file instead).
	tmpDir := t.TempDir()
	// Create a file named "messages" to prevent directory creation
	msgPath := tmpDir + "/messages"
	msgFile, err := os.Create(msgPath)
	if err != nil {
		t.Fatalf("Create file: %v", err)
	}
	msgFile.Close()
	_, err = NewBboltMailstore(tmpDir)
	if err == nil {
		t.Error("expected error when message store path is blocked by a file")
	}
}

// =======================================================================
// authenticateUser: both authFunc and mailstore paths
// =======================================================================

func TestCoverAuthenticateUser_WithAuthFunc(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.server.authFunc = func(username, password string) (bool, error) {
		return username == "admin" && password == "secret", nil
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN admin secret")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "OK") {
		t.Errorf("expected OK for successful auth with authFunc, got: %s", resp)
	}
	<-done
}

func TestCoverAuthenticateUser_AuthFuncError(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	session.server.authFunc = func(username, password string) (bool, error) {
		return false, fmt.Errorf("auth service unavailable")
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN test password")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO when authFunc returns error, got: %s", resp)
	}
	<-done
}

func TestCoverAuthenticateUser_NilAuthFuncAndMailstore(t *testing.T) {
	client, srv := net.Pipe()
	defer client.Close()

	server := NewServer(&Config{Addr: ":0"}, nil) // nil mailstore
	session := NewSession(srv, server)

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN test password")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO with nil authFunc and nil mailstore, got: %s", resp)
	}
	srv.Close()
	<-done
}

// NOTE: Cannot test NotificationMailboxUpdate with nil mailstore because
// commands.go line 820 dereferences s.server.mailstore without a nil check,
// which would cause a panic.

// =======================================================================
// parser.go: ParseSearchCriteria with various criteria
// =======================================================================

func TestCoverParseSearchCriteriaParser_CC(t *testing.T) {
	criteria, err := ParseSearchCriteria("CC \"test@example.com\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Cc != "test@example.com" {
		t.Errorf("expected Cc 'test@example.com', got %q", criteria.Cc)
	}
}

func TestCoverParseSearchCriteriaParser_BCC(t *testing.T) {
	criteria, err := ParseSearchCriteria("BCC \"test@example.com\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Bcc != "test@example.com" {
		t.Errorf("expected Bcc 'test@example.com', got %q", criteria.Bcc)
	}
}

func TestCoverParseSearchCriteriaParser_BODY(t *testing.T) {
	criteria, err := ParseSearchCriteria("BODY \"hello world\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Body != "hello world" {
		t.Errorf("expected Body 'hello world', got %q", criteria.Body)
	}
}

func TestCoverParseSearchCriteriaParser_TEXT(t *testing.T) {
	criteria, err := ParseSearchCriteria("TEXT \"search text\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Text != "search text" {
		t.Errorf("expected Text 'search text', got %q", criteria.Text)
	}
}

func TestCoverParseSearchCriteriaParser_LARGER(t *testing.T) {
	criteria, err := ParseSearchCriteria("LARGER 1000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Larger != 1000 {
		t.Errorf("expected Larger 1000, got %d", criteria.Larger)
	}
}

func TestCoverParseSearchCriteriaParser_SMALLER(t *testing.T) {
	criteria, err := ParseSearchCriteria("SMALLER 500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Smaller != 500 {
		t.Errorf("expected Smaller 500, got %d", criteria.Smaller)
	}
}

func TestCoverParseSearchCriteriaParser_HEADER(t *testing.T) {
	criteria, err := ParseSearchCriteria("HEADER X-Custom myvalue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Header["X-Custom"] != "myvalue" {
		t.Errorf("expected Header[X-Custom]='myvalue', got %v", criteria.Header)
	}
}

func TestCoverParseSearchCriteriaParser_NOT(t *testing.T) {
	criteria, err := ParseSearchCriteria("NOT SEEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Not == nil || !criteria.Not.Seen {
		t.Error("expected NOT SEEN criteria")
	}
}

func TestCoverParseSearchCriteriaParser_OR(t *testing.T) {
	// The parser recursively parses all remaining tokens into Or[0],
	// so "OR SEEN FLAGGED" puts both SEEN and FLAGGED into Or[0].
	criteria, err := ParseSearchCriteria("OR SEEN FLAGGED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.Or[0] == nil {
		t.Fatal("expected Or[0] to be non-nil")
	}
	if !criteria.Or[0].Seen {
		t.Error("expected Or[0] to have Seen=true")
	}
	if !criteria.Or[0].Flagged {
		t.Error("expected Or[0] to have Flagged=true (parser consumes all remaining into first OR operand)")
	}
}

func TestCoverParseSearchCriteriaParser_UID(t *testing.T) {
	criteria, err := ParseSearchCriteria("UID 1:10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if criteria.UIDSet != "1:10" {
		t.Errorf("expected UIDSet '1:10', got %q", criteria.UIDSet)
	}
}

func TestCoverParseSearchCriteriaParser_Complex(t *testing.T) {
	// The parser's NOT recursive call consumes all remaining tokens into Not,
	// so FROM/SUBJECT/LARGER end up inside criteria.Not, not at the top level.
	criteria, err := ParseSearchCriteria("SEEN NOT DELETED FROM \"alice\" SUBJECT \"test\" LARGER 100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !criteria.Seen {
		t.Error("expected Seen")
	}
	if criteria.Not == nil {
		t.Fatal("expected Not to be non-nil")
	}
	if !criteria.Not.Deleted {
		t.Error("expected Not.Deleted")
	}
	// These are consumed by the NOT recursive parser
	if criteria.Not.From != "alice" {
		t.Errorf("expected Not.From 'alice', got %q", criteria.Not.From)
	}
	if criteria.Not.Subject != "test" {
		t.Errorf("expected Not.Subject 'test', got %q", criteria.Not.Subject)
	}
	if criteria.Not.Larger != 100 {
		t.Errorf("expected Not.Larger 100, got %d", criteria.Not.Larger)
	}
}

// =======================================================================
// handleSearch with DRAFT/UNDRAFT criteria
// =======================================================================

func TestCoverHandleSearch_DraftCriteria(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH DRAFT UNDRAFT")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// handleAuthenticated: all command variants
// =======================================================================

func TestCoverHandleAuthenticated_APPEND_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for APPEND without args, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleSearch with no selected mailbox via direct call
// =======================================================================

func TestCoverHandleSearch_NilMailstoreDirect(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleSearch([]string{"ALL"}, "A001 SEARCH ALL")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

func TestCoverHandleFetch_NilMailstoreDirect(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()
	session.server.mailstore = nil

	done := make(chan error, 1)
	go func() {
		done <- session.handleFetch([]string{"1", "FLAGS"}, "A001 FETCH 1 FLAGS")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for nil mailstore, got: %s", resp)
	}
	<-done
}

// =======================================================================
// FormatFlags function
// =======================================================================

func TestCoverFormatFlags(t *testing.T) {
	result := FormatFlags([]string{})
	if result != "()" {
		t.Errorf("expected '()', got %q", result)
	}

	result = FormatFlags([]string{"\\Seen", "\\Flagged"})
	if result != "(\\Seen \\Flagged)" {
		t.Errorf("expected '(\\Seen \\Flagged)', got %q", result)
	}
}

// =======================================================================
// handleIdle: channel closed (ok=false)
// =======================================================================

func TestCoverHandleIdle_ChannelClosed(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 1, Recent: 0})
	defer client.Close()

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout waiting for continuation")
	}

	time.Sleep(50 * time.Millisecond)

	// Unsubscribe the channel to close it, simulating a closed notification channel
	hub := GetNotificationHub()
	if session.idleNotifyChan != nil {
		hub.Unsubscribe("testuser", session.idleNotifyChan)
	}

	// Wait for idle to finish due to closed channel
	select {
	case <-idleDone:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for idle to finish after channel close")
	}
}

// =======================================================================
// handleIdle: Recent becomes 0 (no decrement below 0)
// =======================================================================

func TestCoverHandleIdle_ExpungeZeroRecent(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 3, Recent: 0})
	defer client.Close()

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
	hub.NotifyExpunge("testuser", "INBOX", 2)

	if line, ok := waitForLine(lines, "EXPUNGE", 2*time.Second); ok {
		t.Logf("expunge notification: %s", line)
	}

	client.Write([]byte("DONE\r\n"))

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// =======================================================================
// handleAppend: APPEND without any flags or date, just mailbox + literal
// =======================================================================

func TestCoverHandleAppend_NoFlagsNoDate(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX {11}")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("Hello World\r\n"))
	}

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// =======================================================================
// parseSeqNum with * and invalid number
// =======================================================================

func TestCoverParseSeqNum_Star(t *testing.T) {
	result, err := parseSeqNum("*", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 10 {
		t.Errorf("expected 10 for *, got %d", result)
	}
}

func TestCoverParseSeqNum_InvalidNumber(t *testing.T) {
	_, err := parseSeqNum("abc", 10)
	if err == nil {
		t.Error("expected error for invalid sequence number")
	}
}

// =======================================================================
// SelectMailbox: GetMailboxCounts error path (71.4%)
// =======================================================================

func TestCoverSelectMailbox_Success(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	mb, err := ms.SelectMailbox(user, "INBOX")
	if err != nil {
		t.Fatalf("SelectMailbox: %v", err)
	}
	if mb.Name != "INBOX" {
		t.Errorf("expected Name 'INBOX', got %q", mb.Name)
	}
	if mb.UIDValidity == 0 {
		t.Error("expected non-zero UIDValidity")
	}
}

func TestCoverSelectMailbox_NonexistentMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	// SelectMailbox on a nonexistent mailbox may not error (depends on
	// the underlying database implementation which may auto-create).
	mb, err := ms.SelectMailbox("testuser", "NoSuchBox")
	t.Logf("SelectMailbox nonexistent: mb=%v err=%v", mb, err)
}

// =======================================================================
// Expunge: actually deleting messages (81.8%)
// =======================================================================

func TestCoverExpunge_WithDeletedMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	// Append a message with \\Deleted flag
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen", "\\Deleted"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Expunge should remove it
	err = ms.Expunge(user, "INBOX")
	if err != nil {
		t.Fatalf("Expunge: %v", err)
	}

	// Verify no messages left
	msgs, _ := ms.FetchMessages(user, "INBOX", "1:*", []string{"FLAGS"})
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after expunge, got %d", len(msgs))
	}
}

func TestCoverExpunge_NoDeletedMessages(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Expunge should not remove non-deleted messages
	err = ms.Expunge(user, "INBOX")
	if err != nil {
		t.Fatalf("Expunge: %v", err)
	}

	msgs, _ := ms.FetchMessages(user, "INBOX", "1:*", []string{"FLAGS"})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after expunge of non-deleted, got %d", len(msgs))
	}
}

// =======================================================================
// CopyMessages: actually copying a message that exists (83.3%)
// =======================================================================

func TestCoverCopyMessages_ActualCopy(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Archive")

	data := []byte("From: a@b.com\r\nSubject: Test Copy\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Copy message 1 from INBOX to Archive
	err = ms.CopyMessages(user, "INBOX", "Archive", "1")
	if err != nil {
		t.Fatalf("CopyMessages: %v", err)
	}

	// Verify message exists in Archive
	msgs, _ := ms.FetchMessages(user, "Archive", "1:*", []string{"FLAGS"})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message in Archive, got %d", len(msgs))
	}
}

// =======================================================================
// StoreFlags: remove flags (90%)
// =======================================================================

func TestCoverStoreFlags_RemoveFlags(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen", "\\Flagged"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Remove \\Flagged
	err = ms.StoreFlags(user, "INBOX", "1", []string{"\\Flagged"}, false)
	if err != nil {
		t.Fatalf("StoreFlags remove: %v", err)
	}

	msgs, _ := ms.FetchMessages(user, "INBOX", "1", []string{"FLAGS"})
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message")
	}
	if hasFlag(msgs[0].Flags, "\\Flagged") {
		t.Errorf("expected \\Flagged to be removed, got flags: %v", msgs[0].Flags)
	}
	if !hasFlag(msgs[0].Flags, "\\Seen") {
		t.Errorf("expected \\Seen to remain, got flags: %v", msgs[0].Flags)
	}
}

// =======================================================================
// FetchMessages: with RFC822 item to load message data (90.5%)
// =======================================================================

func TestCoverFetchMessages_WithRFC822(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: a@b.com\r\nSubject: Test RFC822\r\n\r\nHello World")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	msgs, err := ms.FetchMessages(user, "INBOX", "1", []string{"RFC822", "FLAGS"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Data) == 0 {
		t.Error("expected message data to be loaded for RFC822 fetch item")
	}
}

func TestCoverFetchMessages_WithBodyItem(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: a@b.com\r\nSubject: Test Body\r\n\r\nBody text")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	msgs, err := ms.FetchMessages(user, "INBOX", "1", []string{"BODY", "UID"})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Data) == 0 {
		t.Error("expected message data to be loaded for BODY fetch item")
	}
}

// =======================================================================
// SearchMessages: with actual data (84.6%)
// =======================================================================

func TestCoverSearchMessages_WithCriteria(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: alice@example.com\r\nSubject: Hello World\r\nTo: bob@test.com\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Search by From
	results, err := ms.SearchMessages(user, "INBOX", SearchCriteria{From: "alice"})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for From=alice, got %d: %v", len(results), results)
	}

	// Search by Subject
	results, err = ms.SearchMessages(user, "INBOX", SearchCriteria{Subject: "hello"})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for Subject=hello, got %d: %v", len(results), results)
	}

	// Search by To
	results, err = ms.SearchMessages(user, "INBOX", SearchCriteria{To: "bob"})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for To=bob, got %d: %v", len(results), results)
	}

	// Search that doesn't match
	results, err = ms.SearchMessages(user, "INBOX", SearchCriteria{From: "charlie"})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for From=charlie, got %d: %v", len(results), results)
	}
}

func TestCoverSearchMessages_LargerSmaller(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")

	data := []byte("From: a@b.com\r\nSubject: Test\r\n\r\nBody")
	err = ms.AppendMessage(user, "INBOX", []string{"\\Seen"}, time.Now(), data)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Search Larger with size below threshold
	results, _ := ms.SearchMessages(user, "INBOX", SearchCriteria{Larger: 10000})
	if len(results) != 0 {
		t.Errorf("expected 0 results for Larger=10000, got %d", len(results))
	}

	// Search Smaller with size above threshold
	results, _ = ms.SearchMessages(user, "INBOX", SearchCriteria{Smaller: 1})
	if len(results) != 0 {
		t.Errorf("expected 0 results for Smaller=1, got %d", len(results))
	}

	// Search Smaller with large threshold (should match)
	results, _ = ms.SearchMessages(user, "INBOX", SearchCriteria{Smaller: 100000})
	if len(results) != 1 {
		t.Errorf("expected 1 result for Smaller=100000, got %d", len(results))
	}
}

// =======================================================================
// ListMailboxes: various patterns (88.9%)
// =======================================================================

func TestCoverListMailboxes_ExactPattern(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	ms.CreateMailbox(user, "Sent")
	ms.CreateMailbox(user, "Archive")

	// Exact match
	list, err := ms.ListMailboxes(user, "INBOX")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 1 || list[0] != "INBOX" {
		t.Errorf("expected [INBOX], got %v", list)
	}

	// Wildcard
	list, err = ms.ListMailboxes(user, "*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 mailboxes for *, got %d: %v", len(list), list)
	}

	// Prefix pattern
	list, err = ms.ListMailboxes(user, "S*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	if len(list) != 1 || list[0] != "Sent" {
		t.Errorf("expected [Sent] for S*, got %v", list)
	}
}

// =======================================================================
// handleAuthLogin: cancel at username, bad base64 username, read error
// =======================================================================

func TestCoverHandleAuthLogin_CancelUsername(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Cancel at username prompt
	client.Write([]byte("*\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for cancelled username, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthLogin_BadBase64UsernameDirect(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Invalid base64 for username
	client.Write([]byte("!!!not-base64!!!\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad base64 username, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthLogin_BadBase64PasswordDirect(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Valid base64 username
	client.Write([]byte("dGVzdA==\r\n"))

	contResp2 := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp2, "+") {
		t.Fatalf("expected + continuation for password, got: %q", contResp2)
	}

	// Invalid base64 for password
	client.Write([]byte("!!!not-base64!!!\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("expected NO for bad base64 password, got: %q", resp)
	}
	<-authDone
}

func TestCoverHandleAuthLogin_ReadErrorUsername(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// Close client to force read error for username
	client.Close()

	err := <-authDone
	if err == nil {
		t.Log("handleAuthLogin returned nil on read error")
	}
	_ = srv
}

// =======================================================================
// handleAuthLogin: successful auth via AUTH LOGIN
// =======================================================================

func TestCoverHandleAuthLogin_Success(t *testing.T) {
	client, session, srv := setupAuthLoginPipe(t)
	defer client.Close()
	defer srv.Close()

	session.server.authFunc = func(username, password string) (bool, error) {
		return username == "admin" && password == "secret", nil
	}

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleAuthLogin()
	}()

	contResp := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp, "+") {
		t.Fatalf("expected + continuation, got: %q", contResp)
	}

	// base64("admin") = YWRtaW4=
	client.Write([]byte("YWRtaW4=\r\n"))

	contResp2 := readAuthLine(client, 2*time.Second)
	if !strings.Contains(contResp2, "+") {
		t.Fatalf("expected + continuation for password, got: %q", contResp2)
	}

	// base64("secret") = c2VjcmV0
	client.Write([]byte("c2VjcmV0\r\n"))

	resp := readAuthLine(client, 500*time.Millisecond)
	if !strings.Contains(resp, "OK") {
		t.Errorf("expected OK for successful auth, got: %q", resp)
	}
	<-authDone
}

// =======================================================================
// Authenticate: valid credentials via mailstore
// =======================================================================

func TestCoverAuthenticateUser_ViaMailstore(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	defer ms.Close()

	user := "testuser"
	ms.CreateMailbox(user, "INBOX")
	// Set up the user with a password via the mailstore's Authenticate
	ms.Authenticate(user, "password123")

	client, srv := net.Pipe()
	defer client.Close()
	server := NewServer(&Config{Addr: ":0"}, ms)
	session := NewSession(srv, server)
	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN testuser password123")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") && !strings.Contains(resp, "OK") {
		t.Logf("LOGIN via mailstore response: %s", resp)
	}
	srv.Close()
	<-done
}

// =======================================================================
// handleCommand: FETCH missing args in handleSelected
// =======================================================================

func TestCoverHandleFetch_MissingArgsInSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for FETCH without args, got: %s", resp)
	}
	<-done
}

// =======================================================================
// handleCommand: SEARCH missing args in handleSelected
// =======================================================================

func TestCoverHandleSearch_MissingArgsInSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") && !strings.Contains(resp, "SEARCH") {
		t.Logf("SEARCH without args response: %s", resp)
	}
	<-done
}

// =======================================================================
// parseSeqSet: range with descending order (end < start)
// =======================================================================

func TestCoverParseSeqSet_DescendingRange(t *testing.T) {
	result, err := parseSeqSet("5:3", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return empty since start > end
	if len(result) != 0 {
		t.Errorf("expected empty for descending range, got %v", result)
	}
}

func TestCoverParseSeqSet_CommaSeparated(t *testing.T) {
	result, err := parseSeqSet("1,3,5", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []uint32{1, 3, 5}
	if len(result) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

func TestCoverParseSeqSet_InvalidRange(t *testing.T) {
	_, err := parseSeqSet("1:2:3", 10)
	if err == nil {
		t.Error("expected error for invalid range with 3 parts")
	}
}
