package pop3

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server represents a POP3 server
type Server struct {
	addr      string
	tlsConfig *TLSConfig
	logger    *slog.Logger

	listener   net.Listener
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
	shutdown   chan struct{}
	stopOnce   sync.Once
	running    atomic.Bool

	authFunc  func(username, password string) (bool, error)
	mailstore Mailstore

	// Auth brute-force protection
	maxLoginAttempts int
	lockoutDuration  time.Duration
	authFailures     map[string][]time.Time // IP -> failure timestamps
	authFailuresMu   sync.Mutex

	// Connection timeouts
	readTimeout  time.Duration
	writeTimeout time.Duration

	// Connection limits
	maxConnections int
	requireTLS     bool
}

// SetRequireTLS sets whether TLS is required before authentication.
func (s *Server) SetRequireTLS(require bool) {
	s.requireTLS = require
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	CertFile string
	KeyFile  string
}

// Mailstore interface for POP3 operations
type Mailstore interface {
	Authenticate(username, password string) (bool, error)
	ListMessages(user string) ([]*Message, error)
	GetMessage(user string, index int) (*Message, error)
	GetMessageData(user string, index int) ([]byte, error)
	DeleteMessage(user string, index int) error
	GetMessageCount(user string) (int, error)
	GetMessageSize(user string, index int) (int64, error)
}

// Message represents a POP3 message
type Message struct {
	Index int
	UID   string
	Size  int64
	Data  []byte
}

// Session represents a POP3 client session
type Session struct {
	id            string
	conn          net.Conn
	reader        *bufio.Reader
	writer        *bufio.Writer
	server        *Server
	state         State
	user              string
	messages          []*Message
	isTLS             bool
	greetingTimestamp string // Timestamp from greeting banner
}

// State represents the POP3 session state
type State int

const (
	StateAuthorization State = iota
	StateTransaction
	StateUpdate
)

// NewServer creates a new POP3 server
func NewServer(addr string, mailstore Mailstore, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		addr:         addr,
		logger:       logger,
		mailstore:    mailstore,
		sessions:     make(map[string]*Session),
		shutdown:     make(chan struct{}),
		authFailures: make(map[string][]time.Time),
	}
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(username, password string) (bool, error)) {
	s.authFunc = fn
}

// SetAuthLimits configures brute-force protection for POP3 AUTH
func (s *Server) SetAuthLimits(maxAttempts int, lockoutDuration time.Duration) {
	s.maxLoginAttempts = maxAttempts
	s.lockoutDuration = lockoutDuration
}

