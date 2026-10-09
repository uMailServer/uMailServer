package smtp

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
)

func TestAuditRegressionF15(t *testing.T) {
	drive := func(replace bool) ([]string, string) {
		a, b := net.Pipe()
		done := make(chan struct{})
		go func() { io.Copy(io.Discard, b); close(done) }()
		defer func() { a.Close(); b.Close(); <-done }()
		var to []string
		var body string
		server := &Server{config: &Config{MaxMessageSize: 1024, MaxRecipients: 10}, onDeliver: func(from string, rcpt []string, data []byte) error {
			to = append([]string(nil), rcpt...)
			body = bdatPayload(data) // generated Message-ID stripped (F5055)
			return nil
		}}
		s := NewSession(a, server)
		s.state = StateGreeted
		command := func(c string) {
			if e := s.HandleCommand(c); e != nil {
				t.Fatal(e)
			}
		}
		if replace {
			command("MAIL FROM:<old@example.test>")
			command("RCPT TO:<old-recipient@example.test> NOTIFY=FAILURE")
			s.reader = bufio.NewReader(strings.NewReader("old"))
			command("BDAT 3")
			if s.bdatBuffer == nil || !bytes.Equal(s.bdatBuffer.Bytes(), []byte("old")) {
				t.Fatal("partial chunk fixture invalid")
			}
		}
		command("MAIL FROM:<new@example.test>")
		command("RCPT TO:<new-recipient@example.test> NOTIFY=NEVER")
		s.reader = bufio.NewReader(strings.NewReader("NEW"))
		command("BDAT 3 LAST")
		return to, body
	}
	c, body := drive(false)
	if len(c) != 1 || c[0] != "new-recipient@example.test" || body != "NEW" {
		t.Fatal("invalid control")
	}
	actual, body := drive(true)
	if len(actual) != 1 || actual[0] != "new-recipient@example.test" || body != "NEW" {
		t.Fatalf("fresh MAIL retained old transaction: recipients=%v body=%q", actual, body)
	}
	a, peer := net.Pipe()
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, peer); close(done) }()
	defer func() { a.Close(); peer.Close(); <-done }()
	s := NewSession(a, &Server{config: &Config{MaxMessageSize: 1024, MaxRecipients: 10}})
	s.state = StateRcptTo
	s.rcptTo = []string{"old@example.test"}
	s.rcptToNotify = []string{"FAILURE"}
	s.data = []byte("old")
	s.isAuth = true
	if e := s.handleMAIL("FROM:<@>"); e != nil {
		t.Fatal(e)
	}
	if len(s.rcptTo) != 1 || s.state != StateRcptTo {
		t.Fatal("invalid MAIL changed transaction")
	}
	for i := 0; i < 2; i++ {
		if e := s.handleMAIL("FROM:<>"); e != nil {
			t.Fatal(e)
		}
		if len(s.rcptTo) != 0 || len(s.rcptToNotify) != 0 || s.data != nil || s.bdatBuffer != nil || s.mailFrom != "" || !s.isAuth || s.state != StateMailFrom {
			t.Fatal("null/repeated MAIL retained transaction or lost authentication")
		}
	}

}
