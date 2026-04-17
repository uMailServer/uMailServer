package pop3

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestSetLoginResultHandler_StoresCallback(t *testing.T) {
	srv := NewServer("127.0.0.1:0", newMockMailstore(), nil)
	called := false
	srv.SetLoginResultHandler(func(string, bool, string, string) { called = true })
	if srv.onLoginResult == nil {
		t.Fatal("handler not stored")
	}
	srv.onLoginResult("u", true, "1.1.1.1", "")
	if !called {
		t.Error("handler not invoked")
	}
}

type loginResultRecord struct {
	user, ip, reason string
	success          bool
}

func TestPOP3_LoginResultCallback_FiresOnSuccessAndFailure(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return user == "good" && pass == "pw", nil
	})
	defer srv.Stop()

	var records []loginResultRecord
	calls := atomic.Int32{}
	srv.SetLoginResultHandler(func(user string, ok bool, ip, reason string) {
		records = append(records, loginResultRecord{user, ip, reason, ok})
		calls.Add(1)
	})

	// Failure
	conn, reader := dialAndRead(t, addr)
	sendCmd(t, conn, reader, "USER good")
	if resp := sendCmd(t, conn, reader, "PASS wrong"); resp[:4] != "-ERR" {
		t.Fatalf("expected -ERR, got %s", resp)
	}
	conn.Close()

	// Success
	conn, reader = dialAndRead(t, addr)
	sendCmd(t, conn, reader, "USER good")
	if resp := sendCmd(t, conn, reader, "PASS pw"); resp[:3] != "+OK" {
		t.Fatalf("expected +OK, got %s", resp)
	}
	conn.Close()

	// Allow callback to fire (it runs in the session goroutine).
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if calls.Load() != 2 {
		t.Fatalf("expected 2 callback calls, got %d", calls.Load())
	}

	failure, success := records[0], records[1]
	if failure.success || failure.reason != "invalid_credentials" {
		t.Errorf("failure record: %+v", failure)
	}
	if !success.success || success.reason != "" {
		t.Errorf("success record: %+v", success)
	}
}

func TestPOP3_LoginResultCallback_LockoutReason(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return false, nil // always fail
	})
	defer srv.Stop()

	srv.SetAuthLimits(1, time.Hour)

	// First failure consumes the lockout slot.
	conn, reader := dialAndRead(t, addr)
	sendCmd(t, conn, reader, "USER u")
	sendCmd(t, conn, reader, "PASS p")
	conn.Close()

	// Now register the callback so we capture only the lockout-triggered event.
	var gotReason string
	calls := atomic.Int32{}
	srv.SetLoginResultHandler(func(_ string, _ bool, _, reason string) {
		gotReason = reason
		calls.Add(1)
	})

	conn, reader = dialAndRead(t, addr)
	sendCmd(t, conn, reader, "USER u")
	resp := sendCmd(t, conn, reader, "PASS p")
	if resp[:4] != "-ERR" {
		t.Fatalf("expected -ERR, got %s", resp)
	}
	conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if calls.Load() == 0 {
		t.Fatal("expected lockout to fire callback")
	}
	if gotReason != "lockout" {
		t.Errorf("reason = %q, want lockout", gotReason)
	}
}
