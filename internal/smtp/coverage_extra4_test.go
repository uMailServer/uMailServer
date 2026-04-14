package smtp

import (
	"bufio"
	"crypto/hmac"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCoverSetUserSecretHandler(t *testing.T) {
	server := NewServer(&Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
	}, nil)

	handler := func(username string) (string, error) {
		return "secret", nil
	}
	server.SetUserSecretHandler(handler)
	if server.onGetUserSecret == nil {
		t.Error("expected onGetUserSecret to be set")
	}
}

func TestCoverDefaultLoggerDirectly(t *testing.T) {
	l := &defaultLogger{}
	l.Debug("debug msg", "key", "val")
	l.Info("info msg", "key", "val")
	l.Warn("warn msg", "key", "val")
	l.Error("error msg", "key", "val")
}

func TestCoverDefaultLoggerViaPipelineProcess(t *testing.T) {
	pipeline := NewPipeline(nil)
	pipeline.AddStage(&testStage{name: "CoverDefaultLogger"})
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultAccept {
		t.Errorf("expected ResultAccept, got %v", result)
	}
}

func TestCoverListenAndServe(t *testing.T) {
	config := &Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	go func() { _ = server.Serve(ln) }()
	time.Sleep(100 * time.Millisecond)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Close()
	server.Stop()
}

func TestCoverListenAndServeTLS(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
		TLSConfig: &tls.Config{
			Certificates:       []tls.Certificate{cert},
			InsecureSkipVerify: true,
		},
	}
	server := NewServer(config, nil)

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatalf("failed to create TLS listener: %v", err)
	}
	go func() { _ = server.Serve(ln) }()
	time.Sleep(100 * time.Millisecond)
	tlsConn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("failed to connect with TLS: %v", err)
	}
	defer tlsConn.Close()
	reader := bufio.NewReader(tlsConn)
	greeting, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	if !strings.HasPrefix(greeting, "220") {
		t.Errorf("expected 220 greeting, got: %s", greeting)
	}
}

func TestCoverEHLO_CRAMMD5Capability(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.server.onGetUserSecret = func(username string) (string, error) {
		return "shared-secret", nil
	}

	go func() {
		s.HandleCommand("EHLO testclient")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fullResp string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read EHLO response: %v", err)
		}
		fullResp += line
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if !strings.Contains(fullResp, "CRAM-MD5") {
		t.Errorf("expected CRAM-MD5 capability when onGetUserSecret is set, got: %q", fullResp)
	}
}

func TestCoverMAIL_SubmissionModeRequireAuth(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.RequireAuth = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isAuth = false
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("MAIL FROM:<user@example.com>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "530") {
		t.Errorf("expected 530 authentication required in submission mode, got: %q", resp)
	}
}

func TestCoverRCPT_FromRcptToState(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.mutex.Lock()
	s.state = StateRcptTo
	s.rcptTo = []string{"first@example.com"}
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("RCPT TO:<second@example.com>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("expected 250 for RCPT TO in RcptTo state, got: %q", resp)
	}
	s.mutex.RLock()
	if len(s.rcptTo) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(s.rcptTo))
	}
	s.mutex.RUnlock()
}

func TestCoverAuthCRAMMD5_NoSecretHandler(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	// onGetUserSecret is nil -> should return 504
	go func() {
		s.HandleCommand("AUTH CRAM-MD5")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "504") {
		t.Errorf("expected 504 when onGetUserSecret is nil, got: %q", resp)
	}
}

func TestCoverAuthCRAMMD5_FullFlow(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	sharedSecret := "test-shared-secret"
	s.server.onGetUserSecret = func(username string) (string, error) {
		if username == "testuser" {
			return sharedSecret, nil
		}
		return "", fmt.Errorf("user not found")
	}
	s.server.onAuth = func(username, password string) (bool, error) {
		return false, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH CRAM-MD5")
	}()

	// Read the 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read challenge: %v", err)
	}
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("expected 334 challenge, got: %q", resp)
	}

	// Extract the base64 challenge and compute correct HMAC
	challengeB64 := strings.TrimSpace(strings.TrimPrefix(resp, "334 "))
	challengeBytes, _ := base64.StdEncoding.DecodeString(challengeB64)
	mac := hmac.New(md5.New, []byte(sharedSecret))
	mac.Write(challengeBytes)
	expectedHex := hex.EncodeToString(mac.Sum(nil))
	clientResponse := base64.StdEncoding.EncodeToString([]byte("testuser " + expectedHex))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(clientResponse + "\r\n"))

	// Read the 235 success response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "235") {
		t.Errorf("expected 235 auth success, got: %q", resp2)
	}
	<-done

	if !s.IsAuthenticated() {
		t.Error("expected session to be authenticated")
	}
	if s.Username() != "testuser" {
		t.Errorf("expected username 'testuser', got %q", s.Username())
	}
}

