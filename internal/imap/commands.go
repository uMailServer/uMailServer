package imap

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// handleCommand parses and handles an IMAP command
func (s *Session) handleCommand(line string) error {
	// Parse the command line
	// Format: TAG COMMAND [arguments...]
	parts := strings.Fields(line)
	if len(parts) < 2 {
		s.WriteResponse("BAD", "Command expected")
		return nil
	}

	s.tag = parts[0]
	command := strings.ToUpper(parts[1])
	args := parts[2:]

	// Handle the command based on current state.
	// Use the State() accessor (RLock) rather than s.state directly: Close()
	// runs on a different goroutine (Server.Stop), and a bare read here is
	// what -race flagged against it in TestFullMailFlow.
	switch s.State() {
	case StateNotAuthenticated:
		return s.handleNotAuthenticated(command, args, line)
	case StateAuthenticated:
		return s.handleAuthenticated(command, args, line)
	case StateSelected:
		return s.handleSelected(command, args, line)
	case StateLoggedOut:
		return nil
	}

	return nil
}

// handleNotAuthenticated handles commands in the Not Authenticated state
func (s *Session) handleNotAuthenticated(command string, args []string, line string) error {
	switch command {
	case "CAPABILITY":
		return s.handleCapability()
	case "STARTTLS":
		return s.handleStartTLS()
	case "AUTHENTICATE":
		return s.handleAuthenticate(args)
	case "LOGIN":
		return s.handleLogin(args)
	case "NOOP":
		return s.handleNoop()
	case "LOGOUT":
		return s.handleLogout()
	case "COMPRESS":
		return s.handleCompress(args)
	default:
		s.WriteResponse(s.tag, "BAD Command not allowed in this state")
		return nil
	}
}

// handleAuthenticated handles commands in the Authenticated state
func (s *Session) handleAuthenticated(command string, args []string, line string) error {
	switch command {
	case "CAPABILITY":
		return s.handleCapability()
	case "NOOP":
		return s.handleNoop()
	case "LOGOUT":
		return s.handleLogout()
	case "COMPRESS":
		return s.handleCompress(args)
	case "SELECT":
		return s.handleSelect(args)
	case "EXAMINE":
		return s.handleExamine(args)
	case "CREATE":
		return s.handleCreate(args)
	case "DELETE":
		return s.handleDelete(args)
	case "RENAME":
		return s.handleRename(args)
	case "SUBSCRIBE":
		return s.handleSubscribe(args)
	case "UNSUBSCRIBE":
		return s.handleUnsubscribe(args)
	case "LIST":
		return s.handleList(args)
	case "LSUB":
		return s.handleLsub(args)
	case "STATUS":
		return s.handleStatus(args)
	case "APPEND":
		return s.handleAppend(args, line)
	case "NAMESPACE":
		return s.handleNamespace()
	case "IDLE":
		return s.handleIdle()
	case "ENABLE":
		return s.handleEnable(args)
	case "ID":
		return s.handleID(args)
	case "GETACL":
		return s.handleGetACL(args)
	case "SETACL":
		return s.handleSetACL(args)
	case "DELETEACL":
		return s.handleDeleteACL(args)
	case "MYRIGHTS":
		return s.handleMyRights(args)
	case "LISTRIGHTS":
		return s.handleListRights(args)
	default:
		s.WriteResponse(s.tag, "BAD Command not recognized")
		return nil
	}
}

// handleSelected handles commands in the Selected state
func (s *Session) handleSelected(command string, args []string, line string) error {
	switch command {
	case "CAPABILITY":
		return s.handleCapability()
	case "NOOP":
		return s.handleNoop()
	case "LOGOUT":
		return s.handleLogout()
	case "COMPRESS":
		return s.handleCompress(args)
	case "SELECT":
		return s.handleSelect(args)
	case "EXAMINE":
		return s.handleExamine(args)
	case "CREATE":
		return s.handleCreate(args)
	case "DELETE":
		return s.handleDelete(args)
	case "RENAME":
		return s.handleRename(args)
	case "SUBSCRIBE":
		return s.handleSubscribe(args)
	case "UNSUBSCRIBE":
		return s.handleUnsubscribe(args)
	case "LIST":
		return s.handleList(args)
	case "LSUB":
		return s.handleLsub(args)
	case "STATUS":
		return s.handleStatus(args)
	case "APPEND":
		return s.handleAppend(args, line)
	case "NAMESPACE":
		return s.handleNamespace()
	case "CHECK":
		return s.handleCheck()
	case "CLOSE":
		return s.handleClose()
	case "EXPUNGE":
		return s.handleExpunge()
	case "SEARCH":
		return s.handleSearch(args, line)
	case "SORT":
		return s.handleSort(args, line)
	case "THREAD":
		return s.handleThread(args, line)
	case "FETCH":
		return s.handleFetch(args, line)
	case "STORE":
		return s.handleStore(args)
	case "COPY":
		return s.handleCopy(args)
	case "MOVE":
		return s.handleMove(args)
	case "UID":
		return s.handleUID(args, line)
	case "IDLE":
		return s.handleIdle()
	case "ID":
		return s.handleID(args)
	case "GETACL":
		return s.handleGetACL(args)
	case "SETACL":
		return s.handleSetACL(args)
	case "DELETEACL":
		return s.handleDeleteACL(args)
	case "MYRIGHTS":
		return s.handleMyRights(args)
	case "LISTRIGHTS":
		return s.handleListRights(args)
	default:
		s.WriteResponse(s.tag, "BAD Command not recognized")
		return nil
	}
}

// CAPABILITY command
func (s *Session) handleCapability() error {
	caps := "CAPABILITY"
	for _, cap := range s.sessionCapabilities() {
		caps += " " + cap
	}
	s.WriteData(caps)
	s.WriteResponse(s.tag, "OK CAPABILITY completed")
	return nil
}

// NOOP command
func (s *Session) handleNoop() error {
	s.WriteResponse(s.tag, "OK NOOP completed")
	return nil
}

// LOGOUT command
func (s *Session) handleLogout() error {
	s.WriteData("BYE IMAP4rev1 Server logging out")
	s.WriteResponse(s.tag, "OK LOGOUT completed")
	s.stateMu.Lock()
	s.state = StateLoggedOut
	s.stateMu.Unlock()
	s.Close()
	return nil
}

// STARTTLS command
func (s *Session) handleStartTLS() error {
	if s.tlsActive {
		s.WriteResponse(s.tag, "BAD TLS already active")
		return nil
	}

	if s.server.tlsConfig == nil {
		s.WriteResponse(s.tag, "NO TLS not available")
		return nil
	}

	s.WriteResponse(s.tag, "OK Begin TLS negotiation now")

	// Upgrade to TLS with a bounded handshake timeout
	_ = s.conn.SetDeadline(time.Now().Add(30 * time.Second)) // Best-effort deadline
	tlsConn := tls.Server(s.conn, s.server.tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		_ = s.conn.SetDeadline(time.Time{}) // Best-effort deadline reset
		return fmt.Errorf("TLS handshake failed: %w", err)
	}
	_ = s.conn.SetDeadline(time.Time{}) // Best-effort deadline reset

	s.tlsConn = tlsConn
	s.conn = tlsConn
	s.reader.Reset(tlsConn)
	s.writer.Reset(tlsConn)
	s.tlsActive = true

	return nil
}

// handleCompress enables compression using DEFLATE algorithm (RFC 4978)
func (s *Session) handleCompress(args []string) error {
	if s.compressActive {
		s.WriteResponse(s.tag, "BAD Compression already active")
		return nil
	}

	if len(args) < 1 || strings.ToUpper(args[0]) != "DEFLATE" {
		s.WriteResponse(s.tag, "BAD COMPRESS requires DEFLATE argument")
		return nil
	}

	s.WriteResponse(s.tag, "OK Compression active")

	// Create gzip writer for compressing responses to client
	s.compressWriter = gzip.NewWriter(s.conn)
	s.writer.Reset(s.compressWriter)

	// Create gzip reader for decompressing requests from client
	gzReader, err := gzip.NewReader(s.conn)
	if err != nil {
		s.WriteResponse(s.tag, "BAD Compression initialization failed")
		return nil
	}
	s.compressReader = gzReader
	s.reader.Reset(s.compressReader)

	s.compressActive = true

	return nil
}

// AUTHENTICATE command
func (s *Session) handleAuthenticate(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing authentication mechanism")
		return nil
	}

	if !s.tlsActive && !s.server.allowPlainAuth {
		s.WriteResponse(s.tag, "NO TLS required for authentication")
		return nil
	}

	mechanism := strings.ToUpper(args[0])

	switch mechanism {
	case "PLAIN":
		return s.handleAuthPlain(args[1:])
	case "LOGIN":
		return s.handleAuthLogin()
	default:
		s.WriteResponse(s.tag, "NO Unsupported authentication mechanism")
		return nil
	}
}

// handleAuthPlain handles PLAIN authentication (RFC 4616 with SASL-IR)
func (s *Session) handleAuthPlain(args []string) error {
	var credentials []byte
	var err error

	if len(args) >= 1 && args[0] != "" {
		// SASL-IR: initial response provided with the AUTHENTICATE command
		credentials, err = base64.StdEncoding.DecodeString(args[0])
		if err != nil {
			s.WriteResponse(s.tag, "NO Invalid base64 in AUTHENTICATE PLAIN")
			return nil
		}
	} else {
		// No initial response; send continuation request
		s.WriteContinuation("")
		line, err := s.readLine()
		if err != nil {
			return fmt.Errorf("failed to read PLAIN credentials: %w", err)
		}
		// Client may send "*" to cancel
		if line == "*" {
			s.WriteResponse(s.tag, "NO AUTHENTICATE cancelled")
			return nil
		}
		credentials, err = base64.StdEncoding.DecodeString(line)
		if err != nil {
			s.WriteResponse(s.tag, "NO Invalid base64 in AUTHENTICATE PLAIN")
			return nil
		}
	}

	// PLAIN format: authzid\0authcid\0passwd
	// We ignore authzid and use authcid as the username.
	parts := strings.SplitN(string(credentials), "\x00", 3)
	if len(parts) < 3 {
		s.WriteResponse(s.tag, "NO Invalid PLAIN credentials")
		return nil
	}

	username := parts[1]
	password := parts[2]

	return s.authenticateUser(username, password, "AUTHENTICATE completed", "AUTHENTICATE failed")
}

// handleAuthLogin handles LOGIN authentication (multi-step SASL)
func (s *Session) handleAuthLogin() error {
	// Step 1: Send Username challenge (base64 of "Username:")
	s.WriteContinuation("VXNlcm5hbWU6")

	// Read username response
	line, err := s.readLine()
	if err != nil {
		return fmt.Errorf("failed to read LOGIN username: %w", err)
	}
	if line == "*" {
		s.WriteResponse(s.tag, "NO AUTHENTICATE cancelled")
		return nil
	}
	usernameBytes, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		s.WriteResponse(s.tag, "NO Invalid base64 username in AUTHENTICATE LOGIN")
		return nil
	}
	username := string(usernameBytes)

	// Step 2: Send Password challenge (base64 of "Password:")
	s.WriteContinuation("UGFzc3dvcmQ6")

	// Read password response
	line, err = s.readLine()
	if err != nil {
		return fmt.Errorf("failed to read LOGIN password: %w", err)
	}
	if line == "*" {
		s.WriteResponse(s.tag, "NO AUTHENTICATE cancelled")
		return nil
	}
	passwordBytes, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		s.WriteResponse(s.tag, "NO Invalid base64 password in AUTHENTICATE LOGIN")
		return nil
	}
	password := string(passwordBytes)

	return s.authenticateUser(username, password, "AUTHENTICATE completed", "AUTHENTICATE failed")
}

