// Package sieve implements RFC 5228 - Sieve: An Email Filtering Language
package sieve

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
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
	sessionUser string                       // Authenticated user for this session
	authHandler func(user, pass string) bool // Auth validation function
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
	ln, err := net.Listen("tcp", ManageSieveListenAddr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	s.ln = ln
	s.wg.Add(1)
	go s.serve(ln)

	// Start TLS listener if TLS config is provided (on separate port)
	if s.tlsCfg != nil {
		tlsLn, err := tls.Listen("tcp", ManageSieveTLSListenAddr, s.tlsCfg)
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
	defer conn.Close()

	// Create session state for this connection
	session := &manageSieveSession{
		conn:    conn,
		reader:  &manageSieveReader{r: conn},
		user:    "", // Not authenticated yet
		manager: s.manager,
	}

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

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
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		if err := s.processCommandSession(session, line); err != nil {
			s.sendResponse(conn, "NO %s", err.Error())
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
	r   io.Reader
	buf []byte
}

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
	}
	return strings.TrimRight(string(line), "\r"), nil
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

	switch cmd {
	case "AUTHENTICATE":
		return s.cmdAuthenticate(session, args)
	case "LOGOUT":
		s.sendResponse(session.conn, "OK \"Logout successful\"")
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

func parseManageSieveLine(line string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false

	for _, ch := range line {
		switch ch {
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

// cmdAuthenticate handles AUTHENTICATE command
// Format: AUTHENTICATE <mechanism> <initial-response>
func (s *ManageSieveServer) cmdAuthenticate(session *manageSieveSession, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("AUTHENTICATE requires mechanism")
	}

	mechanism := strings.ToUpper(args[0])

	// Handle PLAIN authentication mechanism
	if mechanism == "PLAIN" {
		// Send continuation request
		if err := s.sendResponse(session.conn, "OK \"Continue authentication\""); err != nil {
			return err
		}

		// Read the authentication data
		data, err := session.reader.ReadLine()
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
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
			s.sendResponse(session.conn, "OK \"Authentication successful\"")
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
			s.sendResponse(session.conn, "OK \"Authentication successful\"")
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

	scriptName := args[0]
	scriptSize := 0
	fmt.Sscanf(args[1], "%d", &scriptSize)

	if scriptSize <= 0 || scriptSize > 1024*1024 {
		return fmt.Errorf("invalid script size")
	}

	// Read script content
	scriptBytes := make([]byte, scriptSize)
	totalRead := 0
	for totalRead < scriptSize {
		n, err := session.reader.r.Read(scriptBytes[totalRead:])
		if err != nil {
			return fmt.Errorf("failed to read script: %w", err)
		}
		totalRead += n
	}

	// Consume trailing newline
	session.reader.ReadLine()

	scriptContent := string(scriptBytes)

	// Validate script
	if err := s.manager.ValidateScript(scriptContent); err != nil {
		return fmt.Errorf("script validation failed: %w", err)
	}

	// Store script for authenticated user
	if err := s.manager.StoreScript(session.user, scriptName, scriptContent); err != nil {
		return fmt.Errorf("failed to store script: %w", err)
	}

	s.sendResponse(session.conn, "OK \"Script stored\"")
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

	s.sendResponse(session.conn, "OK \"List scripts\"")
	for _, name := range scripts {
		if name == activeName {
			s.sendResponse(session.conn, "%s \"active script\"", name)
		} else {
			s.sendResponse(session.conn, "%s", name)
		}
	}
	s.sendResponse(session.conn, "OK \"List scripts complete\"")
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

	scriptName := args[0]
	if scriptName == "" {
		return fmt.Errorf("script name cannot be empty")
	}

	// Set active script for the user
	if err := s.manager.SetActiveScriptByName(session.user, scriptName); err != nil {
		return fmt.Errorf("failed to set active script: %w", err)
	}

	s.sendResponse(session.conn, "OK \"Set active script\"")
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

	scriptName := args[0]
	if scriptName == "" {
		return fmt.Errorf("script name cannot be empty")
	}

	// Delete script for the user
	s.manager.DeleteScript(session.user, scriptName)
	s.sendResponse(session.conn, "OK \"Script deleted\"")
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

	scriptName := args[0]

	// Get script source for the authenticated user
	source := s.manager.GetScriptSource(session.user, scriptName)
	if source == "" {
		return fmt.Errorf("script not found: %s", scriptName)
	}

	// Send script content
	s.sendResponse(session.conn, "{%d}", len(source))
	session.conn.Write([]byte(source))
	s.sendResponse(session.conn, "OK \"Get script complete\"")
	return nil
}

// cmdCheckScript handles CHECKSCRIPT command
// Format: CHECKSCRIPT <script-size>
func (s *ManageSieveServer) cmdCheckScript(session *manageSieveSession, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("CHECKSCRIPT requires script-size")
	}

	scriptSize := 0
	fmt.Sscanf(args[0], "%d", &scriptSize)

	if scriptSize <= 0 || scriptSize > 1024*1024 {
		return fmt.Errorf("invalid script size")
	}

	// Read script content
	scriptBytes := make([]byte, scriptSize)
	totalRead := 0
	for totalRead < scriptSize {
		n, err := session.reader.r.Read(scriptBytes[totalRead:])
		if err != nil {
			return fmt.Errorf("failed to read script: %w", err)
		}
		totalRead += n
	}

	// Consume trailing newline
	session.reader.ReadLine()

	scriptContent := string(scriptBytes)

	// Validate script
	if err := s.manager.ValidateScript(scriptContent); err != nil {
		return fmt.Errorf("script validation failed: %w", err)
	}

	s.sendResponse(session.conn, "OK \"Script is valid\"")
	return nil
}

// cmdNoop handles NOOP command
func (s *ManageSieveServer) cmdNoop(conn net.Conn) error {
	s.sendResponse(conn, "OK \"NOOP completed\"")
	return nil
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
		s.tlsLn.Close()
	}
	if s.ln != nil {
		s.ln.Close()
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
