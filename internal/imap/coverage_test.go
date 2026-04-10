package imap

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// ---------- helpers ----------

// selfSignedCert generates a self-signed TLS certificate for testing.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	cert.Leaf, _ = x509.ParseCertificate(certDER)
	return cert
}

// setupSessionWithPipe creates a client/server net.Pipe pair, a Session on the
// server side in the requested state, and returns (clientConn, session).
func setupSessionWithPipe(t *testing.T, state State, user string, selected *Mailbox) (net.Conn, *Session) {
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

// drainConn reads all available data from conn with a deadline.
func drainConn(conn net.Conn, timeout time.Duration) string {
	conn.SetReadDeadline(time.Now().Add(timeout))
	var result string
	buf := make([]byte, 8192)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			result += string(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return result
}

// scanLines reads lines from conn into a channel using a bufio.Scanner.
// Returns a channel that receives scanned lines. Stops when the scanner
// finishes or the connection is closed.
func scanLines(conn net.Conn) <-chan string {
	ch := make(chan string, 20)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			select {
			case ch <- scanner.Text():
			default: // drop if full
			}
		}
	}()
	return ch
}

// waitForLine waits for a line containing substr from the channel, with timeout.
func waitForLine(ch <-chan string, substr string, timeout time.Duration) (string, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return "", false
			}
			if strings.Contains(line, substr) {
				return line, true
			}
		case <-timer.C:
			return "", false
		}
	}
}

// ---------- handleSelected coverage via handleCommand ----------

func TestCoverageHandleSelectedAllCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"CAPABILITY", "A001 CAPABILITY"},
		{"NOOP", "A002 NOOP"},
		{"LOGOUT", "A003 LOGOUT"},
		{"SELECT", "A004 SELECT INBOX"},
		{"EXAMINE", "A005 EXAMINE INBOX"},
		{"CREATE", "A006 CREATE NewFolder"},
		{"DELETE", "A007 DELETE OldFolder"},
		{"RENAME", "A008 RENAME A B"},
		{"SUBSCRIBE", "A009 SUBSCRIBE INBOX"},
		{"UNSUBSCRIBE", "A010 UNSUBSCRIBE INBOX"},
		{"LIST", "A011 LIST \"\" *"},
		{"LSUB", "A012 LSUB \"\" *"},
		{"STATUS", "A013 STATUS INBOX (MESSAGES)"},
		{"NAMESPACE", "A015 NAMESPACE"},
		{"CHECK", "A016 CHECK"},
		{"CLOSE", "A017 CLOSE"},
		{"EXPUNGE", "A018 EXPUNGE"},
		{"SEARCH", "A019 SEARCH ALL"},
		{"FETCH", "A020 FETCH 1:* FLAGS"},
		{"STORE", "A021 STORE 1 +FLAGS (\\Seen)"},
		{"COPY", "A022 COPY 1 Sent"},
		{"MOVE", "A023 MOVE 1 Trash"},
		{"UID_FETCH", "A024 UID FETCH 1:* FLAGS"},
		{"UID_STORE", "A025 UID STORE 1 +FLAGS (\\Seen)"},
		{"UID_COPY", "A026 UID COPY 1 Sent"},
		{"UID_MOVE", "A027 UID MOVE 1 Trash"},
		{"UID_SEARCH", "A028 UID SEARCH ALL"},
		{"UID_EXPUNGE", "A029 UID EXPUNGE 1:*"},
		{"UNKNOWN", "A031 BOGUS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
			defer client.Close()

			cmdDone := make(chan error, 1)
			go func() {
				cmdDone <- session.handleCommand(tt.command)
			}()

			// Drain all responses from the client side.
			drainConn(client, 200*time.Millisecond)

			err := <-cmdDone
			if err != nil {
				t.Logf("handleCommand(%q) returned error: %v (may be expected)", tt.command, err)
			}
		})
	}
}

// ---------- handleStartTLS with real TLS handshake ----------