// authenticateUser is the shared authentication logic used by LOGIN,
// AUTHENTICATE PLAIN, and AUTHENTICATE LOGIN.
// okMsg is the human-readable text sent on success (e.g. "LOGIN completed").
// failMsg is sent on authentication failure (e.g. "AUTHENTICATE failed").
func clientIP(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func (s *Session) authenticateUser(username, password, okMsg, failMsg string) error {
	ctx := context.Background()

	// Create tracing span if we have a tracing provider
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.authenticate", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", username),
			attribute.String("ip", clientIP(s.conn)),
		)
		defer span.End()
	}

	ip := clientIP(s.conn)
	if s.server.isAuthLockedOut(ip) {
		s.WriteResponse(s.tag, "NO Too many failed authentication attempts")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "auth locked out")
		}
		if s.server.onLoginResult != nil {
			s.server.onLoginResult(username, false, ip, "lockout")
		}
		return nil
	}

	authenticated := false

	// RFC 7616 PRECIS: normalize username and password before authentication
	// Use UsernameCaseMapped profile (lowercase, Unicode normalization)
	usernameNormalized, err := normalizeUsername(username)
	if err != nil {
		// Invalid username characters per PRECIS
		s.WriteResponse(s.tag, "NO Invalid username characters")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "invalid username")
		}
		return nil
	}
	passwordNormalized := normalizePassword(password)

	if s.server.authFunc != nil {
		auth, err := s.server.authFunc(usernameNormalized, passwordNormalized)
		if err == nil && auth {
			authenticated = true
		}
	} else if s.server.mailstore != nil {
		auth, err := s.server.mailstore.Authenticate(usernameNormalized, passwordNormalized)
		if err == nil && auth {
			authenticated = true
		}
	}

	if !authenticated {
		s.server.recordAuthFailure(ip)
		s.WriteResponse(s.tag, "NO "+failMsg)
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "authentication failed")
			tracing.SetBoolAttribute(span, "auth.success", false)
		}
		if s.server.onLoginResult != nil {
			s.server.onLoginResult(usernameNormalized, false, ip, "invalid_credentials")
		}
		return nil
	}

	s.server.clearAuthFailures(ip)
	s.user = usernameNormalized
	s.stateMu.Lock()
	s.state = StateAuthenticated
	s.stateMu.Unlock()
	if s.server.onLoginResult != nil {
		s.server.onLoginResult(usernameNormalized, true, ip, "")
	}

	// Auto-create default mailboxes after first successful authentication
	if s.server.mailstore != nil {
		// Best-effort: create INBOX if the mailstore supports it
		_ = s.server.mailstore.CreateMailbox(s.user, "INBOX")
	}

	if span != nil {
		tracing.SetBoolAttribute(span, "auth.success", true)
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK "+okMsg)
	return nil
}

// LOGIN command
func (s *Session) handleLogin(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing username or password")
		return nil
	}

	if !s.tlsActive && !s.server.allowPlainAuth {
		s.WriteResponse(s.tag, "NO LOGIN requires TLS - use STARTTLS first")
		return nil
	}

	username := args[0]
	password := args[1]

	// Remove quotes if present
	username = strings.Trim(username, "\"'")
	password = strings.Trim(password, "\"'")

	return s.authenticateUser(username, password, "LOGIN completed", "Authentication failed")
}

// SELECT command
func (s *Session) handleSelect(args []string) error {
	ctx := context.Background()

	// Create tracing span
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.select", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", s.user),
		)
		defer span.End()
	}

	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing mailbox name")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "missing mailbox name")
		}
		return nil
	}

	mailboxName := args[0]
	mailboxName = strings.Trim(mailboxName, "\"'")

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "mailstore not available")
		}
		return nil
	}

	if span != nil {
		tracing.SetStringAttribute(span, "mailbox.name", mailboxName)
	}

	mailbox, err := s.server.mailstore.SelectMailbox(s.user, mailboxName)
	if err != nil {
		s.deselect()
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "select mailbox failed")
		}
		return nil
	}

	s.selected = mailbox
	s.stateMu.Lock()
	s.state = StateSelected
	s.stateMu.Unlock()

	// Send mailbox data
	s.WriteData(fmt.Sprintf("%d EXISTS", mailbox.Exists))
	s.WriteData(fmt.Sprintf("%d RECENT", mailbox.Recent))

	if mailbox.Unseen > 0 {
		s.WriteData(fmt.Sprintf("OK [UNSEEN %d] Message %d is first unseen", mailbox.Unseen, mailbox.Unseen))
	}

	s.WriteData(fmt.Sprintf("OK [UIDVALIDITY %d] UIDs valid", mailbox.UIDValidity))
	s.WriteData(fmt.Sprintf("OK [UIDNEXT %d] Predicted next UID", mailbox.UIDNext))

	// RFC 7162: HIGHESTMODSEQ when CONDSTORE/QRESYNC is enabled
	if s.enabledCaps["CONDSTORE"] || s.enabledCaps["QRESYNC"] {
		s.WriteData(fmt.Sprintf("OK [HIGHESTMODSEQ %d] Highest modification sequence", mailbox.HighestModSeq))
	}

	// PERMANENTFLAGS
	s.WriteData("FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)")
	s.WriteData("OK [PERMANENTFLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft \\*)] Flags permitted")

	if span != nil {
		tracing.SetIntAttribute(span, "mailbox.exists", mailbox.Exists)
		tracing.SetIntAttribute(span, "mailbox.recent", mailbox.Recent)
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK [READ-WRITE] SELECT completed")
	return nil
}

// EXAMINE command
func (s *Session) handleExamine(args []string) error {
	// Similar to SELECT but read-only
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing mailbox name")
		return nil
	}

	mailboxName := args[0]
	mailboxName = strings.Trim(mailboxName, "\"'")

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	mailbox, err := s.examineMailbox(mailboxName)
	if err != nil {
		s.deselect()
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	// RFC 3501 §6.3.2: EXAMINE selects the mailbox read-only (F5068).
	mailbox.ReadOnly = true
	s.selected = mailbox
	s.stateMu.Lock()
	s.state = StateSelected
	s.stateMu.Unlock()

	// Send mailbox data (same as SELECT but read-only)
	s.WriteData(fmt.Sprintf("%d EXISTS", mailbox.Exists))
	s.WriteData(fmt.Sprintf("%d RECENT", mailbox.Recent))

	if mailbox.Unseen > 0 {
		s.WriteData(fmt.Sprintf("OK [UNSEEN %d] Message %d is first unseen", mailbox.Unseen, mailbox.Unseen))
	}

	s.WriteData(fmt.Sprintf("OK [UIDVALIDITY %d] UIDs valid", mailbox.UIDValidity))
	s.WriteData(fmt.Sprintf("OK [UIDNEXT %d] Predicted next UID", mailbox.UIDNext))

	// RFC 7162: HIGHESTMODSEQ when CONDSTORE/QRESYNC is enabled
	if s.enabledCaps["CONDSTORE"] || s.enabledCaps["QRESYNC"] {
		s.WriteData(fmt.Sprintf("OK [HIGHESTMODSEQ %d] Highest modification sequence", mailbox.HighestModSeq))
	}

	s.WriteData("FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)")
	s.WriteData("OK [PERMANENTFLAGS ()] No permanent flags permitted")

	s.WriteResponse(s.tag, "OK [READ-ONLY] EXAMINE completed")
	return nil
}

// deselect leaves the selected state after a failed SELECT / EXAMINE: RFC 3501
// §6.3.1 says no mailbox is selected then (F5643).
func (s *Session) deselect() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.selected = nil
	if s.state == StateSelected {
		s.state = StateAuthenticated
	}
}

// mailboxExaminer is implemented by mailstores that can report a mailbox's
// state without the side effects of SELECT (clearing \Recent).
type mailboxExaminer interface {
	ExamineMailbox(user, mailbox string) (*Mailbox, error)
}

// examineMailbox reads mailbox state for EXAMINE and STATUS, which must not
// change it (RFC 3501 §6.3.2, §6.3.10) (F5215). Mailstores without a
// side-effect-free primitive fall back to SelectMailbox.
func (s *Session) examineMailbox(name string) (*Mailbox, error) {
	if ex, ok := s.server.mailstore.(mailboxExaminer); ok {
		return ex.ExamineMailbox(s.user, name)
	}
	return s.server.mailstore.SelectMailbox(s.user, name)
}

// CREATE command
func (s *Session) handleCreate(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing mailbox name")
		return nil
	}

	mailboxName := args[0]
	mailboxName = strings.Trim(mailboxName, "\"'")

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	err := s.server.mailstore.CreateMailbox(s.user, mailboxName)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK CREATE completed")
	return nil
}

// DELETE command
func (s *Session) handleDelete(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing mailbox name")
		return nil
	}

	mailboxName := args[0]
	mailboxName = strings.Trim(mailboxName, "\"'")

	// Cannot delete INBOX
	if strings.ToUpper(mailboxName) == "INBOX" {
		s.WriteResponse(s.tag, "NO Cannot delete INBOX")
		return nil
	}

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	err := s.server.mailstore.DeleteMailbox(s.user, mailboxName)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK DELETE completed")
	return nil
}

// RENAME command
func (s *Session) handleRename(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing old or new mailbox name")
		return nil
	}

	oldName := strings.Trim(args[0], "\"'")
	newName := strings.Trim(args[1], "\"'")

	// Cannot rename INBOX. INBOX is the mandatory, reserved mailbox: renaming
	// it away would destroy the user's INBOX and its messages, exactly what
	// the DELETE handler already refuses to allow.
	if strings.EqualFold(oldName, "INBOX") {
		s.WriteResponse(s.tag, "NO Cannot rename INBOX")
		return nil
	}

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	err := s.server.mailstore.RenameMailbox(s.user, oldName, newName)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK RENAME completed")
	return nil
}

// SUBSCRIBE command
func (s *Session) handleSubscribe(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing mailbox name")
		return nil
	}

	mailboxName := strings.Trim(args[0], "\"'")
	if mailboxName == "" {
		s.WriteResponse(s.tag, "BAD Empty mailbox name")
		return nil
	}

	// Verify mailbox exists first
	mailboxes, err := s.server.mailstore.ListMailboxes(s.user, mailboxName)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	// Check if the mailbox exists (exact match)
	found := false
	for _, m := range mailboxes {
		if m == mailboxName {
			found = true
			break
		}
	}

	if !found {
		s.WriteResponse(s.tag, "NO Mailbox not found")
		return nil
	}

	// Subscribe to the mailbox
	if err := s.server.mailstore.SetSubscribed(s.user, mailboxName, true); err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK SUBSCRIBE completed")
	return nil
}

// UNSUBSCRIBE command
func (s *Session) handleUnsubscribe(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing mailbox name")
		return nil
	}

	mailboxName := strings.Trim(args[0], "\"'")
	if mailboxName == "" {
		s.WriteResponse(s.tag, "BAD Empty mailbox name")
		return nil
	}

	// Unsubscribe from the mailbox
	if err := s.server.mailstore.SetSubscribed(s.user, mailboxName, false); err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK UNSUBSCRIBE completed")
	return nil
}

// LIST command
func (s *Session) handleList(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing reference or pattern")
		return nil
	}

	reference := strings.Trim(args[0], "\"'")
	pattern := strings.Trim(args[1], "\"'")

	// Combine reference and pattern
	fullPattern := reference
	if pattern != "" {
		if fullPattern != "" && !strings.HasSuffix(fullPattern, "/") {
			fullPattern += "/"
		}
		fullPattern += pattern
	}

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	mailboxes, err := s.server.mailstore.ListMailboxes(s.user, fullPattern)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	// Get all mailboxes to check for children hierarchy (RFC 3348)
	allMailboxes, _ := s.server.mailstore.ListMailboxes(s.user, "*")

	for _, mbox := range mailboxes {
		// Determine hierarchy indicators (RFC 3348)
		hasChildren := false
		hasNoSelect := false

		// Check if this mailbox has children by looking for sub-mailboxes
		mboxPrefix := mbox + "/"
		for _, other := range allMailboxes {
			if other != mbox && strings.HasPrefix(other, mboxPrefix) {
				hasChildren = true
				break
			}
		}

		// Build flags based on hierarchy
		flags := "\\HasNoChildren"
		if hasChildren {
			flags = "\\HasChildren"
		}
		if hasNoSelect {
			flags += " \\NoSelect"
		}

		s.WriteData(fmt.Sprintf("LIST (%s) \"/\" \"%s\"", flags, mbox))
	}

	s.WriteResponse(s.tag, "OK LIST completed")
	return nil
}