func TestCoverAuthCRAMMD5_InvalidResponse(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onGetUserSecret = func(username string) (string, error) {
		return "secret", nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH CRAM-MD5")
	}()

	// Read the 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("expected 334, got: %q", resp)
	}
	// Send invalid base64 response
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("not-valid-base64!!!\r\n"))

	// Read 535 failure
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "535") {
		t.Errorf("expected 535 for invalid CRAM-MD5 response, got: %q", resp2)
	}
	<-done
}

func TestCoverAuthCRAMMD5_WrongHMAC(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onGetUserSecret = func(username string) (string, error) {
		return "correct-secret", nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH CRAM-MD5")
	}()

	// Read the 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("expected 334, got: %q", resp)
	}
	// Send a response with wrong HMAC
	wrongResponse := base64.StdEncoding.EncodeToString([]byte("testuser deadbeef"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(wrongResponse + "\r\n"))

	// Read 535 failure
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "535") {
		t.Errorf("expected 535 for wrong HMAC, got: %q", resp2)
	}
	<-done
}

func TestCoverAuthCRAMMD5_SecretLookupError(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onGetUserSecret = func(username string) (string, error) {
		return "", fmt.Errorf("user not found")
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH CRAM-MD5")
	}()

	// Read the 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("expected 334, got: %q", resp)
	}
	// Send response with valid base64
	response := base64.StdEncoding.EncodeToString([]byte("unknown deadbeef"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(response + "\r\n"))

	// Read 535 failure
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "535") {
		t.Errorf("expected 535 for secret lookup error, got: %q", resp2)
	}
	<-done
}

func TestCoverAuthCRAMMD5_ConnectionClosedDuringChallenge(t *testing.T) {
	t.Skip("CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken")
	s, clientConn, _ := createSessionWithPipe(t)
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onGetUserSecret = func(username string) (string, error) {
		return "secret", nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH CRAM-MD5")
	}()

	time.Sleep(50 * time.Millisecond)
	clientConn.Close()
	<-done
}

func TestCoverSTARTTLS_WriteResponseError(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}
	s.server.config.TLSConfig = &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	clientConn.Close()
	err = s.handleSTARTTLS()
	if err == nil {
		t.Error("expected error when connection closed during STARTTLS WriteResponse")
	}
}

func TestCoverAuthLOGIN_PasswordReadError(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onAuth = func(username, password string) (bool, error) {
		return true, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	time.Sleep(50 * time.Millisecond)
	clientConn.Close()
	<-done
}

func TestCoverValidateEmail_InternationalEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "international with non-ASCII",
			input:   "\xc3\xa9tudiant@university.fr",
			want:    "\xc3\xa9tudiant@university.fr",
			wantErr: false,
		},
		{name: "starts with @ only", input: "@", want: "", wantErr: true},
		{name: "starts with @domain", input: "@domain.com", want: "", wantErr: true},
		{name: "ends with @", input: "user@", want: "", wantErr: true},
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

func TestCoverPipelineLoggerAllMethods(t *testing.T) {
	pl := NewPipelineLogger(nil)
	if pl == nil {
		t.Error("expected non-nil PipelineLogger")
	}
}

func TestCoverServe_AcceptErrorAfterStop(t *testing.T) {
	config := &Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}
	server := NewServer(config, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_ = server.Serve(ln)
		done <- nil
	}()
	time.Sleep(50 * time.Millisecond)
	server.Stop()
	err = <-done
	if err != nil {
		t.Errorf("expected nil from Serve after Stop, got: %v", err)
	}
}

func TestCoverParseCommand_WhitespaceOnly(t *testing.T) {
	cmd, arg := parseCommand("   ")
	if cmd != "" || arg != "" {
		t.Errorf("expected empty cmd/arg for whitespace-only input, got cmd=%q arg=%q", cmd, arg)
	}
}

func TestCoverListenAndServe_BindError(t *testing.T) {
	config := &Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
	}
	server := NewServer(config, nil)
	// Bind to an invalid address - this will fail
	err := server.ListenAndServe("256.256.256.256:25")
	if err == nil {
		t.Error("expected error from ListenAndServe with invalid address")
		server.Stop()
	}
}

