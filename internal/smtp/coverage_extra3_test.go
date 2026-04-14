package smtp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
)

func TestAuthDKIMStage_Process_MultipleFailures(t *testing.T) {
	resolver := &mockAuthDNSResolver{}
	verifier := auth.NewDKIMVerifier(resolver)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stage := NewAuthDKIMStage(verifier, logger)

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	ctx.Headers["DKIM-Signature"] = []string{
		"v=1; d=example.com; s=sel1; a=rsa-sha256; bh=abc; b=xyz; h=from",
		"v=1; d=example.com; s=sel2; a=rsa-sha256; bh=def; b=uvw; h=from",
	}
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.DKIMResult.Valid {
		t.Error("Expected DKIM valid=false")
	}
	if ctx.SpamScore < 2.0 {
		t.Errorf("Expected spam score >= 2.0 (1.0 per failed sig), got %f", ctx.SpamScore)
	}
}

func TestAuthDMARCStage_Process_EvalError(t *testing.T) {
	resolver := &mockAuthDNSResolver{
		txtErr: map[string]error{
			"_dmarc.example.com": fmt.Errorf("DNS timeout"),
		},
	}
	evaluator := auth.NewDMARCEvaluator(resolver)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stage := NewAuthDMARCStage(evaluator, logger)

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	ctx.SPFResult = SPFResult{Result: "pass", Domain: "example.com"}
	ctx.DKIMResult = DKIMResult{Valid: true, Domain: "example.com"}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept on DMARC temperror, got %v", result)
	}
	if ctx.DMARCResult.Result != "temperror" {
		t.Errorf("Expected DMARC result 'temperror', got %q", ctx.DMARCResult.Result)
	}
}

func TestAuthDMARCStage_Process_NilEvaluator(t *testing.T) {
	stage := NewAuthDMARCStage(nil, nil)
	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
	if ctx.DMARCResult.Result != "none" {
		t.Errorf("Expected DMARC result 'none', got %q", ctx.DMARCResult.Result)
	}
}

func TestAuthSPFStage_Process_WithLogger(t *testing.T) {
	resolver := &mockAuthDNSResolver{
		txtRecords: map[string][]string{
			"example.com": {"v=spf1 ip4:1.2.3.4 -all"},
		},
	}
	checker := auth.NewSPFChecker(resolver)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stage := NewAuthSPFStage(checker, logger)

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

func TestDefaultLoggerViaPipeline(t *testing.T) {
	pipeline := NewPipeline(nil)
	pipeline.AddStage(&testStage{name: "Test"})

	ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "s@example.com", []string{"r@example.com"}, []byte("data"))
	result, err := pipeline.Process(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

func TestNetDNSResolver_LookupTXT(t *testing.T) {
	r := NewNetDNSResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	records, err := r.LookupTXT(ctx, "example.com")
	if err != nil {
		t.Logf("LookupTXT error (may be expected in restricted env): %v", err)
	} else {
		t.Logf("LookupTXT returned %d records", len(records))
	}
}

func TestNetDNSResolver_LookupIP(t *testing.T) {
	r := NewNetDNSResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ips, err := r.LookupIP(ctx, "example.com")
	if err != nil {
		t.Logf("LookupIP error (may be expected in restricted env): %v", err)
	} else {
		t.Logf("LookupIP returned %d IPs", len(ips))
		for _, ip := range ips {
			if ip == nil {
				t.Error("Expected non-nil IP in result")
			}
		}
	}
}

func TestNetDNSResolver_LookupMX(t *testing.T) {
	r := NewNetDNSResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mxs, err := r.LookupMX(ctx, "example.com")
	if err != nil {
		t.Logf("LookupMX error (may be expected in restricted env): %v", err)
	} else {
		t.Logf("LookupMX returned %d MX records", len(mxs))
	}
}

func TestValidateEmail_InternationalAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "UTF-8 local part",
			input:   "user\xc3\xa9@example.com",
			want:    "user\xc3\xa9@example.com",
			wantErr: false,
		},
		{
			name:    "valid international",
			input:   "m\xc3\xbcller@example.com",
			want:    "m\xc3\xbcller@example.com",
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

func TestHandleCommand_STARTTLSDispatch(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("STARTTLS")
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "502") && !strings.HasPrefix(resp, "503") && !strings.HasPrefix(resp, "220") {
		t.Errorf("Expected 502/503/220 for STARTTLS, got: %q", resp)
	}
}

func TestHandleAuthLOGIN_AuthHandlerError(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	s.server.onAuth = func(username, password string) (bool, error) {
		return false, fmt.Errorf("auth service unavailable")
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 prompt, got: %q", resp)
	}

	clientConn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("testuser")) + "\r\n"))

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "334") {
		t.Fatalf("Expected 334 password prompt, got: %q", resp2)
	}

	clientConn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("testpass")) + "\r\n"))

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "535") {
		t.Errorf("Expected 535 auth failure when handler returns error, got: %q", resp3)
	}
	<-done
}

