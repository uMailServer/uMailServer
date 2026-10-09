// Package sieve implements RFC 5228 - Sieve: An Email Filtering Language
package sieve

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	defer func() { _ = conn.Close() }()

	// Create session state for this connection
	session := &manageSieveSession{
		conn:    conn,
		reader:  &manageSieveReader{r: conn},
		user:    "", // Not authenticated yet
		manager: s.manager,
	}

	// Set read timeout
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // Best-effort

	// Send greeting
	if err := s.sendResponse(conn, "OK \"ManageSieve server ready\""); err != nil {
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
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // Best-effort

		if err := s.processCommandSession(session, line); err != nil {
			slog.Error("managesieve command error", "error", err)
			_ = s.sendResponse(conn, "NO command failed")
			return
		}
	}
}

// manageSieveSession holds state for a single ManageSieve session
type manageSieveSession struct {
	conn    net.Conn
	reader  *manageSieveReader
	user    string // Authenticated username
	manager *Manager
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
		return s.cmdAuthenticate(session, args)
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

// readScriptArg reads the script argument of PUTSCRIPT/CHECKSCRIPT. It accepts
// an RFC 5804 literal ("{N+}" or "{N}", whose command line ends with the CRLF
// after the N octets) as well as a bare octet count followed by exactly that
// many octets. F5040.
func readScriptArg(session *manageSieveSession, arg string) (string, error) {
	literal := strings.HasPrefix(arg, "{") && strings.HasSuffix(arg, "}")
	sizeText := arg
	if literal {
		sizeText = strings.TrimSuffix(strings.TrimSuffix(arg[1:], "}"), "+")
	}
	scriptSize := 0
	_, _ = fmt.Sscanf(sizeText, "%d", &scriptSize)

	if scriptSize <= 0 || scriptSize > 1024*1024 {
		return "", fmt.Errorf("invalid script size")
	}

	// Read script content
	scriptBytes := make([]byte, scriptSize)
	totalRead := 0
	for totalRead < scriptSize {
		n, err := session.reader.r.Read(scriptBytes[totalRead:])
		if err != nil {
			return "", fmt.Errorf("failed to read script: %w", err)
		}
		totalRead += n
	}

	// RFC 5804 §2.3: the script is exactly script-size octets. For the bare
	// count form nothing follows; for a literal the command line still ends
	// with CRLF, which must be consumed so the next command stays in sync.
	if literal {
		rest, err := session.reader.ReadLine()
		if err != nil {
			return "", fmt.Errorf("failed to read end of command: %w", err)
		}
		if strings.TrimSpace(rest) != "" {
			return "", fmt.Errorf("unexpected data after script literal")
		}
	}
	return string(scriptBytes), nil
}

// cmdAuthenticate handles AUTHENTICATE command
// Format: AUTHENTICATE <mechanism> <initial-response>
func (s *ManageSieveServer) cmdAuthenticate(session *manageSieveSession, args []string) error {
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
			// Send continuation request
			if err := s.sendResponse(session.conn, "OK \"Continue authentication\""); err != nil {
				return err
			}

			// Read the authentication data
			line, err := session.reader.ReadLine()
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			data = unquoteManageSieveArg(line)
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

		// Validate credentials using auth handler
		if s.authHandler != nil && s.authHandler(authcid, password) {
			session.user = authcid
			if err := s.sendResponse(session.conn, "OK \"Authentication successful\""); err != nil {
				return err
			}
			return nil
		}

		return fmt.Errorf("authentication failed")
	}

	// Handle LOGIN authentication mechanism
	if mechanism == "LOGIN" {
		// Request username
		if err := s.sendResponse(session.conn, "OK \"Continue authentication\""); err != nil {
			return err
		}

		// Read username (base64)
		username64, err := session.reader.ReadLine()
		if err != nil {
			return fmt.Errorf("authentication failed")
		}

		username, err := decodeBase64(username64)
		if err != nil {
			return fmt.Errorf("invalid username")
		}

		// Request password
		if err := s.sendResponse(session.conn, "OK \"Continue authentication\""); err != nil {
			return err
		}

		// Read password (base64)
		password64, err := session.reader.ReadLine()
		if err != nil {
			return fmt.Errorf("authentication failed")
		}

		password, err := decodeBase64(password64)
		if err != nil {
			return fmt.Errorf("invalid password")
		}

		// Validate credentials
		if s.authHandler != nil && s.authHandler(string(username), string(password)) {
			session.user = string(username)
			if err := s.sendResponse(session.conn, "OK \"Authentication successful\""); err != nil {
				return err
			}
			return nil
		}

		return fmt.Errorf("authentication failed")
	}

	return fmt.Errorf("unsupported authentication mechanism: %s", mechanism)
}

// decodeBase64 decodes a base64 string
func decodeBase64(s string) ([]byte, error) {
	// First try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return decoded, nil
	}
	// If it fails, return the original string as-is (some clients send plain text)
	return []byte(s), nil
}

// cmdPutScript handles PUTSCRIPT command
// Format: PUTSCRIPT <script-name> <script-size>
func (s *ManageSieveServer) cmdPutScript(session *manageSieveSession, args []string) error {
	if session.user == "" {
		return fmt.Errorf("not authenticated")
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

	// Store script for authenticated user
	if err := s.manager.StoreScript(session.user, scriptName, scriptContent); err != nil {
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
		return fmt.Errorf("script name cannot be empty")
	}

	// Set active script for the user
	if err := s.manager.SetActiveScriptByName(session.user, scriptName); err != nil {
		return fmt.Errorf("failed to set active script: %w", err)
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

	// Delete script for the user
	s.manager.DeleteScript(session.user, scriptName)
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
	source := s.manager.GetScriptSource(session.user, scriptName)
	if source == "" {
		return fmt.Errorf("script not found: %s", scriptName)
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
		return fmt.Errorf("not authenticated")
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
