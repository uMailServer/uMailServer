package pop3

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func capaLines(t *testing.T, requireTLS bool) []string {
	t.Helper()
	srv := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), mailstore: &top4985Store{}, requireTLS: requireTLS}
	c1, c2 := net.Pipe()
	defer c2.Close()
	sess := NewSession(c1, srv)
	go func() { sess.Handle(); c1.Close() }()
	r := bufio.NewReader(c2)
	_ = c2.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c2.Write([]byte("CAPA\r\n")); err != nil {
		t.Fatal(err)
	}
	var out []string
	if _, err := r.ReadString('\n'); err != nil { // +OK line
		t.Fatal(err)
	}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "." {
			return out
		}
		out = append(out, l)
	}
}

// F7073: CAPA must not advertise USER when TLS is required but not active.
func TestF7073_CapaHidesUserWhenTLSRequired(t *testing.T) {
	has := func(ls []string, w string) bool {
		for _, l := range ls {
			if l == w {
				return true
			}
		}
		return false
	}
	if has(capaLines(t, true), "USER") {
		t.Fatal("USER advertised although TLS is required and inactive")
	}
	if !has(capaLines(t, false), "USER") {
		t.Fatal("USER should be advertised when TLS is not required")
	}
}

type errListener struct{ n atomic.Int64 }

func (l *errListener) Accept() (net.Conn, error) { l.n.Add(1); return nil, errors.New("persistent") }
func (l *errListener) Close() error              { return nil }
func (l *errListener) Addr() net.Addr            { return &net.TCPAddr{} }

// F7074 probe: persistent Accept error rate.
func TestF7074_AcceptErrorSpin(t *testing.T) {
	srv := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), shutdown: make(chan struct{})}
	srv.running.Store(true)
	l := &errListener{}
	srv.listener = l
	go srv.acceptLoop()
	time.Sleep(200 * time.Millisecond)
	close(srv.shutdown)
	t.Logf("accept calls in 200ms: %d", l.n.Load())
	if l.n.Load() > 100 {
		t.Fatalf("accept loop spins: %d calls in 200ms", l.n.Load())
	}
}
