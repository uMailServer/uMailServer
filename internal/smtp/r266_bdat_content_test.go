package smtp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func r266Run(t *testing.T, useBDAT bool, body string) (delivered int, finalReply string) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error { delivered++; return nil })
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	defer func() { _ = clientConn.Close(); <-done }()
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(clientConn)
	reply := func() string {
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("INVALID: %v", err)
			}
			if len(l) >= 4 && l[3] == ' ' {
				return strings.TrimSpace(l)
			}
		}
	}
	send := func(s string) { go func() { _, _ = clientConn.Write([]byte(s)) }() }
	reply()
	for _, c := range []string{"EHLO c.test\r\n", "MAIL FROM:<a@b.test>\r\n", "RCPT TO:<c@d.test>\r\n"} {
		send(c)
		reply()
	}
	if useBDAT {
		send(fmt.Sprintf("BDAT %d LAST\r\n%s", len(body), body))
	} else {
		send("DATA\r\n")
		if got := reply(); !strings.HasPrefix(got, "354") {
			t.Fatalf("INVALID: DATA got %s", got)
		}
		send(body + ".\r\n")
	}
	finalReply = reply()
	return
}

// F7400: BDAT must hold the content rules DATA enforces for non-BINARYMIME
// messages: no NUL bytes, no line over 1000 octets (RFC 5322 §2.1.1).
func TestR266BDATContentRules(t *testing.T) {
	line := func(n int) string { return "Subject: x\r\n\r\n" + strings.Repeat("A", n-2) + "\r\n" }
	cases := []struct {
		name string
		body string
		want int
	}{
		{"nul in header", "Subject: x\r\nX-A: a\x00b\r\n\r\nbody\r\n", 0},
		{"line of 1001 octets", line(1001), 0},
		{"line of 1000 octets", line(1000), 1},
		{"long bare-LF line", "Subject: x\n\n" + strings.Repeat("A", 1200) + "\r\n", 0},
		{"clean", "Subject: x\r\n\r\nok\r\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, _ := r266Run(t, true, c.body); got != c.want {
				t.Fatalf("BDAT delivered=%d, want %d", got, c.want)
			}
			if c.want == 0 {
				if got, _ := r266Run(t, false, c.body); got != 0 {
					t.Fatalf("INVALID: DATA control delivered")
				}
			}
		})
	}
}