func TestCoverListenAndServeTLS_BindError(t *testing.T) {
	config := &Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
	}
	server := NewServer(config, nil)
	err := server.ListenAndServeTLS("256.256.256.256:25", &tls.Config{})
	if err == nil {
		t.Error("expected error from ListenAndServeTLS with invalid address")
		server.Stop()
	}
}

func TestCoverDATA_DeliveryError(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.server.onDeliver = func(from string, to []string, data []byte) error {
		return fmt.Errorf("delivery failed")
	}
	s.mutex.Lock()
	s.state = StateRcptTo
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("DATA")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("expected 354, got: %q", resp)
	}
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "451") {
		t.Errorf("expected 451 for delivery error, got: %q", resp2)
	}
	<-done
}

func TestCoverDATA_MessageTooLarge(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.MaxMessageSize = 10 // Very small limit
	s.mutex.Lock()
	s.state = StateRcptTo
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("DATA")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("expected 354, got: %q", resp)
	}
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("Subject: This is a long subject that exceeds the limit\r\n\r\nBody content here\r\n.\r\n"))

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "552") {
		t.Errorf("expected 552 for message too large, got: %q", resp2)
	}
	<-done
}

func TestCoverBDAT_LastChunkWithDelivery(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	var deliveredFrom, deliveredData string
	var deliveredTo []string
	s.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredFrom = from
		deliveredTo = to
		deliveredData = string(data)
		return nil
	}
	s.mutex.Lock()
	s.state = StateRcptTo
	s.mailFrom = "sender@example.com"
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("BDAT 5 LAST")
	}()

	// Write the chunk data
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("Hello"))

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("expected 250 for BDAT LAST, got: %q", resp)
	}
	<-done

	if deliveredFrom != "sender@example.com" {
		t.Errorf("expected from 'sender@example.com', got %q", deliveredFrom)
	}
	if deliveredData != "Hello" {
		t.Errorf("expected data 'Hello', got %q", deliveredData)
	}
	if len(deliveredTo) != 1 || deliveredTo[0] != "rcpt@example.com" {
		t.Errorf("expected to ['rcpt@example.com'], got %v", deliveredTo)
	}
}

func TestCoverBDAT_DeliveryError(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.server.onDeliver = func(from string, to []string, data []byte) error {
		return fmt.Errorf("delivery error")
	}
	s.mutex.Lock()
	s.state = StateRcptTo
	s.mailFrom = "sender@example.com"
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("BDAT 5 LAST")
	}()

	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("Hello"))

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "451") {
		t.Errorf("expected 451 for BDAT delivery error, got: %q", resp)
	}
	<-done
}

func TestCoverBDAT_MultiChunk(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	var deliveredData string
	s.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = string(data)
		return nil
	}
	s.mutex.Lock()
	s.state = StateRcptTo
	s.mailFrom = "sender@example.com"
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	// First chunk (non-last)
	done1 := make(chan error, 1)
	go func() {
		done1 <- s.HandleCommand("BDAT 5")
	}()
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("Hello"))
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("expected 250 for non-last BDAT, got: %q", resp)
	}
	<-done1

	// Second chunk (last)
	done2 := make(chan error, 1)
	go func() {
		done2 <- s.HandleCommand("BDAT 5 LAST")
	}()
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("World"))
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "250") {
		t.Errorf("expected 250 for BDAT LAST, got: %q", resp2)
	}
	<-done2

	if deliveredData != "HelloWorld" {
		t.Errorf("expected delivered data 'HelloWorld', got %q", deliveredData)
	}
}

func TestCoverBDAT_ChunkTooLarge(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.MaxMessageSize = 5
	s.mutex.Lock()
	s.state = StateRcptTo
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("BDAT 100")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "552") {
		t.Errorf("expected 552 for chunk too large, got: %q", resp)
	}
}

func TestCoverBDAT_BadSize(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.mutex.Lock()
	s.state = StateRcptTo
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("BDAT abc")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("expected 501 for bad BDAT size, got: %q", resp)
	}
}

func TestCoverBDAT_MissingSize(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.mutex.Lock()
	s.state = StateRcptTo
	s.rcptTo = []string{"rcpt@example.com"}
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("BDAT")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("expected 501 for missing BDAT params, got: %q", resp)
	}
}