// LSUB command
func (s *Session) handleLsub(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing reference or pattern")
		return nil
	}

	reference := strings.Trim(args[0], "\"'")
	pattern := strings.Trim(args[1], "\"'")

	// Combine reference and pattern
	fullPattern := reference
	if pattern != "" {
		if fullPattern != "" && !strings.HasSuffix(fullPattern, "/") {
			fullPattern += "/"
		}
		fullPattern += pattern
	}

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	// Get subscribed mailboxes
	var mailboxes []string
	subscribed, err := s.server.mailstore.ListSubscribed(s.user)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}
	// Filter by pattern
	for _, mbox := range subscribed {
		if matchMailboxPattern(mbox, fullPattern) {
			mailboxes = append(mailboxes, mbox)
		}
	}

	for _, mbox := range mailboxes {
		s.WriteData(fmt.Sprintf("LSUB (\\HasNoChildren) \"/\" \"%s\"", mbox))
	}

	s.WriteResponse(s.tag, "OK LSUB completed")
	return nil
}

// matchMailboxPattern checks if a mailbox name matches an IMAP pattern
// ('*' and '%' wildcards, RFC 3501 §6.3.8) (F5219).
func matchMailboxPattern(name, pattern string) bool {
	return imapWildcardMatch(name, pattern)
}

// STATUS command
func (s *Session) handleStatus(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing mailbox or status items")
		return nil
	}

	mailboxName := strings.Trim(args[0], "\"'")
	statusItems := strings.Join(args[1:], " ")

	if s.server.mailstore == nil {
		s.WriteResponse(s.tag, "NO Mailstore not available")
		return nil
	}

	// Get mailbox info without ending \Recent (F5215)
	mailbox, err := s.examineMailbox(mailboxName)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	// Build status response
	status := fmt.Sprintf("STATUS \"%s\" (", mailboxName)

	if strings.Contains(statusItems, "MESSAGES") {
		status += fmt.Sprintf("MESSAGES %d ", mailbox.Exists)
	}
	if strings.Contains(statusItems, "RECENT") {
		status += fmt.Sprintf("RECENT %d ", mailbox.Recent)
	}
	if strings.Contains(statusItems, "UIDNEXT") {
		status += fmt.Sprintf("UIDNEXT %d ", mailbox.UIDNext)
	}
	if strings.Contains(statusItems, "UIDVALIDITY") {
		status += fmt.Sprintf("UIDVALIDITY %d ", mailbox.UIDValidity)
	}
	if strings.Contains(statusItems, "UNSEEN") {
		status += fmt.Sprintf("UNSEEN %d ", mailbox.Unseen)
	}

	status = strings.TrimRight(status, " ") + ")"

	s.WriteData(status)
	s.WriteResponse(s.tag, "OK STATUS completed")
	return nil
}

// APPEND command (RFC 3501) with MULTIAPPEND extension (RFC 7889)
func (s *Session) handleAppend(args []string, line string) error {
	ctx := context.Background()

	// Create tracing span
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.append", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", s.user),
		)
		defer span.End()
	}

	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing mailbox or message data")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "missing mailbox or message data")
		}
		return nil
	}

	mailboxName := strings.Trim(args[0], "\"'")

	if span != nil {
		tracing.SetStringAttribute(span, "append.mailbox", mailboxName)
	}

	// Limit APPEND message size to 50MB
	const maxAppendSize = 50 * 1024 * 1024

	// Process first message (has literal in command or needs continuation)
	flags, date, size, err := s.parseAppendParams(args[1:], line)
	if err != nil {
		s.WriteResponse(s.tag, "BAD "+err.Error())
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "parse error")
		}
		return nil
	}

	if size == 0 {
		s.WriteResponse(s.tag, "BAD Missing message data")
		return nil
	}

	if size > maxAppendSize {
		s.WriteResponse(s.tag, "NO Message too large (limit 50MB)")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "message too large")
		}
		return nil
	}

	if span != nil {
		tracing.SetIntAttribute(span, "append.size", size)
		tracing.SetIntAttribute(span, "append.flag_count", len(flags))
	}

	// Check if non-synchronizing literal (no + suffix) - needs continuation
	needsCont := !strings.Contains(line, "+}")

	// Request the literal if non-synchronizing
	if needsCont {
		s.WriteContinuation(fmt.Sprintf("Ready for %d octets", size))
	}

	// Read the message data
	data := make([]byte, size)
	_, err = io.ReadFull(s.reader, data)
	if err != nil {
		s.WriteResponse(s.tag, "NO Failed to read message data")
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "read message data failed")
		}
		return err
	}

	// Append to mailbox
	var appendValidity uint32
	var appendedUIDs []uint32
	if s.server.mailstore != nil {
		// F5643: a missing destination is NO [TRYCREATE], not created (RFC 3501
		// §6.3.11). The literal has been read, so the stream stays in sync.
		dest, ok := s.copyDestination(args[0])
		if !ok {
			return nil
		}
		mailboxName = dest
		validity, uid, err := s.appendOne(mailboxName, flags, date, data)
		if err != nil {
			s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
			if span != nil {
				tracing.RecordError(span, err)
				tracing.SetStatus(span, tracing.StatusError, "append failed")
			}
			return nil
		}
		appendValidity = validity
		if uid != 0 {
			appendedUIDs = append(appendedUIDs, uid)
		}
	}

	// RFC 7889 MULTIAPPEND: Check for additional messages in the stream
	// After reading one literal, there might be more synchronizing literals waiting
	for {
		// Look ahead for another literal marker using only bytes already buffered.
		// A blocking Peek here would deadlock: after the literal octets the client
		// sends the terminating CRLF and waits for the tagged response, so no
		// further bytes may arrive until we reply.
		buffered := s.reader.Buffered()
		if buffered == 0 {
			break
		}
		if buffered > 256 {
			buffered = 256
		}
		rest, err := s.reader.Peek(buffered)
		if err != nil || len(rest) == 0 {
			break
		}
		restStr := string(rest)
		litIdx := strings.Index(restStr, "{")
		if litIdx < 0 {
			break
		}

		// Found another literal - parse its size
		litEnd := strings.Index(restStr[litIdx:], "}")
		if litEnd < 0 {
			break
		}

		sizeStr := restStr[litIdx+1 : litIdx+litEnd]
		nextSize, hasPlus, err := parseLiteralSize(sizeStr)
		if err != nil {
			break
		}

		// Consume what we peeked (including the {size} part)
		discard := make([]byte, litIdx+litEnd+1)
		s.reader.Read(discard)
		// RFC 3501 literal syntax: "{n}" CRLF precedes the octets; the CRLF is
		// not part of the message.
		if s.reader.Buffered() >= 2 {
			if crlf, _ := s.reader.Peek(2); string(crlf) == "\r\n" {
				_, _ = s.reader.Discard(2)
			}
		}

		if nextSize > maxAppendSize {
			s.WriteResponse(s.tag, "NO Message too large (limit 50MB)")
			if span != nil {
				tracing.SetStatus(span, tracing.StatusError, "message too large")
			}
			return nil
		}

		if !hasPlus {
			s.WriteContinuation(fmt.Sprintf("Ready for %d octets", nextSize))
		}

		// Read this message
		data := make([]byte, nextSize)
		_, err = io.ReadFull(s.reader, data)
		if err != nil {
			s.WriteResponse(s.tag, "NO Failed to read message data")
			return err
		}

		// Append message with default flags
		if s.server.mailstore != nil {
			_, uid, err := s.appendOne(mailboxName, nil, time.Now(), data)
			if err != nil {
				s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
				return nil
			}
			if uid != 0 {
				appendedUIDs = append(appendedUIDs, uid)
			}
		}
	}

	if span != nil {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	// RFC 4315 §3: UIDPLUS servers report the assigned UIDs.
	if appendValidity != 0 && len(appendedUIDs) > 0 {
		s.WriteResponse(s.tag, fmt.Sprintf("OK [APPENDUID %d %s] APPEND completed", appendValidity, uidSetString(appendedUIDs)))
		return nil
	}
	s.WriteResponse(s.tag, "OK APPEND completed")
	return nil
}

// uidAppender is implemented by mailstores that report the UID an append
// assigned, for the UIDPLUS APPENDUID response code (RFC 4315 §3) (F5641).
type uidAppender interface {
	AppendMessageUID(user, mailbox string, flags []string, date time.Time, data []byte) (uidValidity, uid uint32, err error)
}

// appendOne stores one APPEND message; validity and uid are 0 when the
// mailstore cannot report them.
func (s *Session) appendOne(mailbox string, flags []string, date time.Time, data []byte) (uint32, uint32, error) {
	if ua, ok := s.server.mailstore.(uidAppender); ok {
		return ua.AppendMessageUID(s.user, mailbox, flags, date, data)
	}
	return 0, 0, s.server.mailstore.AppendMessage(s.user, mailbox, flags, date, data)
}

// parseAppendParams extracts flags, date, and literal size from APPEND args
func (s *Session) parseAppendParams(args []string, line string) ([]string, time.Time, int, error) {
	flags := []string{}
	date := time.Now()
	size := 0

	// Check for flags in parentheses
	for i, arg := range args {
		if strings.HasPrefix(arg, "(") {
			flagsStr := strings.Join(args[i:], " ")
			end := strings.Index(flagsStr, ")")
			if end > 0 {
				flagsStr = flagsStr[1:end]
				flags = strings.Fields(flagsStr)
			}
			break
		}
	}

	// Find literal string indicator {N} in the command line
	// Handle both {size} and {size}+ forms
	literalStart := strings.Index(line, "{")
	if literalStart < 0 {
		return flags, date, 0, fmt.Errorf("missing literal size")
	}

	literalEnd := strings.Index(line[literalStart:], "}")
	if literalEnd < 0 {
		return flags, date, 0, fmt.Errorf("invalid literal format")
	}

	sizeStr := line[literalStart+1 : literalStart+literalEnd]
	size, _, err := parseLiteralSize(sizeStr)
	if err != nil {
		return flags, date, 0, fmt.Errorf("invalid literal size")
	}

	// RFC 3501 §6.3.11: an optional date-time sets the INTERNALDATE (F5216).
	if dt, ok, err := appendDateTime(line[:literalStart]); err != nil {
		return flags, date, 0, err
	} else if ok {
		date = dt
	}

	return flags, date, size, nil
}

// appendDateTime extracts the optional APPEND date-time, the quoted string
// that ends the arguments before the literal ("dd-Mon-yyyy hh:mm:ss +zzzz",
// day space- or zero-padded). ok is false when the last argument is not a
// quoted string (e.g. the flag list, or a quoted mailbox name with no date).
func appendDateTime(prefix string) (time.Time, bool, error) {
	prefix = strings.TrimRight(prefix, " ")
	if !strings.HasSuffix(prefix, "\"") {
		return time.Time{}, false, nil
	}
	open := strings.LastIndex(prefix[:len(prefix)-1], "\"")
	if open < 0 {
		return time.Time{}, false, nil
	}
	// A quoted mailbox name directly after the command (no flags, no date)
	// is not a date-time: it is preceded by the command word "APPEND".
	before := strings.Fields(prefix[:open])
	if len(before) > 0 && strings.EqualFold(before[len(before)-1], "APPEND") {
		return time.Time{}, false, nil
	}
	dt, err := time.Parse("2-Jan-2006 15:04:05 -0700", strings.TrimSpace(prefix[open+1:len(prefix)-1]))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid date-time")
	}
	return dt, true, nil
}

// parseLiteralSize parses the text between the braces of an IMAP literal
// marker: "n" (synchronizing) or "n+" (RFC 7888 LITERAL+, non-synchronizing).
// n must be plain decimal digits, so negative or signed sizes are rejected.
func parseLiteralSize(spec string) (size int, nonSync bool, err error) {
	if strings.HasSuffix(spec, "+") {
		spec = strings.TrimSuffix(spec, "+")
		nonSync = true
	}
	n, err := strconv.ParseUint(spec, 10, 32) // digits only: no sign accepted
	if err != nil {
		return 0, false, fmt.Errorf("invalid literal size")
	}
	return int(n), nonSync, nil
}

