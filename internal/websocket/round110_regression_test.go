package websocket

import (
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A client that connects and never reads must not stall Broadcast forever
// (F5925): the write deadline makes the stuck write fail and the client is
// dropped.
func TestBroadcastDoesNotBlockOnStalledClient(t *testing.T) {
	old := sseWriteTimeout
	sseWriteTimeout = 300 * time.Millisecond
	defer func() { sseWriteTimeout = old }()

	srv := NewSSEServer(slog.Default())
	srv.SetAuthFunc(func(string) (string, bool, error) { return "stall", false, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(4096)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nX-Auth-Token: t\r\n\r\n"))
	deadline := time.Now().Add(3 * time.Second)
	for srv.GetConnectedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	big := strings.Repeat("x", 1<<20)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			srv.Broadcast("big", big)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Broadcast blocked on a client that is not reading")
	}
	deadline = time.Now().Add(3 * time.Second)
	for srv.GetConnectedCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.GetConnectedCount(); n != 0 {
		t.Fatalf("stalled client still registered: %d", n)
	}
}
