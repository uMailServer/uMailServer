package server

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"testing"
)

var (
	freePortMu   sync.Mutex
	freePortUsed = map[int]bool{}
)

// reserveFreePort returns a TCP port that is free right now, never handed out
// before in this process, and below the Linux ephemeral range (32768+). The
// kernel hands the same ephemeral ports to outbound client sockets, so the old
// "listen on :0, close, re-bind" pattern raced with loopback client
// connections and failed with EADDRINUSE under -race load.
func reserveFreePort(t testing.TB) int {
	t.Helper()
	freePortMu.Lock()
	defer freePortMu.Unlock()
	for i := 0; i < 200; i++ {
		p := 12000 + rand.Intn(20000)
		if freePortUsed[p] {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		ln.Close()
		freePortUsed[p] = true
		return p
	}
	// Fallback: kernel-assigned port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
