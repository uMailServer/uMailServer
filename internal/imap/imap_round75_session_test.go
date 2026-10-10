package imap

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// round75Session returns a command runner over a real BboltMailstore whose
// INBOX holds msgs (INTERNALDATEs 10-Jan-2024 12:00 UTC, +1 day each) and
// which also has an Archive mailbox. A handler panic is returned as a
// "panic=" line instead of killing the test binary. INBOX is not selected.
func round75Session(t *testing.T, msgs ...string) func(string) []string {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	user := "user@example.com"
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(user, mb); err != nil {
			t.Fatal(err)
		}
	}
	for i, m := range msgs {
		if err := ms.AppendMessage(user, "INBOX", nil, time.Date(2024, 1, 10+i, 12, 0, 0, 0, time.UTC), []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	client, srvConn := net.Pipe()
	s := NewSession(srvConn, NewServer(&Config{}, ms))
	s.state, s.user = StateAuthenticated, user
	lines := make(chan string, 1024)
	go func() {
		r := bufio.NewReader(client)
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				_, _ = io.Copy(io.Discard, r)
				close(lines)
				return
			}
			lines <- strings.TrimRight(l, "\r\n")
		}
	}()
	t.Cleanup(func() { _ = srvConn.Close(); _ = client.Close() })
	return func(line string) []string {
		tag := strings.Fields(line)[0]
		done := make(chan string, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Sprint("panic=", r)
					return
				}
				done <- ""
			}()
			_ = s.handleCommand(line)
		}()
		var out []string
		finished := done
		timeout := time.After(10 * time.Second)
		for {
			select {
			case l := <-lines:
				out = append(out, l)
				if strings.HasPrefix(l, tag+" ") {
					if finished != nil {
						<-finished
					}
					return out
				}
			case p := <-finished:
				if p != "" {
					return append(out, p)
				}
				finished = nil
			case <-timeout:
				t.Fatalf("no tagged reply to %q (got %q)", line, out)
			}
		}
	}
}

// round75Line returns the first line with prefix, or a marker plus all lines.
func round75Line(lines []string, prefix string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return "<none> " + strings.Join(lines, " | ")
}

func round75Tagged(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// round75Msg builds a message from header lines (CRLF-joined) and a body.
func round75Msg(headers ...string) string {
	return strings.Join(headers, "\r\n") + "\r\n\r\nbody\r\n"
}
