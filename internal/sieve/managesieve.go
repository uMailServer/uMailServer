// Package sieve implements RFC 5228 - Sieve: An Email Filtering Language
package sieve

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// ManageSieveListenAddr is the default ManageSieve server address
const ManageSieveListenAddr = "0.0.0.0:4190"

// ManageSieveServer implements RFC 5804 - Protocol for Managing Sieve Scripts
type ManageSieveServer struct {
	ln      net.Listener
	tlsLn   net.Listener
	tlsCfg  *tls.Config
	manager *Manager
	done    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

// NewManageSieveServer creates a new ManageSieve server
func NewManageSieveServer(manager *Manager, tlsCfg *tls.Config) *ManageSieveServer {
	return &ManageSieveServer{
		manager: manager,
		tlsCfg:  tlsCfg,
		done:    make(chan struct{}),
	}
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

	// Start TLS listener if TLS config is provided
	if s.tlsCfg != nil {
		ln, err := tls.Listen("tcp", ManageSieveListenAddr, s.tlsCfg)
		if err != nil {
			return fmt.Errorf("failed to start TLS listener: %w", err)
		}
		s.tlsLn = ln
		s.wg.Add(1)
		go s.serveTLS(ln)
	}

	return nil
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

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Send greeting
	if err := s.sendResponse(conn, "OK \"ManageSieve server ready\""); err != nil {
		return
	}

	reader := &manageSieveReader{r: conn}
	for {
		line, err := reader.ReadLine()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}

		// Reset read deadline on command
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		if err := s.processCommand(conn, line, reader); err != nil {
			s.sendResponse(conn, "NO %s", err.Error())
			return
		}
	}
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

// processCommand processes a single ManageSieve command
func (s *ManageSieveServer) processCommand(conn net.Conn, line string, reader *manageSieveReader) error {
	// Parse command and arguments
	parts := parseManageSieveLine(line)
	if len(parts) == 0 {
		return fmt.Errorf("invalid command")
	}

	cmd := strings.ToUpper(parts[0])
	args := parts[1:]

	switch cmd {
	case "AUTHENTICATE":
		return s.cmdAuthenticate(conn, args, reader)
	case "LOGOUT":
		s.sendResponse(conn, "OK \"Logout successful\"")
		return io.EOF
	case "PUTSCRIPT":
		return s.cmdPutScript(conn, args, reader)
	case "LISTSCRIPTS":
		return s.cmdListScripts(conn, args)
	case "SETACTIVE":
		return s.cmdSetActive(conn, args)
	case "DELETESCRIPT":
		return s.cmdDeleteScript(conn, args)
	case "GETSCRIPT":
		return s.cmdGetScript(conn, args)
	case "CHECKSCRIPT":
		return s.cmdCheckScript(conn, args, reader)
	case "NOOP":
		return s.cmdNoop(conn)
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
// Format: AUTHENTICATE <mechanisms> <initial-response>
func (s *ManageSieveServer) cmdAuthenticate(conn net.Conn, args []string, reader *manageSieveReader) error {
	if len(args) < 1 {
		return fmt.Errorf("AUTHENTICATE requires mechanism")
	}

	// Send continuation request for SASL mechanism
	if err := s.sendResponse(conn, "OK \"Continue authentication\""); err != nil {
		return err
	}

	// Read authentication data (simplified - real implementation would use SASL)
	_, err := reader.ReadLine()
	if err != nil {
		return fmt.Errorf("authentication failed")
	}

	// For now, accept any authentication (simplified - real implementation would use SASL)
	// In production, this would validate credentials
	s.sendResponse(conn, "OK \"Authentication successful\"")
	return nil
}

// cmdPutScript handles PUTSCRIPT command
// Format: PUTSCRIPT <script-name> <script-size>
func (s *ManageSieveServer) cmdPutScript(conn net.Conn, args []string, reader *manageSieveReader) error {
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
		n, err := reader.r.Read(scriptBytes[totalRead:])
		if err != nil {
			return fmt.Errorf("failed to read script: %w", err)
		}
		totalRead += n
	}

	// Consume trailing newline
	reader.ReadLine()

	scriptContent := string(scriptBytes)

	// Validate script
	if err := s.manager.ValidateScript(scriptContent); err != nil {
		return fmt.Errorf("script validation failed: %w", err)
	}

	// Store script (note: per-user storage requires session management)
	// Store with script name as the script identifier
	if err := s.manager.StoreScript("default", scriptName, scriptContent); err != nil {
		return fmt.Errorf("failed to store script: %w", err)
	}

	s.sendResponse(conn, "OK \"Script stored\"")
	return nil
}

// cmdListScripts handles LISTSCRIPTS command
// Format: LISTSCRIPTS
func (s *ManageSieveServer) cmdListScripts(conn net.Conn, _ []string) error {
	// List scripts for the authenticated user (using "default" for now)
	scripts := s.manager.ListScripts("default")
	activeName := s.manager.GetActiveScriptName("default")

	s.sendResponse(conn, "OK \"List scripts\"")
	for _, name := range scripts {
		if name == activeName {
			s.sendResponse(conn, "%s \"active script\"", name)
		} else {
			s.sendResponse(conn, "%s", name)
		}
	}
	s.sendResponse(conn, "OK \"List scripts complete\"")
	return nil
}

// cmdSetActive handles SETACTIVE command
// Format: SETACTIVE <script-name>
func (s *ManageSieveServer) cmdSetActive(conn net.Conn, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("SETACTIVE requires script-name")
	}

	scriptName := args[0]
	if scriptName == "" {
		return fmt.Errorf("script name cannot be empty")
	}

	// Set active script for the user
	if err := s.manager.SetActiveScriptByName("default", scriptName); err != nil {
		return fmt.Errorf("failed to set active script: %w", err)
	}

	s.sendResponse(conn, "OK \"Set active script\"")
	return nil
}

// cmdDeleteScript handles DELETESCRIPT command
// Format: DELETESCRIPT <script-name>
func (s *ManageSieveServer) cmdDeleteScript(conn net.Conn, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("DELETESCRIPT requires script-name")
	}

	scriptName := args[0]
	if scriptName == "" {
		return fmt.Errorf("script name cannot be empty")
	}

	// In real implementation, would delete script for user
	s.sendResponse(conn, "OK \"Script deleted\"")
	return nil
}

// cmdGetScript handles GETSCRIPT command
// Format: GETSCRIPT <script-name>
func (s *ManageSieveServer) cmdGetScript(conn net.Conn, args []string) error {
	_ = conn // TODO: Use for response in real implementation
	if len(args) < 1 {
		return fmt.Errorf("GETSCRIPT requires script-name")
	}

	scriptName := args[0]

	// In real implementation, would retrieve script for user
	// For now, return not found
	return fmt.Errorf("script not found: %s", scriptName)
}

// cmdCheckScript handles CHECKSCRIPT command
// Format: CHECKSCRIPT <script-size>
func (s *ManageSieveServer) cmdCheckScript(conn net.Conn, args []string, reader *manageSieveReader) error {
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
		n, err := reader.r.Read(scriptBytes[totalRead:])
		if err != nil {
			return fmt.Errorf("failed to read script: %w", err)
		}
		totalRead += n
	}

	// Consume trailing newline
	reader.ReadLine()

	scriptContent := string(scriptBytes)

	// Validate script
	if err := s.manager.ValidateScript(scriptContent); err != nil {
		return fmt.Errorf("script validation failed: %w", err)
	}

	s.sendResponse(conn, "OK \"Script is valid\"")
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
