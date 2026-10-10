// Package sieve implements RFC 5228 - Sieve: An Email Filtering Language
package sieve

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

// ManageSieveListenAddr is the default ManageSieve server address
const ManageSieveListenAddr = "0.0.0.0:4190"

// ManageSieveTLSListenAddr is the TLS listener address
const ManageSieveTLSListenAddr = "0.0.0.0:4191"

// ManageSieveServer implements RFC 5804 - Protocol for Managing Sieve Scripts
type ManageSieveServer struct {
	ln          net.Listener
	tlsLn       net.Listener
	tlsCfg      *tls.Config
	manager     *Manager
	done        chan struct{}
	wg          sync.WaitGroup
	mu          sync.Mutex
	running     bool
	authHandler func(user, pass string) bool // Auth validation function

	// userResolver maps an authenticated login to the key its scripts are
	// stored under (SetUserResolver). F5631.
	userResolver func(login string) string

	// addr and tlsAddr override ManageSieveListenAddr and
	// ManageSieveTLSListenAddr when set (SetListenAddrs).
	addr    string
	tlsAddr string

	// tracingProvider wraps every command in a `managesieve.<COMMAND>`
	// server-kind span when set.
	tracingProvider *tracing.Provider
}

// NewManageSieveServer creates a new ManageSieve server
func NewManageSieveServer(manager *Manager, tlsCfg *tls.Config) *ManageSieveServer {
	return &ManageSieveServer{
		manager: manager,
		tlsCfg:  tlsCfg,
		done:    make(chan struct{}),
	}
}

// SetAuthHandler sets the authentication handler for ManageSieve
func (s *ManageSieveServer) SetAuthHandler(handler func(user, pass string) bool) {
	s.authHandler = handler
}

// SetUserResolver sets the function that maps an authenticated login to the
// canonical user key scripts are stored under, so ManageSieve and mail
// delivery agree on one identity (an LDAP uid login "jdoe" is mailbox
// jdoe@example.com). Without it the login string is the key. F5631.
func (s *ManageSieveServer) SetUserResolver(resolve func(login string) string) {
	s.userResolver = resolve
}

// sessionUser returns the script key for an authenticated login. F5631.
func (s *ManageSieveServer) sessionUser(login string) string {
	if s.userResolver != nil {
		if key := s.userResolver(login); key != "" {
			return key
		}
	}
	return login
}

// SetListenAddrs sets the plain and TLS listen addresses used by Listen;
// an empty value keeps the default. The server wires the configured
// managesieve bind/port here (F5116).
func (s *ManageSieveServer) SetListenAddrs(addr, tlsAddr string) {
	s.addr = addr
	s.tlsAddr = tlsAddr
}

// SetTracingProvider wires an OpenTelemetry provider so every command is
// wrapped in a `managesieve.<COMMAND>` span.
func (s *ManageSieveServer) SetTracingProvider(provider *tracing.Provider) {
	s.tracingProvider = provider
}

// Listen starts the ManageSieve server
func (s *ManageSieveServer) Listen() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("ManageSieve server already running")
	}
	s.running = true
	s.mu.Unlock()

	// Start plain TCP listener
	// #nosec G102 -- Bind address is a configurable default constant
	addr := ManageSieveListenAddr
	if s.addr != "" {
		addr = s.addr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	s.ln = ln
	s.wg.Add(1)
	go s.serve(ln)

	// Start TLS listener if TLS config is provided (on separate port)
	if s.tlsCfg != nil {
		// #nosec G102 -- Bind address is a configurable default constant
		tlsAddr := ManageSieveTLSListenAddr
		if s.tlsAddr != "" {
			tlsAddr = s.tlsAddr
		}
		tlsLn, err := tls.Listen("tcp", tlsAddr, s.tlsCfg)
		if err != nil {
			return fmt.Errorf("failed to start TLS listener: %w", err)
		}
		s.tlsLn = tlsLn
		s.wg.Add(1)
		go s.serveTLS(tlsLn)
	}

	return nil
}