func TestCoverageHandleStartTLSWithRealTLS(t *testing.T) {
	cert := selfSignedCert(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	clientRaw, srvRaw := net.Pipe()

	server := NewServer(&Config{Addr: ":0", TLSConfig: tlsConfig}, &mockMailstore{})
	session := NewSession(srvRaw, server)
	session.state = StateNotAuthenticated
	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleStartTLS()
	}()

	// Read the "OK Begin TLS negotiation now" on the raw client conn.
	clientRaw.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := clientRaw.Read(buf)
	if err != nil {
		t.Fatalf("reading initial response: %v", err)
	}
	initial := string(buf[:n])
	if !strings.Contains(initial, "Begin TLS negotiation") {
		t.Fatalf("expected 'Begin TLS negotiation', got: %s", initial)
	}

	// Upgrade client side to TLS.
	tlsClient := tls.Client(clientRaw, &tls.Config{InsecureSkipVerify: true})
	defer tlsClient.Close()

	err = tlsClient.Handshake()
	if err != nil {
		t.Fatalf("client TLS handshake: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("handleStartTLS returned error: %v", err)
	}

	if !session.tlsActive {
		t.Error("expected tlsActive to be true after STARTTLS")
	}
}

// ---------- handleStartTLS already active ----------

func TestCoverageHandleStartTLSAlreadyActive(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tlsActive = true
	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleStartTLS()
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("expected BAD for already active TLS, got: %s", resp)
	}
	<-done
}

// ---------- handleStartTLS with TLS config but handshake fails ----------

func TestCoverageHandleStartTLSHandshakeFailure(t *testing.T) {
	cert := selfSignedCert(t)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	clientRaw, srvRaw := net.Pipe()

	server := NewServer(&Config{Addr: ":0", TLSConfig: tlsConfig}, &mockMailstore{})
	session := NewSession(srvRaw, server)
	session.state = StateNotAuthenticated
	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleStartTLS()
	}()

	// Read the "OK Begin TLS negotiation now"
	clientRaw.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _ := clientRaw.Read(buf)
	initial := string(buf[:n])
	if !strings.Contains(initial, "Begin TLS") {
		t.Fatalf("expected 'Begin TLS', got: %s", initial)
	}

	// Close client to force handshake error on server side.
	clientRaw.Close()

	err := <-done
	if err == nil {
		t.Log("handleStartTLS returned nil even with handshake failure")
	}
}

// ---------- handleIdle full flow with DONE ----------

func TestCoverageHandleIdleWithDone(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)

	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout waiting for continuation")
	}

	client.Write([]byte("DONE\r\n"))

	if _, ok := waitForLine(lines, "OK", 2*time.Second); !ok {
		t.Fatal("timeout waiting for OK after DONE")
	}

	if err := <-idleDone; err != nil {
		t.Errorf("handleIdle returned error: %v", err)
	}
}

// ---------- handleIdle with new message notification ----------

func TestCoverageHandleIdleNewMsgNotification(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 5, Recent: 1})
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
	hub.NotifyNewMessage("testuser", "INBOX", 42, 6)

	if line, ok := waitForLine(lines, "EXISTS", 2*time.Second); ok {
		t.Logf("notification: %s", line)
	}

	// Close the notification channel to exit IDLE via the notification path.
	// This ensures idleCleanup can unblock the DONE-reading goroutine via
	// SetReadDeadline instead of hitting the 5-second doneChan timeout.
	GetNotificationHub().Unsubscribe("testuser", session.idleNotifyChan)

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for idle to finish")
	}
}

// ---------- handleIdle with flag change notification ----------

func TestCoverageHandleIdleFlagNotification(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 2, Recent: 0})
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
	hub.NotifyFlagsChanged("testuser", "INBOX", 1, 1, []string{"\\Seen", "\\Flagged"})

	if line, ok := waitForLine(lines, "FETCH", 2*time.Second); ok {
		t.Logf("flags notification: %s", line)
	}

	GetNotificationHub().Unsubscribe("testuser", session.idleNotifyChan)

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---------- handleIdle with expunge notification ----------

func TestCoverageHandleIdleExpungeNotification(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 3, Recent: 1})
	defer client.Close()

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout")
	}

	time.Sleep(50 * time.Millisecond)

	hub := GetNotificationHub()
	hub.NotifyExpunge("testuser", "INBOX", 2)

	if line, ok := waitForLine(lines, "EXPUNGE", 2*time.Second); ok {
		t.Logf("expunge notification: %s", line)
	}

	GetNotificationHub().Unsubscribe("testuser", session.idleNotifyChan)

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---------- handleIdle with mailbox update notification ----------

func TestCoverageHandleIdleMailboxUpdateNotification(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 1, Recent: 0})
	defer client.Close()

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout")
	}

	time.Sleep(50 * time.Millisecond)

	hub := GetNotificationHub()
	hub.NotifyMailboxUpdate("testuser", "INBOX")

	// Drain any notification responses
	waitForLine(lines, "", 2*time.Second)

	GetNotificationHub().Unsubscribe("testuser", session.idleNotifyChan)

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---------- handleIdle notification for wrong mailbox (should be ignored) ----------

