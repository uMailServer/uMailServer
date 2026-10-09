package imap

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSessionClose_SynchronizedWithState guards the write in Session.Close().
//
// Close() sets s.state = StateLoggedOut. The RLock-protected readers
// (State(), Handle()) only serialize with a writer that holds the same
// stateMu, so an unlocked write in Close() is a genuine data race even though
// the reader is locked. Close() previously wrote s.state with no lock, and
// Server.Stop() -- its only production caller -- holds the server's sessionsMu,
// not the session's stateMu, so the old "caller must hold stateMu" contract was
// violated at every real call site. -race flagged this in TestFullMailFlow.
//
// Run under -race (CI does); it is a no-op assertion otherwise.
func TestSessionClose_SynchronizedWithState(t *testing.T) {
	client, srv := net.Pipe()
	defer client.Close()
	s := NewSession(srv, NewServer(&Config{Addr: ":0"}, &mockMailstore{}))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // reader: State() holds stateMu.RLock
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			_ = s.State()
		}
	}()
	go func() { // writer: Close() must hold stateMu.Lock
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			s.Close()
		}
	}()
	wg.Wait()

	if got := s.State(); got != StateLoggedOut {
		t.Fatalf("State() = %v, want StateLoggedOut after Close()", got)
	}
}

// TestSessionClose_SynchronizedWithHandle covers the production path that CI
// actually failed: a session goroutine running Handle() (which re-reads s.state
// on each loop iteration) while Close() is called from Server.Stop().
func TestSessionClose_SynchronizedWithHandle(t *testing.T) {
	client, srv := net.Pipe()
	defer client.Close()
	s := NewSession(srv, NewServer(&Config{Addr: ":0", ReadTimeout: 0}, &mockMailstore{}))

	go func() {
		// Feed a few commands so Handle() loops back and re-reads s.state.
		for i := 0; i < 50; i++ {
			if _, err := client.Write([]byte("a1 CAPABILITY\r\n")); err != nil {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
		_ = client.Close()
	}()

	done := make(chan struct{})
	go func() { defer close(done); s.Handle() }()

	// Close concurrently with the running Handle loop.
	for i := 0; i < 200; i++ {
		s.Close()
	}
	<-done
}

// discardConn is a net.Conn whose Write never blocks, so handleCommand can be
// driven in a tight loop without a reader draining the pipe.
type discardConn struct{ closed atomic.Bool }

func (c *discardConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *discardConn) Write(p []byte) (int, error) {
	return len(p), nil
}
func (c *discardConn) Close() error                     { c.closed.Store(true); return nil }
func (c *discardConn) LocalAddr() net.Addr              { return dummyAddr{} }
func (c *discardConn) RemoteAddr() net.Addr             { return dummyAddr{} }
func (c *discardConn) SetDeadline(time.Time) error      { return nil }
func (c *discardConn) SetReadDeadline(time.Time) error  { return nil }
func (c *discardConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "test" }
func (dummyAddr) String() string  { return "test" }

// TestSessionClose_SynchronizedWithHandleCommand is the deterministic guard
// for the other half of the original race: handleCommand() read s.state with a
// bare `switch s.state` while Close() wrote it from Server.Stop's goroutine.
// It now reads through State() (RLock). Reverting that to a bare read makes
// this loop trip -race deterministically.
func TestSessionClose_SynchronizedWithHandleCommand(t *testing.T) {
	conn := &discardConn{}
	s := NewSession(conn, NewServer(&Config{Addr: ":0"}, &mockMailstore{}))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // reader: handleCommand dispatches on s.State()
		defer wg.Done()
		for i := 0; i < 3000; i++ {
			_ = s.handleCommand("a1 CAPABILITY")
		}
	}()
	go func() { // writer: Close()
		defer wg.Done()
		for i := 0; i < 3000; i++ {
			s.Close()
		}
	}()
	wg.Wait()
}

// TestHandleClose_SynchronizedWithClose guards the CLOSE command's state
// transition: handleClose wrote s.state without stateMu while Server.Stop ->
// Session.Close() writes it under stateMu from another goroutine. Fails under
// -race if the lock is removed.
func TestHandleClose_SynchronizedWithClose(t *testing.T) {
	client, srv := net.Pipe()
	defer client.Close()
	go func() { _, _ = io.Copy(io.Discard, client) }()
	s := NewSession(srv, NewServer(&Config{Addr: ":0"}, &mockMailstore{}))
	s.state = StateSelected
	s.selected = &Mailbox{Name: "INBOX"}
	s.tag = "c1"

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _ = s.handleClose() }()
	go func() { defer wg.Done(); <-start; s.Close() }()
	close(start)
	wg.Wait()
}