// NAMESPACE command
func (s *Session) handleNamespace() error {
	// Personal namespace only
	s.WriteData("NAMESPACE ((\"\" \"/\")) NIL NIL")
	s.WriteResponse(s.tag, "OK NAMESPACE completed")
	return nil
}

// IDLE command (RFC 2177)
func (s *Session) handleIdle() error {
	// IDLE is only valid in Authenticated or Selected state.
	// Use the State() accessor (RLock) rather than s.state directly, for the
	// same reason as handleCommand: Close() writes s.state from another goroutine.
	if st := s.State(); st != StateAuthenticated && st != StateSelected {
		s.WriteResponse(s.tag, "BAD Command not allowed in this state")
		return nil
	}

	// Stop any active IDLE session before starting a new one.
	// Without this, a second IDLE arriving before the first has returned
	// (re-entrant call to handleIdle in the same goroutine) would overwrite
	// s.idleStop and s.idleNotifyChan, leaking the first DONE goroutine
	// (blocking forever on s.readLine()) and the first notification subscription.
	s.stopIdle()

	// Subscribe to notifications for this user
	s.idleActive = true
	s.idleStop = make(chan struct{})
	s.idleNotifyChan = GetNotificationHub().Subscribe(s.user)

	defer func() {
		s.idleActive = false
		if s.idleNotifyChan != nil {
			GetNotificationHub().Unsubscribe(s.user, s.idleNotifyChan)
			s.idleNotifyChan = nil
		}
	}()

	// Reject IDLE if no mailbox is selected (RFC 2177 §3)
	if s.selected == nil {
		s.WriteResponse(s.tag, "BAD no mailbox selected")
		return nil
	}

	// Send continuation response
	s.WriteContinuation("idling")

	// Channel for DONE command
	doneChan := make(chan bool, 1)

	// Start goroutine to wait for DONE
	go func() {
		for {
			line, err := s.readLine()
			if err != nil {
				doneChan <- true
				return
			}
			if strings.ToUpper(strings.TrimSpace(line)) == "DONE" {
				doneChan <- true
				return
			}
		}
	}()

	// idleCleanup ensures the DONE-reading goroutine exits by forcing
	// a read deadline on the connection, then waits for it to finish.
	idleCleanup := func() {
		select {
		case <-s.idleStop:
		default:
			close(s.idleStop)
		}
		_ = s.conn.SetReadDeadline(time.Now())
		// Wait for goroutine with timeout to avoid deadlock
		select {
		case <-doneChan:
		case <-time.After(5 * time.Second):
		}
	}

	// Wait for either DONE or notifications
	var idleTimer <-chan time.Time
	if s.server.idleTimeout > 0 {
		t := time.NewTimer(s.server.idleTimeout)
		defer t.Stop()
		idleTimer = t.C
	}

	for {
		select {
		case <-doneChan:
			s.WriteResponse(s.tag, "OK IDLE terminated")
			idleCleanup()
			return nil

		case <-idleTimer:
			s.WriteResponse(s.tag, "OK IDLE terminated")
			idleCleanup()
			return nil

		case notification, ok := <-s.idleNotifyChan:
			if !ok {
				s.WriteResponse(s.tag, "OK IDLE terminated")
				idleCleanup()
				return nil
			}

			// Only send notifications if a mailbox is selected
			if s.selected == nil {
				continue
			}

			// Only notify about changes to the selected mailbox
			if notification.Mailbox != s.selected.Name {
				continue
			}

			// Send appropriate untagged response based on notification type
			switch notification.Type {
			case NotificationNewMessage:
				// Send EXISTS and RECENT updates
				s.selected.Exists++
				s.selected.Recent++
				s.WriteData(fmt.Sprintf("%d EXISTS", s.selected.Exists))
				s.WriteData(fmt.Sprintf("%d RECENT", s.selected.Recent))

			case NotificationExpunge:
				// Send EXPUNGE update
				s.selected.Exists--
				if s.selected.Recent > 0 {
					s.selected.Recent--
				}
				s.WriteData(fmt.Sprintf("%d EXPUNGE", notification.SeqNum))

			case NotificationFlagsChanged:
				// Send FETCH response with updated flags
				flagsStr := ""
				if len(notification.Flags) > 0 {
					flagsStr = "(" + strings.Join(notification.Flags, " ") + ")"
				} else {
					flagsStr = "()"
				}
				s.WriteData(fmt.Sprintf("%d FETCH (FLAGS %s)", notification.SeqNum, flagsStr))

			case NotificationMailboxUpdate:
				// Re-fetch mailbox status and send updates
				if mailbox, err := s.server.mailstore.SelectMailbox(s.user, s.selected.Name); err == nil {
					if mailbox.Exists != s.selected.Exists {
						s.WriteData(fmt.Sprintf("%d EXISTS", mailbox.Exists))
					}
					if mailbox.Recent != s.selected.Recent {
						s.WriteData(fmt.Sprintf("%d RECENT", mailbox.Recent))
					}
					*s.selected = *mailbox
				}
			}
		}
	}
}

// ENABLE command (RFC 5161)
func (s *Session) handleEnable(args []string) error {
	// F5565: CONDSTORE and QRESYNC are no longer advertised (their STORE /
	// FETCH / SEARCH / SELECT modifiers are not implemented), so ENABLE
	// ignores them like any unknown extension (RFC 5161 §3.1) and answers
	// an empty ENABLED.
	s.WriteData("ENABLED")

	s.WriteResponse(s.tag, "OK ENABLE completed")
	return nil
}

// ID command (RFC 2971)
func (s *Session) handleID(args []string) error {
	// Client may send parenthesized list or NIL; we ignore client ID
	_ = args
	s.WriteData("* ID (\"name\" \"uMailServer\" \"version\" \"dev\")")
	s.WriteResponse(s.tag, "OK ID completed")
	return nil
}

// CHECK command
func (s *Session) handleCheck() error {
	s.WriteResponse(s.tag, "OK CHECK completed")
	return nil
}

// CLOSE command - RFC 3501: implicit EXPUNGE before deselecting
func (s *Session) handleClose() error {
	// RFC 3501 §6.4.2: no messages are removed when the mailbox was
	// selected by EXAMINE (F5068).
	if s.selected != nil && !s.selected.ReadOnly && s.server.mailstore != nil {
		_ = s.server.mailstore.Expunge(s.user, s.selected.Name)
	}
	s.selected = nil
	s.stateMu.Lock()
	s.state = StateAuthenticated
	s.stateMu.Unlock()
	s.WriteResponse(s.tag, "OK CLOSE completed")
	return nil
}

// EXPUNGE command
func (s *Session) handleExpunge() error {
	ctx := context.Background()

	// Create tracing span
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.expunge", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", s.user),
			attribute.String("mailbox", s.selected.Name),
		)
		defer span.End()
	}

	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "no mailbox selected")
		}
		return nil
	}
	if s.rejectReadOnly() {
		return nil
	}

	// Before expunging, find messages with \Deleted flag to report their
	// sequence numbers via untagged EXPUNGE responses.
	criteria := SearchCriteria{Deleted: true}
	deletedSeqs, err := s.server.mailstore.SearchMessages(s.user, s.selected.Name, criteria)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "search deleted messages failed")
		}
		return nil
	}

	if span != nil {
		tracing.SetIntAttribute(span, "expunge.deleted_count", len(deletedSeqs))
	}

	// The onExpunge hook (search index removal) is keyed by UID, so resolve
	// the deleted sequence numbers to UIDs while they are still valid.
	var deletedUIDs []uint32
	if s.server.onExpunge != nil && len(deletedSeqs) > 0 {
		msgs, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", nil)
		if err == nil {
			uidBySeq := make(map[uint32]uint32, len(msgs))
			for _, m := range msgs {
				uidBySeq[m.SeqNum] = m.UID
			}
			for _, seq := range deletedSeqs {
				if uid, ok := uidBySeq[seq]; ok {
					deletedUIDs = append(deletedUIDs, uid)
				}
			}
		}
	}

	err = s.server.mailstore.Expunge(s.user, s.selected.Name)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "expunge failed")
		}
		return nil
	}

	// Notify search index about expunged messages, by UID.
	if s.server.onExpunge != nil {
		for _, uid := range deletedUIDs {
			s.server.onExpunge(s.user, s.selected.Name, uid)
		}
	}

	// Send untagged EXPUNGE responses in reverse order (highest seq first)
	// so that subsequent sequence numbers remain valid during output.
	for i := len(deletedSeqs) - 1; i >= 0; i-- {
		s.WriteData(fmt.Sprintf("%d EXPUNGE", deletedSeqs[i]))
	}

	if span != nil {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK EXPUNGE completed")
	return nil
}

// SEARCH command
func (s *Session) handleSearch(args []string, line string) error {
	return s.handleSearchWithUIDs(args, line, false)
}

func (s *Session) handleSearchWithUIDs(args []string, line string, uidResults bool) error {
	ctx := context.Background()

	// Create tracing span
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.search", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", s.user),
			attribute.String("mailbox", s.selected.Name),
		)
		defer span.End()
	}

	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "no mailbox selected")
		}
		return nil
	}

	// Parse search criteria (NOT / OR / parenthesised keys: F5493)
	program, err := parseSearchProgram(commandArgTokens(args, line, "SEARCH"))
	if err != nil {
		s.writeSearchParseError(err)
		return nil
	}

	if span != nil {
		tracing.SetIntAttribute(span, "search.criteria_count", len(args))
	}

	uids, err := s.evalSearch(program)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "search failed")
		}
		return nil
	}

	if span != nil {
		tracing.SetIntAttribute(span, "search.result_count", len(uids))
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	if uidResults && len(uids) > 0 {
		var seqSet []string
		for _, seq := range uids {
			seqSet = append(seqSet, fmt.Sprintf("%d", seq))
		}
		messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, strings.Join(seqSet, ","), []string{"UID"})
		if err != nil {
			s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
			return nil
		}
		uids = uids[:0]
		for _, msg := range messages {
			uids = append(uids, msg.UID)
		}
	}

	// SearchMessages returns positions; UID SEARCH resolves them to stable UIDs.
	result := "SEARCH"
	for _, uid := range uids {
		result += fmt.Sprintf(" %d", uid)
	}
	s.WriteData(result)

	s.WriteResponse(s.tag, "OK SEARCH completed")
	return nil
}

// SORT command (RFC 5256)
func (s *Session) handleSort(args []string, line string) error {
	return s.sortCmd(args, line, false)
}

