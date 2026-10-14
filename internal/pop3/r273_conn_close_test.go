package pop3

import (
	"bufio"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
)

type r273CloseConn struct {
	net.Conn
	closed atomic.Int32
}

func (c *r273CloseConn) Close() error { c.closed.Add(1); return c.Conn.Close() }

// F7470: the server must close the socket itself once a session ends.
func TestHandleConnection_ClosesConnWhenSessionEnds(t *testing.T) {
	failCmds := []string{}
	for i := 0; i < maxFailedPassPerConn; i++ {
		failCmds = append(failCmds, "USER x", "PASS y")
	}
	cases := map[string][]string{
		"quit":            {"QUIT"},
		"auth-failure":    failCmds,
		"quit-after-noop": {"NOOP", "QUIT"},
	}
	for name, cmds := range cases {
		t.Run(name, func(t *testing.T) {
			srv := NewServer("127.0.0.1:0", &mockMailstore{}, nil)
			srv.SetAuthFunc(func(u, p string) (bool, error) { return false, nil })
			a, b := net.Pipe()
			sc := &r273CloseConn{Conn: b}
			done := make(chan struct{})
			go func() { srv.handleConnection(sc); close(done) }()
			r := bufio.NewReader(a)
			_, _ = r.ReadString('\n')
			for _, c := range cmds {
				fmt.Fprintf(a, "%s\r\n", c)
				_, _ = r.ReadString('\n')
			}
			<-done
			_ = a.Close()
			if sc.closed.Load() == 0 {
				t.Fatalf("server connection not closed after session ended")
			}
		})
	}
}
