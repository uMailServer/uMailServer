// Package av provides virus scanning capabilities for email messages.
// It supports ClamAV integration via TCP or Unix socket connections.
package av

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

// ScanResult represents the result of a virus scan
type ScanResult struct {
	Infected bool
	Virus    string
}

// Scanner scans messages for viruses
type Scanner struct {
	addr        string
	timeout     time.Duration
	enabled     bool
	action      string // "reject", "quarantine", "tag"
}

// Config holds virus scanner configuration
type Config struct {
	Enabled bool          `yaml:"enabled" json:"enabled"`
	Addr    string        `yaml:"addr" json:"addr"`        // ClamAV address (e.g., "127.0.0.1:3310" or "/var/run/clamav/clamd.ctl")
	Timeout time.Duration `yaml:"timeout" json:"timeout"`   // Scan timeout
	Action  string        `yaml:"action" json:"action"`     // "reject", "quarantine", "tag"
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

// Scan scans data for viruses using ClamAV
func (s *Scanner) Scan(data []byte) (*ScanResult, error) {
	if !s.IsEnabled() {
		return &ScanResult{Infected: false}, nil
	}

	conn, err := net.DialTimeout("tcp", s.addr, s.timeout)
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

		// Write length (4 bytes, big-endian)
		length := uint32(len(chunk))
		_, err := conn.Write([]byte{
			byte(length >> 24),
			byte(length >> 16),
			byte(length >> 8),
			byte(length),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to write chunk length: %w", err)
		}

		_, err = conn.Write(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to write chunk data: %w", err)
		}
		offset = end
	}

	// Send termination marker (0-length chunk)
	_, err = conn.Write([]byte{0, 0, 0, 0})
	if err != nil {
		return nil, fmt.Errorf("failed to send termination: %w", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
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
	} else if strings.HasSuffix(response, "ERROR") {
		return nil, fmt.Errorf("ClamAV error: %s", response)
	}

	return result, nil
}

// ScanVersion queries ClamAV for its version
func (s *Scanner) ScanVersion() (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("scanner not enabled")
	}

	conn, err := net.DialTimeout("tcp", s.addr, s.timeout)
	if err != nil {
		return "", fmt.Errorf("failed to connect to ClamAV: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("zVERSION\000"))
	if err != nil {
		return "", fmt.Errorf("failed to send VERSION command: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
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

	conn, err := net.DialTimeout("tcp", s.addr, s.timeout)
	if err != nil {
		return fmt.Errorf("ClamAV not reachable at %s: %w", s.addr, err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("zPING\000"))
	if err != nil {
		return fmt.Errorf("failed to send PING: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read PONG: %w", err)
	}

	if strings.TrimSpace(response) != "PONG" {
		return fmt.Errorf("unexpected ClamAV response: %s", response)
	}

	return nil
}