// isAuthLockedOut returns true if the given IP is temporarily locked out
func (s *Server) isAuthLockedOut(ip string) bool {
	if s.maxLoginAttempts <= 0 {
		return false
	}
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()

	cutoff := time.Now().Add(-s.lockoutDuration)
	var recent []time.Time
	for _, t := range s.authFailures[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	s.authFailures[ip] = recent
	return len(recent) >= s.maxLoginAttempts
}

// recordAuthFailure records a failed authentication attempt from the given IP
func (s *Server) recordAuthFailure(ip string) {
	if s.maxLoginAttempts <= 0 {
		return
	}
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	s.authFailures[ip] = append(s.authFailures[ip], time.Now())
}

// clearAuthFailures removes recorded failures for the given IP
func (s *Server) clearAuthFailures(ip string) {
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	delete(s.authFailures, ip)
}

// SetReadTimeout sets the read timeout for POP3 connections.
func (s *Server) SetReadTimeout(d time.Duration) {
	s.readTimeout = d
}

// SetWriteTimeout sets the write timeout for POP3 connections.
func (s *Server) SetWriteTimeout(d time.Duration) {
	s.writeTimeout = d
}

// SetMaxConnections sets the maximum number of concurrent POP3 connections.
func (s *Server) SetMaxConnections(n int) {
	s.maxConnections = n
}

// SetTLSConfig sets the TLS configuration
func (s *Server) SetTLSConfig(config *TLSConfig) {
	s.tlsConfig = config
}

// getTLSConfig builds a *tls.Config from the configured cert/key files.
func (s *Server) getTLSConfig() (*tls.Config, error) {
	if s.tlsConfig == nil {
		return nil, fmt.Errorf("TLS config not provided")
	}
	cert, err := tls.LoadX509KeyPair(s.tlsConfig.CertFile, s.tlsConfig.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// StartTLS starts the POP3 server with implicit TLS.
func (s *Server) StartTLS() error {
	config, err := s.getTLSConfig()
	if err != nil {
		return err
	}

	listener, err := tls.Listen("tcp", s.addr, config)
	if err != nil {
		return fmt.Errorf("failed to listen TLS: %w", err)
	}

	s.listener = listener
	s.running.Store(true)

	s.logger.Info("POP3 server started with TLS", "addr", s.addr)

	go s.acceptLoop()

	return nil
}

// Start starts the POP3 server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = listener
	s.running.Store(true)

	s.logger.Info("POP3 server started", "addr", s.addr)

	go s.acceptLoop()

	return nil
}

// Stop stops the POP3 server
func (s *Server) Stop() error {
	s.running.Store(false)
	s.stopOnce.Do(func() { close(s.shutdown) })

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.logger.Debug("failed to close listener", "error", err)
		}
	}

	// Close all sessions
	s.sessionsMu.Lock()
	for _, session := range s.sessions {
		session.Close()
	}
	s.sessions = make(map[string]*Session)
	s.sessionsMu.Unlock()

	s.logger.Info("POP3 server stopped")
	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.shutdown:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if s.running.Load() {
				s.logger.Error("Failed to accept connection", "error", err)
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single POP3 connection
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in POP3 connection handler", "error", r)
			_ = conn.Close()
		}
	}()

	s.sessionsMu.RLock()
	atLimit := s.maxConnections > 0 && len(s.sessions) >= s.maxConnections
	s.sessionsMu.RUnlock()
	if atLimit {
		if _, err := conn.Write([]byte("-ERR Too many connections\r\n")); err != nil {
			s.logger.Debug("failed to write connection limit response", "error", err)
		}
		_ = conn.Close()
		return
	}

	session := NewSession(conn, s)

	s.sessionsMu.Lock()
	s.sessions[session.ID()] = session
	s.sessionsMu.Unlock()

	s.logger.Info("New POP3 session", "session", session.ID(), "remote", conn.RemoteAddr())

	// Send greeting with timestamp
	session.WriteResponse(fmt.Sprintf("+OK POP3 server ready <%s>", session.greetingTimestamp))

	// Handle commands
	session.Handle()

	// Cleanup
	s.sessionsMu.Lock()
	delete(s.sessions, session.ID())
	s.sessionsMu.Unlock()

	s.logger.Info("POP3 session ended", "session", session.ID())
}

// NewSession creates a new POP3 session
func NewSession(conn net.Conn, server *Server) *Session {
	// Generate greeting timestamp: timestamp.secret@domain
	timestamp := time.Now().Unix()
	secret := generateSessionID()
	return &Session{
		id:                generateSessionID(),
		conn:              conn,
		reader:            bufio.NewReader(conn),
		writer:            bufio.NewWriter(conn),
		server:            server,
		greetingTimestamp: fmt.Sprintf("%d.%s", timestamp, secret),
		state:             StateAuthorization,
	}
}

// ID returns the session ID
func (s *Session) ID() string {
	return s.id
}

// Close closes the session
func (s *Session) Close() {
	if err := s.conn.Close(); err != nil {
		s.server.logger.Debug("failed to close connection", "error", err)
	}
}

// Handle processes commands from the client
func (s *Session) Handle() {
	for {
		line, err := s.readLine()
		if err != nil {
			return
		}

		if line == "" {
			continue
		}

		s.server.logger.Debug("POP3 command", "session", s.id, "line", truncateCommand(line, 80))

		if err := s.handleCommand(line); err != nil {
			return
		}
	}
}

// readLine reads a line from the connection
func (s *Session) readLine() (string, error) {
	if s.server.readTimeout > 0 {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.server.readTimeout)); err != nil {
			s.server.logger.Debug("failed to set read deadline", "error", err)
		}
	}
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// setWriteDeadline sets the write deadline if configured.
func (s *Session) setWriteDeadline() {
	if s.server.writeTimeout > 0 {
		if err := s.conn.SetWriteDeadline(time.Now().Add(s.server.writeTimeout)); err != nil {
			s.server.logger.Debug("failed to set write deadline", "error", err)
		}
	}
}

