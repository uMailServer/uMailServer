package pop3

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
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
	running    bool

	authFunc  func(username, password string) (bool, error)
	mailstore Mailstore
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
	id       string
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	server   *Server
	state    State
	user     string
	messages []*Message
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
		addr:      addr,
		logger:    logger,
		mailstore: mailstore,
		sessions:  make(map[string]*Session),
		shutdown:  make(chan struct{}),
	}
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(username, password string) (bool, error)) {
	s.authFunc = fn
}

// SetTLSConfig sets the TLS configuration
func (s *Server) SetTLSConfig(config *TLSConfig) {
	s.tlsConfig = config
}

// Start starts the POP3 server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = listener
	s.running = true

	s.logger.Info("POP3 server started", "addr", s.addr)

	go s.acceptLoop()

	return nil
}

// Stop stops the POP3 server
func (s *Server) Stop() error {
	s.running = false
	close(s.shutdown)

	if s.listener != nil {
		s.listener.Close()
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
			if s.running {
				s.logger.Error("Failed to accept connection", "error", err)
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single POP3 connection
func (s *Server) handleConnection(conn net.Conn) {
	session := NewSession(conn, s)

	s.sessionsMu.Lock()
	s.sessions[session.ID()] = session
	s.sessionsMu.Unlock()

	s.logger.Info("New POP3 session", "session", session.ID(), "remote", conn.RemoteAddr())

	// Send greeting
	session.WriteResponse("+OK POP3 server ready")

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
	return &Session{
		id:     generateSessionID(),
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
		server: server,
		state:  StateAuthorization,
	}
}

// ID returns the session ID
func (s *Session) ID() string {
	return s.id
}

// Close closes the session
func (s *Session) Close() {
	s.conn.Close()
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

		s.server.logger.Debug("POP3 command", "session", s.id, "line", line)

		if err := s.handleCommand(line); err != nil {
			return
		}
	}
}

// readLine reads a line from the connection
func (s *Session) readLine() (string, error) {
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// WriteResponse writes a POP3 response
func (s *Session) WriteResponse(response string) {
	s.writer.WriteString(response + "\r\n")
	s.writer.Flush()
}

// WriteDataLine writes a data line
func (s *Session) WriteDataLine(line string) {
	// Escape lines starting with "."
	if strings.HasPrefix(line, ".") {
		line = "." + line
	}
	s.writer.WriteString(line + "\r\n")
}

// WriteDataEnd writes the end of data marker
func (s *Session) WriteDataEnd() {
	s.writer.WriteString(".\r\n")
	s.writer.Flush()
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

// handleAuthorizationCommand handles commands in AUTHORIZATION state
func (s *Session) handleAuthorizationCommand(command string, args []string) error {
	switch command {
	case "USER":
		if len(args) < 1 {
			s.WriteResponse("-ERR Usage: USER <username>")
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

		// Authenticate
		if s.server.authFunc != nil {
			ok, err := s.server.authFunc(s.user, args[0])
			if err != nil || !ok {
				s.WriteResponse("-ERR Authentication failed")
				s.user = ""
				return nil
			}
		}

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
		// Get total size
		var totalSize int64
		for _, msg := range s.messages {
			if msg != nil {
				totalSize += msg.Size
			}
		}
		s.WriteResponse(fmt.Sprintf("+OK %d %d", len(s.messages), totalSize))

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
		s.writer.Write(msg.Data)
		if !strings.HasSuffix(string(msg.Data), "\n") {
			s.writer.WriteString("\r\n")
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
			s.server.mailstore.DeleteMessage(s.user, i)
		}
	}

	s.WriteResponse("+OK")
	return fmt.Errorf("quit")
}

// sendTop sends headers + specified number of lines
func (s *Session) sendTop(data []byte, lines int) {
	// Find end of headers
	content := string(data)
	headerEnd := strings.Index(content, "\r\n\r\n")
	if headerEnd == -1 {
		headerEnd = strings.Index(content, "\n\n")
	}

	if headerEnd == -1 {
		// No headers found, send all
		s.writer.Write(data)
		s.WriteDataEnd()
		return
	}

	// Send headers
	s.writer.WriteString(content[:headerEnd])
	s.writer.WriteString("\r\n\r\n")

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
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
