package imap

import (
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
// because it blocks waiting for gzip headers from the pipe
