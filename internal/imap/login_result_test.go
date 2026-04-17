package imap

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// authMailstoreOK is a stub that always authenticates.
type authMailstoreOK struct{ mockMailstore }

func (m *authMailstoreOK) Authenticate(_, _ string) (bool, error) { return true, nil }

// authMailstoreFail is a stub that always rejects.
type authMailstoreFail struct{ mockMailstore }

func (m *authMailstoreFail) Authenticate(_, _ string) (bool, error) { return false, nil }

func TestSetLoginResultHandler_StoresCallback(t *testing.T) {
	s := NewServer(&Config{Addr: ":0"}, &mockMailstore{})
	called := false
	s.SetLoginResultHandler(func(string, bool, string, string) { called = true })
	if s.onLoginResult == nil {
		t.Fatal("handler not stored")
	}
	s.onLoginResult("u", true, "1.1.1.1", "")
	if !called {
		t.Error("handler not invoked")
	}
}

// runAuthenticateUser drives a session through authenticateUser using a
// net.Pipe so the callback can observe the result without a real socket.
func runAuthenticateUser(t *testing.T, s *Server, user, pass string) {
	t.Helper()
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sess := NewSession(c1, s)
	sess.tag = "A1"
	sess.tlsActive = true // bypass TLS gate

	done := make(chan struct{})
	go func() {
		_ = sess.authenticateUser(user, pass, "ok", "fail")
		close(done)
	}()

	// Drain the response so writer doesn't block.
	go func() {
		buf := make([]byte, 256)
		c2.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _ = c2.Read(buf)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("authenticateUser hung")
	}
}

func TestAuthenticateUser_FiresCallbackOnSuccess(t *testing.T) {
	s := NewServer(&Config{Addr: ":0"}, &authMailstoreOK{})
	s.SetAllowPlainAuth(true)

	var gotUser, gotIP, gotReason string
	var gotSuccess atomic.Bool
	called := atomic.Int32{}
	s.SetLoginResultHandler(func(u string, ok bool, ip, reason string) {
		called.Add(1)
		gotUser, gotIP, gotReason = u, ip, reason
		gotSuccess.Store(ok)
	})

	runAuthenticateUser(t, s, "alice@example.com", "pw")
	if called.Load() != 1 {
		t.Fatalf("callback fires: got %d, want 1", called.Load())
	}
	if !gotSuccess.Load() {
		t.Error("expected success=true")
	}
	if gotUser != "alice@example.com" {
		t.Errorf("user = %q", gotUser)
	}
	if gotReason != "" {
		t.Errorf("reason = %q (want empty)", gotReason)
	}
	if gotIP == "" {
		t.Error("expected IP to be populated")
	}
}

func TestAuthenticateUser_FiresCallbackWithReasonOnFailure(t *testing.T) {
	s := NewServer(&Config{Addr: ":0"}, &authMailstoreFail{})
	s.SetAllowPlainAuth(true)

	var gotReason string
	var gotSuccess atomic.Bool
	called := atomic.Int32{}
	s.SetLoginResultHandler(func(_ string, ok bool, _, reason string) {
		called.Add(1)
		gotReason = reason
		gotSuccess.Store(ok)
	})

	runAuthenticateUser(t, s, "bob@example.com", "wrong")
	if called.Load() != 1 {
		t.Fatalf("callback fires: got %d, want 1", called.Load())
	}
	if gotSuccess.Load() {
		t.Error("expected success=false")
	}
	if gotReason != "invalid_credentials" {
		t.Errorf("reason = %q, want invalid_credentials", gotReason)
	}
}

func TestAuthenticateUser_FiresCallbackWithLockoutReason(t *testing.T) {
	s := NewServer(&Config{Addr: ":0"}, &authMailstoreFail{})
	s.SetAllowPlainAuth(true)
	s.SetAuthLimits(1, time.Hour)

	// Trigger one failure to put the IP into lockout state.
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	sess := NewSession(c1, s)
	sess.tag = "A1"
	sess.tlsActive = true
	go func() {
		buf := make([]byte, 256)
		c2.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _ = c2.Read(buf)
	}()
	_ = sess.authenticateUser("u", "p", "ok", "fail")

	// Now set the callback only for the second attempt and drive it again.
	var gotReason string
	called := atomic.Int32{}
	s.SetLoginResultHandler(func(_ string, _ bool, _, reason string) {
		called.Add(1)
		gotReason = reason
	})
	runAuthenticateUser(t, s, "u", "p")
	if called.Load() != 1 {
		t.Fatalf("expected lockout to fire callback once, got %d", called.Load())
	}
	if gotReason != "lockout" {
		t.Errorf("reason = %q, want lockout", gotReason)
	}
}