// serve handles plain TCP connections
func (s *ManageSieveServer) serve(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// serveTLS handles TLS connections
func (s *ManageSieveServer) serveTLS(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn handles a ManageSieve connection
func (s *ManageSieveServer) handleConn(conn net.Conn) {
	defer s.wg.Done()

	// Create session state for this connection
	_, isTLS := conn.(*tls.Conn)
	session := &manageSieveSession{
		conn:    conn,
		reader:  &manageSieveReader{r: conn},
		user:    "", // Not authenticated yet
		manager: s.manager,
		tls:     isTLS,
	}
	// F5461: STARTTLS replaces session.conn; close whichever is current.
	defer func() { _ = session.conn.Close() }()

	// Set read timeout
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // Best-effort

	// F5460: RFC 5804 §1.7 — the greeting is the capability list then OK.
	if err := s.sendCapabilities(session, "OK \"ManageSieve server ready\""); err != nil {
		return
	}

	for {
		line, err := session.reader.ReadLine()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}

		// Reset read deadline on command
		_ = session.conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // Best-effort

		err = s.processCommandSession(session, line)
		if err == nil {
			continue
		}
		if err == io.EOF { // LOGOUT already answered OK
			return
		}
		slog.Error("managesieve command error", "error", err)
		// F5462: a failed command is answered with NO and the session
		// continues (RFC 5804 §1.3), unless the stream cannot be trusted.
		if !s.sendNo(session, err) {
			return
		}
	}
}

// manageSieveNo is a command failure answered with an RFC 5804 NO response.
// code is an optional response code (§1.3); closeConn ends the session after
// the NO, for failures that leave the stream out of sync or failed
// authentication. F5462.
type manageSieveNo struct {
	code      string
	msg       string
	closeConn bool
}

func (e *manageSieveNo) Error() string { return e.msg }

// sendNo answers err with a NO response and reports whether the session may
// continue.
func (s *ManageSieveServer) sendNo(session *manageSieveSession, err error) bool {
	resp := "NO "
	closeConn := false
	var no *manageSieveNo
	if errors.As(err, &no) {
		if no.code != "" {
			resp += "(" + no.code + ") "
		}
		closeConn = no.closeConn
	}
	msg := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
	if werr := s.sendResponse(session.conn, "%s", resp+quoteManageSieveString(msg)); werr != nil {
		return false
	}
	return !closeConn
}

// sendCapabilities writes the RFC 5804 §1.7 capability response followed by
// the final line ok. F5460.
func (s *ManageSieveServer) sendCapabilities(session *manageSieveSession, ok string) error {
	exts := make([]string, 0, len(supportedExtensions))
	for ext := range supportedExtensions {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	// F5614: no cleartext-password mechanism is offered while STARTTLS is
	// still available (RFC 5804 §2.1; IMAP LOGINDISABLED / SMTP 538 here).
	sasl := `"SASL" "PLAIN LOGIN"`
	if s.authNeedsTLS(session) {
		sasl = `"SASL" ""`
	}
	lines := []string{
		`"IMPLEMENTATION" "uMailServer"`,
		sasl,
		`"SIEVE" ` + quoteManageSieveString(strings.Join(exts, " ")),
	}
	if s.tlsCfg != nil && !session.tls {
		lines = append(lines, `"STARTTLS"`)
	}
	lines = append(lines, `"VERSION" "1.0"`, ok)
	return s.sendResponse(session.conn, "%s", strings.Join(lines, "\r\n"))
}

// authNeedsTLS reports whether AUTHENTICATE must wait for STARTTLS: TLS is
// configured but this connection is still cleartext. F5614.
func (s *ManageSieveServer) authNeedsTLS(session *manageSieveSession) bool {
	return s.tlsCfg != nil && !session.tls
}

// manageSieveSession holds state for a single ManageSieve session
type manageSieveSession struct {
	conn    net.Conn
	reader  *manageSieveReader
	user    string // Authenticated username
	manager *Manager
	tls     bool // connection is TLS (implicit or after STARTTLS)
}

type manageSieveReader struct {
	r io.Reader
}

// maxManageSieveLineLength bounds one command line (excluding CRLF). F5009:
// without it an unauthenticated peer could make the server buffer an
// arbitrarily long line.
const maxManageSieveLineLength = 8192

func (r *manageSieveReader) ReadLine() (string, error) {
	var line []byte
	for {
		b := make([]byte, 1)
		n, err := r.r.Read(b)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", io.EOF
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
		if len(line) > maxManageSieveLineLength+1 { // +1 for a trailing CR
			return "", fmt.Errorf("command line too long")
		}
	}
	text := strings.TrimRight(string(line), "\r")
	if len(text) > maxManageSieveLineLength {
		return "", fmt.Errorf("command line too long")
	}
	return text, nil
}

// processCommandSession processes a single ManageSieve command using session state
func (s *ManageSieveServer) processCommandSession(session *manageSieveSession, line string) error {
	// Parse command and arguments
	parts := parseManageSieveLine(line)
	if len(parts) == 0 {
		return fmt.Errorf("invalid command")
	}

	cmd := strings.ToUpper(parts[0])
	args := parts[1:]

	span := s.startCommandSpan(cmd, session)
	defer span.End()
	if session.user != "" {
		tracing.SetStringAttribute(span, "user", session.user)
	}

	err := s.dispatchCommand(session, cmd, args)
	if err != nil && err != io.EOF {
		tracing.SetStatus(span, tracing.StatusError, err.Error())
	}
	if cmd == "AUTHENTICATE" {
		tracing.SetBoolAttribute(span, "auth.success", err == nil && session.user != "")
	}
	return err
}

// dispatchCommand runs the actual command handler. Split out from
// processCommandSession so the tracing wrapper has a single error site.
func (s *ManageSieveServer) dispatchCommand(session *manageSieveSession, cmd string, args []string) error {
	switch cmd {
	case "AUTHENTICATE":
		err := s.cmdAuthenticate(session, args)
		var no *manageSieveNo
		if err != nil && !errors.As(err, &no) {
			// A failed authentication ends the connection after the NO.
			return &manageSieveNo{msg: err.Error(), closeConn: true}
		}
		return err
	case "CAPABILITY":
		if len(args) != 0 {
			return fmt.Errorf("CAPABILITY takes no arguments")
		}
		return s.sendCapabilities(session, "OK \"Capability completed\"")
	case "STARTTLS":
		return s.cmdStartTLS(session, args)
	case "HAVESPACE":
		return s.cmdHaveSpace(session, args)
	case "RENAMESCRIPT":
		return s.cmdRenameScript(session, args)
	case "LOGOUT":
		if err := s.sendResponse(session.conn, "OK \"Logout successful\""); err != nil {
			return err
		}
		return io.EOF
	case "PUTSCRIPT":
		return s.cmdPutScript(session, args)
	case "LISTSCRIPTS":
		return s.cmdListScripts(session, args)
	case "SETACTIVE":
		return s.cmdSetActive(session, args)
	case "DELETESCRIPT":
		return s.cmdDeleteScript(session, args)
	case "GETSCRIPT":
		return s.cmdGetScript(session, args)
	case "CHECKSCRIPT":
		return s.cmdCheckScript(session, args)
	case "NOOP":
		return s.cmdNoop(session.conn)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// startCommandSpan begins a `managesieve.<COMMAND>` server-kind span. Returns
// a no-op span (safe to call End on) when the provider is nil or disabled.
func (s *ManageSieveServer) startCommandSpan(cmd string, session *manageSieveSession) otrace.Span {
	if s.tracingProvider == nil || !s.tracingProvider.IsEnabled() {
		return otrace.SpanFromContext(context.Background())
	}
	ip := ""
	if session.conn != nil {
		if host, _, err := net.SplitHostPort(session.conn.RemoteAddr().String()); err == nil {
			ip = host
		}
	}
	_, span := s.tracingProvider.StartSpanWithKind(context.Background(),
		"managesieve."+cmd,
		tracing.SpanKindServer,
		attribute.String("managesieve.command", cmd),
		attribute.String("ip", ip),
	)
	return span
}

func parseManageSieveLine(line string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, ch := range line {
		if escaped {
			// F5040: \" and \\ inside a quoted string do not end it.
			escaped = false
			current.WriteRune(ch)
			continue
		}
		switch ch {
		case '\\':
			escaped = inQuote
			current.WriteRune(ch)
		case '"':
			inQuote = !inQuote
			current.WriteRune(ch)
		case ' ':
			if inQuote {
				current.WriteRune(ch)
			} else if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// unquoteManageSieveArg returns the value of an RFC 5804 quoted string
// ("..." with \\ and \" escapes). Unquoted atoms are returned unchanged. F5040.
func unquoteManageSieveArg(arg string) string {
	if len(arg) < 2 || arg[0] != '"' || arg[len(arg)-1] != '"' {
		return arg
	}
	inner := arg[1 : len(arg)-1]
	var b strings.Builder
	for idx := 0; idx < len(inner); idx++ {
		if inner[idx] == '\\' && idx+1 < len(inner) {
			idx++
		}
		b.WriteByte(inner[idx])
	}
	return b.String()
}

// quoteManageSieveString renders s as an RFC 5804 quoted string.
func quoteManageSieveString(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// maxManageSieveScriptSize bounds one script (PUTSCRIPT/CHECKSCRIPT/HAVESPACE).
const maxManageSieveScriptSize = 1024 * 1024

// readScriptArg reads the script argument of PUTSCRIPT/CHECKSCRIPT. It accepts
// an RFC 5804 literal ("{N+}" or "{N}", whose command line ends with the CRLF
// after the N octets) as well as a bare octet count followed by exactly that
// many octets. F5040.
func readScriptArg(session *manageSieveSession, arg string) (string, error) {
	// F5615: RFC 5804 §2.6/§2.12 — the script is a string, which may be a
	// quoted string as well as a literal. Nothing follows on the wire, so a
	// malformed one keeps the session.
	if strings.HasPrefix(arg, `"`) {
		if !quotedStringComplete(arg) {
			return "", fmt.Errorf("invalid quoted script")
		}
		return unquoteManageSieveArg(arg), nil
	}
	literal := strings.HasPrefix(arg, "{") && strings.HasSuffix(arg, "}")
	sizeText := arg
	if literal {
		sizeText = strings.TrimSuffix(strings.TrimSuffix(arg[1:], "}"), "+")
	}
	// F5467: an empty script ({0+}) is valid; a malformed size is not. The
	// octets that may follow cannot be skipped safely, so failures close.
	scriptSize, err := strconv.Atoi(sizeText)
	if err != nil || scriptSize < 0 || scriptSize > maxManageSieveScriptSize {
		return "", &manageSieveNo{msg: "invalid script size", closeConn: true}
	}

	// Read script content
	scriptBytes := make([]byte, scriptSize)
	totalRead := 0
	for totalRead < scriptSize {
		n, err := session.reader.r.Read(scriptBytes[totalRead:])
		if err != nil {
			return "", &manageSieveNo{msg: fmt.Sprintf("failed to read script: %v", err), closeConn: true}
		}
		totalRead += n
	}

	// RFC 5804 §2.3: the script is exactly script-size octets. For the bare
	// count form nothing follows; for a literal the command line still ends
	// with CRLF, which must be consumed so the next command stays in sync.
	if literal {
		rest, err := session.reader.ReadLine()
		if err != nil {
			return "", &manageSieveNo{msg: fmt.Sprintf("failed to read end of command: %v", err), closeConn: true}
		}
		if strings.TrimSpace(rest) != "" {
			return "", &manageSieveNo{msg: "unexpected data after script literal", closeConn: true}
		}
	}
	return string(scriptBytes), nil
}

// quotedStringComplete reports whether arg is exactly one quoted string whose
// closing quote is not escaped. F5615.
func quotedStringComplete(arg string) bool {
	if len(arg) < 2 || arg[0] != '"' {
		return false
	}
	for i := 1; i < len(arg); i++ {
		switch arg[i] {
		case '\\':
			i++
		case '"':
			return i == len(arg)-1
		}
	}
	return false
}

// cmdAuthenticate handles AUTHENTICATE command
// Format: AUTHENTICATE <mechanism> <initial-response>
func (s *ManageSieveServer) cmdAuthenticate(session *manageSieveSession, args []string) error {
	// F5612: AUTHENTICATE is only valid in non-authenticated state; a second
	// one must not switch the session identity.
	if session.user != "" {
		return &manageSieveNo{msg: "already authenticated"}
	}
	// F5614: refuse cleartext passwords while STARTTLS is available, before
	// any challenge solicits one.
	if s.authNeedsTLS(session) {
		return &manageSieveNo{code: "ENCRYPT-NEEDED", msg: "use STARTTLS before AUTHENTICATE"}
	}
	if len(args) < 1 {
		return fmt.Errorf("AUTHENTICATE requires mechanism")
	}

	mechanism := strings.ToUpper(unquoteManageSieveArg(args[0]))

	// Handle PLAIN authentication mechanism
	if mechanism == "PLAIN" {
		var data string
		if len(args) >= 2 {
			// F5040: RFC 5804 §2.1 SASL initial response.
			data = unquoteManageSieveArg(args[1])
		} else {
			// F5466: RFC 5804 §2.1 — the continuation is an (empty)
			// base64 server-challenge string, not a final OK.
			line, err := s.saslStep(session, "")
			if err != nil {
				return err
			}
			data = line
		}

		// Decode PLAIN auth: [authzid]\x00authcid\x00password
		// The data is base64 encoded
		decoded, err := decodeBase64(data)
		if err != nil {
			return fmt.Errorf("invalid authentication data")
		}

		parts := strings.Split(string(decoded), "\x00")
		if len(parts) < 3 {
			return fmt.Errorf("invalid PLAIN authentication format")
		}

		// parts[0] = authzid (authorization identity, can be empty)
		// parts[1] = authcid (authentication identity/username)
		// parts[2] = password
		authcid := parts[1]
		password := parts[2]

		// F5613: proxy authorization is not supported, so an authzid other
		// than the authcid must fail (RFC 4616 §2) rather than be ignored.
		if parts[0] != "" && !strings.EqualFold(parts[0], authcid) {
			return fmt.Errorf("authorization identity not permitted")
		}

		// Validate credentials using auth handler
		if s.authHandler != nil && s.authHandler(authcid, password) {
			session.user = s.sessionUser(authcid)
			if err := s.sendResponse(session.conn, "OK \"Authentication successful\""); err != nil {
				return err
			}
			return nil
		}

		return fmt.Errorf("authentication failed")
	}

	// Handle LOGIN authentication mechanism
	if mechanism == "LOGIN" {
		// F5466: base64 "Username:" / "Password:" challenges.
		username64, err := s.saslStep(session, "Username:")
		if err != nil {
			return err
		}

		username, err := decodeBase64(username64)
		if err != nil {
			return fmt.Errorf("invalid username")
		}

		password64, err := s.saslStep(session, "Password:")
		if err != nil {
			return err
		}

		password, err := decodeBase64(password64)
		if err != nil {
			return fmt.Errorf("invalid password")
		}

		// Validate credentials
		if s.authHandler != nil && s.authHandler(string(username), string(password)) {
			session.user = s.sessionUser(string(username))
			if err := s.sendResponse(session.conn, "OK \"Authentication successful\""); err != nil {
				return err
			}
			return nil
		}

		return fmt.Errorf("authentication failed")
	}

	return fmt.Errorf("unsupported authentication mechanism: %s", mechanism)
}

// saslStep sends a base64 server-challenge string and returns the client's
// response with its quotes removed. A "*" response cancels the exchange
// (RFC 5804 §2.1) with a NO that keeps the session. F5466.
func (s *ManageSieveServer) saslStep(session *manageSieveSession, challenge string) (string, error) {
	if err := s.sendResponse(session.conn, "%s", quoteManageSieveString(base64.StdEncoding.EncodeToString([]byte(challenge)))); err != nil {
		return "", err
	}
	line, err := session.reader.ReadLine()
	if err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}
	resp := unquoteManageSieveArg(line)
	if resp == "*" {
		return "", &manageSieveNo{msg: "authentication cancelled"}
	}
	return resp, nil
}

// decodeBase64 decodes a SASL response. RFC 5804 §2.1 requires base64; F5611:
// invalid input is an error, not raw text taken as credentials.
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// unauthenticatedScriptCommand is the refusal of PUTSCRIPT/CHECKSCRIPT before
// AUTHENTICATE. When the script argument is a literal or octet count its
// octets are still on the wire and are never read here, so they would be
// parsed as commands; the connection is closed instead. F5632.
func unauthenticatedScriptCommand(args []string) error {
	no := &manageSieveNo{msg: "not authenticated"}
	if len(args) > 0 {
		last := args[len(args)-1]
		if strings.HasPrefix(last, "{") || (last != "" && strings.Trim(last, "0123456789") == "") {
			no.closeConn = true
		}
	}
	return no
}

// cmdPutScript handles PUTSCRIPT command
// Format: PUTSCRIPT <script-name> <script-size>
func (s *ManageSieveServer) cmdPutScript(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return unauthenticatedScriptCommand(args)
	}

	if len(args) < 2 {
		return fmt.Errorf("PUTSCRIPT requires script-name and script-size")
	}

	scriptName := unquoteManageSieveArg(args[0])
	scriptContent, err := readScriptArg(session, args[1])
	if err != nil {
		return err
	}

	// Validate script
	if err := s.manager.ValidateScript(scriptContent); err != nil {
		return fmt.Errorf("script validation failed: %w", err)
	}

	// Store script for authenticated user, within the per-user quota (F5610)
	if err := s.manager.storeScript(session.user, scriptName, scriptContent, true); err != nil {
		if code := quotaResponseCode(err); code != "" {
			return &manageSieveNo{code: code, msg: err.Error()}
		}
		return fmt.Errorf("failed to store script: %w", err)
	}

	if err := s.sendResponse(session.conn, "OK \"Script stored\""); err != nil {
		return err
	}
	return nil
}

// cmdListScripts handles LISTSCRIPTS command
// Format: LISTSCRIPTS
func (s *ManageSieveServer) cmdListScripts(session *manageSieveSession, _ []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
	}

	// List scripts for the authenticated user
	scripts := s.manager.ListScripts(session.user)
	activeName := s.manager.GetActiveScriptName(session.user)

	// F5040: RFC 5804 §2.7 — one quoted name per line, the active one
	// followed by ACTIVE, then the final OK (no OK before the entries).
	for _, name := range scripts {
		quoted := quoteManageSieveString(name)
		if name == activeName {
			if err := s.sendResponse(session.conn, "%s ACTIVE", quoted); err != nil {
				return err
			}
		} else {
			if err := s.sendResponse(session.conn, "%s", quoted); err != nil {
				return err
			}
		}
	}
	if err := s.sendResponse(session.conn, "OK \"List scripts complete\""); err != nil {
		return err
	}
	return nil
}

// cmdSetActive handles SETACTIVE command
// Format: SETACTIVE <script-name>
func (s *ManageSieveServer) cmdSetActive(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
	}

	if len(args) < 1 {
		return fmt.Errorf("SETACTIVE requires script-name")
	}

	scriptName := unquoteManageSieveArg(args[0])
	if scriptName == "" {
		// F5464: RFC 5804 §2.8 — SETACTIVE "" deactivates all scripts.
		if err := s.manager.deactivateScript(session.user); err != nil {
			return &manageSieveNo{msg: err.Error()}
		}
		return s.sendResponse(session.conn, "OK \"No active script\"")
	}

	// Set active script for the user (its only failure is a missing script)
	if err := s.manager.SetActiveScriptByName(session.user, scriptName); err != nil {
		code := "NONEXISTENT"
		if errors.Is(err, errScriptStorage) {
			code = ""
		}
		return &manageSieveNo{code: code, msg: fmt.Sprintf("failed to set active script: %v", err)}
	}

	if err := s.sendResponse(session.conn, "OK \"Set active script\""); err != nil {
		return err
	}
	return nil
}

// cmdDeleteScript handles DELETESCRIPT command
// Format: DELETESCRIPT <script-name>
func (s *ManageSieveServer) cmdDeleteScript(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
	}

	if len(args) < 1 {
		return fmt.Errorf("DELETESCRIPT requires script-name")
	}

	scriptName := unquoteManageSieveArg(args[0])
	if scriptName == "" {
		return fmt.Errorf("script name cannot be empty")
	}

	// F5463: RFC 5804 §2.10 — the active script cannot be deleted, and a
	// missing script is NO (NONEXISTENT).
	if err := s.manager.deleteInactiveScript(session.user, scriptName); err != nil {
		code := "NONEXISTENT"
		if errors.Is(err, errScriptActive) {
			code = "ACTIVE"
		} else if errors.Is(err, errScriptStorage) {
			code = ""
		}
		return &manageSieveNo{code: code, msg: err.Error()}
	}
	if err := s.sendResponse(session.conn, "OK \"Script deleted\""); err != nil {
		return err
	}
	return nil
}

// cmdGetScript handles GETSCRIPT command
// Format: GETSCRIPT <script-name>
func (s *ManageSieveServer) cmdGetScript(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
	}

	if len(args) < 1 {
		return fmt.Errorf("GETSCRIPT requires script-name")
	}

	scriptName := unquoteManageSieveArg(args[0])

	// Get script source for the authenticated user
	source, ok := s.manager.scriptSource(session.user, scriptName)
	if !ok {
		return &manageSieveNo{code: "NONEXISTENT", msg: fmt.Sprintf("script not found: %s", scriptName)}
	}

	// Send script content
	if err := s.sendResponse(session.conn, "{%d}", len(source)); err != nil {
		return err
	}
	// F5040: RFC 5804 §2.9 — the literal is followed by CRLF before OK.
	if _, err := session.conn.Write([]byte(source + "\r\n")); err != nil {
		return err
	}
	if err := s.sendResponse(session.conn, "OK \"Get script complete\""); err != nil {
		return err
	}
	return nil
}

// cmdCheckScript handles CHECKSCRIPT command
// Format: CHECKSCRIPT <script-size>
func (s *ManageSieveServer) cmdCheckScript(session *manageSieveSession, args []string) error {
	// F5037: RFC 5804 §2.12 CHECKSCRIPT is only valid in authenticated state.
	if session.user == "" {
		return unauthenticatedScriptCommand(args)
	}

	if len(args) < 1 {
		return fmt.Errorf("CHECKSCRIPT requires script-size")
	}

	scriptContent, err := readScriptArg(session, args[0])
	if err != nil {
		return err
	}

	// Validate script
	if err := s.manager.ValidateScript(scriptContent); err != nil {
		return fmt.Errorf("script validation failed: %w", err)
	}

	if err := s.sendResponse(session.conn, "OK \"Script is valid\""); err != nil {
		return err
	}
	return nil
}

// cmdStartTLS handles STARTTLS (RFC 5804 §2.2). After the handshake the
// capabilities are sent again. F5461.
func (s *ManageSieveServer) cmdStartTLS(session *manageSieveSession, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("STARTTLS takes no arguments")
	}
	if s.tlsCfg == nil || session.tls {
		return fmt.Errorf("TLS not available")
	}
	if session.user != "" {
		return fmt.Errorf("STARTTLS is only valid before AUTHENTICATE")
	}
	if err := s.sendResponse(session.conn, "OK \"Begin TLS negotiation now\""); err != nil {
		return err
	}
	tlsConn := tls.Server(session.conn, s.tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		return &manageSieveNo{msg: "TLS negotiation failed", closeConn: true}
	}
	session.conn = tlsConn
	session.reader.r = tlsConn
	session.tls = true
	return s.sendCapabilities(session, "OK \"TLS negotiation successful\"")
}

// cmdHaveSpace handles HAVESPACE <script-name> <script-size> (RFC 5804 §2.5).
// F5465.
func (s *ManageSieveServer) cmdHaveSpace(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
	}
	if len(args) != 2 {
		return fmt.Errorf("HAVESPACE requires script-name and script-size")
	}
	if unquoteManageSieveArg(args[0]) == "" {
		return fmt.Errorf("script name cannot be empty")
	}
	size, err := strconv.Atoi(args[1])
	if err != nil || size < 0 {
		return fmt.Errorf("invalid script size")
	}
	if size > maxManageSieveScriptSize {
		return &manageSieveNo{code: "QUOTA/MAXSIZE", msg: "script exceeds the maximum size"}
	}
	// F5610: the per-user script count and storage quota.
	if err := s.manager.haveSpace(session.user, unquoteManageSieveArg(args[0]), size); err != nil {
		return &manageSieveNo{code: quotaResponseCode(err), msg: err.Error()}
	}
	return s.sendResponse(session.conn, "OK \"Putscript would succeed\"")
}

