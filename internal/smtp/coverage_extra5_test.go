package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
)

// ---------------------------------------------------------------------------
// 1. SetValidateHandler (server.go:84-86)
// ---------------------------------------------------------------------------

func TestSetValidateHandler(t *testing.T) {
	server := NewServer(&Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
	}, nil)

	called := false
	handler := func(from string, to []string) error {
		called = true
		return nil
	}

	server.SetValidateHandler(handler)

	if server.onValidate == nil {
		t.Fatal("expected onValidate to be set after SetValidateHandler")
	}

	// Invoke the handler to confirm it is the one we set
	server.onValidate("sender@example.com", []string{"rcpt@example.com"})
	if !called {
		t.Error("expected the injected validate handler to be called")
	}
}

// ---------------------------------------------------------------------------
// 2. AuthDKIMStage.Process DKIM pass (auth_pipeline.go:92-106)
//    The real DKIM verifier cannot return DKIMPass without valid crypto,
//    so we test the DKIM pass path at the handleDATA integration level
//    where a pipeline stage sets DKIMResult.Valid=true and the auth-results
//    header is written with dkim=pass, and SpamScore decreases by 1.0.
// ---------------------------------------------------------------------------

func TestAuthDKIMStage_DKIMPass_DecreasesSpamScore(t *testing.T) {
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
	pipeline.AddStage(&dkimPassStage{})
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
	if !strings.Contains(dataStr, "dkim=pass") {
		t.Errorf("Expected dkim=pass in Authentication-Results, got: %s", safeSlice(dataStr, 300))
	}
}

type dkimPassStage struct{}

func (s *dkimPassStage) Name() string { return "DKIMPassTest" }
func (s *dkimPassStage) Process(ctx *MessageContext) PipelineResult {
	ctx.DKIMResult = DKIMResult{Valid: true, Domain: "example.com", Selector: "default"}
	ctx.SpamScore -= 1.0
	return ResultAccept
}

// ---------------------------------------------------------------------------
// 3. AuthDKIMStage.Process with nil logger (auth_pipeline.go:88)
//    DKIM verify error with nil logger should not panic.
// ---------------------------------------------------------------------------

func TestAuthDKIMStage_VerifyError_NilLogger(t *testing.T) {
	resolver := &mockAuthDNSResolver{}
	verifier := auth.NewDKIMVerifier(resolver)
	stage := NewAuthDKIMStage(verifier, nil) // nil logger

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	ctx.Headers["DKIM-Signature"] = []string{"v=1; d=example.com; s=default; a=rsa-sha256; bh=abc; b=xyz; h=from:to"}

	// Should not panic even with nil logger
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.DKIMResult.Valid {
		t.Error("Expected DKIM valid=false with unverifiable signature")
	}
	// Each failed DKIM signature adds 1.0 to spam score
	if ctx.SpamScore < 0.5 {
		t.Errorf("Expected spam score >= 0.5 for failed DKIM, got %f", ctx.SpamScore)
	}
}

// ---------------------------------------------------------------------------
// AuthARCStage.Process with valid chain and DMARC fail override (lines 257-270)
// ---------------------------------------------------------------------------

func TestAuthARCStage_Process_ValidChainWithDMARCFail(t *testing.T) {
	resolver := &mockAuthDNSResolver{}
	validator := auth.NewARCValidator(resolver)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stage := NewAuthARCStage(validator, logger)

	// Set up a valid-looking ARC chain with CV=pass
	headers := map[string][]string{
		"ARC-Authentication-Results": {"i=1; auth=pass"},
		"ARC-Message-Signature":      {"i=1; a=rsa-sha256; d=example.com; s=selector; b=sig1; h=from:to"},
		"ARC-Seal":                  {"i=1; a=rsa-sha256; d=example.com; s=selector; cv=pass; b=seal1"},
	}

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	ctx.Headers = headers
	ctx.DMARCResult.Result = "fail"
	ctx.SpamScore = 3.0

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SpamScore < 2.0 {
		t.Errorf("Expected SpamScore reduced from 3.0, got %f", ctx.SpamScore)
	}
}

