package sieve

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type r231Listener struct{ n atomic.Int64 }

func (l *r231Listener) Accept() (net.Conn, error) { l.n.Add(1); return nil, errors.New("persistent") }
func (l *r231Listener) Close() error              { return nil }
func (l *r231Listener) Addr() net.Addr            { return &net.TCPAddr{} }

// F7080: persistent Accept errors must back off, not spin.
func TestF7080_AcceptErrorSpin(t *testing.T) {
	for name, fn := range map[string]func(*ManageSieveServer, net.Listener){
		"plain": (*ManageSieveServer).serve, "tls": (*ManageSieveServer).serveTLS} {
		s := &ManageSieveServer{done: make(chan struct{})}
		l := &r231Listener{}
		s.wg.Add(1)
		go fn(s, l)
		time.Sleep(200 * time.Millisecond)
		close(s.done)
		s.wg.Wait()
		if l.n.Load() > 100 {
			t.Fatalf("%s: accept loop spins: %d calls in 200ms", name, l.n.Load())
		}
	}
}
