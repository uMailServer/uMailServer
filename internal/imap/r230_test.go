package imap

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type errListener230 struct{ n atomic.Int64 }

func (l *errListener230) Accept() (net.Conn, error) { l.n.Add(1); return nil, errors.New("persistent") }
func (l *errListener230) Close() error              { return nil }
func (l *errListener230) Addr() net.Addr            { return &net.TCPAddr{} }

// F7074: persistent Accept errors must back off, not spin.
func TestF7074_IMAPAcceptErrorBackoff(t *testing.T) {
	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), shutdown: make(chan struct{})}
	s.running.Store(true)
	l := &errListener230{}
	done := make(chan struct{})
	go func() { s.acceptLoop(l); close(done) }()
	time.Sleep(200 * time.Millisecond)
	close(s.shutdown)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acceptLoop did not exit on shutdown")
	}
	if n := l.n.Load(); n > 100 {
		t.Fatalf("accept loop spins: %d calls in 200ms", n)
	}
}
