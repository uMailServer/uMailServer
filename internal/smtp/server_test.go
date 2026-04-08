package smtp

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestServer(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	t.Run("ListenAndServe", func(t *testing.T) {
		// Start server on random port
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}

		go func() {
			server.Serve(ln)
		}()

		// Give server time to start
		time.Sleep(100 * time.Millisecond)

		// Connect to server
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Read greeting
		reader := bufio.NewReader(conn)
		greeting, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read greeting: %v", err)
		}

		if !strings.HasPrefix(greeting, "220") {
			t.Errorf("Expected 220 greeting, got: %s", greeting)
		}

		server.Stop()
	})
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantArg string
	}{
		{"EHLO example.com", "EHLO", "example.com"},
		{"MAIL FROM:<test@example.com>", "MAIL", "FROM:<test@example.com>"},
		{"QUIT", "QUIT", ""},
		{"NOOP", "NOOP", ""},
		{"  RCPT  TO:<test>  ", "RCPT", "TO:<test>"},
	}

	for _, tt := range tests {
		cmd, arg := parseCommand(tt.input)
		if cmd != tt.wantCmd {
			t.Errorf("parseCommand(%q) cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
		}
		if arg != tt.wantArg {
			t.Errorf("parseCommand(%q) arg = %q, want %q", tt.input, arg, tt.wantArg)
		}
	}
}