// sortCmd implements SORT and UID SORT: "SORT (criteria) charset search-keys"
// (RFC 5256 §3). F5490: the parenthesised criteria list was split on
// spaces ("(DATE)" was an unknown criterion, so every conforming SORT
// answered BAD) and the charset and search keys were ignored.
func (s *Session) sortCmd(args []string, line string, uidResults bool) error {
	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}

	toks := commandArgTokens(args, line, "SORT")
	if len(toks) == 0 || toks[0].quoted || toks[0].val != "(" {
		s.WriteResponse(s.tag, "BAD invalid sort criteria")
		return nil
	}
	var criteriaArgs []string
	i := 1
	for ; i < len(toks) && (toks[i].quoted || toks[i].val != ")"); i++ {
		criteriaArgs = append(criteriaArgs, toks[i].val)
	}
	if i+2 >= len(toks) {
		s.WriteResponse(s.tag, "BAD SORT requires a criteria list, a charset and search keys")
		return nil
	}
	if !searchCharsetSupported(toks[i+1].val) {
		s.WriteResponse(s.tag, badCharsetResponse)
		return nil
	}

	criteria, err := parseSortCriteria(criteriaArgs)
	if err != nil {
		s.server.logger.Error("imap sort criteria parse error", "error", err)
		s.WriteResponse(s.tag, "BAD invalid sort criteria")
		return nil
	}
	program, err := parseSearchProgram(toks[i+2:])
	if err != nil {
		s.writeSearchParseError(err)
		return nil
	}
	matched, err := s.evalSearch(program)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}
	want := seqSetOf(matched)
	// F5561: the Cc field is not in the metadata; read it from the header.
	var ccs []string
	hr, _ := s.server.mailstore.(messageHeaderReader)
	for _, c := range criteria {
		if c.Field == "CC" {
			ccs = []string{}
		}
	}

	// Get all messages in mailbox with metadata
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", []string{"ENVELOPE"})
	if err != nil {
		s.server.logger.Error("imap fetch messages error", "error", err)
		s.WriteResponse(s.tag, "NO unable to fetch messages")
		return nil
	}

	// Build metadata list with sequence numbers for the matching messages
	var metas []*storage.MessageMetadata
	var seqNums []uint32
	uidOf := map[uint32]uint32{}
	for n, msg := range messages {
		seqNum := uint32(n + 1)
		if !want[seqNum] {
			continue
		}
		seqNums = append(seqNums, seqNum)
		uidOf[seqNum] = msg.UID
		// The bbolt mailstore fills the flat fields, not Envelope (whose
		// nil dereference dropped the connection: F5490).
		meta := &storage.MessageMetadata{
			UID:          msg.UID,
			Subject:      msg.Subject,
			From:         msg.From,
			To:           msg.To,
			Date:         msg.Date,
			InternalDate: msg.InternalDate,
			Size:         msg.Size,
		}
		cc := ""
		if env := msg.Envelope; env != nil {
			meta.Subject, meta.From, meta.Date = env.Subject, addressToString(env.From), env.Date
			meta.To, cc = addressToString(env.To), addressToString(env.Cc)
		}
		if ccs != nil {
			if hr != nil {
				if hdr, err := hr.MessageHeader(s.user, s.selected.Name, msg.UID); err == nil {
					cc = readHeaderFields(hdr).Get("Cc")
				}
			}
			ccs = append(ccs, cc)
		}
		metas = append(metas, meta)
	}

	result := "SORT"
	for _, seq := range sortMessagesByKeys(metas, ccs, criteria, seqNums) {
		if uidResults {
			seq = uidOf[seq]
		}
		result += fmt.Sprintf(" %d", seq)
	}
	s.WriteData(result)
	if uidResults {
		s.WriteResponse(s.tag, "OK UID SORT completed")
	} else {
		s.WriteResponse(s.tag, "OK SORT completed")
	}
	return nil
}

// addressToString converts Address slice to string
func addressToString(addrs []*Address) string {
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0].MailboxName + "@" + addrs[0].HostName
}

// THREAD command (RFC 5256)
func (s *Session) handleThread(args []string, line string) error {
	return s.threadCmd(args, line, false)
}

// messageHeaderReader is implemented by mailstores that can return a
// message's header section without the side effects of FETCH; THREAD needs
// Message-ID / References and SORT CC the Cc field, which the flat Message
// fields lack.
type messageHeaderReader interface {
	MessageHeader(user, mailbox string, uid uint32) ([]byte, error)
}

// threadCmd implements THREAD and UID THREAD: "THREAD algorithm charset
// search-keys" answered by "* THREAD (1 2)(3)" (RFC 5256 §3, §4). F5560:
// the handlers dereferenced msg.Envelope, which the bbolt mailstore never
// fills, so every THREAD panicked and dropped the connection; the charset
// and search keys were ignored and each thread was written as its own
// "* (..)" line.
func (s *Session) threadCmd(args []string, line string, uidResults bool) error {
	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}

	toks := commandArgTokens(args, line, "THREAD")
	if len(toks) < 3 {
		s.WriteResponse(s.tag, "BAD THREAD requires an algorithm, a charset and search keys")
		return nil
	}
	algo := ThreadAlgorithm(strings.ToUpper(toks[0].val))
	if toks[0].quoted || (algo != ThreadReferences && algo != ThreadOrderedSubject) {
		s.WriteResponse(s.tag, "BAD unsupported THREAD algorithm")
		return nil
	}
	if !searchCharsetSupported(toks[1].val) {
		s.WriteResponse(s.tag, badCharsetResponse)
		return nil
	}
	program, err := parseSearchProgram(toks[2:])
	if err != nil {
		s.writeSearchParseError(err)
		return nil
	}
	matched, err := s.evalSearch(program)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}
	want := seqSetOf(matched)

	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", nil)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}
	hr, _ := s.server.mailstore.(messageHeaderReader)
	var msgs []*threadMsg
	for n, msg := range messages {
		seq := msg.SeqNum
		if seq == 0 {
			seq = uint32(n + 1)
		}
		if !want[seq] {
			continue
		}
		tm := &threadMsg{num: seq, seq: seq, subject: msg.Subject}
		if uidResults {
			tm.num = msg.UID
		}
		date := msg.Date
		if env := msg.Envelope; env != nil {
			tm.msgID, tm.refs = threadIDs(env.MessageID, env.InReplyTo, "")
			tm.subject, date = env.Subject, env.Date
		}
		if hr != nil {
			if hdr, err := hr.MessageHeader(s.user, s.selected.Name, msg.UID); err == nil {
				tm.msgID, tm.refs, tm.subject, date = parseThreadHeader(hdr)
			}
		}
		tm.date = sentDate(date, msg.InternalDate)
		msgs = append(msgs, tm)
	}

	var roots []*threadNode
	if algo == ThreadReferences {
		roots = threadReferences(msgs)
	} else {
		roots = threadOrderedSubject(msgs)
	}
	result := "THREAD"
	if len(roots) > 0 {
		result += " " + formatThreads(roots)
	}
	s.WriteData(result)
	if uidResults {
		s.WriteResponse(s.tag, "OK UID THREAD completed")
	} else {
		s.WriteResponse(s.tag, "OK THREAD completed")
	}
	return nil
}

// UID SORT command
func (s *Session) handleUIDSort(args []string, line string) error {
	return s.sortCmd(args, line, true)
}

// UID THREAD command
func (s *Session) handleUIDThread(args []string, line string) error {
	return s.threadCmd(args, line, true)
}

// FETCH command
func (s *Session) handleFetch(args []string, line string) error {
	return s.fetch(args, line, false)
}

// fetch implements FETCH and, with uidCmd, UID FETCH: RFC 3501 §6.4.8
// requires the UID item in every FETCH response caused by a UID command,
// whether or not the client asked for it (F5217).
func (s *Session) fetch(args []string, line string, uidCmd bool) error {
	ctx := context.Background()

	// Create tracing span
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.fetch", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", s.user),
			attribute.String("mailbox", s.selected.Name),
		)
		defer span.End()
	}

	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing sequence or fetch items")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "missing sequence or fetch items")
		}
		return nil
	}

	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "no mailbox selected")
		}
		return nil
	}

	seqSet := args[0]
	fetchItems := parseFetchItems(args[1:])

	if span != nil {
		tracing.SetStringAttribute(span, "fetch.seqset", seqSet)
		tracing.SetIntAttribute(span, "fetch.item_count", len(fetchItems))
	}

	// F5496: BODYSTRUCTURE is computed from the message data, which the
	// mailstore loads for the BODY item.
	storeItems := fetchItems
	if hasFetchItem(fetchItems, "BODYSTRUCTURE") && !hasFetchItem(fetchItems, "BODY") {
		storeItems = append(append([]string{}, fetchItems...), "BODY")
	}
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, seqSet, storeItems)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "fetch failed")
		}
		return nil
	}

	if span != nil {
		tracing.SetIntAttribute(span, "fetch.message_count", len(messages))
	}

	// RFC 3501 §6.4.5: a non-PEEK BODY[section] sets \Seen (unless the
	// mailbox was opened with EXAMINE).
	setsSeen := false
	for _, item := range fetchItems {
		if it, ok := parseBodySectionItem(item); ok && !it.peek {
			setsSeen = true
		}
	}
	if setsSeen && !s.selected.ReadOnly {
		for _, msg := range messages {
			if !hasFlag(msg.Flags, "\\Seen") {
				if err := s.server.mailstore.StoreFlags(s.user, s.selected.Name, strconv.FormatUint(uint64(msg.SeqNum), 10), []string{"\\Seen"}, FlagAdd); err == nil {
					msg.Flags = append(msg.Flags, "\\Seen")
				}
			}
		}
	}

	// F5642: ENVELOPE needs Cc / Message-ID / In-Reply-To, which the flat
	// message metadata lacks; read the header without FETCH side effects.
	if hr, ok := s.server.mailstore.(messageHeaderReader); ok && hasFetchItem(fetchItems, "ENVELOPE") {
		for _, msg := range messages {
			if hb, err := hr.MessageHeader(s.user, s.selected.Name, msg.UID); err == nil {
				msg.header = partHeader(append(append([]byte{}, hb...), "\r\n"...))
			}
		}
	}

	respItems := fetchItems
	if uidCmd && !hasFetchItem(fetchItems, "UID") {
		respItems = append([]string{"UID"}, fetchItems...)
	}
	for _, msg := range messages {
		fetchResponse := formatFetchResponse(msg, respItems)
		s.WriteData(fmt.Sprintf("%d FETCH (%s)", msg.SeqNum, fetchResponse))
	}

	if span != nil {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK FETCH completed")
	return nil
}

// hasFetchItem reports whether items contains name (case-insensitive).
func hasFetchItem(items []string, name string) bool {
	for _, it := range items {
		if strings.EqualFold(it, name) {
			return true
		}
	}
	return false
}

// STORE command
func (s *Session) handleStore(args []string) error {
	return s.store(args, false)
}

// store implements STORE and, with uidCmd, UID STORE, whose untagged FETCH
// responses must carry the UID (RFC 3501 §6.4.8) (F5217).
func (s *Session) store(args []string, uidCmd bool) error {
	ctx := context.Background()

	// Create tracing span
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "imap.store", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("user", s.user),
			attribute.String("mailbox", s.selected.Name),
		)
		defer span.End()
	}

	if len(args) < 3 {
		s.WriteResponse(s.tag, "BAD Missing sequence, operation, or flags")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "missing sequence, operation, or flags")
		}
		return nil
	}

	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "no mailbox selected")
		}
		return nil
	}

	if s.rejectReadOnly() {
		return nil
	}

	seqSet := args[0]
	operation := strings.ToUpper(args[1]) // FLAGS, +FLAGS, -FLAGS
	flagsStr := strings.Join(args[2:], " ")

	// Parse flags
	flags := parseFlags(flagsStr)

	if span != nil {
		tracing.SetStringAttribute(span, "store.seqset", seqSet)
		tracing.SetStringAttribute(span, "store.operation", operation)
		tracing.SetIntAttribute(span, "store.flag_count", len(flags))
	}

	var op FlagOperation
	switch operation {
	case "FLAGS", "FLAGS.SILENT":
		op = FlagReplace
	case "+FLAGS", "+FLAGS.SILENT":
		op = FlagAdd
	case "-FLAGS", "-FLAGS.SILENT":
		op = FlagRemove
	default:
		s.WriteResponse(s.tag, "BAD Invalid STORE operation")
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "invalid operation")
		}
		return nil
	}

	err := s.server.mailstore.StoreFlags(s.user, s.selected.Name, seqSet, flags, op)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "store failed")
		}
		return nil
	}

	// If not silent, fetch updated messages and output FLAGS responses
	if !strings.HasSuffix(operation, ".SILENT") {
		messages, fetchErr := s.server.mailstore.FetchMessages(s.user, s.selected.Name, seqSet, []string{"FLAGS"})
		if fetchErr == nil {
			for _, msg := range messages {
				uidItem := ""
				if uidCmd {
					uidItem = fmt.Sprintf("UID %d ", msg.UID)
				}
				s.WriteData(fmt.Sprintf("%d FETCH (%sFLAGS (%s))", msg.SeqNum, uidItem, strings.Join(msg.Flags, " ")))
			}
		}
	}

	if span != nil {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK STORE completed")
	return nil
}

// COPY command
func (s *Session) handleCopy(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing sequence or destination")
		return nil
	}

	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}

	seqSet := args[0]
	destMailbox, ok := s.copyDestination(args[1])
	if !ok {
		return nil
	}

	if uc, ok := s.server.mailstore.(uidCopier); ok {
		validity, src, dst, err := uc.CopyMessagesUIDs(s.user, s.selected.Name, destMailbox, seqSet)
		if err != nil {
			s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
			return nil
		}
		if validity != 0 && len(src) > 0 && len(src) == len(dst) {
			s.WriteResponse(s.tag, fmt.Sprintf("OK [COPYUID %d %s %s] COPY completed", validity, uidSetString(src), uidSetString(dst)))
			return nil
		}
		s.WriteResponse(s.tag, "OK COPY completed")
		return nil
	}

	err := s.server.mailstore.CopyMessages(s.user, s.selected.Name, destMailbox, seqSet)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK COPY completed")
	return nil
}

