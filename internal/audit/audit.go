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
	AdminAction    EventType = "admin_action"
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
	if maxSizeMB <= 0 {
		maxSizeMB = 100 // a zero limit would rotate on every write (F5791)
	}
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
	closed     bool
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

	if r.closed {
		return 0, os.ErrClosed
	}
	if r.file == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
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
		r.file = nil // reopened below or lazily by Write on failure (F5792)
	}
	timestamp := time.Now().Format("20060102-150405.000000000")
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
	matches := r.backupMatches()

	validMatches := r.filterValidMatches(matches)
	r.cleanupByAge(validMatches)

	if r.maxBackups > 0 {
		r.cleanupByCount()
	}
}

// backupMatches lists rotated files by literal name prefix; filepath.Glob
// would misread metacharacters in the configured path and never match (F6750).
func (r *rotatingWriter) backupMatches() []string {
	dir := filepath.Dir(r.filename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefix := filepath.Base(r.filename) + "."
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
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
	validMatches := r.filterValidMatches(r.backupMatches())
	if len(validMatches) <= r.maxBackups {
		return
	}

	for len(validMatches) > r.maxBackups {
		oldest, oldestInfo := r.findOldest(validMatches)
		if oldestInfo == nil {
			return
		}
		if err := os.Remove(oldest); err != nil {
			return
		}
		validMatches = r.filterValidMatches(validMatches)
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
		err := r.file.Close()
		r.file = nil
		r.closed = true
		return err
	}
	r.closed = true
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

	event.Details = redactDetails(event.Details)

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

// LogAdminAction logs an administrative mutation (domain, alias, queue, key
// rotation, rate-limit changes) with the acting admin and the affected object.
func (l *Logger) LogAdminAction(actor, ip, action, target string) {
	if l == nil {
		return
	}
	_ = l.Log(Event{
		Type:    AdminAction,
		User:    actor,
		IP:      ip,
		Success: true,
		Details: map[string]string{"action": action, "target": target},
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

// LogProtocolLogin records the result of a non-API authentication attempt
// (SMTP submission, IMAP, POP3). service should be one of "smtp", "imap",
// "pop3"; reason is recorded only on failure and may be empty.
func (l *Logger) LogProtocolLogin(service, user, ip string, success bool, reason string) {
	evt := Event{
		User:    user,
		IP:      ip,
		Success: success,
		Service: service,
	}
	if success {
		evt.Type = LoginSuccess
	} else {
		evt.Type = LoginFailure
		if reason != "" {
			evt.Details = map[string]string{"reason": reason}
		}
	}
	_ = l.Log(evt)
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

var (
	trustedMu      sync.RWMutex
	trustedProxies []string
)

// SetTrustedProxies configures the proxy addresses (exact IPs or CIDRs, the
// same list as the API's trusted_proxies) whose X-Forwarded-For / X-Real-IP
// headers ExtractIP honours. With none configured the headers are ignored (F6801).
func SetTrustedProxies(list []string) {
	trustedMu.Lock()
	trustedProxies = append([]string(nil), list...)
	trustedMu.Unlock()
}

func isTrustedProxy(ip string) bool {
	trustedMu.RLock()
	list := trustedProxies
	trustedMu.RUnlock()
	return inProxyList(ip, list)
}

func inProxyList(ip string, list []string) bool {
	parsed := net.ParseIP(ip)
	for _, t := range list {
		t = strings.TrimSpace(t)
		if t == ip {
			return true
		}
		if parsed == nil {
			continue
		}
		if _, n, err := net.ParseCIDR(t); err == nil && n.Contains(parsed) {
			return true
		}
		if tp := net.ParseIP(t); tp != nil && tp.Equal(parsed) {
			return true
		}
	}
	return false
}

// ExtractIP returns the client IP for an audit record using the globally
// configured trusted proxies (see SetTrustedProxies).
func ExtractIP(r *http.Request) string {
	trustedMu.RLock()
	list := trustedProxies
	trustedMu.RUnlock()
	return ClientIP(r, list)
}

// ClientIP returns the client IP. Forwarding headers are client-controlled, so
// they count only when the direct peer is in trusted. The X-Forwarded-For chain
// is walked right-to-left and the rightmost entry that is not itself a trusted
// proxy is returned (leftmost entries can be forged by the client). An
// unparseable entry stops the walk and the peer address is used (F6990).
func ClientIP(r *http.Request, trusted []string) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if !inProxyList(peer, trusted) {
		return peer
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			e := strings.TrimSpace(parts[i])
			if net.ParseIP(e) == nil {
				return peer
			}
			if !inProxyList(e, trusted) {
				return e
			}
		}
		return peer
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" && net.ParseIP(xri) != nil {
		return xri
	}
	return peer
}

// Close closes the audit logger
func (l *Logger) Close() error {
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

// redactDetails returns details with the values of secret-looking keys
// replaced, so credentials passed by a caller never reach the log (F6419).
func redactDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return details
	}
	out := make(map[string]string, len(details))
	for k, v := range details {
		lk := strings.ToLower(k)
		for _, w := range []string{"password", "passwd", "secret", "token", "authorization", "cookie", "apikey", "api_key", "totp", "otp", "credential"} {
			if strings.Contains(lk, w) {
				v = "[REDACTED]"
				break
			}
		}
		out[k] = v
	}
	return out
}