// WriteResponse writes a POP3 response
func (s *Session) WriteResponse(response string) {
	s.setWriteDeadline()
	if _, err := s.writer.WriteString(response + "\r\n"); err != nil {
		s.server.logger.Debug("failed to write response", "error", err)
		return
	}
	if err := s.writer.Flush(); err != nil {
		s.server.logger.Debug("failed to flush response", "error", err)
	}
}

// WriteDataLine writes a data line
func (s *Session) WriteDataLine(line string) {
	// Escape lines starting with "."
	if strings.HasPrefix(line, ".") {
		line = "." + line
	}
	s.setWriteDeadline()
	if _, err := s.writer.WriteString(line + "\r\n"); err != nil {
		s.server.logger.Debug("failed to write data line", "error", err)
	}
}

// WriteDataEnd writes the end of data marker
func (s *Session) WriteDataEnd() {
	s.setWriteDeadline()
	if _, err := s.writer.WriteString(".\r\n"); err != nil {
		s.server.logger.Debug("failed to write data end", "error", err)
		return
	}
	if err := s.writer.Flush(); err != nil {
		s.server.logger.Debug("failed to flush data end", "error", err)
	}
}

// handleCommand parses and handles a POP3 command
func (s *Session) handleCommand(line string) error {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		s.WriteResponse("-ERR Invalid command")
		return nil
	}

	command := strings.ToUpper(parts[0])
	args := parts[1:]

	switch s.state {
	case StateAuthorization:
		return s.handleAuthorizationCommand(command, args)
	case StateTransaction:
		return s.handleTransactionCommand(command, args)
	case StateUpdate:
		return s.handleUpdateCommand(command, args)
	}

	return nil
}

// truncateCommand truncates a command for safe logging.
func truncateCommand(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// handleAuthorizationCommand handles commands in AUTHORIZATION state
func (s *Session) handleAuthorizationCommand(command string, args []string) error {
	switch command {
	case "USER":
		if len(args) < 1 {
			s.WriteResponse("-ERR Usage: USER <username>")
			return nil
		}
		if s.server.requireTLS && !s.isTLS {
			s.WriteResponse("-ERR TLS required for authentication")
			return nil
		}
		s.user = args[0]
		s.WriteResponse("+OK")

	case "PASS":
		if len(args) < 1 {
			s.WriteResponse("-ERR Usage: PASS <password>")
			return nil
		}
		if s.user == "" {
			s.WriteResponse("-ERR USER required first")
			return nil
		}
		if s.server.requireTLS && !s.isTLS {
			s.WriteResponse("-ERR TLS required for authentication")
			return nil
		}

		host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String())
		if err != nil {
			host = s.conn.RemoteAddr().String()
		}
		if s.server.isAuthLockedOut(host) {
			s.WriteResponse("-ERR Too many failed authentication attempts")
			s.user = ""
			return nil
		}

		// Authenticate
		authenticated := false
		if s.server.authFunc != nil {
			ok, err := s.server.authFunc(s.user, args[0])
			if err == nil && ok {
				authenticated = true
			}
		} else {
			ok, err := s.server.mailstore.Authenticate(s.user, args[0])
			if err == nil && ok {
				authenticated = true
			}
		}

		if !authenticated {
			s.server.recordAuthFailure(host)
			s.WriteResponse("-ERR Authentication failed")
			s.user = ""
			return nil
		}

		s.server.clearAuthFailures(host)

		// Load messages
		messages, err := s.server.mailstore.ListMessages(s.user)
		if err != nil {
			s.WriteResponse("-ERR Unable to load messages")
			return nil
		}

		s.messages = messages
		s.state = StateTransaction
		s.WriteResponse("+OK")

	case "QUIT":
		s.WriteResponse("+OK")
		return fmt.Errorf("quit")

	case "STLS":
		if s.server.tlsConfig == nil {
			s.WriteResponse("-ERR Command not available")
			return nil
		}
		if s.isTLS {
			s.WriteResponse("-ERR Already using TLS")
			return nil
		}
		s.WriteResponse("+OK Begin TLS negotiation")
		config, err := s.server.getTLSConfig()
		if err != nil {
			s.WriteResponse("-ERR TLS configuration error")
			return nil
		}
		_ = s.conn.SetDeadline(time.Now().Add(30 * time.Second))
		tlsConn := tls.Server(s.conn, config)
		if err := tlsConn.Handshake(); err != nil {
			_ = s.conn.SetDeadline(time.Time{})
			return err
		}
		_ = s.conn.SetDeadline(time.Time{})
		s.conn = tlsConn
		s.reader = bufio.NewReader(tlsConn)
		s.writer = bufio.NewWriter(tlsConn)
		s.isTLS = true
		s.state = StateAuthorization
		s.user = ""
		s.messages = nil
		return nil

	case "CAPA":
		s.WriteResponse("+OK Capability list follows")
		s.WriteDataLine("USER")
		s.WriteDataLine("PASS")
		s.WriteDataLine("STAT")
		s.WriteDataLine("LIST")
		s.WriteDataLine("RETR")
		s.WriteDataLine("DELE")
		s.WriteDataLine("NOOP")
		s.WriteDataLine("RSET")
		s.WriteDataLine("QUIT")
		s.WriteDataLine("UIDL")
		s.WriteDataLine("TOP")
		if s.server.tlsConfig != nil && !s.isTLS {
			s.WriteDataLine("STLS")
		}
		s.WriteDataEnd()

	default:
		s.WriteResponse("-ERR Command not valid in this state")
	}

	return nil
}