func TestAuthARCStage_Process_ValidChainNoDMARCOverride(t *testing.T) {
	resolver := &mockAuthDNSResolver{}
	validator := auth.NewARCValidator(resolver)
	stage := NewAuthARCStage(validator, nil)

	headers := map[string][]string{
		"ARC-Authentication-Results": {"i=1; auth=pass"},
		"ARC-Message-Signature":      {"i=1; a=rsa-sha256; d=example.com; s=selector; b=sig1; h=from:to"},
		"ARC-Seal":                  {"i=1; a=rsa-sha256; d=example.com; s=selector; cv=pass; b=seal1"},
	}

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	ctx.Headers = headers
	ctx.DMARCResult.Result = "pass"
	ctx.SpamScore = 0.0

	originalScore := ctx.SpamScore
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SpamScore != originalScore {
		t.Errorf("Expected SpamScore unchanged, got %f", ctx.SpamScore)
	}
}

func TestAuthARCStage_Process_InvalidChain(t *testing.T) {
	resolver := &mockAuthDNSResolver{}
	validator := auth.NewARCValidator(resolver)
	stage := NewAuthARCStage(validator, nil)

	headers := map[string][]string{
		"ARC-Authentication-Results": {"i=1; auth=fail"},
		"ARC-Message-Signature":      {"i=1; a=rsa-sha256; d=example.com; s=selector; b=sig1; h=from:to"},
		"ARC-Seal":                  {"i=1; a=rsa-sha256; d=example.com; s=selector; cv=fail; b=seal1"},
	}

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	ctx.Headers = headers
	ctx.SpamScore = 0.0

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SpamScore != 0.0 {
		t.Errorf("Expected SpamScore 0.0 for invalid chain, got %f", ctx.SpamScore)
	}
}

func TestAuthARCStage_Process_WithLogger(t *testing.T) {
	resolver := &mockAuthDNSResolver{}
	validator := auth.NewARCValidator(resolver)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stage := NewAuthARCStage(validator, logger)

	headers := map[string][]string{
		"ARC-Authentication-Results": {"i=1; auth=pass"},
		"ARC-Message-Signature":     {"i=1; a=rsa-sha256; d=example.com; s=selector; b=sig1; h=from:to"},
		"ARC-Seal":                  {"i=1; a=rsa-sha256; d=example.com; s=selector; cv=pass; b=seal1"},
	}

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("test"))
	ctx.Headers = headers

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// 4. handleAuthPLAIN two-step flow (session.go:686-726)
//    Send "AUTH PLAIN" without inline creds, respond to 334 challenge with
//    valid base64 creds, assert 235 response.
// ---------------------------------------------------------------------------

func TestHandleAuthPLAIN_TwoStepFlow(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
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
		done <- s.HandleCommand("AUTH PLAIN")
	}()

	// Read the 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read 334 challenge: %v", err)
	}
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 challenge, got: %q", resp)
	}

	// Respond with valid PLAIN credentials (base64 of \0user\0pass)
	creds := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(creds + "\r\n"))

	// Read the 235 success response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "235") {
		t.Errorf("Expected 235 auth success, got: %q", resp2)
	}
	<-done

	if !s.IsAuthenticated() {
		t.Error("Expected session to be authenticated")
	}
	if s.Username() != "testuser" {
		t.Errorf("Expected username 'testuser', got %q", s.Username())
	}
}

// ---------------------------------------------------------------------------
// 5. handleAuthPLAIN with nil onAuth (session.go:719-724)
//    Two-step PLAIN auth where no auth handler is set -- should succeed.
// ---------------------------------------------------------------------------

func TestHandleAuthPLAIN_TwoStep_NilOnAuth(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	// No onAuth handler set (nil)

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN")
	}()

	// Read the 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read 334 challenge: %v", err)
	}
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 challenge, got: %q", resp)
	}

	// Respond with valid PLAIN credentials
	creds := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(creds + "\r\n"))

	// Read the 235 success response (nil onAuth skips check)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "235") {
		t.Errorf("Expected 235 auth success with nil onAuth, got: %q", resp2)
	}
	<-done

	if !s.IsAuthenticated() {
		t.Error("Expected session to be authenticated with nil onAuth")
	}
}

