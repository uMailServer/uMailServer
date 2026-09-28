package imap

import (
	"net"
	"sync"
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