func TestCoverageHandleIdleWrongMailboxNotification(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "testuser", &Mailbox{Name: "INBOX", Exists: 1, Recent: 0})
	defer client.Close()

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout")
	}

	time.Sleep(50 * time.Millisecond)

	hub := GetNotificationHub()
	hub.NotifyNewMessage("testuser", "Sent", 10, 1)

	GetNotificationHub().Unsubscribe("testuser", session.idleNotifyChan)

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---------- handleIdle with no selected mailbox ----------

func TestCoverageHandleIdleNoSelectedMailbox(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "testuser", nil)
	defer client.Close()

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout")
	}

	GetNotificationHub().Unsubscribe("testuser", session.idleNotifyChan)

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ---------- handleIdle with connection error ----------

func TestCoverageHandleIdleReadError(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)

	idleDone := make(chan error, 1)
	go func() {
		idleDone <- session.handleIdle()
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout")
	}

	// Close the notification channel first to exit IDLE via the
	// notification path, then close the client to clean up the pipe.
	// This ordering ensures idleCleanup can unblock the DONE-reading
	// goroutine via SetReadDeadline instead of hitting the 5-second timeout.
	GetNotificationHub().Unsubscribe("test", session.idleNotifyChan)
	client.Close()

	select {
	case <-idleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for idle to finish after connection close")
	}
}

// ---------- handleIdle via handleCommand in selected state ----------

func TestCoverageSelectedIdleViaHandleCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 IDLE")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout waiting for continuation")
	}

	client.Write([]byte("DONE\r\n"))

	if _, ok := waitForLine(lines, "OK", 2*time.Second); !ok {
		t.Fatal("timeout waiting for OK")
	}

	if err := <-done; err != nil {
		t.Errorf("handleCommand IDLE returned error: %v", err)
	}
}

// ---------- handleIdle via handleCommand in authenticated state ----------

func TestCoverageAuthenticatedIdleViaHandleCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 IDLE")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "idling", 2*time.Second); !ok {
		t.Fatal("timeout")
	}

	client.Write([]byte("DONE\r\n"))

	if _, ok := waitForLine(lines, "OK", 2*time.Second); !ok {
		t.Log("timeout waiting for OK after DONE")
	}

	<-done
}

// ---------- failingMailstore for error-path coverage ----------

type failingMailstore struct {
	*mockMailstore
	listErr    error
	createErr  error
	deleteErr  error
	renameErr  error
	selectErr  error
	appendErr  error
	expungeErr error
	storeErr   error
	copyErr    error
	moveErr    error
	searchErr  error
	fetchErr   error
}

func (f *failingMailstore) ListMailboxes(user, pattern string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.mockMailstore.ListMailboxes(user, pattern)
}

func (f *failingMailstore) CreateMailbox(user, mailbox string) error {
	if f.createErr != nil {
		return f.createErr
	}
	return f.mockMailstore.CreateMailbox(user, mailbox)
}

func (f *failingMailstore) DeleteMailbox(user, mailbox string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.mockMailstore.DeleteMailbox(user, mailbox)
}

func (f *failingMailstore) RenameMailbox(user, oldName, newName string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	return f.mockMailstore.RenameMailbox(user, oldName, newName)
}

func (f *failingMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	if f.selectErr != nil {
		return nil, f.selectErr
	}
	return f.mockMailstore.SelectMailbox(user, mailbox)
}

func (f *failingMailstore) AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	return f.mockMailstore.AppendMessage(user, mailbox, flags, date, data)
}

func (f *failingMailstore) Expunge(user, mailbox string) error {
	if f.expungeErr != nil {
		return f.expungeErr
	}
	return f.mockMailstore.Expunge(user, mailbox)
}

func (f *failingMailstore) StoreFlags(user, mailbox string, seqSet string, flags []string, op FlagOperation) error {
	if f.storeErr != nil {
		return f.storeErr
	}
	return f.mockMailstore.StoreFlags(user, mailbox, seqSet, flags, op)
}

func (f *failingMailstore) CopyMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	if f.copyErr != nil {
		return f.copyErr
	}
	return f.mockMailstore.CopyMessages(user, sourceMailbox, destMailbox, seqSet)
}

func (f *failingMailstore) MoveMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	return f.mockMailstore.MoveMessages(user, sourceMailbox, destMailbox, seqSet)
}

func (f *failingMailstore) SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.mockMailstore.SearchMessages(user, mailbox, criteria)
}