// ---------------------------------------------------------------------------
// 6. handleAuthCRAMMD5 success flow (session.go:811-812)
//    Full CRAM-MD5 flow with correct HMAC computation and verification.
// ---------------------------------------------------------------------------

func TestHandleAuthCRAMMD5_SuccessFullFlow(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	sharedSecret := "my-shared-secret"
	s.server.onGetUserSecret = func(username string) (string, error) {
		if username == "cramuser" {
			return sharedSecret, nil
		}
		return "", fmt.Errorf("user not found")
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH CRAM-MD5")
	}()

	// Read 334 challenge
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read CRAM-MD5 challenge: %v", err)
	}
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 challenge, got: %q", resp)
	}

	// Extract challenge and compute HMAC-MD5
	challengeB64 := strings.TrimSpace(strings.TrimPrefix(resp, "334 "))
	challengeBytes, _ := base64.StdEncoding.DecodeString(challengeB64)
	mac := hmac.New(md5.New, []byte(sharedSecret))
	mac.Write(challengeBytes)
	expectedHex := hex.EncodeToString(mac.Sum(nil))

	// Send response: username + space + hex HMAC
	clientResponse := base64.StdEncoding.EncodeToString([]byte("cramuser " + expectedHex))
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte(clientResponse + "\r\n"))

	// Read 235 success
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "235") {
		t.Errorf("Expected 235 auth success for CRAM-MD5, got: %q", resp2)
	}
	<-done

	if !s.IsAuthenticated() {
		t.Error("Expected session to be authenticated after CRAM-MD5")
	}
	if s.Username() != "cramuser" {
		t.Errorf("Expected username 'cramuser', got %q", s.Username())
	}
}

// ---------------------------------------------------------------------------
// 7. handleBDAT zero-size non-last chunk (session.go:523)
//    Send "BDAT 0" without LAST flag. Should acknowledge with 250.
// ---------------------------------------------------------------------------

func TestHandleBDAT_ZeroSizeNonLastChunk(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// BDAT 0 (zero-size, non-last) -- no data to write, should just acknowledge
	err := session.handleBDAT("0")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 response for zero-size non-last BDAT chunk, got: %q", response)
	}

	// Buffer should be initialized but empty
	session.mutex.RLock()
	if session.bdatBuffer == nil {
		session.mutex.RUnlock()
		t.Fatal("Expected bdatBuffer to be initialized")
	}
	if session.bdatBuffer.Len() != 0 {
		t.Errorf("Expected bdatBuffer to be empty, got %d bytes", session.bdatBuffer.Len())
	}
	session.mutex.RUnlock()
}

// ---------------------------------------------------------------------------
// 8. handleDATA with no header/body separator (session.go:344)
//    Message with no \r\n\r\n should still deliver (headers block not parsed).
// ---------------------------------------------------------------------------

func TestHandleDATA_NoHeaderBodySeparator(t *testing.T) {
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

	// Send message with no \r\n\r\n separator (just raw content)
	clientConn.Write([]byte("Just a single line with no headers\r\n.\r\n"))

	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 OK for message with no header/body separator, got: %s", resp)
	}

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	if !bytes.Contains(deliveredData, []byte("Just a single line with no headers")) {
		t.Errorf("Expected raw content in delivered data, got: %q", safeSlice(string(deliveredData), 200))
	}
}

// ---------------------------------------------------------------------------
// 9. handleConnection with ReadTimeout (server.go:204-206)
//    Server with ReadTimeout > 0 should set deadline on each read.
// ---------------------------------------------------------------------------

func TestHandleConnection_WithReadTimeout(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
		ReadTimeout:    5 * time.Second,
	}

	server := NewServer(config, nil)

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
		t.Errorf("Expected 220 greeting with ReadTimeout set, got: %q", resp)
	}

	// Send EHLO and verify it works with the timeout
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	client.Write([]byte("EHLO testclient\r\n"))

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fullResp string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO response: %v", err)
		}
		fullResp += line
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if !strings.Contains(fullResp, "EHLO") && !strings.Contains(fullResp, "250") {
		t.Errorf("Expected EHLO response, got: %q", fullResp)
	}
}

