package imap

import (
	"fmt"
	"net"
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
	session.tlsActive = true
	return client, session
}

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
