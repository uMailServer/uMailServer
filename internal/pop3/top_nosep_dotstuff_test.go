package pop3

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// F4985: TOP on a message without a header/body blank line must emit the
// dot-stuffed message followed by exactly ONE terminator, so the next command
// response stays in sync.

type top4985Store struct{ data []byte }

func (a *top4985Store) Authenticate(string, string) (bool, error) { return true, nil }
func (a *top4985Store) ListMessages(string) ([]*Message, error) {
	return []*Message{{Index: 1, UID: "u1", Size: int64(len(a.data))}}, nil
}
func (a *top4985Store) GetMessage(string, int) (*Message, error) { return nil, nil }
func (a *top4985Store) GetMessageData(string, int) ([]byte, error) {
	return a.data, nil
}
func (a *top4985Store) DeleteMessage(string, int) error           { return nil }
func (a *top4985Store) GetMessageCount(string) (int, error)       { return 1, nil }
func (a *top4985Store) GetMessageSize(string, int) (int64, error) { return 0, nil }

// top4985Top runs USER/PASS/TOP/NOOP over net.Pipe and returns the raw TOP
// data lines (up to the first "."), and the line read as the NOOP reply.
func top4985Top(t *testing.T, raw, topCmd string) ([]string, string) {
	t.Helper()
	srv := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), mailstore: &top4985Store{data: []byte(raw)}}
	c1, c2 := net.Pipe()
	defer c2.Close()
	sess := NewSession(c1, srv)
	go func() { sess.Handle(); c1.Close() }()
	rd := bufio.NewReader(c2)
	readLine := func() string {
		l, err := rd.ReadString('\n')
		if err != nil {
			t.Fatalf("INVALID harness read: %v", err)
		}
		return strings.TrimRight(l, "\r\n")
	}
	send := func(c string) {
		if _, err := c2.Write([]byte(c + "\r\n")); err != nil {
			t.Fatalf("INVALID harness write: %v", err)
		}
	}
	send("USER a")
	if r := readLine(); !strings.HasPrefix(r, "+OK") {
		t.Fatalf("INVALID harness USER: %q", r)
	}
	send("PASS b")
	if r := readLine(); !strings.HasPrefix(r, "+OK") {
		t.Fatalf("INVALID harness PASS: %q", r)
	}
	send(topCmd)
	if r := readLine(); !strings.HasPrefix(r, "+OK") {
		t.Fatalf("INVALID harness TOP status: %q", r)
	}
	var lines []string
	for {
		l := readLine()
		if l == "." {
			break
		}
		lines = append(lines, l)
	}
	send("NOOP")
	return lines, readLine()
}

func TestTOPRegressionSeparatorControl(t *testing.T) {
	lines, noop := top4985Top(t, "Subject: x\r\n\r\nB1\r\n.dot\r\nB3\r\n", "TOP 1 2")
	want := []string{"Subject: x", "", "B1", "..dot"}
	t.Logf("CONTROL EXPECTED: %q noop=+OK | ACTUAL: %q noop=%q", want, lines, noop)
	if strings.Join(lines, "|") != strings.Join(want, "|") || noop != "+OK" {
		t.Fatalf("INVALID CONTROL")
	}
}

func TestTOPNoSeparatorDotStuffedSingleTerminator(t *testing.T) {
	lines, noop := top4985Top(t, "From: a@example.com\r\nSubject: NoSep\r\n.hidden\r\nLine1", "TOP 1 1")
	want := []string{"From: a@example.com", "Subject: NoSep", "..hidden", "Line1"}
	t.Logf("EXPECTED: %q noop=+OK | ACTUAL: %q noop=%q", want, lines, noop)
	if strings.Join(lines, "|") != strings.Join(want, "|") || noop != "+OK" {
		t.Fatalf("DEFECT F4985: TOP without header separator is not dot-stuffed/terminated once")
	}
}

// Edge cases: bare LF, dotted header, empty message.
func TestTOPDotStuffingEdges(t *testing.T) {
	// Bare-LF message without separator, ending in a lone "." line.
	lines, noop := top4985Top(t, "Subject: y\n.\n", "TOP 1 0")
	want := []string{"Subject: y", ".."}
	if strings.Join(lines, "|") != strings.Join(want, "|") || noop != "+OK" {
		t.Fatalf("EDGE bare-LF: want %q noop=+OK got %q noop=%q", want, lines, noop)
	}
	// Header line beginning with "." in a message that has a separator.
	lines, noop = top4985Top(t, ".X: 1\r\nSubject: z\r\n\r\nbody\r\n", "TOP 1 1")
	want = []string{"..X: 1", "Subject: z", "", "body"}
	if strings.Join(lines, "|") != strings.Join(want, "|") || noop != "+OK" {
		t.Fatalf("EDGE dotted header: want %q noop=+OK got %q noop=%q", want, lines, noop)
	}
	// Empty message.
	lines, noop = top4985Top(t, "", "TOP 1 5")
	if len(lines) > 1 || noop != "+OK" {
		t.Fatalf("EDGE empty: got %q noop=%q", lines, noop)
	}
}
