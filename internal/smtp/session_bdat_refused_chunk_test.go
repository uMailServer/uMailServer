package smtp

// F5058: when BDAT is refused (503 bad sequence, 552 too large) the server
// replies without consuming the <size> chunk octets that follow the command
// line, so the message content is then parsed and executed as SMTP commands
// (RFC 3030 §2: the chunk is always <size> octets of data; same smuggling
// shape as F4905 for DATA).

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func regF5058Session(t *testing.T, maxSize int64, delivered *int) (net.Conn, *bufio.Reader) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{
		Hostname:       "mx.test",
		MaxMessageSize: maxSize,
		MaxRecipients:  10,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}, nil)
	srv.SetDeliveryHandler(func(string, []string, []byte) error { *delivered++; return nil })
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(clientConn)
	regF5058Reply(t, r)
	return clientConn, r
}

func regF5058Reply(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(l) >= 4 && l[3] == ' ' {
			return l[:3]
		}
	}
}

// payload is chunk content that happens to look like commands
const regF5058Payload = "NOOP\r\nRSET\r\n"

// run sends the setup commands, then the BDAT line with its chunk in one
// write (PIPELINING), then QUIT; returns the codes received after setup.
func regF5058Run(t *testing.T, maxSize int64, rcpt bool, bdat string) ([]string, int) {
	t.Helper()
	delivered := 0
	c, r := regF5058Session(t, maxSize, &delivered)
	send := func(s string) { _, _ = c.Write([]byte(s)) }
	send("EHLO c.example\r\n")
	regF5058Reply(t, r)
	send("MAIL FROM:<a@example.org>\r\n")
	regF5058Reply(t, r)
	if rcpt {
		send("RCPT TO:<b@mx.test>\r\n")
		regF5058Reply(t, r)
	}
	go send(bdat + regF5058Payload + "QUIT\r\n")
	var codes []string
	for {
		code := regF5058Reply(t, r)
		codes = append(codes, code)
		if code == "221" {
			return codes, delivered
		}
	}
}

func TestF5058Control(t *testing.T) {
	codes, delivered := regF5058Run(t, 1<<20, true, fmt.Sprintf("BDAT %d LAST\r\n", len(regF5058Payload)))
	if strings.Join(codes, " ") != "250 221" || delivered != 1 {
		t.Fatalf("control: codes=%v delivered=%d", codes, delivered)
	}
}

func regF5058Expect(t *testing.T, label string, codes []string, want string) {
	t.Helper()
	got := strings.Join(codes, " ")
	if got != want {
		t.Errorf("F5058 %s: chunk octets executed as commands: %v", label, codes)
	}
}

func TestF5058BadSequence(t *testing.T) {
	codes, _ := regF5058Run(t, 1<<20, false, fmt.Sprintf("BDAT %d LAST\r\n", len(regF5058Payload)))
	regF5058Expect(t, "503", codes, "503 221")
}

func TestF5058TooLarge(t *testing.T) {
	codes, delivered := regF5058Run(t, 5, true, fmt.Sprintf("BDAT %d LAST\r\n", len(regF5058Payload)))
	regF5058Expect(t, "552", codes, "552 221")
	if delivered != 0 {
		t.Errorf("F5058 oversize delivered")
	}
}

// Edge cases.

func TestF5058EdgeZeroAndBadSize(t *testing.T) {
	codes, _ := regF5058Run(t, 1<<20, false, "BDAT 0 LAST\r\n")
	// zero-size chunk: nothing to discard, payload lines are real commands
	if got := strings.Join(codes, " "); got != "503 250 250 221" {
		t.Errorf("F5058 BDAT 0 refused: %v", codes)
	}
	codes, _ = regF5058Run(t, 1<<20, false, "BDAT abc LAST\r\n")
	if got := strings.Join(codes, " "); got != "503 250 250 221" {
		t.Errorf("F5058 bad size refused: %v", codes)
	}
}

func TestF5058EdgeCumulativeThenNewTransaction(t *testing.T) {
	delivered := 0
	c, r := regF5058Session(t, 16, &delivered)
	send := func(s string) { _, _ = c.Write([]byte(s)) }
	send("EHLO c.example\r\n")
	regF5058Reply(t, r)
	send("MAIL FROM:<a@example.org>\r\n")
	regF5058Reply(t, r)
	send("RCPT TO:<b@mx.test>\r\n")
	regF5058Reply(t, r)
	go send("BDAT 10\r\n0123456789BDAT 12 LAST\r\n" + regF5058Payload +
		"MAIL FROM:<a@example.org>\r\nRCPT TO:<b@mx.test>\r\nBDAT 3 LAST\r\nabcQUIT\r\n")
	var codes []string
	for {
		code := regF5058Reply(t, r)
		codes = append(codes, code)
		if code == "221" {
			break
		}
	}
	if got := strings.Join(codes, " "); got != "250 552 250 250 250 221" || delivered != 1 {
		t.Errorf("F5058 cumulative 552 + new transaction: %v delivered=%d", codes, delivered)
	}
}
