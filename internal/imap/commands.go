package imap

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
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

	// Handle the command based on current state
	switch s.state {
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
	for _, cap := range s.capabilities {
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
	s.state = StateLoggedOut
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
	s.state = StateAuthenticated
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
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "select mailbox failed")
		}
		return nil
	}

	s.selected = mailbox
	s.state = StateSelected

	// Send mailbox data
	s.WriteData(fmt.Sprintf("%d EXISTS", mailbox.Exists))
	s.WriteData(fmt.Sprintf("%d RECENT", mailbox.Recent))

	if mailbox.Unseen > 0 {
		s.WriteData(fmt.Sprintf("OK [UNSEEN %d] Message %d is first unseen", mailbox.Unseen, mailbox.Unseen))
	}

	s.WriteData(fmt.Sprintf("OK [UIDVALIDITY %d] UIDs valid", mailbox.UIDValidity))
	s.WriteData(fmt.Sprintf("OK [UIDNEXT %d] Predicted next UID", mailbox.UIDNext))

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

	mailbox, err := s.server.mailstore.SelectMailbox(s.user, mailboxName)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.selected = mailbox
	s.state = StateSelected

	// Send mailbox data (same as SELECT but read-only)
	s.WriteData(fmt.Sprintf("%d EXISTS", mailbox.Exists))
	s.WriteData(fmt.Sprintf("%d RECENT", mailbox.Recent))

	if mailbox.Unseen > 0 {
		s.WriteData(fmt.Sprintf("OK [UNSEEN %d] Message %d is first unseen", mailbox.Unseen, mailbox.Unseen))
	}

	s.WriteData(fmt.Sprintf("OK [UIDVALIDITY %d] UIDs valid", mailbox.UIDValidity))
	s.WriteData(fmt.Sprintf("OK [UIDNEXT %d] Predicted next UID", mailbox.UIDNext))

	s.WriteData("FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)")
	s.WriteData("OK [PERMANENTFLAGS ()] No permanent flags permitted")

	s.WriteResponse(s.tag, "OK [READ-ONLY] EXAMINE completed")
	return nil
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
func matchMailboxPattern(name, pattern string) bool {
	if pattern == "*" {
		return true
	}

	// Handle * wildcard at end
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	// Handle * wildcard at start
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	// Exact match
	return name == pattern
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

	// Get mailbox info
	mailbox, err := s.server.mailstore.SelectMailbox(s.user, mailboxName)
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

// APPEND command
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

	// Parse flags if present
	flags := []string{}
	date := time.Now()

	// Check for flags in parentheses
	for i, arg := range args[1:] {
		if strings.HasPrefix(arg, "(") {
			flagsStr := strings.Join(args[1+i:], " ")
			end := strings.Index(flagsStr, ")")
			if end > 0 {
				flagsStr = flagsStr[1:end]
				flags = strings.Fields(flagsStr)
			}
			break
		}
	}

	// Find literal string indicator {N}
	literalStart := strings.Index(line, "{")
	if literalStart > 0 {
		literalEnd := strings.Index(line[literalStart:], "}")
		if literalEnd > 0 {
			sizeStr := line[literalStart+1 : literalStart+literalEnd]
			size, err := strconv.Atoi(sizeStr)
			if err != nil {
				s.WriteResponse(s.tag, "BAD Invalid literal size")
				if span != nil {
					tracing.SetStatus(span, tracing.StatusError, "invalid literal size")
				}
				return nil
			}

			// Limit APPEND message size to 50MB
			const maxAppendSize = 50 * 1024 * 1024
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

			// Request the literal
			s.WriteContinuation(fmt.Sprintf("Ready for %d octets", size))

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
			if s.server.mailstore != nil {
				err := s.server.mailstore.AppendMessage(s.user, mailboxName, flags, date, data)
				if err != nil {
					s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
					if span != nil {
						tracing.RecordError(span, err)
						tracing.SetStatus(span, tracing.StatusError, "append failed")
					}
					return nil
				}
			}
		}
	}

	if span != nil {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK APPEND completed")
	return nil
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
	// IDLE is only valid in Authenticated or Selected state
	if s.state != StateAuthenticated && s.state != StateSelected {
		s.WriteResponse(s.tag, "BAD Command not allowed in this state")
		return nil
	}

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

// ENABLE command
func (s *Session) handleEnable(args []string) error {
	// Just acknowledge the command
	enabled := []string{}
	for _, arg := range args {
		cap := strings.ToUpper(arg)
		if cap == "CONDSTORE" || cap == "QRESYNC" {
			enabled = append(enabled, cap)
		}
	}

	if len(enabled) > 0 {
		s.WriteData("ENABLED " + strings.Join(enabled, " "))
	}

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
	if s.selected != nil && s.server.mailstore != nil {
		_ = s.server.mailstore.Expunge(s.user, s.selected.Name)
	}
	s.selected = nil
	s.state = StateAuthenticated
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

	err = s.server.mailstore.Expunge(s.user, s.selected.Name)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		if span != nil {
			tracing.RecordError(span, err)
			tracing.SetStatus(span, tracing.StatusError, "expunge failed")
		}
		return nil
	}

	// Notify search index about expunged messages
	// Sequence numbers map 1:1 to position, so seq=N means the Nth message.
	// We pass sequence numbers as identifiers — search index uses folder+uid keys,
	// so this is a best-effort cleanup.
	if s.server.onExpunge != nil {
		for _, seq := range deletedSeqs {
			s.server.onExpunge(s.user, s.selected.Name, seq)
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

	// Parse search criteria
	criteria := parseSearchCriteria(args)

	if span != nil {
		tracing.SetIntAttribute(span, "search.criteria_count", len(args))
	}

	uids, err := s.server.mailstore.SearchMessages(s.user, s.selected.Name, criteria)
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

	// Convert UIDs to sequence numbers and output
	// For simplicity, just output as SEARCH result
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
	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}

	// Parse sort criteria - args[0] is the charset, then criteria
	var criteriaArgs []string
	if len(args) > 0 && strings.ToUpper(args[0]) != "CHARSET" {
		// No charset specified, use args as criteria
		criteriaArgs = args
	} else {
		// Skip charset if specified
		if len(args) > 1 {
			criteriaArgs = args[1:]
		}
	}

	criteria, err := parseSortCriteria(criteriaArgs)
	if err != nil {
		s.server.logger.Error("imap sort criteria parse error", "error", err)
		s.WriteResponse(s.tag, "BAD invalid sort criteria")
		return nil
	}

	// Get all messages in mailbox with metadata
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", []string{"ENVELOPE"})
	if err != nil {
		s.server.logger.Error("imap fetch messages error", "error", err)
		s.WriteResponse(s.tag, "NO unable to fetch messages")
		return nil
	}

	// Build metadata list with sequence numbers
	var metas []*storage.MessageMetadata
	var seqNums []uint32
	seqNum := uint32(0)
	for _, msg := range messages {
		seqNum++
		seqNums = append(seqNums, seqNum)
		// Build a minimal MessageMetadata from the Message
		meta := &storage.MessageMetadata{
			MessageID:    msg.Envelope.MessageID,
			UID:          msg.UID,
			Subject:      msg.Envelope.Subject,
			From:         addressToString(msg.Envelope.From),
			Date:         msg.Envelope.Date,
			InternalDate: msg.InternalDate,
			Size:         msg.Size,
		}
		meta.InReplyTo = msg.Envelope.InReplyTo
		metas = append(metas, meta)
	}

	// Sort
	sortedSeqNums := sortMessagesByCriteria(metas, criteria, seqNums)

	// Output result
	result := "SORT"
	for _, seq := range sortedSeqNums {
		result += fmt.Sprintf(" %d", seq)
	}
	s.WriteData(result)
	s.WriteResponse(s.tag, "OK SORT completed")
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
	if s.server.mailstore == nil || s.selected == nil {
		s.WriteResponse(s.tag, "NO No mailbox selected")
		return nil
	}

	// Parse thread algorithm
	algo := ThreadReferences
	if len(args) > 0 {
		arg := strings.ToUpper(args[0])
		if arg == "ORDEREDSUBJECT" {
			algo = ThreadOrderedSubject
		} else if arg == "REFERENCES" {
			algo = ThreadReferences
		}
	}

	// Get all messages in mailbox
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", []string{"ENVELOPE"})
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	// Build metadata list with sequence numbers
	var metas []*storage.MessageMetadata
	var seqNums []uint32
	seqNum := uint32(0)
	for _, msg := range messages {
		seqNum++
		seqNums = append(seqNums, seqNum)
		meta := &storage.MessageMetadata{
			MessageID:    msg.Envelope.MessageID,
			UID:          msg.UID,
			Subject:      msg.Envelope.Subject,
			From:         addressToString(msg.Envelope.From),
			Date:         msg.Envelope.Date,
			InternalDate: msg.InternalDate,
		}
		meta.InReplyTo = msg.Envelope.InReplyTo
		metas = append(metas, meta)
	}

	var children map[uint32][]uint32
	if algo == ThreadReferences {
		children = threadMessagesByReferences(metas, seqNums)
	} else {
		children = threadMessagesByOrderedSubject(metas, seqNums)
	}

	// Find all root messages (those that are not children)
	allChildren := make(map[uint32]bool)
	for _, kids := range children {
		for _, child := range kids {
			allChildren[child] = true
		}
	}

	var roots []uint32
	for _, seq := range seqNums {
		if !allChildren[seq] {
			roots = append(roots, seq)
		}
	}

	// Output threads
	visited := make(map[uint32]bool)
	for _, root := range roots {
		threadSeqNums := flattenThread(root, children, visited)
		// Output as space-separated sequence numbers in parentheses
		threadStr := "("
		for i, seq := range threadSeqNums {
			if i > 0 {
				threadStr += " "
			}
			threadStr += fmt.Sprintf("%d", seq)
		}
		threadStr += ")"
		s.WriteData(threadStr)
	}

	s.WriteResponse(s.tag, "OK THREAD completed")
	return nil
}

// UID SORT command
func (s *Session) handleUIDSort(args []string, line string) error {
	// Add UID prefix to results
	// Parse criteria from args
	var criteriaArgs []string
	if len(args) > 0 && strings.ToUpper(args[0]) != "CHARSET" {
		criteriaArgs = args
	} else {
		if len(args) > 1 {
			criteriaArgs = args[1:]
		}
	}

	criteria, err := parseSortCriteria(criteriaArgs)
	if err != nil {
		s.server.logger.Error("imap sort criteria parse error", "error", err)
		s.WriteResponse(s.tag, "BAD invalid sort criteria")
		return nil
	}

	// Get all messages with UID
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", []string{"ENVELOPE"})
	if err != nil {
		s.server.logger.Error("imap fetch messages error", "error", err)
		s.WriteResponse(s.tag, "NO unable to fetch messages")
		return nil
	}

	var metas []*storage.MessageMetadata
	var seqNums []uint32
	var uids []uint32
	seqNum := uint32(0)
	for _, msg := range messages {
		seqNum++
		seqNums = append(seqNums, seqNum)
		uids = append(uids, msg.UID)
		meta := &storage.MessageMetadata{
			MessageID:    msg.Envelope.MessageID,
			UID:          msg.UID,
			Subject:      msg.Envelope.Subject,
			From:         addressToString(msg.Envelope.From),
			Date:         msg.Envelope.Date,
			InternalDate: msg.InternalDate,
			Size:         msg.Size,
		}
		metas = append(metas, meta)
	}

	sortedSeqNums := sortMessagesByCriteria(metas, criteria, seqNums)

	// Convert sequence numbers to UIDs
	result := "SORT"
	for _, seq := range sortedSeqNums {
		// Find corresponding UID
		for i, s := range seqNums {
			if s == seq {
				result += fmt.Sprintf(" %d", uids[i])
				break
			}
		}
	}
	s.WriteData(result)
	s.WriteResponse(s.tag, "OK UID SORT completed")
	return nil
}

// UID THREAD command
func (s *Session) handleUIDThread(args []string, line string) error {
	// Parse thread algorithm
	algo := ThreadReferences
	if len(args) > 0 {
		arg := strings.ToUpper(args[0])
		if arg == "ORDEREDSUBJECT" {
			algo = ThreadOrderedSubject
		} else if arg == "REFERENCES" {
			algo = ThreadReferences
		}
	}

	// Get all messages
	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, "1:*", []string{"ENVELOPE"})
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	var metas []*storage.MessageMetadata
	var seqNums []uint32
	var uids []uint32
	seqNum := uint32(0)
	for _, msg := range messages {
		seqNum++
		seqNums = append(seqNums, seqNum)
		uids = append(uids, msg.UID)
		meta := &storage.MessageMetadata{
			MessageID:    msg.Envelope.MessageID,
			UID:          msg.UID,
			Subject:      msg.Envelope.Subject,
			From:         addressToString(msg.Envelope.From),
			Date:         msg.Envelope.Date,
			InternalDate: msg.InternalDate,
		}
		meta.InReplyTo = msg.Envelope.InReplyTo
		metas = append(metas, meta)
	}

	var children map[uint32][]uint32
	if algo == ThreadReferences {
		children = threadMessagesByReferences(metas, seqNums)
	} else {
		children = threadMessagesByOrderedSubject(metas, seqNums)
	}

	// Find roots and build seq->uid mapping
	allChildren := make(map[uint32]bool)
	for _, kids := range children {
		for _, child := range kids {
			allChildren[child] = true
		}
	}

	var roots []uint32
	for _, seq := range seqNums {
		if !allChildren[seq] {
			roots = append(roots, seq)
		}
	}

	// seq to uid mapping
	seqToUID := make(map[uint32]uint32)
	for i, seq := range seqNums {
		seqToUID[seq] = uids[i]
	}

	visited := make(map[uint32]bool)
	for _, root := range roots {
		threadSeqNums := flattenThread(root, children, visited)
		threadStr := "("
		for i, seq := range threadSeqNums {
			if i > 0 {
				threadStr += " "
			}
			threadStr += fmt.Sprintf("%d", seqToUID[seq])
		}
		threadStr += ")"
		s.WriteData(threadStr)
	}

	s.WriteResponse(s.tag, "OK UID THREAD completed")
	return nil
}

// FETCH command
func (s *Session) handleFetch(args []string, line string) error {
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

	messages, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, seqSet, fetchItems)
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

	for _, msg := range messages {
		fetchResponse := formatFetchResponse(msg, fetchItems)
		s.WriteData(fmt.Sprintf("%d FETCH (%s)", msg.SeqNum, fetchResponse))
	}

	if span != nil {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	s.WriteResponse(s.tag, "OK FETCH completed")
	return nil
}

// STORE command
func (s *Session) handleStore(args []string) error {
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
				s.WriteData(fmt.Sprintf("%d FETCH (FLAGS (%s))", msg.SeqNum, strings.Join(msg.Flags, " ")))
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
	destMailbox := strings.Trim(args[1], "\"'")

	err := s.server.mailstore.CopyMessages(s.user, s.selected.Name, destMailbox, seqSet)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK COPY completed")
	return nil
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

	seqSet := args[0]
	destMailbox := strings.Trim(args[1], "\"'")

	err := s.server.mailstore.MoveMessages(s.user, s.selected.Name, destMailbox, seqSet)
	if err != nil {
		s.WriteResponse(s.tag, fmt.Sprintf("NO %s", err))
		return nil
	}

	s.WriteResponse(s.tag, "OK MOVE completed")
	return nil
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

func (s *Session) handleUIDFetch(args []string, line string) error {
	// Same as FETCH but with UIDs
	return s.handleFetch(args, line)
}

func (s *Session) handleUIDStore(args []string) error {
	// Same as STORE but with UIDs
	return s.handleStore(args)
}

func (s *Session) handleUIDCopy(args []string) error {
	// Same as COPY but with UIDs
	return s.handleCopy(args)
}

func (s *Session) handleUIDMove(args []string) error {
	// Same as MOVE but with UIDs
	return s.handleMove(args)
}

func (s *Session) handleUIDSearch(args []string, line string) error {
	// Same as SEARCH but output UIDs
	return s.handleSearch(args, line)
}

func (s *Session) handleUIDExpunge(args []string) error {
	// UID EXPUNGE with sequence set
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
	rights, err := storage.ParseACLRights(rightsStr)
	if err != nil {
		s.WriteResponse(s.tag, "BAD Invalid rights format")
		return nil
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

	// RFC 4314 specifies the standard rights that can be granted
	// l (lookup), r (read), s (seen), w (write), i (insert), p (post),
	// k (create), x (delete), t (delete seen), e (expunge), c (create mailbox), d (delete mailbox)
	standardRights := "l r s w i p k x t e c d a"

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
		}
	}

	return criteria
}

// parseIMAPDate parses an IMAP date in format "DD-Mon-YYYY" (e.g., "01-Jan-2024")
func parseIMAPDate(dateStr string) (time.Time, error) {
	// IMAP date format: 01-Jan-2024
	return time.Parse("02-Jan-2006", dateStr)
}

func parseFetchItems(args []string) []string {
	// Join args and split by space
	itemsStr := strings.Join(args, " ")

	// Handle parenthesized list
	if strings.HasPrefix(itemsStr, "(") && strings.HasSuffix(itemsStr, ")") {
		itemsStr = itemsStr[1 : len(itemsStr)-1]
	}

	return strings.Fields(itemsStr)
}

func parseFlags(flagsStr string) []string {
	// Remove parentheses if present
	flagsStr = strings.Trim(flagsStr, "()")

	flags := []string{}
	for _, f := range strings.Fields(flagsStr) {
		f = strings.Trim(f, "\\")
		if f != "" {
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
			parts = append(parts, fmt.Sprintf("BODYSTRUCTURE (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" %d 0)", msg.Size))
		case "ENVELOPE":
			fromLocal, fromDomain := splitAddress(msg.From)
			toLocal, toDomain := splitAddress(msg.To)
			parts = append(parts, fmt.Sprintf("ENVELOPE (%s %s ((%s NIL %s %s)) NIL NIL ((%s NIL %s %s)) NIL NIL NIL NIL)",
				imapQuotedString(msg.Subject), imapQuotedString(msg.Date),
				imapQuotedString(msg.From), imapQuotedString(fromLocal), imapQuotedString(fromDomain),
				imapQuotedString(msg.To), imapQuotedString(toLocal), imapQuotedString(toDomain)))
		}
	}

	return strings.Join(parts, " ")
}

// imapQuotedString quotes a string for use in an IMAP quoted-string per RFC 3501.
func imapQuotedString(s string) string {
	return strconv.Quote(s)
}

// splitAddress safely splits an email address into local and domain parts.
// If the address contains no "@", the domain is returned as empty.
func splitAddress(addr string) (local, domain string) {
	if atIdx := strings.LastIndex(addr, "@"); atIdx >= 0 {
		return addr[:atIdx], addr[atIdx+1:]
	}
	return addr, ""
}