// handleTransactionCommand handles commands in TRANSACTION state
func (s *Session) handleTransactionCommand(command string, args []string) error {
	switch command {
	case "STAT":
		// Count only non-deleted messages and their total size
		var count int
		var totalSize int64
		for _, msg := range s.messages {
			if msg != nil {
				count++
				totalSize += msg.Size
			}
		}
		s.WriteResponse(fmt.Sprintf("+OK %d %d", count, totalSize))

	case "LIST":
		if len(args) == 0 {
			// List all messages
			s.WriteResponse("+OK")
			for i, msg := range s.messages {
				if msg != nil {
					s.WriteDataLine(fmt.Sprintf("%d %d", i+1, msg.Size))
				}
			}
			s.WriteDataEnd()
		} else {
			// List specific message
			index, err := strconv.Atoi(args[0])
			if err != nil || index < 1 || index > len(s.messages) {
				s.WriteResponse("-ERR No such message")
				return nil
			}
			msg := s.messages[index-1]
			if msg == nil {
				s.WriteResponse("-ERR Message deleted")
				return nil
			}
			s.WriteResponse(fmt.Sprintf("+OK %d %d", index, msg.Size))
		}

	case "RETR":
		if len(args) < 1 {
			s.WriteResponse("-ERR Usage: RETR <msg>")
			return nil
		}
		index, err := strconv.Atoi(args[0])
		if err != nil || index < 1 || index > len(s.messages) {
			s.WriteResponse("-ERR No such message")
			return nil
		}

		msg := s.messages[index-1]
		if msg == nil {
			s.WriteResponse("-ERR Message deleted")
			return nil
		}

		// Load message data if not loaded
		if msg.Data == nil {
			var loadErr error
			msg.Data, loadErr = s.server.mailstore.GetMessageData(s.user, index-1)
			if loadErr != nil {
				s.WriteResponse("-ERR Failed to read message")
				return nil
			}
		}

		s.WriteResponse(fmt.Sprintf("+OK %d octets", len(msg.Data)))
		s.setWriteDeadline()
		_, _ = s.writer.Write(msg.Data)
		if !strings.HasSuffix(string(msg.Data), "\n") {
			_, _ = s.writer.WriteString("\r\n")
		}
		s.WriteDataEnd()

	case "DELE":
		if len(args) < 1 {
			s.WriteResponse("-ERR Usage: DELE <msg>")
			return nil
		}
		index, err := strconv.Atoi(args[0])
		if err != nil || index < 1 || index > len(s.messages) {
			s.WriteResponse("-ERR No such message")
			return nil
		}

		if s.messages[index-1] == nil {
			s.WriteResponse("-ERR Message already deleted")
			return nil
		}

		// Mark for deletion (will be deleted in UPDATE state)
		s.messages[index-1] = nil
		s.WriteResponse("+OK")

	case "NOOP":
		s.WriteResponse("+OK")

	case "RSET":
		// Unmark all deleted messages
		messages, err := s.server.mailstore.ListMessages(s.user)
		if err != nil {
			s.WriteResponse("-ERR Unable to reset")
			return nil
		}
		s.messages = messages
		s.WriteResponse("+OK")

	case "UIDL":
		if len(args) == 0 {
			// List all UIDs
			s.WriteResponse("+OK")
			for i, msg := range s.messages {
				if msg != nil {
					s.WriteDataLine(fmt.Sprintf("%d %s", i+1, msg.UID))
				}
			}
			s.WriteDataEnd()
		} else {
			// List specific UID
			index, err := strconv.Atoi(args[0])
			if err != nil || index < 1 || index > len(s.messages) {
				s.WriteResponse("-ERR No such message")
				return nil
			}
			msg := s.messages[index-1]
			if msg == nil {
				s.WriteResponse("-ERR Message deleted")
				return nil
			}
			s.WriteResponse(fmt.Sprintf("+OK %d %s", index, msg.UID))
		}

	case "TOP":
		if len(args) < 2 {
			s.WriteResponse("-ERR Usage: TOP <msg> <lines>")
			return nil
		}
		index, err := strconv.Atoi(args[0])
		if err != nil || index < 1 || index > len(s.messages) {
			s.WriteResponse("-ERR No such message")
			return nil
		}

		lines, err := strconv.Atoi(args[1])
		if err != nil || lines < 0 {
			s.WriteResponse("-ERR Invalid line count")
			return nil
		}

		msg := s.messages[index-1]
		if msg == nil {
			s.WriteResponse("-ERR Message deleted")
			return nil
		}

		// Load message data
		if msg.Data == nil {
			var loadErr error
			msg.Data, loadErr = s.server.mailstore.GetMessageData(s.user, index-1)
			if loadErr != nil {
				s.WriteResponse("-ERR Failed to read message")
				return nil
			}
		}

		// Send headers + specified lines
		s.WriteResponse("+OK")
		s.sendTop(msg.Data, lines)
		s.WriteDataEnd()

	case "QUIT":
		// Enter UPDATE state
		s.state = StateUpdate
		return s.handleUpdateCommand("QUIT", args)

	case "CAPA":
		s.WriteResponse("+OK Capability list follows")
		s.WriteDataLine("STAT")
		s.WriteDataLine("LIST")
		s.WriteDataLine("RETR")
		s.WriteDataLine("DELE")
		s.WriteDataLine("NOOP")
		s.WriteDataLine("RSET")
		s.WriteDataLine("QUIT")
		s.WriteDataLine("UIDL")
		s.WriteDataLine("TOP")
		s.WriteDataEnd()

	default:
		s.WriteResponse("-ERR Unknown command")
	}

	return nil
}