func (f *failingMailstore) FetchMessages(user, mailbox string, seqSet string, items []string) ([]*Message, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.mockMailstore.FetchMessages(user, mailbox, seqSet, items)
}

func (f *failingMailstore) EnsureDefaultMailboxes(user string) error {
	return nil
}

func setupSessionWithFailingStore(t *testing.T, state State, user string, selected *Mailbox) (net.Conn, *Session) {
	t.Helper()
	client, srv := net.Pipe()
	store := &failingMailstore{mockMailstore: &mockMailstore{}}
	server := NewServer(&Config{Addr: ":0"}, store)
	session := NewSession(srv, server)
	session.state = state
	session.user = user
	session.selected = selected
	session.tlsActive = true
	return client, session
}

// ---------- handleCreate error paths ----------

func TestCoverageHandleCreate_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.createErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CREATE TestFolder")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleCreate_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CREATE TestFolder")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleDelete error paths ----------

func TestCoverageHandleDelete_INBOX(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 DELETE INBOX")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for DELETE INBOX, got: %s", resp)
	}
	<-done
}

func TestCoverageHandleDelete_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.deleteErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 DELETE SomeFolder")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleDelete_Selected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 DELETE INBOX")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleRename error paths ----------

func TestCoverageHandleRename_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.renameErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 RENAME OldName NewName")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleRename_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 RENAME OnlyOneArg")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Logf("RENAME with one arg: %s", resp)
	}
	<-done
}

// ---------- handleList error paths ----------

func TestCoverageHandleList_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.listErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LIST \"\" *")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleSelect/Examine error ----------

func TestCoverageHandleSelect_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.selectErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SELECT INBOX")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleExamine_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.selectErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXAMINE INBOX")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleAppend error ----------

func TestCoverageHandleAppend_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.appendErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX {10}")
	}()

	// Read continuation
	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("0123456789\r\n"))
	}

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleAppend_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleCopy/Move no selected ----------

func TestCoverageHandleCopy_NoSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 COPY 1 Sent")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleMove_NoSelected(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 MOVE 1 Trash")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleCopy/Move errors ----------

func TestCoverageHandleCopy_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.copyErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 COPY 1 Sent")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleMove_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.moveErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 MOVE 1 Trash")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleSearch error ----------

func TestCoverageHandleSearch_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.searchErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH ALL")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleFetch error ----------

func TestCoverageHandleFetch_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.fetchErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH 1:* FLAGS")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleStore error ----------

func TestCoverageHandleStore_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.storeErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 +FLAGS (\\Seen)")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleExpunge error ----------

func TestCoverageHandleExpunge_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.expungeErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXPUNGE")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleClose/Check coverage ----------

func TestCoverageHandleClose(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CLOSE")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleCheck(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CHECK")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleLSUB error ----------

func TestCoverageHandleLSUB_Error(t *testing.T) {
	client, session := setupSessionWithFailingStore(t, StateAuthenticated, "test", nil)
	defer client.Close()

	store := session.server.mailstore.(*failingMailstore)
	store.listErr = fmt.Errorf("db error")

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LSUB \"\" *")
	}()

	drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- matchPattern coverage ----------

func TestMatchPattern_ExactMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		name     string
		expected bool
	}{
		{"INBOX", "INBOX", true},
		{"*", "anything", true},
		{"INBOX", "INBOX/Sub", false},
		{"Sent*", "Sent", true},
		{"Sent*", "SentItems", true},
		{"*BOX", "INBOX", true},
		{"*BOX", "OUTBOX", true},
		{"INBOX", "Sent", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.name, tt.pattern)
		if got != tt.expected {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.name, tt.pattern, got, tt.expected)
		}
	}
}

// ---------- matchesCriteria coverage ----------

