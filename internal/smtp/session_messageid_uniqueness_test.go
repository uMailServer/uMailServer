package smtp

import (
	"bufio"
	"bytes"
	"net"
	"net/mail"
	"strings"
	"testing"
)

type messageIDCaptureConn struct {
	net.Conn
	output bytes.Buffer
}

func (c *messageIDCaptureConn) Write(p []byte) (int, error) {
	return c.output.Write(p)
}

func TestHandleDATAGeneratesDistinctMessageIDsPerTransaction(t *testing.T) {
	session := NewSession(&messageIDCaptureConn{}, &Server{
		config: &Config{Hostname: "mail.example.test", MaxMessageSize: 10000},
	})
	sessionID := session.ID()
	var delivered []byte
	session.server.onDeliver = func(_ string, _ []string, data []byte) error {
		delivered = append([]byte(nil), data...)
		return nil
	}
	deliver := func(header string) string {
		t.Helper()
		session.state = StateRcptTo
		session.mailFrom = "from@example.test"
		session.rcptTo = []string{"to@example.test"}
		session.reader = bufio.NewReader(strings.NewReader("Subject: ordinary\r\n" + header + "\r\nbody\r\n.\r\n"))
		delivered = nil
		if err := session.handleDATA(); err != nil {
			t.Fatal(err)
		}
		message, err := mail.ReadMessage(bytes.NewReader(delivered))
		if err != nil {
			t.Fatal(err)
		}
		id := message.Header.Get("Message-ID")
		if id == "" {
			t.Fatal("delivered message has no Message-ID")
		}
		return id
	}
	if got := deliver("Message-ID: <explicit@example.test>\r\n"); got != "<explicit@example.test>" {
		t.Fatalf("explicit Message-ID changed: %s", got)
	}
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := deliver("")
		if seen[id] {
			t.Fatalf("distinct transactions reused Message-ID %s", id)
		}
		seen[id] = true
	}
	if got := deliver("Message-ID: <after-generated@example.test>\r\n"); got != "<after-generated@example.test>" {
		t.Fatalf("explicit Message-ID changed after generated messages: %s", got)
	}
	if session.ID() != sessionID {
		t.Fatal("session identity changed across mail transactions")
	}
}