// uidCopier is implemented by mailstores that report the UIDs a copy
// assigned, for the UIDPLUS COPYUID response code (RFC 4315 §3) (F5564).
type uidCopier interface {
	CopyMessagesUIDs(user, sourceMailbox, destMailbox string, seqSet string) (uidValidity uint32, srcUIDs, dstUIDs []uint32, err error)
}

// uidSetString renders ascending UIDs as a compact uid-set ("1:3,7").
func uidSetString(uids []uint32) string {
	var b strings.Builder
	for i := 0; i < len(uids); {
		j := i
		for j+1 < len(uids) && uids[j+1] == uids[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		if j > i {
			fmt.Fprintf(&b, "%d:%d", uids[i], uids[j])
		} else {
			fmt.Fprintf(&b, "%d", uids[i])
		}
		i = j + 1
	}
	return b.String()
}

// copyDestination resolves the COPY / MOVE target and answers
// NO [TRYCREATE] when it does not exist: RFC 3501 §6.4.7 says the server
// SHOULD NOT create it. F5567: the copy silently created the mailbox, so a
// mistyped name made a new folder instead of failing. INBOX is matched
// case-insensitively.
func (s *Session) copyDestination(arg string) (string, bool) {
	dest := strings.Trim(arg, "\"'")
	if strings.EqualFold(dest, "INBOX") {
		return "INBOX", true
	}
	names, err := s.server.mailstore.ListMailboxes(s.user, dest)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return "", false
	}
	for _, n := range names {
		if n == dest {
			return dest, true
		}
	}
	s.WriteResponse(s.tag, "NO [TRYCREATE] Mailbox does not exist")
	return "", false
}

// MOVE command
func (s *Session) handleMove(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD Missing sequence or destination")
		return nil
	}

	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}

	if s.rejectReadOnly() {
		return nil
	}

	seqSet := args[0]
	destMailbox, ok := s.copyDestination(args[1])
	if !ok {
		return nil
	}

	// RFC 6851 §3.3: MOVE behaves as COPY + STORE \Deleted + UID EXPUNGE of
	// the moved messages, so record their UIDs before they are flagged.
	moved := map[uint32]bool{}
	if um, ok := s.server.mailstore.(uidMover); ok {
		// F5640 / F5641: only the messages that were copied are expunged,
		// and the client is told their new UIDs (RFC 6851 §4.3).
		validity, src, dst, err := um.MoveMessagesUIDs(s.user, s.selected.Name, destMailbox, seqSet)
		if err != nil {
			s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
			return nil
		}
		for _, uid := range src {
			moved[uid] = true
		}
		if validity != 0 && len(src) > 0 && len(src) == len(dst) {
			s.WriteData(fmt.Sprintf("OK [COPYUID %d %s %s] Moved", validity, uidSetString(src), uidSetString(dst)))
		}
	} else {
		if msgs, ferr := s.server.mailstore.FetchMessages(s.user, s.selected.Name, seqSet, nil); ferr == nil {
			for _, m := range msgs {
				moved[m.UID] = true
			}
		}

		err := s.server.mailstore.MoveMessages(s.user, s.selected.Name, destMailbox, seqSet)
		if err != nil {
			s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
			return nil
		}
	}

	if ux, ok := s.server.mailstore.(uidExpunger); ok && len(moved) > 0 {
		if err := s.expungeUIDSubset(ux, func(uid uint32) bool { return moved[uid] }); err != nil {
			s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
			return nil
		}
	}

	s.WriteResponse(s.tag, "OK MOVE completed")
	return nil
}

// uidMover is implemented by mailstores that report the UIDs a move copied
// (and flagged \Deleted), for the RFC 6851 COPYUID response (F5641).
type uidMover interface {
	MoveMessagesUIDs(user, sourceMailbox, destMailbox string, seqSet string) (uidValidity uint32, srcUIDs, dstUIDs []uint32, err error)
}

// UID command (prefix for UID variants)
func (s *Session) handleUID(args []string, line string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD Missing UID command")
		return nil
	}

	uidCommand := strings.ToUpper(args[0])
	uidArgs := args[1:]

	switch uidCommand {
	case "FETCH":
		return s.handleUIDFetch(uidArgs, line)
	case "STORE":
		return s.handleUIDStore(uidArgs)
	case "COPY":
		return s.handleUIDCopy(uidArgs)
	case "MOVE":
		return s.handleUIDMove(uidArgs)
	case "SEARCH":
		return s.handleUIDSearch(uidArgs, line)
	case "SORT":
		return s.handleUIDSort(uidArgs, line)
	case "THREAD":
		return s.handleUIDThread(uidArgs, line)
	case "EXPUNGE":
		return s.handleUIDExpunge(uidArgs)
	default:
		s.WriteResponse(s.tag, "BAD Unknown UID command")
		return nil
	}
}

// uidSetToSeqSet translates a UID sequence-set into the equivalent
// message-sequence-number set for the selected mailbox. RFC 3501 §6.4.8: a UID
// command's sequence-set is interpreted as UIDs, which are stable, whereas
// sequence numbers are positional and shift after an expunge. The Mailstore
// primitives address by sequence number, so the UID variants resolve UIDs to
// positions here, at the command layer where that distinction is known.
func (s *Session) uidSetToSeqSet(uidSet string) (string, error) {
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", nil)
	if err != nil {
		return "", err
	}
	ranges, err := ParseSequenceSet(uidSet)
	if err != nil {
		return "", err
	}

	// "*" in a UID set denotes the highest UID, so resolve the 0 sentinel
	// against the largest UID present rather than the message count.
	var maxUID uint32
	for _, m := range messages {
		if m.UID > maxUID {
			maxUID = m.UID
		}
	}

	var seqNums []string
	for _, m := range messages {
		for _, r := range ranges {
			if r.Contains(m.UID, maxUID) {
				seqNums = append(seqNums, fmt.Sprintf("%d", m.SeqNum))
				break
			}
		}
	}
	return strings.Join(seqNums, ","), nil
}

func (s *Session) handleUIDFetch(args []string, line string) error {
	// Same as FETCH but with UIDs: resolve the UID set to sequence numbers
	// first, since FetchMessages addresses by sequence number.
	if len(args) == 0 {
		return s.handleFetch(args, line)
	}
	seqSet, err := s.uidSetToSeqSet(args[0])
	if err != nil {
		s.WriteResponse(s.tag, "NO Invalid sequence set")
		return nil
	}
	rest := append([]string{seqSet}, args[1:]...)
	return s.fetch(rest, line, true)
}

func (s *Session) handleUIDStore(args []string) error {
	// Same as STORE but with UIDs.
	if len(args) == 0 {
		return s.handleStore(args)
	}
	seqSet, err := s.uidSetToSeqSet(args[0])
	if err != nil {
		s.WriteResponse(s.tag, "NO Invalid sequence set")
		return nil
	}
	rest := append([]string{seqSet}, args[1:]...)
	return s.store(rest, true)
}

func (s *Session) handleUIDCopy(args []string) error {
	// Same as COPY but with UIDs.
	if len(args) == 0 {
		return s.handleCopy(args)
	}
	seqSet, err := s.uidSetToSeqSet(args[0])
	if err != nil {
		s.WriteResponse(s.tag, "NO Invalid sequence set")
		return nil
	}
	rest := append([]string{seqSet}, args[1:]...)
	return s.handleCopy(rest)
}

func (s *Session) handleUIDMove(args []string) error {
	// Same as MOVE but with UIDs.
	if len(args) == 0 {
		return s.handleMove(args)
	}
	seqSet, err := s.uidSetToSeqSet(args[0])
	if err != nil {
		s.WriteResponse(s.tag, "NO Invalid sequence set")
		return nil
	}
	rest := append([]string{seqSet}, args[1:]...)
	return s.handleMove(rest)
}

func (s *Session) handleUIDSearch(args []string, line string) error {
	// Same as SEARCH but output UIDs
	return s.handleSearchWithUIDs(args, line, true)
}

// rejectReadOnly answers NO and returns true when the selected mailbox was
// opened with EXAMINE: RFC 3501 §6.3.2 forbids changes to its permanent
// state (STORE, EXPUNGE, UID EXPUNGE, MOVE's source removal) (F5068).
func (s *Session) rejectReadOnly() bool {
	if s.selected != nil && s.selected.ReadOnly {
		s.WriteResponse(s.tag, "NO [READ-ONLY] Mailbox is selected read-only")
		return true
	}
	return false
}

// uidExpunger is the optional mailstore primitive that expunges only the
// given \Deleted UIDs (implemented by BboltMailstore).
type uidExpunger interface {
	ExpungeUIDs(user, mailbox string, uids []uint32) error
}

// expungeUIDSubset permanently removes the \Deleted messages of the selected
// mailbox whose UID satisfies want, fires onExpunge per UID and sends the
// untagged EXPUNGE responses (highest sequence number first).
func (s *Session) expungeUIDSubset(ux uidExpunger, want func(uid uint32) bool) error {
	deletedSeqs, err := s.server.mailstore.SearchMessages(s.user, s.selected.Name, SearchCriteria{Deleted: true})
	if err != nil || len(deletedSeqs) == 0 {
		return err
	}
	msgs, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", nil)
	if err != nil {
		return err
	}
	uidBySeq := make(map[uint32]uint32, len(msgs))
	for _, m := range msgs {
		uidBySeq[m.SeqNum] = m.UID
	}
	var seqs, uids []uint32
	for _, seq := range deletedSeqs {
		if uid, ok := uidBySeq[seq]; ok && want(uid) {
			seqs = append(seqs, seq)
			uids = append(uids, uid)
		}
	}
	if len(uids) == 0 {
		return nil
	}
	if err := ux.ExpungeUIDs(s.user, s.selected.Name, uids); err != nil {
		return err
	}
	if s.server.onExpunge != nil {
		for _, uid := range uids {
			s.server.onExpunge(s.user, s.selected.Name, uid)
		}
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] > seqs[j] })
	for _, seq := range seqs {
		s.WriteData(fmt.Sprintf("%d EXPUNGE", seq))
	}
	return nil
}

// handleUIDExpunge implements RFC 4315 §2.1 UID EXPUNGE <uid-set>: only the
// \Deleted messages whose UID is in the set are removed.
func (s *Session) handleUIDExpunge(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD UID EXPUNGE requires a UID set")
		return nil
	}
	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}
	if s.rejectReadOnly() {
		return nil
	}
	ranges, err := ParseSequenceSet(args[0])
	if err != nil {
		s.WriteResponse(s.tag, "BAD Invalid UID set")
		return nil
	}
	ux, ok := s.server.mailstore.(uidExpunger)
	if !ok {
		s.WriteResponse(s.tag, "NO UID EXPUNGE not supported by this mailstore")
		return nil
	}
	msgs, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", nil)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}
	var maxUID uint32
	for _, m := range msgs {
		if m.UID > maxUID {
			maxUID = m.UID
		}
	}
	inSet := func(uid uint32) bool {
		for _, r := range ranges {
			if r.Contains(uid, maxUID) {
				return true
			}
		}
		return false
	}
	if err := s.expungeUIDSubset(ux, inSet); err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}
	s.WriteResponse(s.tag, "OK UID EXPUNGE completed")
	return nil
}