func TestMatchesCriteria_FlagChecks(t *testing.T) {
	tests := []struct {
		name     string
		meta     *storage.MessageMetadata
		criteria SearchCriteria
		expected bool
	}{
		{
			name:     "All matches everything",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{All: true},
			expected: true,
		},
		{
			name:     "Answered true - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Answered"}},
			criteria: SearchCriteria{Answered: true},
			expected: true,
		},
		{
			name:     "Answered true - no flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Answered: true},
			expected: false,
		},
		{
			name:     "Deleted true - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Deleted"}},
			criteria: SearchCriteria{Deleted: true},
			expected: true,
		},
		{
			name:     "Flagged true - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Flagged"}},
			criteria: SearchCriteria{Flagged: true},
			expected: true,
		},
		{
			name:     "Seen true - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Seen"}},
			criteria: SearchCriteria{Seen: true},
			expected: true,
		},
		{
			name:     "Unanswered - no \\Answered flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Unanswered: true},
			expected: true,
		},
		{
			name:     "Unanswered - has \\Answered flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Answered"}},
			criteria: SearchCriteria{Unanswered: true},
			expected: false,
		},
		{
			name:     "Undeleted - no \\Deleted flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Undeleted: true},
			expected: true,
		},
		{
			name:     "Undeleted - has \\Deleted flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Deleted"}},
			criteria: SearchCriteria{Undeleted: true},
			expected: false,
		},
		{
			name:     "Unflagged - no \\Flagged flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Unflagged: true},
			expected: true,
		},
		{
			name:     "Unflagged - has \\Flagged flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Flagged"}},
			criteria: SearchCriteria{Unflagged: true},
			expected: false,
		},
		{
			name:     "Unseen - no \\Seen flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Unseen: true},
			expected: true,
		},
		{
			name:     "Unseen - has \\Seen flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Seen"}},
			criteria: SearchCriteria{Unseen: true},
			expected: false,
		},
		{
			name:     "New - has \\Recent flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Recent"}},
			criteria: SearchCriteria{New: true},
			expected: true,
		},
		{
			name:     "New - no \\Recent flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{New: true},
			expected: false,
		},
		{
			name:     "Old - no \\Recent flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Old: true},
			expected: true,
		},
		{
			name:     "Old - has \\Recent flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Recent"}},
			criteria: SearchCriteria{Old: true},
			expected: false,
		},
		{
			name:     "Recent - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Recent"}},
			criteria: SearchCriteria{Recent: true},
			expected: true,
		},
		{
			name:     "Draft - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Draft"}},
			criteria: SearchCriteria{Draft: true},
			expected: true,
		},
		{
			name:     "Undraft - no flag",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Undraft: true},
			expected: true,
		},
		{
			name:     "Undraft - has flag",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Draft"}},
			criteria: SearchCriteria{Undraft: true},
			expected: false,
		},
		{
			name:     "From match",
			meta:     &storage.MessageMetadata{From: "alice@example.com"},
			criteria: SearchCriteria{From: "alice"},
			expected: true,
		},
		{
			name:     "From no match",
			meta:     &storage.MessageMetadata{From: "bob@example.com"},
			criteria: SearchCriteria{From: "alice"},
			expected: false,
		},
		{
			name:     "To match",
			meta:     &storage.MessageMetadata{To: "bob@example.com"},
			criteria: SearchCriteria{To: "bob"},
			expected: true,
		},
		{
			name:     "Subject match",
			meta:     &storage.MessageMetadata{Subject: "Hello World"},
			criteria: SearchCriteria{Subject: "hello"},
			expected: true,
		},
		{
			name:     "Larger - size is larger",
			meta:     &storage.MessageMetadata{Size: 1000},
			criteria: SearchCriteria{Larger: 500},
			expected: true,
		},
		{
			name:     "Larger - size not larger",
			meta:     &storage.MessageMetadata{Size: 100},
			criteria: SearchCriteria{Larger: 500},
			expected: false,
		},
		{
			name:     "Smaller - size is smaller",
			meta:     &storage.MessageMetadata{Size: 100},
			criteria: SearchCriteria{Smaller: 500},
			expected: true,
		},
		{
			name:     "Smaller - size not smaller",
			meta:     &storage.MessageMetadata{Size: 1000},
			criteria: SearchCriteria{Smaller: 500},
			expected: false,
		},
		{
			name:     "NOT criteria - inner matches so NOT fails",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Seen"}},
			criteria: SearchCriteria{Not: &SearchCriteria{Seen: true}},
			expected: false,
		},
		{
			name:     "NOT criteria - inner does not match so NOT passes",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Not: &SearchCriteria{Seen: true}},
			expected: true,
		},
		{
			name:     "OR criteria - both match",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Seen", "\\Flagged"}},
			criteria: SearchCriteria{Or: [2]*SearchCriteria{{Seen: true}, {Flagged: true}}},
			expected: true,
		},
		{
			name:     "OR criteria - first matches",
			meta:     &storage.MessageMetadata{Flags: []string{"\\Seen"}},
			criteria: SearchCriteria{Or: [2]*SearchCriteria{{Seen: true}, {Flagged: true}}},
			expected: true,
		},
		{
			name:     "OR criteria - neither matches",
			meta:     &storage.MessageMetadata{Flags: []string{}},
			criteria: SearchCriteria{Or: [2]*SearchCriteria{{Seen: true}, {Flagged: true}}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesCriteria(tt.meta, nil, &tt.criteria)
			if got != tt.expected {
				t.Errorf("matchesCriteria() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------- parseSeqSet extra coverage ----------

func TestParseSeqSetExtra(t *testing.T) {
	tests := []struct {
		input   string
		maxSeq  uint32
		want    []uint32
		wantErr bool
	}{
		{"1", 5, []uint32{1}, false},
		{"1:3", 5, []uint32{1, 2, 3}, false},
		{"1,3,5", 5, []uint32{1, 3, 5}, false},
		{"*", 5, []uint32{5}, false},
		{"1:*", 5, []uint32{1, 2, 3, 4, 5}, false},
		{"*:1", 5, []uint32{}, false},
		{"", 5, nil, true},
		{"2:4", 10, []uint32{2, 3, 4}, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSeqSet(tt.input, tt.maxSeq)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSeqSet(%q, %d) error = %v, wantErr %v", tt.input, tt.maxSeq, err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("parseSeqSet(%q, %d) = %v, want %v", tt.input, tt.maxSeq, got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("parseSeqSet(%q, %d)[%d] = %d, want %d", tt.input, tt.maxSeq, i, v, tt.want[i])
				}
			}
		})
	}
}

// ---------- hasFlag extra coverage ----------

func TestHasFlagExtra(t *testing.T) {
	flags := []string{"\\Seen", "\\Flagged", "\\Recent"}

	if !hasFlag(flags, "\\Seen") {
		t.Error("Expected \\Seen to be found")
	}
	if !hasFlag(flags, "\\seen") {
		t.Error("Expected case-insensitive match for \\seen")
	}
	if hasFlag(flags, "\\Deleted") {
		t.Error("Expected \\Deleted to not be found")
	}
	if hasFlag([]string{}, "\\Seen") {
		t.Error("Expected empty flags to not contain \\Seen")
	}
}

// ---------- parseMessageHeaders extra coverage ----------

func TestParseMessageHeadersExtra(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		wantSubject string
		wantFrom    string
		wantTo      string
		wantDate    string
	}{
		{
			name:        "full headers",
			data:        []byte("Subject: Test\r\nFrom: a@b.com\r\nTo: c@d.com\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\n\r\nBody"),
			wantSubject: "Test",
			wantFrom:    "a@b.com",
			wantTo:      "c@d.com",
			wantDate:    "Mon, 1 Jan 2024 00:00:00 +0000",
		},
		{
			name:        "LF line endings",
			data:        []byte("Subject: Hello\nFrom: x@y.com\n\nBody"),
			wantSubject: "Hello",
			wantFrom:    "x@y.com",
		},
		{
			name:        "empty data",
			data:        []byte(""),
			wantSubject: "",
		},
		{
			name:        "no blank line separator",
			data:        []byte("Subject: NoBody"),
			wantSubject: "NoBody",
		},
		{
			name:        "mixed headers",
			data:        []byte("From: me@test.com\r\nTo: you@test.com\r\nSubject: Mixed\r\nDate: today\r\n\r\n"),
			wantSubject: "Mixed",
			wantFrom:    "me@test.com",
			wantTo:      "you@test.com",
			wantDate:    "today",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, from, to, date := parseMessageHeaders(tt.data)
			if subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tt.wantSubject)
			}
			if from != tt.wantFrom {
				t.Errorf("from = %q, want %q", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("to = %q, want %q", to, tt.wantTo)
			}
			if date != tt.wantDate {
				t.Errorf("date = %q, want %q", date, tt.wantDate)
			}
		})
	}
}