func TestCoverAuthLOGIN_UsernameReadError(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onAuth = func(username, password string) (bool, error) {
		return true, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Read the username prompt, then close
	time.Sleep(50 * time.Millisecond)
	clientConn.Close()
	<-done
}

func TestCoverAuthLOGIN_InvalidBase64Username(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("expected 334 for username prompt, got: %q", resp)
	}
	// Send invalid base64
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("!!!invalid!!!\r\n"))

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "501") {
		t.Errorf("expected 501 for invalid base64 username, got: %q", resp2)
	}
	<-done
}

func TestCoverAuthLOGIN_InvalidBase64Password(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Read username prompt and send valid username
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = reader.ReadString('\n')
	username := base64.StdEncoding.EncodeToString([]byte("testuser"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(username + "\r\n"))

	// Read password prompt and send invalid base64
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	pwdPrompt, _ := reader.ReadString('\n')
	if !strings.HasPrefix(pwdPrompt, "334") {
		t.Fatalf("expected 334 for password prompt, got: %q", pwdPrompt)
	}
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("!!!bad!!!\r\n"))

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "501") {
		t.Errorf("expected 501 for invalid base64 password, got: %q", resp3)
	}
	<-done
}

func TestCoverAuthLOGIN_AuthReject(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onAuth = func(username, password string) (bool, error) {
		return false, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Username
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = reader.ReadString('\n')
	username := base64.StdEncoding.EncodeToString([]byte("testuser"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(username + "\r\n"))

	// Password
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = reader.ReadString('\n')
	password := base64.StdEncoding.EncodeToString([]byte("testpass"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(password + "\r\n"))

	// Should get 535
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "535") {
		t.Errorf("expected 535 for auth reject, got: %q", resp3)
	}
	<-done
}

func TestCoverAuthLOGIN_Success(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onAuth = func(username, password string) (bool, error) {
		if username == "testuser" && password == "testpass" {
			return true, nil
		}
		return false, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Username
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = reader.ReadString('\n')
	username := base64.StdEncoding.EncodeToString([]byte("testuser"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(username + "\r\n"))

	// Password
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = reader.ReadString('\n')
	password := base64.StdEncoding.EncodeToString([]byte("testpass"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(password + "\r\n"))

	// Should get 235 success
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "235") {
		t.Errorf("expected 235 for auth success, got: %q", resp3)
	}
	<-done

	if !s.IsAuthenticated() {
		t.Error("expected session to be authenticated")
	}
	if s.Username() != "testuser" {
		t.Errorf("expected username 'testuser', got %q", s.Username())
	}
}

func TestCoverAuthPLAIN_InlineCredentials(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onAuth = func(username, password string) (bool, error) {
		if username == "testuser" && password == "testpass" {
			return true, nil
		}
		return false, nil
	}

	// PLAIN format: \0username\0password
	creds := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))
	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN " + creds)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "235") {
		t.Errorf("expected 235 for PLAIN auth success, got: %q", resp)
	}
	<-done
}

func TestCoverAuthPLAIN_InvalidBase64(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN !!!invalid!!!")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("expected 501 for invalid PLAIN base64, got: %q", resp)
	}
	<-done
}

func TestCoverAuthPLAIN_BadFormat(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	// Missing null separators
	creds := base64.StdEncoding.EncodeToString([]byte("just-a-string"))
	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN " + creds)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("expected 501 for bad PLAIN format, got: %q", resp)
	}
	<-done
}

func TestCoverAuthPLAIN_AuthReject(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()
	s.server.onAuth = func(username, password string) (bool, error) {
		return false, fmt.Errorf("rejected")
	}

	creds := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))
	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN " + creds)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "535") {
		t.Errorf("expected 535 for PLAIN auth reject, got: %q", resp)
	}
	<-done
}

func TestCoverAuthPLAIN_ConnectionClosed(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
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
	<-done
}

func TestCoverAUTH_AlreadyAuthenticated(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isAuth = true
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("AUTH PLAIN test")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "503") {
		t.Errorf("expected 503 already authenticated, got: %q", resp)
	}
}

func TestCoverAUTH_RequiresTLS(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = false
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("AUTH PLAIN test")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "538") {
		t.Errorf("expected 538 encryption required, got: %q", resp)
	}
}

func TestCoverAUTH_UnrecognizedMechanism(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("AUTH OAUTHBEARER")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "504") {
		t.Errorf("expected 504 unrecognized auth type, got: %q", resp)
	}
}

func TestCoverAUTH_BeforeEHLO(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()
	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateNew
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("AUTH PLAIN test")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "503") {
		t.Errorf("expected 503 bad sequence, got: %q", resp)
	}
}