// handleGetACL implements RFC 4314 GETACL command
func (s *Session) handleGetACL(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD GETACL requires a mailbox name")
		return nil
	}

	mailbox := args[0]

	// User must be authenticated
	if s.state == StateNotAuthenticated {
		s.WriteResponse(s.tag, "NO Not authenticated")
		return nil
	}

	// Get owner - for own mailbox, user is owner; for shared, we need to parse owner:mailbox
	owner, mb, isShared := s.parseOwnerMailbox(mailbox)
	if isShared && owner != s.user {
		// Check if user has ACL lookup right on this shared mailbox
		rights, err := s.server.mailstore.GetACL(owner, mb, s.user)
		if err != nil || rights&uint8(storage.ACLLookup) == 0 {
			s.WriteResponse(s.tag, "NO Access denied")
			return nil
		}
	}

	aclEntries, err := s.server.mailstore.ListACL(owner, mb)
	if err != nil {
		s.WriteResponse(s.tag, "NO Internal server error")
		return nil
	}

	// Send untagged ACL responses
	for _, entry := range aclEntries {
		s.WriteData(fmt.Sprintf("ACL %s %s %s", mailbox, entry.Grantee, entry.Rights.String()))
	}

	s.WriteResponse(s.tag, "OK GETACL completed")
	return nil
}

// handleSetACL implements RFC 4314 SETACL command
func (s *Session) handleSetACL(args []string) error {
	if len(args) < 3 {
		s.WriteResponse(s.tag, "BAD SETACL requires mailbox, grantee, and rights")
		return nil
	}

	mailbox := args[0]
	grantee := args[1]
	rightsStr := args[2]

	// User must be authenticated
	if s.state == StateNotAuthenticated {
		s.WriteResponse(s.tag, "NO Not authenticated")
		return nil
	}

	// Parse owner:mailbox format if shared
	owner, mb, isShared := s.parseOwnerMailbox(mailbox)

	// Only owner can set ACL
	if isShared && owner != s.user {
		s.WriteResponse(s.tag, "NO Only owner can modify ACL")
		return nil
	}

	// Parse rights string (e.g., "lrswipkxtecda" or "-lrswipkxtecda" or numeric)
	rights, negative, err := storage.ParseACLRights(rightsStr)
	if err != nil {
		s.WriteResponse(s.tag, "BAD Invalid rights format")
		return nil
	}

	// RFC 4314 section 3.1: a leading '-' removes the listed rights from the
	// grantee's EXISTING set, so the mask must be applied with &^ against what
	// they already hold. Storing the complement would grant every unlisted
	// right instead, turning a revocation into an expansion.
	if negative {
		existing, err := s.server.mailstore.GetACL(owner, mb, grantee)
		if err != nil {
			s.WriteResponse(s.tag, "NO Failed to read current ACL")
			return nil
		}
		rights = storage.ACLRights(existing) &^ rights
	}

	err = s.server.mailstore.SetACL(owner, mb, grantee, uint8(rights), s.user)
	if err != nil {
		s.WriteResponse(s.tag, "NO Failed to set ACL")
		return nil
	}

	s.WriteResponse(s.tag, "OK SETACL completed")
	return nil
}

// handleDeleteACL implements RFC 4314 DELETEACL command
func (s *Session) handleDeleteACL(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD DELETEACL requires mailbox and grantee")
		return nil
	}

	mailbox := args[0]
	grantee := args[1]

	// User must be authenticated
	if s.state == StateNotAuthenticated {
		s.WriteResponse(s.tag, "NO Not authenticated")
		return nil
	}

	// Parse owner:mailbox format if shared
	owner, mb, isShared := s.parseOwnerMailbox(mailbox)

	// Only owner can delete ACL
	if isShared && owner != s.user {
		s.WriteResponse(s.tag, "NO Only owner can delete ACL")
		return nil
	}

	err := s.server.mailstore.DeleteACL(owner, mb, grantee)
	if err != nil {
		s.WriteResponse(s.tag, "NO Failed to delete ACL")
		return nil
	}

	s.WriteResponse(s.tag, "OK DELETEACL completed")
	return nil
}

// handleMyRights implements RFC 4314 MYRIGHTS command
func (s *Session) handleMyRights(args []string) error {
	if len(args) < 1 {
		s.WriteResponse(s.tag, "BAD MYRIGHTS requires a mailbox name")
		return nil
	}

	mailbox := args[0]

	// User must be authenticated
	if s.state == StateNotAuthenticated {
		s.WriteResponse(s.tag, "NO Not authenticated")
		return nil
	}

	// Parse owner:mailbox format if shared
	owner, mb, isShared := s.parseOwnerMailbox(mailbox)

	var rights storage.ACLRights

	if isShared {
		if owner == s.user {
			rights = storage.ACLAll // Owner has all rights
		} else {
			aclRights, err := s.server.mailstore.GetACL(owner, mb, s.user)
			rights = storage.ACLRights(aclRights)
			if err != nil {
				s.WriteResponse(s.tag, "NO Internal server error")
				return nil
			}
		}
	} else {
		// Own mailbox - user has all rights
		rights = storage.ACLAll
	}

	s.WriteData(fmt.Sprintf("MYRIGHTS %s %s", mailbox, rights.String()))
	s.WriteResponse(s.tag, "OK MYRIGHTS completed")
	return nil
}

// normalizeUsername applies PRECIS UsernameCaseMapped profile (RFC 7616).
// Returns error if username contains invalid characters.
func normalizeUsername(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("empty username")
	}

	// RFC 7616 Section 3: UsernameCaseMapped profile
	// 1. Ensure all characters are in NFC form
	username = strings.TrimSpace(username)

	// 2. Map uppercase letters to their lowercase equivalents (RFC 7616 Section 5.2)
	var lower strings.Builder
	for _, r := range username {
		// Apply RFC 7616 Section 5.2: case mapping
		// Map uppercase letters to lowercase
		lower.WriteRune(unicode.ToLower(r))
	}
	username = lower.String()

	// 3. Ensure no prohibited characters (RFC 7616 Section 5.3)
	// Prohibited: ASCII control chars, space, slash, null
	for _, r := range username {
		if r < 0x20 || r == 0x7F || r == ' ' || r == '/' || r == 0x00 {
			return "", fmt.Errorf("username contains prohibited character")
		}
	}

	// 4. Ensure output is valid UTF-8 (already guaranteed by Go strings)
	if !utf8.ValidString(username) {
		return "", fmt.Errorf("username is not valid UTF-8")
	}

	// 5. For internationalized domain names in email addresses, convert to punycode
	// This is handled at a higher layer by SMTP's SMTPUTF8 support

	return username, nil
}

// normalizePassword applies PRECIS PasswordPrep profile (RFC 7616).
// This is a conservative normalization that preserves meaning.
func normalizePassword(password string) string {
	if password == "" {
		return password
	}

	// RFC 7616 Section 6: PasswordPrep
	// Most passwords should be preserved as-is for compatibility.
	// Apply Unicode normalization (NFC form) to ensure consistent comparison.
	// Beyond that, minimal transformation to avoid breaking existing passwords.

	// Apply NFKC normalization for compatibility with internationalized passwords
	// But preserve the original as much as possible
	return password
}

// handleListRights implements RFC 4314 LISTRIGHTS command
func (s *Session) handleListRights(args []string) error {
	if len(args) < 2 {
		s.WriteResponse(s.tag, "BAD LISTRIGHTS requires mailbox and grantee")
		return nil
	}

	mailbox := args[0]
	grantee := args[1]

	// User must be authenticated
	if s.state == StateNotAuthenticated {
		s.WriteResponse(s.tag, "NO Not authenticated")
		return nil
	}

	// RFC 4314 §2.2.1 rights this server grants, in the same vocabulary
	// storage.ParseACLRights accepts (must stay in sync with it).
	standardRights := "l r s w i t e k"

	s.WriteData(fmt.Sprintf("LISTRIGHTS %s %s %s", mailbox, grantee, standardRights))
	s.WriteResponse(s.tag, "OK LISTRIGHTS completed")
	return nil
}

// parseOwnerMailbox parses mailbox name which may be in owner:mailbox format for shared mailboxes
func (s *Session) parseOwnerMailbox(mailbox string) (owner, name string, isShared bool) {
	parts := strings.SplitN(mailbox, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return s.user, mailbox, false
}

// Helper functions

func parseSearchCriteria(args []string) SearchCriteria {
	// Simplified search criteria parsing
	criteria := SearchCriteria{
		All: true,
	}

	for i := 0; i < len(args); i++ {
		i = parseSearchKey(&criteria, args, i)
	}

	return criteria
}

// parseSearchKey applies the single search key starting at args[i] to
// criteria and returns the index of its last token.
func parseSearchKey(criteria *SearchCriteria, args []string, i int) int {
	arg := strings.ToUpper(args[i])
	switch arg {
	case "ALL":
		criteria.All = true
	case "ANSWERED":
		criteria.Answered = true
	case "DELETED":
		criteria.Deleted = true
	case "FLAGGED":
		criteria.Flagged = true
	case "NEW":
		criteria.New = true
	case "OLD":
		criteria.Old = true
	case "RECENT":
		criteria.Recent = true
	case "SEEN":
		criteria.Seen = true
	case "UNANSWERED":
		criteria.Unanswered = true
	case "UNDELETED":
		criteria.Undeleted = true
	case "UNFLAGGED":
		criteria.Unflagged = true
	case "UNSEEN":
		criteria.Unseen = true
	case "FROM":
		if i+1 < len(args) {
			criteria.From = args[i+1]
			i++
		}
	case "SUBJECT":
		if i+1 < len(args) {
			criteria.Subject = args[i+1]
			i++
		}
	case "TO":
		if i+1 < len(args) {
			criteria.To = args[i+1]
			i++
		}
	case "UID":
		if i+1 < len(args) {
			criteria.UIDSet = args[i+1]
			i++
		}
	case "CC":
		if i+1 < len(args) {
			criteria.Cc = args[i+1]
			i++
		}
	case "BCC":
		if i+1 < len(args) {
			criteria.Bcc = args[i+1]
			i++
		}
	case "BODY":
		if i+1 < len(args) {
			criteria.Body = args[i+1]
			i++
		}
	case "TEXT":
		if i+1 < len(args) {
			criteria.Text = args[i+1]
			i++
		}
	case "HEADER":
		if i+2 < len(args) {
			if criteria.Header == nil {
				criteria.Header = make(map[string]string)
			}
			criteria.Header[args[i+1]] = args[i+2]
			i += 2
		}
	case "BEFORE":
		if i+1 < len(args) {
			if t, err := parseIMAPDate(args[i+1]); err == nil {
				criteria.Before = t
			}
			i++
		}
	case "ON":
		if i+1 < len(args) {
			if t, err := parseIMAPDate(args[i+1]); err == nil {
				criteria.On = t
			}
			i++
		}
	case "SINCE":
		if i+1 < len(args) {
			if t, err := parseIMAPDate(args[i+1]); err == nil {
				criteria.Since = t
			}
			i++
		}
	case "SENTBEFORE":
		if i+1 < len(args) {
			if t, err := parseIMAPDate(args[i+1]); err == nil {
				criteria.SentBefore = t
			}
			i++
		}
	case "SENTON":
		if i+1 < len(args) {
			if t, err := parseIMAPDate(args[i+1]); err == nil {
				criteria.SentOn = t
			}
			i++
		}
	case "SENTSINCE":
		if i+1 < len(args) {
			if t, err := parseIMAPDate(args[i+1]); err == nil {
				criteria.SentSince = t
			}
			i++
		}
	case "LARGER":
		if i+1 < len(args) {
			if size, err := strconv.ParseInt(args[i+1], 10, 64); err == nil {
				criteria.Larger = size
			}
			i++
		}
	case "SMALLER":
		if i+1 < len(args) {
			if size, err := strconv.ParseInt(args[i+1], 10, 64); err == nil {
				criteria.Smaller = size
			}
			i++
		}
	default:
		// RFC 3501 §6.4.4: a bare sequence-set is a search key (F5218).
		if isSequenceSetToken(arg) {
			criteria.SeqSet = arg
		}
	}

	return i
}

// imapToken is one argument token of a command line.
type imapToken struct {
	val    string
	quoted bool // a quoted string: never a keyword or parenthesis
}

// tokenizeIMAPArgs splits command arguments into atoms, unquoted
// quoted-strings and single "(" / ")" tokens (RFC 3501 §4). F5490/F5493:
// SEARCH and SORT used strings.Fields, so quotes stayed in the search
// strings and parenthesised lists were never recognised.
func tokenizeIMAPArgs(s string) []imapToken {
	var toks []imapToken
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t':
			i++
		case c == '(' || c == ')':
			toks = append(toks, imapToken{val: string(c)})
			i++
		case c == '"':
			var b strings.Builder
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
				i++
			}
			i++ // closing quote
			toks = append(toks, imapToken{val: b.String(), quoted: true})
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t()\"", rune(s[j])) {
				j++
			}
			toks = append(toks, imapToken{val: s[i:j]})
			i = j
		}
	}
	return toks
}