func TestAddressToString(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*Address
		want  string
	}{
		{
			name:  "nil slice",
			addrs: nil,
			want:  "",
		},
		{
			name:  "empty slice",
			addrs: []*Address{},
			want:  "",
		},
		{
			name: "single address",
			addrs: []*Address{
				{MailboxName: "john", HostName: "example.com"},
			},
			want: "john@example.com",
		},
		{
			name: "multiple addresses - only first used",
			addrs: []*Address{
				{MailboxName: "first", HostName: "example.com"},
				{MailboxName: "second", HostName: "other.com"},
			},
			want: "first@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addressToString(tt.addrs)
			if got != tt.want {
				t.Errorf("addressToString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Mailstore tests
// ---------------------------------------------------------------------------

func TestNewBboltMailstoreWithInterfaces(t *testing.T) {
	store := NewBboltMailstoreWithInterfaces(nil, nil)
	if store == nil {
		t.Fatal("Expected non-nil mailstore")
	}
	if store.db != nil {
		t.Error("Expected nil db")
	}
	if store.msgStore != nil {
		t.Error("Expected nil msgStore")
	}
	if store.mdnSent == nil {
		t.Error("Expected non-nil mdnSent map")
	}
}

func TestBboltMailstoreSetMDNHandler(t *testing.T) {
	store := NewBboltMailstoreWithInterfaces(nil, nil)

	called := false
	handler := func(from, to, messageID, inReplyTo string, msg []byte) error {
		called = true
		return nil
	}

	store.SetMDNHandler(handler)
	if store.mdnHandler == nil {
		t.Error("Expected non-nil mdnHandler")
	}

	// Call the handler to verify it works
	err := store.mdnHandler("from@test.com", "to@test.com", "msg123", "", nil)
	if err != nil {
		t.Errorf("mdnHandler returned error: %v", err)
	}
	if !called {
		t.Error("Expected handler to be called")
	}
}

func TestParseDispositionHeader(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "valid header lowercase",
			msg:  "From: sender@example.com\r\ndisposition-notification-to: <test@example.com>\r\n\r\nBody",
			want: "<test@example.com>",
		},
		{
			name: "missing header",
			msg:  "From: sender@example.com\r\n\r\nBody",
			want: "",
		},
		{
			name: "valid header with extra whitespace",
			msg:  "From: sender@example.com\r\ndisposition-notification-to:    <test@example.com>   \r\n\r\nBody",
			want: "<test@example.com>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDispositionHeader(tt.msg)
			if got != tt.want {
				t.Errorf("parseDispositionHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFlagOperationString(t *testing.T) {
	tests := []struct {
		op   FlagOperation
		want string
	}{
		{FlagAdd, "add"},
		{FlagRemove, "remove"},
		{FlagReplace, "replace"},
		{FlagOperation(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.op.String()
			if got != tt.want {
				t.Errorf("FlagOperation(%d).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}

func TestParseSortCriteria(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []SortCriterion
		wantErr bool
	}{
		{
			name: "single criterion",
			args: []string{"DATE"},
			want: []SortCriterion{{Field: "DATE", Descending: true}},
		},
		{
			name: "with reverse",
			args: []string{"REVERSE", "SUBJECT"},
			want: []SortCriterion{{Field: "SUBJECT", Descending: false}},
		},
		{
			name: "multiple criteria",
			args: []string{"ARRIVAL", "SUBJECT", "SIZE"},
			want: []SortCriterion{
				{Field: "ARRIVAL", Descending: true},
				{Field: "SUBJECT", Descending: true},
				{Field: "SIZE", Descending: true},
			},
		},
		{
			name: "case insensitive",
			args: []string{"date", "reverse", "from"},
			want: []SortCriterion{
				{Field: "DATE", Descending: true},
				{Field: "FROM", Descending: false},
			},
		},
		{
			name:    "unsupported SCORE",
			args:    []string{"SCORE"},
			wantErr: true,
		},
		{
			name:    "unknown criterion",
			args:    []string{"UNKNOWN"},
			wantErr: true,
		},
		{
			name:    "empty",
			args:    []string{},
			wantErr: true,
		},
		{
			name: "CC criterion",
			args: []string{"CC"},
			want: []SortCriterion{{Field: "CC", Descending: true}},
		},
		{
			name: "TO criterion",
			args: []string{"TO"},
			want: []SortCriterion{{Field: "TO", Descending: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSortCriteria(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSortCriteria() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != len(tt.want) {
				t.Errorf("parseSortCriteria() returned %d criteria, want %d", len(got), len(tt.want))
				return
			}
			for i, c := range got {
				if c.Field != tt.want[i].Field || c.Descending != tt.want[i].Descending {
					t.Errorf("parseSortCriteria()[%d] = %+v, want %+v", i, c, tt.want[i])
				}
			}
		})
	}
}

func TestSortMessagesByCriteria(t *testing.T) {
	now := time.Now()
	messages := []*storage.MessageMetadata{
		{UID: 1, Subject: "Alpha", Size: 100, Date: now.Format(time.RFC1123Z), InternalDate: now},
		{UID: 2, Subject: "Beta", Size: 200, Date: now.Add(-time.Hour).Format(time.RFC1123Z), InternalDate: now.Add(-time.Hour)},
		{UID: 3, Subject: "Alpha", Size: 150, Date: now.Add(-2 * time.Hour).Format(time.RFC1123Z), InternalDate: now.Add(-2 * time.Hour)},
	}
	seqNums := []uint32{1, 2, 3}

	// Test SORT BY SUBJECT
	criteria := []SortCriterion{{Field: "SUBJECT", Descending: true}}
	result := sortMessagesByCriteria(messages, criteria, seqNums)
	if len(result) != 3 {
		t.Errorf("Expected 3 results, got %d", len(result))
	}

	// Test SORT BY SIZE
	criteria = []SortCriterion{{Field: "SIZE", Descending: false}}
	result = sortMessagesByCriteria(messages, criteria, seqNums)
	if len(result) != 3 {
		t.Errorf("Expected 3 results, got %d", len(result))
	}

	// Test empty messages
	result = sortMessagesByCriteria([]*storage.MessageMetadata{}, []SortCriterion{{Field: "SUBJECT", Descending: true}}, seqNums)
	if result != nil {
		t.Errorf("Expected nil for empty messages, got %v", result)
	}
}

func TestThreadMessagesByReferences(t *testing.T) {
	now := time.Now()
	messages := []*storage.MessageMetadata{
		{UID: 1, MessageID: "<msg1@test.com>", Subject: "Root", InternalDate: now},
		{UID: 2, MessageID: "<msg2@test.com>", InReplyTo: "<msg1@test.com>", Subject: "Reply1", InternalDate: now},
		{UID: 3, MessageID: "<msg3@test.com>", InReplyTo: "<msg1@test.com>", Subject: "Reply2", InternalDate: now},
		{UID: 4, MessageID: "<msg4@test.com>", Subject: "Orphan", InternalDate: now},
	}
	seqNums := []uint32{1, 2, 3, 4}

	result := threadMessagesByReferences(messages, seqNums)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// msg1 should have msg2 and msg3 as children
	if len(result[1]) != 2 {
		t.Errorf("Expected 2 children for msg1, got %d", len(result[1]))
	}
}

func TestThreadMessagesByOrderedSubject(t *testing.T) {
	now := time.Now()
	messages := []*storage.MessageMetadata{
		{UID: 1, Subject: "Alpha", InternalDate: now},
		{UID: 2, Subject: "Beta", InternalDate: now.Add(-time.Hour)},
		{UID: 3, Subject: "Alpha", InternalDate: now.Add(-2 * time.Hour)},
	}
	seqNums := []uint32{1, 2, 3}

	result := threadMessagesByOrderedSubject(messages, seqNums)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

func TestFlattenThread(t *testing.T) {
	children := map[uint32][]uint32{
		1: {2, 3},
		2: {4, 5},
	}
	visited := make(map[uint32]bool)

	result := flattenThread(1, children, visited)
	if len(result) != 5 {
		t.Errorf("Expected 5 messages in thread, got %d", len(result))
	}
}

func TestServerTimeouts(t *testing.T) {
	config := &Config{Logger: slog.Default()}
	srv := NewServer(config, &mockMailstore{})

	// Test SetReadTimeout
	srv.SetReadTimeout(30 * time.Second)
	if srv.readTimeout != 30*time.Second {
		t.Errorf("Expected readTimeout 30s, got %v", srv.readTimeout)
	}

	// Test SetWriteTimeout
	srv.SetWriteTimeout(45 * time.Second)
	if srv.writeTimeout != 45*time.Second {
		t.Errorf("Expected writeTimeout 45s, got %v", srv.writeTimeout)
	}

	// Test SetIdleTimeout
	srv.SetIdleTimeout(5 * time.Minute)
	if srv.idleTimeout != 5*time.Minute {
		t.Errorf("Expected idleTimeout 5m, got %v", srv.idleTimeout)
	}

	// Test SetMaxConnections
	srv.SetMaxConnections(100)
	if srv.maxConnections != 100 {
		t.Errorf("Expected maxConnections 100, got %d", srv.maxConnections)
	}
}

func TestServerSetOnExpunge(t *testing.T) {
	config := &Config{Logger: slog.Default()}
	srv := NewServer(config, &mockMailstore{})

	called := false
	srv.SetOnExpunge(func(user, mailbox string, uid uint32) {
		called = true
	})

	if srv.onExpunge == nil {
		t.Error("Expected onExpunge to be set")
	}

	// Call the callback to verify it works
	srv.onExpunge("testuser", "INBOX", 123)
	if !called {
		t.Error("Expected onExpunge callback to be called")
	}
}
