package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EventType represents the type of audit event
type EventType string

const (
	LoginSuccess   EventType = "login_success"
	LoginFailure   EventType = "login_failure"
	Logout         EventType = "logout"
	AccountCreate  EventType = "account_create"
	AccountUpdate  EventType = "account_update"
	AccountDelete  EventType = "account_delete"
	TOTPEnable     EventType = "totp_enable"
	TOTPDisable    EventType = "totp_disable"
	PasswordChange EventType = "password_change"
)

// Event represents a single audit log entry
type Event struct {
	Timestamp string            `json:"timestamp"`
	Type      EventType         `json:"type"`
	User      string            `json:"user,omitempty"`
	IP        string            `json:"ip,omitempty"`
	Success   bool              `json:"success"`
	Details   map[string]string `json:"details,omitempty"`
	Service   string            `json:"service"` // "api", "smtp", "imap", "pop3"
}

// Logger handles structured audit logging with rotation
type Logger struct {
	writer   io.WriteCloser
	mu       sync.Mutex
	logPath  string
	rotating bool
}

// NewLogger creates a new audit logger that writes to the specified path.
// If logPath is empty, audit logging is disabled.
func NewLogger(logPath string, maxSizeMB, maxBackups, maxAgeDays int) (*Logger, error) {
	if logPath == "" {
		return &Logger{}, nil // disabled
	}

	// Ensure directory exists
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("audit: failed to create directory: %w", err)
	}

	// Create rotating writer for audit logs
	w, err := newRotatingWriter(logPath, maxSizeMB, maxBackups, maxAgeDays)
	if err != nil {
		return nil, err
	}

	return &Logger{
		writer:   w,
		logPath:  logPath,
		rotating: true,
	}, nil
}

// newRotatingWriter creates a rotating writer for audit logs
func newRotatingWriter(filename string, maxSizeMB, maxBackups, maxAgeDays int) (*rotatingWriter, error) {
	r := &rotatingWriter{
		filename:   filename,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
		maxAge:     time.Duration(maxAgeDays) * 24 * time.Hour,
	}

	// Open or create file
	if err := r.open(); err != nil {
		return nil, err
	}

	return r, nil
}

type rotatingWriter struct {
	filename   string
	maxSize    int64
	maxBackups int
	maxAge     time.Duration
	mu         sync.Mutex
	file       *os.File
	size       int64
}

func (r *rotatingWriter) open() error {
	info, err := os.Stat(r.filename)
	if err == nil {
		r.size = info.Size()
	} else {
		r.size = 0
	}

	f, err := os.OpenFile(r.filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open audit log: %w", err)
	}
	r.file = f
	return nil
}

func (r *rotatingWriter) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingWriter) rotate() error {
	if r.file != nil {
		if err := r.file.Close(); err != nil {
			return fmt.Errorf("failed to close audit log before rotation: %w", err)
		}
	}
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s.%s", r.filename, timestamp)
	if _, err := os.Stat(r.filename); err == nil {
		if err := os.Rename(r.filename, backupName); err != nil {
			return fmt.Errorf("failed to rotate audit log: %w", err)
		}
	}
	if err := r.open(); err != nil {
		return err
	}
	go r.cleanup()
	return nil
}

func (r *rotatingWriter) cleanup() {
	if r.maxBackups <= 0 && r.maxAge <= 0 {
		return
	}
	matches, err := filepath.Glob(r.filename + ".*")
	if err != nil {
		return
	}

	validMatches := r.filterValidMatches(matches)
	r.cleanupByAge(validMatches)

	if r.maxBackups > 0 {
		r.cleanupByCount()
	}
}

func (r *rotatingWriter) filterValidMatches(matches []string) []string {
	valid := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, err := os.Stat(match); err == nil {
			valid = append(valid, match)
		}
	}
	return valid
}

func (r *rotatingWriter) cleanupByAge(matches []string) {
	if r.maxAge <= 0 {
		return
	}
	cutoff := time.Now().Add(-r.maxAge)
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(match)
		}
	}
}

