// Package av provides virus scanning capabilities for email messages.
// It supports ClamAV integration via TCP or Unix socket connections.
package av

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

// ScanResult represents the result of a virus scan
type ScanResult struct {
	Infected bool
	Virus    string
	// Skipped is true when no scan took place because the scanner is
	// disabled or unconfigured. Infected=false then means "not scanned", not
	// "clean"; callers that must fail closed should check it (F5824).
	Skipped bool
}

// Scanner scans messages for viruses
type Scanner struct {
	addr    string
	timeout time.Duration
	enabled bool
	action  string // "reject", "quarantine", "tag"
	dial    func(network, addr string, timeout time.Duration) (net.Conn, error)
}

// Config holds virus scanner configuration
type Config struct {
	Enabled bool          `yaml:"enabled" json:"enabled"`
	Addr    string        `yaml:"addr" json:"addr"`       // ClamAV address (e.g., "127.0.0.1:3310" or "/var/run/clamav/clamd.ctl")
	Timeout time.Duration `yaml:"timeout" json:"timeout"` // Scan timeout
	Action  string        `yaml:"action" json:"action"`   // "reject", "quarantine", "tag"
}

// NewScanner creates a new virus scanner
func NewScanner(cfg Config) *Scanner {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Action == "" {
		cfg.Action = "reject"
	}
	return &Scanner{
		addr:    cfg.Addr,
		timeout: cfg.Timeout,
		enabled: cfg.Enabled,
		action:  cfg.Action,
		dial: func(network, addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout(network, addr, timeout)
		},
	}
}

// IsEnabled returns whether the scanner is enabled
func (s *Scanner) IsEnabled() bool {
	return s.enabled && s.addr != ""
}

// Action returns the configured action for infected messages
func (s *Scanner) Action() string {
	return s.action
}

func (s *Scanner) connect() (net.Conn, error) {
	network := "tcp"
	if strings.HasPrefix(s.addr, "/") {
		network = "unix"
	}
	return s.dial(network, s.addr, s.timeout)
}

// Scan scans data for viruses using ClamAV
func (s *Scanner) Scan(data []byte) (*ScanResult, error) {
	if !s.IsEnabled() {
		return &ScanResult{Infected: false, Skipped: true}, nil
	}

	conn, err := s.connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ClamAV at %s: %w", s.addr, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Send INSTREAM command for streaming scan
	_, err = conn.Write([]byte("zINSTREAM\000"))
	if err != nil {
		return nil, fmt.Errorf("failed to send INSTREAM command: %w", err)
	}

	// Send data in chunks (ClamAV INSTREAM protocol: 4-byte big-endian length + data)
	chunkSize := 32768
	offset := 0
	for offset < len(data) {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]

		// Length prefix and data go out in one write (4-byte big-endian length).
		frame := make([]byte, 4+len(chunk))
		binary.BigEndian.PutUint32(frame, uint32(len(chunk))) // #nosec G115 -- chunk <= 32 KiB
		copy(frame[4:], chunk)
		if _, err := conn.Write(frame); err != nil {
			return nil, writeFailure("failed to write chunk", err, conn)
		}
		offset = end
	}

	// Send termination marker (0-length chunk)
	_, err = conn.Write([]byte{0, 0, 0, 0})
	if err != nil {
		return nil, writeFailure("failed to send termination", err, conn)
	}

	// Read response
	reader := bufio.NewReader(conn)
	response, err := readClamAVResponse(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read ClamAV response: %w", err)
	}

	response = strings.TrimSpace(response)

	// Parse response
	// Format: "stream: <virus_name> FOUND" or "stream: OK"
	result := &ScanResult{}

	if strings.HasSuffix(response, "FOUND") {
		result.Infected = true
		// Extract virus name
		parts := strings.SplitN(response, ":", 2)
		if len(parts) == 2 {
			virusName := strings.TrimSpace(parts[1])
			virusName = strings.TrimSuffix(virusName, "FOUND")
			result.Virus = strings.TrimSpace(virusName)
		} else {
			result.Virus = "unknown"
		}
	} else if response != "stream: OK" {
		// Only "stream: OK" means the data was scanned and found clean; an
		// ERROR or any other reply (UNKNOWN COMMAND, a non-clamd peer) means
		// it was not scanned and must not be reported as clean (F5107).
		return nil, fmt.Errorf("ClamAV error: %s", response)
	}

	return result, nil
}

// ScanVersion queries ClamAV for its version
func (s *Scanner) ScanVersion() (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("scanner not enabled")
	}

	conn, err := s.connect()
	if err != nil {
		return "", fmt.Errorf("failed to connect to ClamAV: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		return "", fmt.Errorf("failed to set deadline: %w", err)
	}

	_, err = conn.Write([]byte("zVERSION\000"))
	if err != nil {
		return "", fmt.Errorf("failed to send VERSION command: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := readClamAVResponse(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read version: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// Ping checks if ClamAV is reachable
func (s *Scanner) Ping() error {
	if !s.IsEnabled() {
		return fmt.Errorf("scanner not enabled")
	}

	conn, err := s.connect()
	if err != nil {
		return fmt.Errorf("ClamAV not reachable at %s: %w", s.addr, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}

	_, err = conn.Write([]byte("zPING\000"))
	if err != nil {
		return fmt.Errorf("failed to send PING: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := readClamAVResponse(reader)
	if err != nil {
		return fmt.Errorf("failed to read PONG: %w", err)
	}

	if strings.TrimSpace(response) != "PONG" {
		return fmt.Errorf("unexpected ClamAV response: %s", response)
	}

	return nil
}

// Accept NUL framing for z commands and newline framing from legacy peers.
func readClamAVResponse(reader *bufio.Reader) (string, error) {
	var response strings.Builder
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		if b == 0 || b == '\n' {
			return response.String(), nil
		}
		response.WriteByte(b)
	}
}

// writeFailure reports a failed write. When clamd rejects a stream (for
// example "INSTREAM size limit exceeded. ERROR") it replies and closes the
// socket, so the write fails with a reset or broken pipe; the reply explains
// why, so try to read it (F5824).
func writeFailure(what string, err error, conn net.Conn) error {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if reply, rerr := readClamAVResponse(bufio.NewReader(conn)); rerr == nil && strings.TrimSpace(reply) != "" {
		return fmt.Errorf("%s: %w (ClamAV said: %s)", what, err, strings.TrimSpace(reply))
	}
	return fmt.Errorf("%s: %w", what, err)
}