// quotaResponseCode maps a quota error to its RFC 5804 §1.3 response code,
// or "" for other errors. F5610.
func quotaResponseCode(err error) string {
	switch {
	case errors.Is(err, errQuotaMaxScripts):
		return "QUOTA/MAXSCRIPTS"
	case errors.Is(err, errQuotaStorage):
		return "QUOTA"
	}
	return ""
}

// cmdRenameScript handles RENAMESCRIPT <old-name> <new-name> (RFC 5804
// §2.11.1). F5465.
func (s *ManageSieveServer) cmdRenameScript(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
	}
	if len(args) != 2 {
		return fmt.Errorf("RENAMESCRIPT requires old-name and new-name")
	}
	oldName, newName := unquoteManageSieveArg(args[0]), unquoteManageSieveArg(args[1])
	if oldName == "" || newName == "" {
		return fmt.Errorf("script name cannot be empty")
	}
	if err := s.manager.renameScript(session.user, oldName, newName); err != nil {
		code := "NONEXISTENT"
		if errors.Is(err, errScriptExists) {
			code = "ALREADYEXISTS"
		} else if errors.Is(err, errScriptStorage) {
			code = ""
		}
		return &manageSieveNo{code: code, msg: err.Error()}
	}
	return s.sendResponse(session.conn, "OK \"Script renamed\"")
}

// cmdNoop handles NOOP command
func (s *ManageSieveServer) cmdNoop(conn net.Conn) error {
	return s.sendResponse(conn, "OK \"NOOP completed\"")
}

// sendResponse sends a formatted response
func (s *ManageSieveServer) sendResponse(conn net.Conn, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	// ManageSieve uses CRLF line endings
	if !strings.HasSuffix(msg, "\r\n") {
		msg += "\r\n"
	}
	_, err := conn.Write([]byte(msg))
	return err
}

// Close stops the ManageSieve server
func (s *ManageSieveServer) Close() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	close(s.done)

	if s.tlsLn != nil {
		_ = s.tlsLn.Close()
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}

	s.wg.Wait()
	return nil
}

// Addr returns the server's listening address
func (s *ManageSieveServer) Addr() net.Addr {
	if s.tlsLn != nil {
		return s.tlsLn.Addr()
	}
	if s.ln != nil {
		return s.ln.Addr()
	}
	return nil
}