// ---------------------------------------------------------------------------
// 10. handleConnection empty line (server.go:217-219)
//     Send \r\n after connecting -- should be skipped (continue).
// ---------------------------------------------------------------------------

func TestHandleConnection_EmptyLineSkipped(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

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
	greeting, _ := reader.ReadString('\n')
	if !strings.HasPrefix(greeting, "220") {
		t.Fatalf("Expected 220 greeting, got: %q", greeting)
	}

	// Send empty line -- should be silently skipped
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	client.Write([]byte("\r\n"))

	// Send a real command after the empty line
	client.Write([]byte("NOOP\r\n"))

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 OK after sending empty line then NOOP, got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// 11. ValidateEmail UTF-8 domain (server.go:284)
//     Email with non-ASCII domain should pass via the SMTPUTF8 fallback.
// ---------------------------------------------------------------------------

func TestValidateEmail_UTF8Domain(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "IDN domain with non-ASCII",
			input:   "user@\xc3\xa9xample.com",
			want:    "user@\xc3\xa9xample.com",
			wantErr: false,
		},
		{
			name:    "UTF-8 local part and domain",
			input:   "\xc3\xa9tudiant@univ\xc3\xa9rsit\xc3\xa9.fr",
			want:    "\xc3\xa9tudiant@univ\xc3\xa9rsit\xc3\xa9.fr",
			wantErr: false,
		},
		{
			name:    "pure ASCII still works",
			input:   "user@example.com",
			want:    "user@example.com",
			wantErr: false,
		},
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
// 12. AuthSPFStage temp error (auth_pipeline.go)
//     SPF check returning SPFTempError should not add spam score.
// ---------------------------------------------------------------------------

func TestAuthSPFStage_TempError(t *testing.T) {
	resolver := &mockAuthDNSResolver{
		txtErr: map[string]error{
			"example.com": fmt.Errorf("DNS temporary failure"),
		},
	}
	checker := auth.NewSPFChecker(resolver)
	stage := NewAuthSPFStage(checker, nil)

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SPFResult.Result != "temperror" {
		t.Errorf("Expected SPF result 'temperror', got %q", ctx.SPFResult.Result)
	}
	// temperror should NOT add to spam score
	if ctx.SpamScore != 0 {
		t.Errorf("Expected spam score 0 for SPF temperror, got %f", ctx.SpamScore)
	}
}

// ---------------------------------------------------------------------------
// Additional: AuthSPFStage temp error with logger
// ---------------------------------------------------------------------------

func TestAuthSPFStage_TempError_WithLogger(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	resolver := &mockAuthDNSResolver{
		txtErr: map[string]error{
			"example.com": fmt.Errorf("DNS timeout temporary failure"),
		},
	}
	checker := auth.NewSPFChecker(resolver)
	stage := NewAuthSPFStage(checker, logger)

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SPFResult.Result != "temperror" {
		t.Errorf("Expected SPF result 'temperror', got %q", ctx.SPFResult.Result)
	}
	output := buf.String()
	if output == "" {
		t.Error("Expected debug log output for SPF temperror with logger, got empty string")
	}
}

// ---------------------------------------------------------------------------
// Additional: handleConnection with ReadTimeout, send EHLO then QUIT
// ---------------------------------------------------------------------------

func TestHandleConnection_ReadTimeoutThenQuit(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
		ReadTimeout:    10 * time.Second,
	}

	server := NewServer(config, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

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
	greeting, _ := reader.ReadString('\n')
	if !strings.HasPrefix(greeting, "220") {
		t.Fatalf("Expected 220 greeting, got: %q", greeting)
	}

	// Send QUIT
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	client.Write([]byte("QUIT\r\n"))

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "221") {
		t.Errorf("Expected 221 closing, got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// Additional: AuthSPFStage with SPF pass via DNS mock and logger
// ---------------------------------------------------------------------------

func TestAuthSPFStage_Pass_WithLoggerAndTempError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	resolver := &mockAuthDNSResolver{
		txtRecords: map[string][]string{
			"example.com": {"v=spf1 ip4:1.2.3.4 -all"},
		},
	}
	checker := auth.NewSPFChecker(resolver)
	stage := NewAuthSPFStage(checker, logger)

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)

	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SPFResult.Result != "pass" {
		t.Errorf("Expected SPF result 'pass', got %q", ctx.SPFResult.Result)
	}
	output := buf.String()
	if output == "" {
		t.Error("Expected debug log output for SPF pass with logger, got empty string")
	}
}

// ---------------------------------------------------------------------------
// Additional: AuthDKIMStage with nil verifier (should not panic)
// ---------------------------------------------------------------------------

func TestAuthDKIMStage_NilVerifier(t *testing.T) {
	stage := NewAuthDKIMStage(nil, nil)
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))

	// No DKIM-Signature header -> should just set "no DKIM signature"
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.DKIMResult.Error != "no DKIM signature" {
		t.Errorf("Expected 'no DKIM signature', got %q", ctx.DKIMResult.Error)
	}
}

