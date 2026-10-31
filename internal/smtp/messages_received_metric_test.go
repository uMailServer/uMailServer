package smtp

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
)

// F7792: umailserver_smtp_messages_received_total stayed 0 however much mail
// was accepted; nothing called SMTPMessageReceived.
func smtpMessagesReceived() uint64 {
	return metrics.Get().GetStats()["smtp"].(map[string]uint64)["messages"]
}

func acceptOneMessage(t *testing.T, deliverErr error) (reply string) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error { return deliverErr })
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(15 * time.Second))
	c := &r85Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	c.hello(t)
	for _, line := range []string{"MAIL FROM:<a@example.org>\r\n", "RCPT TO:<b@mx.test>\r\n", "DATA\r\n"} {
		if r := c.cmd(t, line); !strings.HasPrefix(r, "250") && !strings.HasPrefix(r, "354") {
			t.Fatalf("%q -> %q", line, r)
		}
	}
	return c.cmd(t, "Subject: x\r\n\r\nbody\r\n.\r\n")
}

func TestAcceptedMessagesAreCounted(t *testing.T) {
	before := smtpMessagesReceived()
	if r := acceptOneMessage(t, nil); !strings.HasPrefix(r, "250") {
		t.Fatalf("DATA end: %q", r)
	}
	if got := smtpMessagesReceived() - before; got != 1 {
		t.Fatalf("EXPECTED messages_received to grow by 1\nACTUAL: %d", got)
	}
}

func TestFailedDeliveryIsNotCountedAsReceived(t *testing.T) {
	before := smtpMessagesReceived()
	if r := acceptOneMessage(t, errors.New("disk full")); strings.HasPrefix(r, "250") {
		t.Fatalf("delivery error was accepted: %q", r)
	}
	if got := smtpMessagesReceived() - before; got != 0 {
		t.Fatalf("a message that was not accepted was counted (%d)", got)
	}
}

func TestAcceptedBDATMessagesAreCounted(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	srv.SetDeliveryHandler(func(from string, to []string, data []byte) error { return nil })
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(15 * time.Second))
	c := &r85Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	c.hello(t)
	for _, line := range []string{"MAIL FROM:<a@example.org>\r\n", "RCPT TO:<b@mx.test>\r\n"} {
		if r := c.cmd(t, line); !strings.HasPrefix(r, "250") {
			t.Fatalf("%q -> %q", line, r)
		}
	}
	before := smtpMessagesReceived()
	body := "Subject: x\r\n\r\nbody\r\n"
	if r := c.cmd(t, "BDAT "+strconv.Itoa(len(body))+" LAST\r\n"+body); !strings.HasPrefix(r, "250") {
		t.Fatalf("BDAT LAST: %q", r)
	}
	if got := smtpMessagesReceived() - before; got != 1 {
		t.Fatalf("EXPECTED messages_received to grow by 1 after BDAT\nACTUAL: %d", got)
	}
}
