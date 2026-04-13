package pop3

import (
	"strings"
	"testing"
)

// --- STLS Test ---

func TestSTLSCommand(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	// Note: can't easily test full STLS without TLS certs
	// Test that STLS is rejected when no TLS config

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	resp := sendCmd(t, conn, reader, "STLS")
	if !strings.HasPrefix(resp, "-ERR Command not available") {
		t.Errorf("STLS no config: expected -ERR Command not available, got %s", resp)
	}
}

// --- Authorization State Error Paths ---

func TestUSERCommand_NoArgs(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	resp := sendCmd(t, conn, reader, "USER")
	if !strings.HasPrefix(resp, "-ERR Usage: USER") {
		t.Errorf("USER no args: expected -ERR Usage, got %s", resp)
	}
}

func TestPASSCommand_NoUSER(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	resp := sendCmd(t, conn, reader, "PASS somepass")
	if !strings.HasPrefix(resp, "-ERR USER required first") {
		t.Errorf("PASS no USER: expected -ERR USER required first, got %s", resp)
	}
}

func TestPASSCommand_NoArgs(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER testuser")
	resp := sendCmd(t, conn, reader, "PASS")
	if !strings.HasPrefix(resp, "-ERR Usage: PASS") {
		t.Errorf("PASS no args: expected -ERR Usage, got %s", resp)
	}
}

// --- Transaction State Error Paths ---

func TestRETRCommand_NoArgs(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	resp := sendCmd(t, conn, reader, "RETR")
	if !strings.HasPrefix(resp, "-ERR Usage: RETR") {
		t.Errorf("RETR no args: expected -ERR Usage, got %s", resp)
	}
}

func TestRETRCommand_InvalidIndex(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	resp := sendCmd(t, conn, reader, "RETR 999")
	if !strings.HasPrefix(resp, "-ERR No such message") {
		t.Errorf("RETR 999: expected -ERR No such message, got %s", resp)
	}
}

func TestDELECommand_InvalidIndex(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	resp := sendCmd(t, conn, reader, "DELE 999")
	if !strings.HasPrefix(resp, "-ERR No such message") {
		t.Errorf("DELE 999: expected -ERR No such message, got %s", resp)
	}
}

func TestTOPCommand_NoArgs(t *testing.T) {
	store := newMockMailstore()
	store.dataMap[0] = []byte("From: test\r\nSubject: Test\r\n\r\nBody\r\n")
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	resp := sendCmd(t, conn, reader, "TOP")
	if !strings.HasPrefix(resp, "-ERR Usage: TOP") {
		t.Errorf("TOP no args: expected -ERR Usage, got %s", resp)
	}
}

func TestTOPCommand_InvalidIndex(t *testing.T) {
	store := newMockMailstore()
	store.dataMap[0] = []byte("From: test\r\nSubject: Test\r\n\r\nBody\r\n")
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// TOP requires 2 args: index and lines
	resp := sendCmd(t, conn, reader, "TOP 999 10")
	if !strings.HasPrefix(resp, "-ERR No such message") {
		t.Errorf("TOP 999 10: expected -ERR No such message, got %s", resp)
	}
}

func TestLISTCommand_AfterDelete(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// Delete message 1
	sendCmd(t, conn, reader, "DELE 1")

	// LIST 1 should fail
	resp := sendCmd(t, conn, reader, "LIST 1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("LIST 1 after DELE: expected -ERR, got %s", resp)
	}
}

func TestUnknownCommand(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	resp := sendCmd(t, conn, reader, "INVALIDCMD")
	if !strings.HasPrefix(resp, "-ERR Unknown command") {
		t.Errorf("INVALIDCMD: expected -ERR Unknown command, got %s", resp)
	}
}

func TestInvalidCommandInState(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	// In AUTHORIZATION state, TRANSACTION commands should be rejected
	resp := sendCmd(t, conn, reader, "STAT")
	if !strings.HasPrefix(resp, "-ERR Command not valid in this state") {
		t.Errorf("STAT in auth state: expected -ERR Command not valid in this state, got %s", resp)
	}
}

// --- Helper functions ---
