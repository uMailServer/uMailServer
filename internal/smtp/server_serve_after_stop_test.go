package smtp

// F5056: Serve called after Stop (the caller's ListenAndServe goroutine
// losing the race against shutdown) returns nil immediately but never closes
// the listener it was handed: Stop already ran, so nobody owns it and the
// port stays bound for the life of the process.

import (
	"net"
	"sync"
	"testing"
	"time"
)

func regF5056Open(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

type regF5056GatedListener struct {
	net.Listener
	once      sync.Once
	accepting chan struct{}
}

func (g *regF5056GatedListener) Accept() (net.Conn, error) {
	g.once.Do(func() { close(g.accepting) })
	return g.Listener.Accept()
}

// Stop while Serve is running closes the listener.
func TestF5056ControlStopWhileServing(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20}, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ret := make(chan struct{})
	gl := &regF5056GatedListener{Listener: ln, accepting: make(chan struct{})}
	go func() { _ = srv.Serve(gl); close(ret) }()
	<-gl.accepting // Serve owns the listener and is in Accept
	_ = srv.Stop()
	<-ret
	if regF5056Open(ln.Addr().String()) {
		t.Fatalf("control: listener still open after Stop")
	}
}

func TestF5056ServeAfterStop(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20}, nil)
	_ = srv.Stop()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_ = srv.Serve(ln)
	open := regF5056Open(ln.Addr().String())
	if open {
		t.Errorf("F5056: Serve returned after Stop but left %s listening", ln.Addr())
	}
}

// Edge cases.

func TestF5056EdgeRepeatedStopAndSecondServe(t *testing.T) {
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20}, nil)
	_ = srv.Stop()
	_ = srv.Stop()
	for i := 0; i < 2; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		if err := srv.Serve(ln); err != nil {
			t.Errorf("F5056 Serve after Stop returned %v", err)
		}
		if regF5056Open(ln.Addr().String()) {
			t.Errorf("F5056 listener %d left open", i)
		}
	}
}
