package smtp

import (
	"bufio"
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// handleDATA: DKIM fail auth results header (with error string)
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineDKIMFailAuthResults(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&dkimFailAuthResultsStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done

	reader.ReadString('\n') // 250

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "Authentication-Results:") {
		t.Errorf("Expected Authentication-Results header, got: %s", safeSlice(dataStr, 300))
	}
	if !strings.Contains(dataStr, "dkim=fail") {
		t.Errorf("Expected dkim=fail in Authentication-Results, got: %s", safeSlice(dataStr, 300))
	}
}

type dkimFailAuthResultsStage struct{}

func (s *dkimFailAuthResultsStage) Name() string { return "DKIMFailAuth" }
func (s *dkimFailAuthResultsStage) Process(ctx *MessageContext) PipelineResult {
	ctx.DKIMResult = DKIMResult{Valid: false, Domain: "example.com", Error: "verification failed"}
	return ResultAccept
}

// ---------------------------------------------------------------------------
// handleDATA: DKIM fail with empty error string (fallback to default)
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineDKIMFailEmptyError(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&dkimFailNoErrorStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n')
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done
	reader.ReadString('\n')

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, `reason="verification failed"`) {
		t.Errorf("Expected default reason 'verification failed' for empty DKIM error, got: %s", safeSlice(dataStr, 400))
	}
}

type dkimFailNoErrorStage struct{}

func (s *dkimFailNoErrorStage) Name() string { return "DKIMFailNoError" }
func (s *dkimFailNoErrorStage) Process(ctx *MessageContext) PipelineResult {
	ctx.DKIMResult = DKIMResult{Valid: false, Domain: "example.com", Error: ""}
	return ResultAccept
}

// ---------------------------------------------------------------------------
// handleDATA: DMARC auth results header
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineDMARCAuthResults(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&dmarcAuthResultsStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n')
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done
	reader.ReadString('\n')

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "Authentication-Results:") {
		t.Errorf("Expected Authentication-Results header, got: %s", safeSlice(dataStr, 300))
	}
	if !strings.Contains(dataStr, "dmarc=fail") {
		t.Errorf("Expected dmarc=fail in Authentication-Results, got: %s", safeSlice(dataStr, 300))
	}
}

type dmarcAuthResultsStage struct{}

func (s *dmarcAuthResultsStage) Name() string { return "DMARCAuth" }
func (s *dmarcAuthResultsStage) Process(ctx *MessageContext) PipelineResult {
	ctx.DMARCResult = DMARCResult{Result: "fail", Policy: "reject"}
	return ResultAccept
}

// ---------------------------------------------------------------------------
// handleDATA: auth results with empty hostname (fallback to localhost)
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineAuthResultsEmptyHostname(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	session.server.config.Hostname = ""

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&authResultsStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n')
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done
	reader.ReadString('\n')

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "Authentication-Results: localhost") {
		t.Errorf("Expected 'Authentication-Results: localhost' for empty hostname, got: %s", safeSlice(dataStr, 300))
	}
}

// ---------------------------------------------------------------------------
// handleDATA: Received header without recipients (empty rcptTo)
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineReceivedNoRecipients(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	session.mutex.Lock()
	session.rcptTo = []string{}
	session.mutex.Unlock()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&testStage{name: "Test"})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n')
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done
	reader.ReadString('\n')

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if strings.Contains(dataStr, "for <") {
		t.Errorf("Expected Received header without 'for' clause when no recipients, got: %s", safeSlice(dataStr, 300))
	}
}

// ---------------------------------------------------------------------------
// handleDATA: existing Message-ID not duplicated
// ---------------------------------------------------------------------------

func TestHandleDATA_ExistingMessageID(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354

	clientConn.Write([]byte("Subject: Test\r\nMessage-ID: <existing@example.com>\r\n\r\nBody\r\n.\r\n"))
	<-done
	reader.ReadString('\n')

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	count := strings.Count(strings.ToLower(dataStr), "message-id:")
	if count != 1 {
		t.Errorf("Expected exactly 1 Message-ID header, found %d in: %s", count, safeSlice(dataStr, 300))
	}
}

// ---------------------------------------------------------------------------
// handleDATA: non-TLS session uses ESMTP proto in Received header
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineNonTLSReceivedHeader(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	session.mutex.Lock()
	session.isTLS = false
	session.mutex.Unlock()

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&testStage{name: "Test"})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n')
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done
	reader.ReadString('\n')

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "with ESMTP") {
		t.Errorf("Expected 'with ESMTP' for non-TLS session, got: %s", safeSlice(dataStr, 300))
	}
	if strings.Contains(dataStr, "with ESMTPS") {
		t.Errorf("Should NOT contain 'with ESMTPS' for non-TLS session, got: %s", safeSlice(dataStr, 300))
	}
}

// ---------------------------------------------------------------------------
// handleSTARTTLS: failed handshake (client does not upgrade)
// ---------------------------------------------------------------------------

