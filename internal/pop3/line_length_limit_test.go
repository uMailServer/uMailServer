package pop3

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// F4987: an unauthenticated client must not be able to make the server buffer
// an arbitrarily long command line. The client streams 1 MiB with no newline;
// the server must reject/close well before consuming it all.

const line4987Total = 1 << 20

// line4987Send writes a USER line padded to n bytes (no newline until the end)
// in 4 KiB chunks and reports how many bytes the server accepted plus the
// first reply line.
func line4987Send(t *testing.T, n int) (int, string) {
	t.Helper()
	srv := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), mailstore: &top4985Store{}}
	c1, c2 := net.Pipe()
	defer c2.Close()
	sess := NewSession(c1, srv)
	handled := make(chan struct{})
	go func() { sess.Handle(); c1.Close(); close(handled) }()

	replies := make(chan string, 1)
	go func() {
		l, err := bufio.NewReader(c2).ReadString('\n')
		if err != nil {
			replies <- "<closed:" + err.Error() + ">"
			return
		}
		replies <- strings.TrimRight(l, "\r\n")
	}()

	payload := append([]byte("USER "), bytes.Repeat([]byte("a"), n-7)...)
	payload = append(payload, '\r', '\n')
	written := 0
	for written < len(payload) {
		end := written + 4096
		if end > len(payload) {
			end = len(payload)
		}
		k, err := c2.Write(payload[written:end])
		written += k
		if err != nil {
			break
		}
	}
	reply := <-replies
	c2.Close()
	<-handled
	return written, reply
}

func TestCommandLineLimitControl(t *testing.T) {
	w, reply := line4987Send(t, 200)
	t.Logf("CONTROL EXPECTED: 200 bytes accepted, +OK | ACTUAL: %d bytes, %q", w, reply)
	if w != 200 || reply != "+OK" {
		t.Fatalf("INVALID CONTROL")
	}
}

func TestCommandLineLimitRejectsUnboundedLine(t *testing.T) {
	w, reply := line4987Send(t, line4987Total)
	t.Logf("EXPECTED: <= 65536 bytes accepted then -ERR/close | ACTUAL: %d bytes accepted, reply %q", w, reply)
	if w > 65536 || strings.HasPrefix(reply, "+OK") {
		t.Fatalf("DEFECT F4987: unauthenticated command line buffered without limit")
	}
}

// Boundary: exactly at and one byte over the limit.
func TestCommandLineLimitBoundary(t *testing.T) {
	if w, reply := line4987Send(t, maxCommandLineLength); reply != "+OK" || w != maxCommandLineLength {
		t.Fatalf("EDGE at limit: want +OK, got %d %q", w, reply)
	}
	if _, reply := line4987Send(t, maxCommandLineLength+1); !strings.HasPrefix(reply, "-ERR") {
		t.Fatalf("EDGE over limit: want -ERR, got %q", reply)
	}
}