// handleUpdateCommand handles commands in UPDATE state
func (s *Session) handleUpdateCommand(command string, args []string) error {
	// Delete all marked messages
	for i, msg := range s.messages {
		if msg == nil {
			_ = s.server.mailstore.DeleteMessage(s.user, i)
		}
	}

	s.WriteResponse("+OK")
	return fmt.Errorf("quit")
}

// sendTop sends headers + specified number of lines
func (s *Session) sendTop(data []byte, lines int) {
	s.setWriteDeadline()
	// Find end of headers
	content := string(data)
	headerEnd := strings.Index(content, "\r\n\r\n")
	if headerEnd == -1 {
		headerEnd = strings.Index(content, "\n\n")
	}

	if headerEnd == -1 {
		// No headers found, send all
		_, _ = s.writer.Write(data)
		s.WriteDataEnd()
		return
	}

	// Send headers
	_, _ = s.writer.WriteString(content[:headerEnd])
	_, _ = s.writer.WriteString("\r\n\r\n")

	// Send specified number of lines
	body := content[headerEnd+4:]
	bodyLines := strings.Split(body, "\n")
	for i, line := range bodyLines {
		if i >= lines {
			break
		}
		s.WriteDataLine(strings.TrimRight(line, "\r"))
	}
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based entropy only on crypto/rand failure
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
