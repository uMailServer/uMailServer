package smtp

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type r231Listener struct{ n atomic.Int64 }

func (l *r231Listener) Accept() (net.Conn, error) { l.n.Add(1); return nil, errors.New("persistent") }
func (l *r231Listener) Close() error              { return nil }
func (l *r231Listener) Addr() net.Addr            { return &net.TCPAddr{} }

// F7081: persistent Accept errors must back off, not spin.
func TestF7081_AcceptErrorSpin(t *testing.T) {
	srv := NewServer(&Config{Hostname: "h"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	l := &r231Listener{}
	done := make(chan struct{})
	go func() { _ = srv.Serve(l); close(done) }()
	time.Sleep(200 * time.Millisecond)
	close(srv.shutdown)
	<-done
	if l.n.Load() > 100 {
		t.Fatalf("accept loop spins: %d calls in 200ms", l.n.Load())
	}
}