func TestHandleAuthPLAIN_AuthHandlerReturnsError(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	s.server.onAuth = func(username, password string) (bool, error) {
		return false, fmt.Errorf("database error")
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN " + encoded)
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "535") {
		t.Errorf("Expected 535 for auth handler error, got: %q", resp)
	}
	<-done
}

func TestHandleAuthLOGIN_NilAuthHandler(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.server.config.IsSubmission = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	// No onAuth handler set (nil) -- auth should succeed

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 prompt, got: %q", resp)
	}

	clientConn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("testuser")) + "\r\n"))

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "334") {
		t.Fatalf("Expected 334 password prompt, got: %q", resp2)
	}

	clientConn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("testpass")) + "\r\n"))

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "235") {
		t.Errorf("Expected 235 auth success with nil handler, got: %q", resp3)
	}
	<-done
}

func TestReadData_DotStuffingVarious(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

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

	clientConn.Write([]byte("Line 1\r\n..Line with dot stuffing\r\n.Line without stuffing (also dot-stuffed)\r\n.\r\n"))

	result := <-done
	if result.err != nil {
		t.Fatalf("readData error: %v", result.err)
	}
	data := string(result.data)
	if !strings.Contains(data, ".Line with dot stuffing") {
		t.Errorf("Expected dot-unstuffed '..' -> '.', got: %q", data)
	}
	if !strings.Contains(data, "Line without stuffing (also dot-stuffed)") {
		t.Errorf("Expected dot-unstuffed '.' -> '', got: %q", data)
	}
}

func TestHandleDATA_DotAtStartOfLine(t *testing.T) {
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

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354

	clientConn.Write([]byte("Subject: Test\r\n\r\n..hidden content\r\n.\r\n"))

	<-done

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 OK, got: %s", resp)
	}

	if !bytes.Contains(deliveredData, []byte(".hidden content")) {
		t.Errorf("Expected dot-unstuffed content '.hidden content', got: %q", string(deliveredData))
	}
}

func TestHandleBDAT_LastWithPipelineHeaders(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&testStage{name: "Test"})
	session.server.pipeline = pipeline

	msgData := "Subject: Test BDAT\r\nFrom: sender@example.com\r\n\r\nBody content\r\n"
	_, err := clientConn.Write([]byte(msgData))
	if err != nil {
		t.Fatalf("Failed to write chunk data: %v", err)
	}

	err = session.handleBDAT(fmt.Sprintf("%d LAST", len(msgData)))
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 response for BDAT with headers, got: %q", response)
	}

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
}

func TestHandleBDAT_MultipleChunksThenLastWithPipeline(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&testStage{name: "Test"})
	session.server.pipeline = pipeline

	// First chunk
	chunk1 := "Subject: Test\r\n"
	_, err := clientConn.Write([]byte(chunk1))
	if err != nil {
		t.Fatalf("Failed to write chunk1: %v", err)
	}
	err = session.handleBDAT(fmt.Sprintf("%d", len(chunk1)))
	if err != nil {
		t.Fatalf("handleBDAT chunk1 error: %v", err)
	}

	// Drain 250 response
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	clientConn.Read(buf)

	// Second (last) chunk
	chunk2 := "\r\nBody content\r\n"
	_, err = clientConn.Write([]byte(chunk2))
	if err != nil {
		t.Fatalf("Failed to write chunk2: %v", err)
	}
	err = session.handleBDAT(fmt.Sprintf("%d LAST", len(chunk2)))
	if err != nil {
		t.Fatalf("handleBDAT chunk2 error: %v", err)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf2 := make([]byte, 512)
	n, _ := clientConn.Read(buf2)
	response := string(buf2[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 for multi-chunk BDAT, got: %q", response)
	}

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	if !strings.HasPrefix(string(deliveredData), "Subject: Test\r\n\r\nBody content") {
		t.Errorf("Expected combined message data, got: %q", string(deliveredData))
	}
}
