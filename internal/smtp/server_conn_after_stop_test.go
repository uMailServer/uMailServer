package smtp

// F5057: a connection accepted just before Stop, whose handleConnection
// goroutine registers in s.connections only after Stop has iterated the map,
// is never closed by Stop: it gets a 220 greeting and a fully working SMTP
// session on a stopped server (goroutine + socket live until the client
// leaves or the read timeout fires).

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// a session registered before Stop is closed by Stop.
func TestF5057ControlRegisteredBeforeStop(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20}, nil)
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	_ = clientConn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(clientConn)
	if l, err := r.ReadString('\n'); err != nil || !strings.HasPrefix(l, "220") {
		t.Fatalf("control: greeting %q %v", l, err)
	}
	_ = srv.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("control: registered session survived Stop")
	}
}

func TestF5057ConnAfterStop(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20}, nil)
	_ = srv.Stop()
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan struct{})
	// The goroutine Serve spawned for a connection accepted before Stop
	// runs only now, after Stop finished.
	go func() { srv.handleConnection(serverConn); close(done) }()
	_ = clientConn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(clientConn)
	var got []string
	if l, err := r.ReadString('\n'); err == nil {
		got = append(got, strings.TrimSpace(l))
		if _, err := clientConn.Write([]byte("NOOP\r\n")); err == nil {
			if l, err := r.ReadString('\n'); err == nil {
				got = append(got, strings.TrimSpace(l))
			}
		}
	}
	served := false
	for _, l := range got {
		if strings.HasPrefix(l, "220") || strings.HasPrefix(l, "250") {
			served = true
		}
	}
	if served {
		t.Errorf("F5057: stopped server served a late connection: %q", got)
	}
	_ = clientConn.Close()
	<-done
}

// Edge cases.

func TestF5057EdgeNoLeakAndMapEmpty(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20}, nil)
	_ = srv.Stop()
	for i := 0; i < 3; i++ {
		serverConn, clientConn := net.Pipe()
		done := make(chan struct{})
		go func() { srv.handleConnection(serverConn); close(done) }()
		// handleConnection must return on its own (client never writes).
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("F5057 late handler did not return")
		}
		_ = clientConn.Close()
	}
	srv.connMu.RLock()
	n := len(srv.connections)
	srv.connMu.RUnlock()
	if n != 0 {
		t.Errorf("F5057 %d sessions registered on a stopped server", n)
	}
}
