package imap

import (
	"strings"
	"testing"
	"time"
)

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

// --- handleAuthPlain coverage ---

func TestHandleAuthPlain_InvalidBase64(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE PLAIN not-valid-base64!!!")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for invalid base64, got: %s", resp)
	}
	<-done
}

func TestHandleAuthPlain_Cancelled(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE PLAIN")
	}()

	// Read the continuation request
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation request, got: %s", resp1)
	}

	// Send cancellation
	client.Write([]byte("*\r\n"))

	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "NO") || !strings.Contains(resp2, "cancelled") {
		t.Errorf("Expected cancellation response, got: %s", resp2)
	}
	<-done
}

func TestHandleAuthPlain_InvalidCredentials(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE PLAIN")
	}()

	// Read the continuation request
	resp1 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp1, "+") {
		t.Errorf("Expected continuation request, got: %s", resp1)
	}

	// Send invalid base64
	client.Write([]byte("invalid\r\n"))

	resp2 := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp2, "NO") {
		t.Errorf("Expected NO for invalid base64, got: %s", resp2)
	}
	<-done
}

func TestHandleAuthPlain_MissingParts(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "test", nil)
	defer client.Close()

	session.tag = "A001"

	// Send credentials with only 2 parts (missing password)
	credsB64 := "dXNlcgBwYXNz" // base64 of "user\x00pass"

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 AUTHENTICATE PLAIN " + credsB64)
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for invalid credentials, got: %s", resp)
	}
	<-done
}