func (r *rotatingWriter) cleanupByCount() {
	matches, err := filepath.Glob(r.filename + ".*")
	if err != nil {
		return
	}
	validMatches := r.filterValidMatches(matches)
	if len(validMatches) <= r.maxBackups {
		return
	}

	oldest, oldestInfo := r.findOldest(validMatches)
	if oldestInfo != nil {
		_ = os.Remove(oldest)
	}
}

func (r *rotatingWriter) findOldest(matches []string) (string, os.FileInfo) {
	if len(matches) == 0 {
		return "", nil
	}
	oldest := matches[0]
	oldestInfo, err := os.Stat(oldest)
	if err != nil {
		return "", nil
	}
	for _, m := range matches[1:] {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.ModTime().Before(oldestInfo.ModTime()) {
			oldest = m
			oldestInfo = info
		}
	}
	return oldest, oldestInfo
}

func (r *rotatingWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// Log writes an audit event
func (l *Logger) Log(event Event) error {
	if l.writer == nil {
		return nil // disabled
	}

	// Add timestamp if not set
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("audit: failed to marshal event: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("audit: failed to write event: %w", err)
	}

	return nil
}

// LogLoginSuccess logs a successful login
func (l *Logger) LogLoginSuccess(user, ip string) {
	_ = l.Log(Event{
		Type:    LoginSuccess,
		User:    user,
		IP:      ip,
		Success: true,
		Service: "api",
	})
}

// LogLoginFailure logs a failed login attempt
func (l *Logger) LogLoginFailure(user, ip, reason string) {
	_ = l.Log(Event{
		Type:    LoginFailure,
		User:    user,
		IP:      ip,
		Success: false,
		Details: map[string]string{"reason": reason},
		Service: "api",
	})
}

// LogLogout logs a logout event
func (l *Logger) LogLogout(user, ip string) {
	_ = l.Log(Event{
		Type:    Logout,
		User:    user,
		IP:      ip,
		Success: true,
		Service: "api",
	})
}

// LogAccountCreate logs account creation
func (l *Logger) LogAccountCreate(actor, target, ip string) {
	_ = l.Log(Event{
		Type:    AccountCreate,
		User:    actor,
		IP:      ip,
		Success: true,
		Details: map[string]string{"target": target},
		Service: "api",
	})
}

// LogAccountUpdate logs account update
func (l *Logger) LogAccountUpdate(actor, target, ip string, changes []string) {
	details := map[string]string{"target": target}
	for _, c := range changes {
		details["change_"+c] = c
	}
	_ = l.Log(Event{
		Type:    AccountUpdate,
		User:    actor,
		IP:      ip,
		Success: true,
		Details: details,
		Service: "api",
	})
}

// LogAccountDelete logs account deletion
func (l *Logger) LogAccountDelete(actor, target, ip string) {
	_ = l.Log(Event{
		Type:    AccountDelete,
		User:    actor,
		IP:      ip,
		Success: true,
		Details: map[string]string{"target": target},
		Service: "api",
	})
}

// LogTOTPEnable logs TOTP 2FA enablement
func (l *Logger) LogTOTPEnable(user, target, ip string) {
	_ = l.Log(Event{
		Type:    TOTPEnable,
		User:    user,
		IP:      ip,
		Success: true,
		Details: map[string]string{"target": target},
		Service: "api",
	})
}

// LogTOTPDisable logs TOTP 2FA disablement
func (l *Logger) LogTOTPDisable(user, target, ip string) {
	_ = l.Log(Event{
		Type:    TOTPDisable,
		User:    user,
		IP:      ip,
		Success: true,
		Details: map[string]string{"target": target},
		Service: "api",
	})
}

// ExtractIP extracts the IP address from a request
func ExtractIP(r *http.Request) string {
	// Check X-Forwarded-For first (for reverse proxy setups)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		xff = strings.TrimSpace(xff)
		if net.ParseIP(xff) != nil {
			return xff
		}
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return ip
}

// Close closes the audit logger
func (l *Logger) Close() error {
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}