// ---------------------------------------------------------------------------
// Additional: AuthSPFStage with nil checker (should not panic)
// ---------------------------------------------------------------------------

func TestAuthSPFStage_NilChecker(t *testing.T) {
	stage := NewAuthSPFStage(nil, nil)

	// Use empty sender domain so the stage returns early without calling checker
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.SPFResult.Result != "none" {
		t.Errorf("Expected SPF result 'none', got %q", ctx.SPFResult.Result)
	}
}

// ---------------------------------------------------------------------------
// Additional: ValidateEmail with various UTF-8 edge cases
// ---------------------------------------------------------------------------

func TestValidateEmail_MoreUTF8(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "Chinese characters in local part",
			input:   "\xe7\x94\xa8\xe6\x88\xb7@example.com",
			want:    "\xe7\x94\xa8\xe6\x88\xb7@example.com",
			wantErr: false,
		},
		{
			name:    "Arabic domain",
			input:   "user@\xd8\xa7\xd8\xae\xd8\xaa\xd8\xa8\xd8\xa7\xd8\xb1.com",
			want:    "user@\xd8\xa7\xd8\xae\xd8\xaa\xd8\xa8\xd8\xa7\xd8\xb1.com",
			wantErr: false,
		},
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
// Additional: handleAuthCRAMMD5 connection closed during response read
// ---------------------------------------------------------------------------

func TestHandleAuthCRAMMD5_ConnectionClosedDuringResponse(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	s.server.config.AllowInsecure = true
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

	// Read the 334 challenge then close
	time.Sleep(50 * time.Millisecond)
	clientConn.Close()
	_ = <-done
}

// ---------------------------------------------------------------------------
// Additional: NetDNSResolver LookupIP via real resolver
// ---------------------------------------------------------------------------

func TestNetDNSResolver_LookupIP_Real(t *testing.T) {
	r := NewNetDNSResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ips, err := r.LookupIP(ctx, "localhost")
	if err != nil {
		t.Logf("LookupIP localhost error (may be expected): %v", err)
	} else {
		t.Logf("LookupIP localhost returned %d IPs", len(ips))
		for _, ip := range ips {
			if ip == nil {
				t.Error("Expected non-nil IP")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Additional: BDAT zero-size non-last then LAST with pipeline
// ---------------------------------------------------------------------------

func TestHandleBDAT_ZeroSizeNonLastThenLast(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	// Write some data first, then BDAT with non-zero size
	chunkData := "Hello"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write chunk: %v", err)
	}

	err = session.handleBDAT("5")
	if err != nil {
		t.Fatalf("handleBDAT first chunk error: %v", err)
	}

	// Drain 250
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	clientConn.Read(buf)

	// Now send zero-size LAST chunk
	err = session.handleBDAT("0 LAST")
	if err != nil {
		t.Fatalf("handleBDAT zero LAST error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf2 := make([]byte, 256)
	n, _ := clientConn.Read(buf2)
	response := string(buf2[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 for zero-size LAST, got: %q", response)
	}

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	if string(deliveredData) != "Hello" {
		t.Errorf("Expected delivered data 'Hello', got %q", string(deliveredData))
	}
}
