package queue

import (
	"bufio"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

// fakeSMTPPeer answers the greeting, RSET and QUIT so a net/smtp client
// over net.Pipe behaves like a live pooled MX connection.
func fakeSMTPPeer(t *testing.T, conn net.Conn) {
	t.Helper()
	go func() {
		defer conn.Close()
		w := bufio.NewWriter(conn)
		r := bufio.NewReader(conn)
		_, _ = w.WriteString("220 fake ESMTP\r\n")
		_ = w.Flush()
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.ToUpper(line), "QUIT") {
				_, _ = w.WriteString("221 bye\r\n")
				_ = w.Flush()
				return
			}
			_, _ = w.WriteString("250 ok\r\n")
			_ = w.Flush()
		}
	}()
}

// TestWithMXConn_PooledPanicReturnsError checks that a panic inside fn on a
// pooled connection is reported as an error rather than a successful delivery,
// and that the connection is not returned to the pool.
func TestWithMXConn_PooledPanicReturnsError(t *testing.T) {
	m := &Manager{
		mxPools:       make(map[string]*mxPool),
		mxPoolSize:    10,
		mxIdleTimeout: time.Hour,
	}
	const mx = "mx.example.com"

	clientSide, serverSide := net.Pipe()
	fakeSMTPPeer(t, serverSide)
	client, err := smtp.NewClient(clientSide, mx)
	if err != nil {
		t.Fatalf("smtp.NewClient: %v", err)
	}
	m.releaseMXConn(mx, client, true)

	err = m.withMXConn(mx, func(*smtp.Client) error {
		panic("boom")
	})
	if err == nil {
		t.Fatal("expected an error for a panic on a pooled connection, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should carry the panic value, got %v", err)
	}

	pool := m.getMXPool(mx)
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if n := len(pool.conns); n != 0 {
		t.Fatalf("panicked connection must not be returned to the pool, pool has %d", n)
	}
}