func TestHandleSTARTTLS_HandshakeError(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	session.server.config.TLSConfig = tlsConfig

	done := make(chan error, 1)
	go func() {
		done <- session.handleSTARTTLS()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, readErr := clientConn.Read(buf)
	if readErr != nil {
		t.Fatalf("Failed to read 220: %v", readErr)
	}
	response := string(buf[:n])
	if !strings.Contains(response, "220") {
		t.Fatalf("Expected 220, got: %q", response)
	}

	// Close without upgrading to TLS
	clientConn.Close()

	err = <-done
	if err == nil {
		t.Error("Expected error from TLS handshake failure")
	}

	if session.IsTLS() {
		t.Error("Expected IsTLS=false after failed TLS handshake")
	}
}

// ---------------------------------------------------------------------------
// handleAuthLOGIN: connection closed during username read
// ---------------------------------------------------------------------------

func TestHandleAuthLOGIN_ReadUsernameError(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	time.Sleep(50 * time.Millisecond)
	clientConn.Close()

	err := <-done
	if err == nil {
		t.Error("Expected error when connection closed during username read")
	}
}

// ---------------------------------------------------------------------------
// handleAuthPLAIN: connection closed during credential read
// ---------------------------------------------------------------------------

func TestHandleAuthPLAIN_ReadCredentialError(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN")
	}()

	time.Sleep(50 * time.Millisecond)
	clientConn.Close()

	err := <-done
	if err == nil {
		t.Error("Expected error when connection closed during PLAIN credential read")
	}
}

// ---------------------------------------------------------------------------
// handleConnection: rate limiter rejects connection
// ---------------------------------------------------------------------------

func TestHandleConnection_RateLimited(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)
	server.SetRateLimiter(&denyRateLimiter{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer ln.Close()

	go server.Serve(ln)
	defer server.Stop()
	time.Sleep(50 * time.Millisecond)

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)
	resp, _ := reader.ReadString('\n')

	if !strings.HasPrefix(resp, "421") {
		t.Errorf("Expected 421 rate limit rejection, got: %q", resp)
	}
}

type denyRateLimiter struct{}

func (d *denyRateLimiter) Allow(key string, limitType string) bool { return false }

// ---------------------------------------------------------------------------
// handleConnection: rate limiter allows connection
// ---------------------------------------------------------------------------

func TestHandleConnection_RateLimiterAllows(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)
	server.SetRateLimiter(&allowRateLimiter{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer ln.Close()

	go server.Serve(ln)
	defer server.Stop()
	time.Sleep(50 * time.Millisecond)

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)
	resp, _ := reader.ReadString('\n')

	if !strings.HasPrefix(resp, "220") {
		t.Errorf("Expected 220 greeting when rate limiter allows, got: %q", resp)
	}
}

type allowRateLimiter struct{}

func (a *allowRateLimiter) Allow(key string, limitType string) bool { return true }

// ---------------------------------------------------------------------------
// parseRcptTo: with trailing parameters
// ---------------------------------------------------------------------------

func TestParseRcptTo_WithParameters(t *testing.T) {
	got, err := parseRcptTo("TO:<user@example.com> NOTIFY=SUCCESS")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if got != "user@example.com" {
		t.Errorf("Expected 'user@example.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// handleRCPT: invalid email validation failure
// ---------------------------------------------------------------------------

func TestHandleRCPT_InvalidEmailValidation(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.state = StateMailFrom

	go func() {
		s.HandleCommand("RCPT TO:<@invalid>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for invalid RCPT TO email, got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// handleMAIL: syntax error in from parameter
// ---------------------------------------------------------------------------

func TestHandleMAIL_SyntaxErrorFromParam(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.mutex.Lock()
	s.state = StateGreeted
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("MAIL invalid-format")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for MAIL with invalid format, got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// ValidateEmail edge cases
// ---------------------------------------------------------------------------

func TestValidateEmail_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"only at sign", "@", "", true},
		{"starts with at", "@domain.com", "", true},
		{"ends with at", "user@", "", true},
		{"normal", "user@domain.com", "user@domain.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateEmail(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resetTransaction when state is StateNew
// ---------------------------------------------------------------------------

func TestResetTransaction_StateNew(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	defer clientConn.Close()

	s.mutex.Lock()
	s.state = StateNew
	s.mailFrom = ""
	s.rcptTo = []string{}
	s.mutex.Unlock()

	s.resetTransaction()

	if s.State() != StateNew {
		t.Errorf("Expected state to remain StateNew after reset, got %v", s.State())
	}
}

// ---------------------------------------------------------------------------
// WriteResponse error path (closed connection)
// ---------------------------------------------------------------------------

func TestWriteResponse_ClosedConnection(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	clientConn.Close()

	err := s.WriteResponse(220, "test")
	if err == nil {
		t.Error("Expected error writing to closed connection")
	}
}

// ---------------------------------------------------------------------------
// WriteMultiLineResponse single line
// ---------------------------------------------------------------------------

func TestWriteMultiLineResponse_SingleLine(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.WriteMultiLineResponse(250, []string{"only line"})
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250 only line") {
		t.Errorf("Expected '250 only line', got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// readData with timeout configured
// ---------------------------------------------------------------------------

func TestReadData_WithReadTimeout(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	session.server.config.ReadTimeout = 5 * time.Second

	done := make(chan struct {
		data []byte
		err  error
	}, 1)
	go func() {
		d, e := session.readData()
		done <- struct {
			data []byte
			err  error
		}{d, e}
	}()

	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))

	result := <-done
	if result.err != nil {
		t.Fatalf("readData returned error: %v", result.err)
	}
	if !strings.Contains(string(result.data), "Body") {
		t.Errorf("Expected body in read data, got: %q", string(result.data))
	}
}

// ---------------------------------------------------------------------------
// handleMAIL: empty from with SIZE parameter
// ---------------------------------------------------------------------------

func TestHandleMAIL_EmptyFromWithParams(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.mutex.Lock()
	s.state = StateGreeted
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("MAIL FROM:<> SIZE=1024")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for empty sender with params, got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// safeSlice helper to avoid slice bounds panics
// ---------------------------------------------------------------------------

func safeSlice(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