func TestParseMailFrom(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"FROM:<sender@example.com>", "sender@example.com", false},
		{"FROM:<>", "", false},
		{"FROM:sender@example.com", "sender@example.com", false},
		{"<sender@example.com>", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := parseMailFrom(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseMailFrom(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseMailFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseRcptTo(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"TO:<recipient@example.com>", "recipient@example.com", false},
		{"TO:recipient@example.com", "recipient@example.com", false},
		{"<recipient@example.com>", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := parseRcptTo(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseRcptTo(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseRcptTo(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"test@example.com", "test@example.com", false},
		{"<test@example.com>", "test@example.com", false},
		{"Test User <test@example.com>", "test@example.com", false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := ValidateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestServerConfig(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.config != config {
		t.Error("expected config to be set")
	}
	if server.config.Hostname != "mail.example.com" {
		t.Errorf("expected hostname 'mail.example.com', got %s", server.config.Hostname)
	}
}

func TestServerSetHandler(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	handler := func(from string, to []string, data []byte) error {
		return nil
	}

	server.SetDeliveryHandler(handler)
	if server.onDeliver == nil {
		t.Error("expected handler to be set")
	}
}

func TestServerSetAuthHandler(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	authHandler := func(username, password string) (bool, error) {
		return true, nil
	}

	server.SetAuthHandler(authHandler)
	if server.onAuth == nil {
		t.Error("expected authHandler to be set")
	}
}

func TestServerWithTLS(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  false,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	server := NewServer(config, nil)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestValidateEmailVariations(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"user@example.com", "user@example.com", false},
		{"<user@example.com>", "user@example.com", false},
		{"User <user@example.com>", "user@example.com", false},
		{"user+tag@example.com", "user+tag@example.com", false},
		{"user.name@example.com", "user.name@example.com", false},
		{"@example.com", "", true},
		{"user@", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := ValidateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseCommandVariations(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantArg string
	}{
		{"EHLO client.example.com", "EHLO", "client.example.com"},
		{"ehlo client.example.com", "EHLO", "client.example.com"},
		{"MAIL FROM:<test@example.com>", "MAIL", "FROM:<test@example.com>"},
		{"RCPT TO:<recipient@example.com>", "RCPT", "TO:<recipient@example.com>"},
		{"QUIT", "QUIT", ""},
		{"NOOP", "NOOP", ""},
		{"RSET", "RSET", ""},
		{"DATA", "DATA", ""},
		{"  SPACES  around  ", "SPACES", "around"},
	}

	for _, tt := range tests {
		cmd, arg := parseCommand(tt.input)
		if cmd != tt.wantCmd {
			t.Errorf("parseCommand(%q) cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
		}
		if arg != tt.wantArg {
			t.Errorf("parseCommand(%q) arg = %q, want %q", tt.input, arg, tt.wantArg)
		}
	}
}

func TestParseMailFromVariations(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"FROM:<sender@example.com>", "sender@example.com", false},
		{"FROM:<>", "", false},
		{"FROM:sender@example.com", "sender@example.com", false},
		{"from:<sender@example.com>", "sender@example.com", false},
		{"FROM: <sender@example.com>", "sender@example.com", false},
		{"<sender@example.com>", "", true},
		{"", "", true},
		{"FROM:", "", false},
	}

	for _, tt := range tests {
		got, err := parseMailFrom(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseMailFrom(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseMailFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestConfigStruct(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 10485760,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowInsecure:  true,
		TLSConfig:      nil,
	}

	if config.MaxMessageSize != 10485760 {
		t.Errorf("expected MaxMessageSize 10485760, got %d", config.MaxMessageSize)
	}
	if config.MaxRecipients != 100 {
		t.Errorf("expected MaxRecipients 100, got %d", config.MaxRecipients)
	}
	if !config.AllowInsecure {
		t.Error("expected AllowInsecure to be true")
	}
}

func TestSetPipeline(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	pipeline := NewPipeline(nil)
	server.SetPipeline(pipeline)
	if server.pipeline == nil {
		t.Error("expected pipeline to be set")
	}
}

func TestGetIPFromAddr(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1:25", "192.168.1.1"},
		{"[::1]:25", "::1"},
		{"127.0.0.1:587", "127.0.0.1"},
		{"invalid", "invalid"},
		{"", ""},
	}

	for _, tt := range tests {
		got := getIPFromAddr(tt.input)
		if got != tt.want {
			t.Errorf("getIPFromAddr(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestListenAndServeTLS(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  false,
	}
	server := NewServer(config, nil)

	// Test with nil TLS config should fail
	err := server.ListenAndServeTLS("127.0.0.1:0", nil)
	if err == nil {
		t.Error("Expected error with nil TLS config")
		server.Stop()
	}
}

func TestListenAndServeInvalidAddr(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	// Use an invalid address that can't be bound
	err := server.ListenAndServe("256.256.256.256:99999")
	if err == nil {
		t.Error("Expected error with invalid address")
		server.Stop()
	}
}

func TestDefaultLogger(t *testing.T) {
	logger := &defaultLogger{}
	// Just verify they don't panic
	logger.Debug("test debug", "key", "value")
	logger.Info("test info", "key", "value")
	logger.Warn("test warn", "key", "value")
	logger.Error("test error", "key", "value")
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is a ..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestServerSetAuthLimits(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	// Set auth limits with maxAttempts=5 and lockoutDuration=10 minutes
	server.SetAuthLimits(5, 10*time.Minute)

	// Verify the values are set
	if server.maxLoginAttempts != 5 {
		t.Errorf("expected maxLoginAttempts=5, got %d", server.maxLoginAttempts)
	}
	if server.lockoutDuration != 10*time.Minute {
		t.Errorf("expected lockoutDuration=10m, got %v", server.lockoutDuration)
	}
}

func TestServerSetAuthLimits_ZeroMaxAttempts(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	// Set auth limits with maxAttempts=0 (disabled)
	server.SetAuthLimits(0, 0)

	if server.maxLoginAttempts != 0 {
		t.Errorf("expected maxLoginAttempts=0, got %d", server.maxLoginAttempts)
	}

	// isAuthLockedOut should return false when maxLoginAttempts is 0
	locked := server.isAuthLockedOut("127.0.0.1")
	if locked {
		t.Error("expected isAuthLockedOut=false when maxLoginAttempts=0")
	}
}

func TestServerIsAuthLockedOut_WithFailures(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	// Set auth limits with maxAttempts=3 and 1 hour lockout
	server.SetAuthLimits(3, time.Hour)

	// Should not be locked out initially
	if server.isAuthLockedOut("192.168.1.1") {
		t.Error("expected not locked out initially")
	}

	// Record some failures
	server.recordAuthFailure("192.168.1.1")
	server.recordAuthFailure("192.168.1.1")

	// Still should not be locked (only 2 failures)
	if server.isAuthLockedOut("192.168.1.1") {
		t.Error("expected not locked out with 2 failures")
	}

	// Third failure should lock out
	server.recordAuthFailure("192.168.1.1")
	if !server.isAuthLockedOut("192.168.1.1") {
		t.Error("expected locked out with 3 failures")
	}
}

func TestServerClearAuthFailures(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	server.SetAuthLimits(3, time.Hour)
	server.recordAuthFailure("192.168.1.1")
	server.recordAuthFailure("192.168.1.1")

	if server.isAuthLockedOut("192.168.1.1") {
		t.Error("expected not locked out before clear")
	}

	server.clearAuthFailures("192.168.1.1")

	if server.isAuthLockedOut("192.168.1.1") {
		t.Error("expected not locked out after clear")
	}
}

func TestServerRecordAuthFailure_Disabled(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	// Set maxLoginAttempts to 0 (disabled)
	server.SetAuthLimits(0, 0)

	// Should not panic
	server.recordAuthFailure("127.0.0.1")
}

// TestServerAuthFailuresCleanupAtThresholdRegression tests the fix for the auth
// failures cleanup bug where cleanup only ran at exact multiples of 100
// (len%100==0) instead of at any count >= 100. This caused stale auth failure
// entries to accumulate between cleanup cycles when len was between 100-199.
// The fix changed the condition from len(authFailures)%100==0 to len(authFailures)>=100.
func TestServerAuthFailuresCleanupAtThresholdRegression(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	// Set auth limits with short lockout duration so old entries can be cleaned
	shortDuration := 100 * time.Millisecond
	server.SetAuthLimits(3, shortDuration)

	// Use unsafe.Pointer to access the unexported authFailures map
	// This is necessary because we need to inject stale entries to test cleanup behavior
	authFailuresPtr := (*map[string][]time.Time)(unsafe.Pointer(
		uintptr(unsafe.Pointer(reflect.ValueOf(server).Elem().FieldByName("authFailures").UnsafeAddr()))))

	// Add 100 recent auth failures (all different IPs)
	for i := 0; i < 100; i++ {
		server.recordAuthFailure(fmt.Sprintf("192.168.1.%d", i))
	}

	// At this point, len(*authFailuresPtr) == 100, cleanup has run but removed nothing (all recent)
	// Wait a bit so new entries will be "older" relative to short lockoutDuration
	time.Sleep(150 * time.Millisecond)

	// Directly add a stale entry (entry older than lockoutDuration)
	// This simulates entries that would exist in a real scenario between cleanup cycles
	staleIP := "10.0.0.1"
	staleTime := time.Now().Add(-200 * time.Millisecond) // older than shortDuration
	(*authFailuresPtr)[staleIP] = []time.Time{staleTime}

	// Verify the stale entry exists
	if times, ok := (*authFailuresPtr)[staleIP]; !ok || len(times) != 1 {
		t.Fatal("failed to inject stale entry for testing")
	}

	// Now len(*authFailuresPtr) == 101
	// With the BUG (len%100==0): cleanup would NOT run at len=101, stale entry stays
	// With the FIX (len>=100): cleanup RUNS at 101, stale entry is removed

	// Add one more failure to trigger cleanup at len=101
	server.recordAuthFailure("192.168.2.1")

	// Check if stale entry was cleaned up
	if times, ok := (*authFailuresPtr)[staleIP]; ok && len(times) > 0 {
		t.Error("stale auth failure entry should have been cleaned up at len=101, but it remains")
		t.Log("This indicates the cleanup condition is still using %100==0 instead of >=100")
	}
}