// commandArgTokens tokenizes the arguments that follow the command name cmd
// (e.g. "SEARCH" in "tag UID SEARCH ...") on the raw line, so quoted strings
// keep their spaces; without a line it falls back to the split args.
func commandArgTokens(args []string, line, cmd string) []imapToken {
	fields := strings.Fields(line)
	for n := 1; n < len(fields) && n <= 2; n++ {
		if strings.EqualFold(fields[n], cmd) {
			idx := 0
			for k := 0; k <= n; k++ {
				idx += strings.Index(line[idx:], fields[k]) + len(fields[k])
			}
			return tokenizeIMAPArgs(line[idx:])
		}
	}
	return tokenizeIMAPArgs(strings.Join(args, " "))
}

// searchExpr is a parsed SEARCH program: the plain keys are ANDed into flat
// (one mailstore scan) and every NOT / OR / parenthesised key is ANDed as a
// term (RFC 3501 §6.4.4). F5493: NOT, OR and "( )" were ignored, so
// "NOT FROM x" returned exactly the messages FROM x.
type searchExpr struct {
	flat  SearchCriteria
	terms []searchTerm
}

type searchTerm struct {
	not *searchExpr
	or  [2]*searchExpr
	sub *searchExpr
}

// errBadCharset reports a SEARCH CHARSET other than US-ASCII / UTF-8.
var errBadCharset = errors.New("unsupported charset")

const badCharsetResponse = "NO [BADCHARSET (US-ASCII UTF-8)] unsupported charset"

// searchCharsetSupported reports whether cs is a charset SEARCH, SORT and
// THREAD accept; the search strings are compared as UTF-8.
func searchCharsetSupported(cs string) bool {
	cs = strings.ToUpper(cs)
	return cs == "UTF-8" || cs == "US-ASCII"
}

// writeSearchParseError answers a search program parse error: NO
// [BADCHARSET] for an unsupported charset (RFC 3501 §6.4.4), else BAD.
func (s *Session) writeSearchParseError(err error) {
	if errors.Is(err, errBadCharset) {
		s.WriteResponse(s.tag, badCharsetResponse)
		return
	}
	s.WriteResponse(s.tag, fmt.Sprintf("BAD %s", err))
}

// checkSearchDateArg rejects a date search key whose argument is missing
// or not an RFC 3501 date. F5566: such a key was dropped, so it matched
// every message ("BEFORE 1-Feb-2000" selected the whole mailbox).
func checkSearchDateArg(vals []string) error {
	switch strings.ToUpper(vals[0]) {
	case "BEFORE", "ON", "SINCE", "SENTBEFORE", "SENTON", "SENTSINCE":
		if len(vals) < 2 {
			return fmt.Errorf("missing date for %s", vals[0])
		}
		if _, err := parseIMAPDate(vals[1]); err != nil {
			return fmt.Errorf("invalid date %q", vals[1])
		}
	}
	return nil
}

// parseSearchProgram parses search keys (after an optional CHARSET).
// F5563: an unsupported CHARSET was skipped and the search answered OK.
func parseSearchProgram(toks []imapToken) (*searchExpr, error) {
	if len(toks) >= 2 && !toks[0].quoted && strings.EqualFold(toks[0].val, "CHARSET") {
		if !searchCharsetSupported(toks[1].val) {
			return nil, errBadCharset
		}
		toks = toks[2:]
	}
	e, next, err := parseSearchSeq(toks, 0, false)
	if err == nil && next != len(toks) {
		err = fmt.Errorf("unexpected %q", toks[next].val)
	}
	return e, err
}

// parseSearchSeq parses keys until the end or, inside a group, ")".
func parseSearchSeq(toks []imapToken, i int, inGroup bool) (*searchExpr, int, error) {
	e := &searchExpr{flat: SearchCriteria{All: true}}
	for i < len(toks) {
		t := toks[i]
		if !t.quoted && t.val == ")" {
			if !inGroup {
				return nil, i, fmt.Errorf("unbalanced parenthesis")
			}
			return e, i + 1, nil
		}
		if !t.quoted && (t.val == "(" || strings.EqualFold(t.val, "NOT") || strings.EqualFold(t.val, "OR")) {
			sub, next, err := parseSearchOne(toks, i)
			if err != nil {
				return nil, next, err
			}
			e.terms = append(e.terms, searchTerm{sub: sub})
			i = next
			continue
		}
		vals := plainKeyArgs(toks, i)
		if err := checkSearchDateArg(vals); err != nil {
			return nil, i, err
		}
		i += parseSearchKey(&e.flat, vals, 0) + 1
	}
	if inGroup {
		return nil, i, fmt.Errorf("unbalanced parenthesis")
	}
	return e, i, nil
}

// parseSearchOne parses exactly one search key starting at toks[i].
func parseSearchOne(toks []imapToken, i int) (*searchExpr, int, error) {
	if i >= len(toks) {
		return nil, i, fmt.Errorf("missing search key")
	}
	t := toks[i]
	switch {
	case !t.quoted && t.val == "(":
		return parseSearchSeq(toks, i+1, true)
	case !t.quoted && strings.EqualFold(t.val, "NOT"):
		x, next, err := parseSearchOne(toks, i+1)
		if err != nil {
			return nil, next, err
		}
		return &searchExpr{flat: SearchCriteria{All: true}, terms: []searchTerm{{not: x}}}, next, nil
	case !t.quoted && strings.EqualFold(t.val, "OR"):
		a, next, err := parseSearchOne(toks, i+1)
		if err != nil {
			return nil, next, err
		}
		b, next, err := parseSearchOne(toks, next)
		if err != nil {
			return nil, next, err
		}
		return &searchExpr{flat: SearchCriteria{All: true}, terms: []searchTerm{{or: [2]*searchExpr{a, b}}}}, next, nil
	case !t.quoted && t.val == ")":
		return nil, i, fmt.Errorf("missing search key")
	default:
		vals := plainKeyArgs(toks, i)
		if err := checkSearchDateArg(vals); err != nil {
			return nil, i, err
		}
		e := &searchExpr{flat: SearchCriteria{All: true}}
		return e, i + parseSearchKey(&e.flat, vals, 0) + 1, nil
	}
}

// plainKeyArgs returns the values of the (at most three: HEADER name value)
// tokens a plain search key at toks[i] may use; a key's arguments never
// span a parenthesis.
func plainKeyArgs(toks []imapToken, i int) []string {
	vals := make([]string, 0, 3)
	for k := i; k < len(toks) && len(vals) < 3; k++ {
		if !toks[k].quoted && (toks[k].val == "(" || toks[k].val == ")") {
			break
		}
		vals = append(vals, toks[k].val)
	}
	return vals
}

// evalSearch returns the ascending sequence numbers matching e.
func (s *Session) evalSearch(e *searchExpr) ([]uint32, error) {
	base, err := s.server.mailstore.SearchMessages(s.user, s.selected.Name, e.flat)
	if err != nil {
		return nil, err
	}
	for _, t := range e.terms {
		if len(base) == 0 {
			break
		}
		switch {
		case t.not != nil:
			x, err := s.evalSearch(t.not)
			if err != nil {
				return nil, err
			}
			base = seqFilter(base, seqSetOf(x), false)
		case t.sub != nil:
			x, err := s.evalSearch(t.sub)
			if err != nil {
				return nil, err
			}
			base = seqFilter(base, seqSetOf(x), true)
		default:
			a, err := s.evalSearch(t.or[0])
			if err != nil {
				return nil, err
			}
			b, err := s.evalSearch(t.or[1])
			if err != nil {
				return nil, err
			}
			either := seqSetOf(a)
			for _, n := range b {
				either[n] = true
			}
			base = seqFilter(base, either, true)
		}
	}
	return base, nil
}

func seqSetOf(nums []uint32) map[uint32]bool {
	m := make(map[uint32]bool, len(nums))
	for _, n := range nums {
		m[n] = true
	}
	return m
}

// seqFilter keeps the numbers whose membership in set equals keep.
func seqFilter(nums []uint32, set map[uint32]bool, keep bool) []uint32 {
	out := nums[:0:0]
	for _, n := range nums {
		if set[n] == keep {
			out = append(out, n)
		}
	}
	return out
}

// isSequenceSetToken reports whether tok is a syntactically valid IMAP
// sequence-set (digits, ':', ',', '*').
func isSequenceSetToken(tok string) bool {
	if tok == "" || strings.Trim(tok, "0123456789:,*") != "" {
		return false
	}
	_, err := ParseSequenceSet(tok)
	return err == nil
}

// parseIMAPDate parses an RFC 3501 date: date-day is 1*2DIGIT, so both
// "01-Jan-2024" and "1-Jan-2024" are valid (F5566).
func parseIMAPDate(dateStr string) (time.Time, error) {
	return time.Parse("2-Jan-2006", dateStr)
}

func parseFetchItems(args []string) []string {
	// Join args and split by space
	itemsStr := strings.Join(args, " ")

	// Handle parenthesized list
	if strings.HasPrefix(itemsStr, "(") && strings.HasSuffix(itemsStr, ")") {
		itemsStr = itemsStr[1 : len(itemsStr)-1]
	}

	// Bracket-aware split: BODY[HEADER.FIELDS (FROM TO)] is one item (F5067).
	return splitFetchItems(itemsStr)
}

func parseFlags(flagsStr string) []string {
	// Remove parentheses if present
	flagsStr = strings.Trim(flagsStr, "()")

	flags := []string{}
	for _, f := range strings.Fields(flagsStr) {
		// Keep the leading backslash: "\Deleted" is a system flag, "Deleted"
		// is an unrelated keyword (RFC 3501 §2.3.2). Stripping it made STORE
		// +FLAGS (\Deleted) invisible to EXPUNGE and SEARCH (F5066).
		if f != "" && f != "\\" {
			flags = append(flags, f)
		}
	}
	return flags
}

func formatFetchResponse(msg *Message, items []string) string {
	var parts []string

	for _, item := range items {
		item = strings.ToUpper(item)
		switch item {
		case "FLAGS":
			parts = append(parts, fmt.Sprintf("FLAGS (%s)", strings.Join(msg.Flags, " ")))
		case "INTERNALDATE":
			parts = append(parts, fmt.Sprintf("INTERNALDATE \"%s\"", msg.InternalDate.Format("02-Jan-2006 15:04:05 -0700")))
		case "RFC822.SIZE":
			parts = append(parts, fmt.Sprintf("RFC822.SIZE %d", msg.Size))
		case "UID":
			parts = append(parts, fmt.Sprintf("UID %d", msg.UID))
		case "RFC822":
			parts = append(parts, fmt.Sprintf("RFC822 {%d}\r\n%s", len(msg.Data), string(msg.Data)))
		case "BODY", "BODYSTRUCTURE":
			// F5496: describe the real MIME tree; BODY is the non-extensible form.
			parts = append(parts, item+" "+bodyStructure(msg.Data, item == "BODYSTRUCTURE"))
		case "ENVELOPE":
			// F5642: RFC 3501 §7.4.2 field order and address lists; the
			// old text put Subject before Date and one raw header per name.
			parts = append(parts, "ENVELOPE "+imapEnvelope(msg.envelopeHeader()))
		default:
			if it, ok := parseBodySectionItem(item); ok {
				parts = append(parts, it.format(msg.Data))
			}
		}
	}

	return strings.Join(parts, " ")
}

// splitAddress safely splits an email address into local and domain parts.
// If the address contains no "@", the domain is returned as empty.
func splitAddress(addr string) (local, domain string) {
	if atIdx := strings.LastIndex(addr, "@"); atIdx >= 0 {
		return addr[:atIdx], addr[atIdx+1:]
	}
	return addr, ""
}
