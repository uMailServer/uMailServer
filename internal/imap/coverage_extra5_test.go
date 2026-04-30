package imap

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- handleAuthLogin coverage ---

func TestHandleAuthLogin_InvalidBase64Username(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE LOGIN")
	}()

	// Read the continuation request (username challenge)
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation request for username, got: %s", resp1)
	}

	// Send invalid base64 as username response
	client.Write([]byte("not-valid-base64!!!\r\n"))

	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "NO") || !strings.Contains(resp2, "Invalid base64") {
		t.Errorf("Expected NO with Invalid base64, got: %s", resp2)
	}
	<-done
}

func TestHandleAuthLogin_InvalidBase64Password(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE LOGIN")
	}()

	// Read the continuation request (username challenge)
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation request for username, got: %s", resp1)
	}

	// Send valid base64 for username "user"
	client.Write([]byte("dXNlcg==\r\n")) // base64 of "user"

	// Read the password challenge
	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "+") {
		t.Errorf("Expected continuation request for password, got: %s", resp2)
	}

	// Send invalid base64 as password response
	client.Write([]byte("not-valid-base64!!!\r\n"))

	resp3 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp3, "NO") || !strings.Contains(resp3, "Invalid base64") {
		t.Errorf("Expected NO with Invalid base64, got: %s", resp3)
	}
	<-done
}

func TestHandleAuthLogin_CancelledUsername(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE LOGIN")
	}()

	// Read the continuation request (username challenge)
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation request, got: %s", resp1)
	}

	// Send cancellation "*"
	client.Write([]byte("*\r\n"))

	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "NO") || !strings.Contains(resp2, "cancelled") {
		t.Errorf("Expected NO cancelled, got: %s", resp2)
	}
	<-done
}

func TestHandleAuthLogin_CancelledPassword(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE LOGIN")
	}()

	// Read the continuation request (username challenge)
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation request for username, got: %s", resp1)
	}

	// Send valid base64 for username "user"
	client.Write([]byte("dXNlcg==\r\n"))

	// Read the password challenge
	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "+") {
		t.Errorf("Expected continuation request for password, got: %s", resp2)
	}

	// Send cancellation "*"
	client.Write([]byte("*\r\n"))

	resp3 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp3, "NO") || !strings.Contains(resp3, "cancelled") {
		t.Errorf("Expected NO cancelled, got: %s", resp3)
	}
	<-done
}

// --- handleAuthenticate with LOGIN mechanism ---

func TestHandleAuthenticate_LOGIN(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE LOGIN")
	}()

	// Read username challenge
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation for username, got: %s", resp1)
	}

	// Send username
	client.Write([]byte("dXNlcg==\r\n"))

	// Read password challenge
	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "+") {
		t.Errorf("Expected continuation for password, got: %s", resp2)
	}

	// Send password (valid base64 for "password")
	client.Write([]byte("cGFzc3dvcmQ=\r\n"))

	resp3 := drainConn(client, 200*time.Millisecond)
	// Should get either OK (auth success) or NO (auth failure)
	if !strings.Contains(resp3, "OK") && !strings.Contains(resp3, "NO") {
		t.Errorf("Expected OK or NO response, got: %s", resp3)
	}
	<-done
}

// --- handleCompress coverage ---

func TestHandleCompress_AlreadyActive(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.compressActive = true
	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 COMPRESS DEFLATE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for already active compression, got: %s", resp)
	}
	<-done
}

func TestHandleCompress_InvalidArgument(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 COMPRESS GZIP")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for invalid argument, got: %s", resp)
	}
	<-done
}

// Note: gzip.NewReader failure (line 260-264) is not easily testable

// --- checkAndSendMDN coverage ---

func TestCheckAndSendMDN_NoHandler(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	ms.checkAndSendMDN("user", "msg1", "from@example.com", "to@example.com", []byte("test"))
}

func TestCheckAndSendMDN_AlreadySent(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	ms.SetMDNHandler(func(_, _, _, _ string, _ []byte) error { return nil })
	ms.mdnSent = map[string]bool{"msg1": true}

	ms.checkAndSendMDN("user", "msg1", "from@example.com", "to@example.com", []byte("test"))
}

func TestCheckAndSendMDN_NoDispositionHeader(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	ms.SetMDNHandler(func(_, _, _, _ string, _ []byte) error { return nil })

	msg := []byte("Subject: Test\r\n\r\nBody")
	ms.checkAndSendMDN("user", "msg2", "from@example.com", "to@example.com", msg)
}

func TestCheckAndSendMDN_InvalidAddress(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	ms.SetMDNHandler(func(_, _, _, _ string, _ []byte) error { return nil })

	msg := []byte("Disposition-Notification-To: <invalid\r\n\r\nBody")
	ms.checkAndSendMDN("user", "msg3", "from@example.com", "to@example.com", msg)
}

func TestCheckAndSendMDN_Success(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	done := make(chan struct{}, 1)
	ms.SetMDNHandler(func(_, _, _, _ string, _ []byte) error {
		done <- struct{}{}
		return nil
	})

	msg := []byte("Disposition-Notification-To: <sender@example.com>\r\n\r\nBody")
	ms.checkAndSendMDN("user", "msg4", "from@example.com", "to@example.com", msg)

	select {
	case <-done:
		// Handler was called
	case <-time.After(2 * time.Second):
		t.Error("expected MDN handler to be called")
	}
}

func TestCheckAndSendMDN_HandlerError(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	done := make(chan struct{}, 1)
	ms.SetMDNHandler(func(_, _, _, _ string, _ []byte) error {
		defer close(done)
		return fmt.Errorf("send failed")
	})

	msg := []byte("Disposition-Notification-To: <sender@example.com>\r\n\r\nBody")
	ms.checkAndSendMDN("user", "msg5", "from@example.com", "to@example.com", msg)

	select {
	case <-done:
		// Handler completed
	case <-time.After(2 * time.Second):
		t.Error("expected MDN handler to complete")
	}

	// After error, the message ID should be removed from mdnSent to allow retry
	ms.mdnSentMu.Lock()
	sent := ms.mdnSent["msg5"]
	ms.mdnSentMu.Unlock()
	if sent {
		t.Error("expected msg5 to be removed from mdnSent after handler error")
	}
}

func TestCheckAndSendMDN_SemaphoreFull(t *testing.T) {
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore failed: %v", err)
	}
	defer ms.Close()

	// Fill the semaphore
	for i := 0; i < cap(ms.mdnSem); i++ {
		ms.mdnSem <- struct{}{}
	}
	defer func() {
		// Drain semaphore
		for i := 0; i < cap(ms.mdnSem); i++ {
			select {
			case <-ms.mdnSem:
			default:
			}
		}
	}()

	ms.SetMDNHandler(func(_, _, _, _ string, _ []byte) error { return nil })

	msg := []byte("Disposition-Notification-To: <sender@example.com>\r\n\r\nBody")
	ms.checkAndSendMDN("user", "msg6", "from@example.com", "to@example.com", msg)
	// Semaphore full - should drop MDN without blocking
}
// because it blocks waiting for gzip headers from the pipe
