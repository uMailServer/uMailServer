package smtp

import (
	"bufio"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

// F7310: RET, ENVID and per-recipient ORCPT reach the delivery handler.
func TestF7310DeliveryHandlerGetsDSNParams(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	srv := NewServer(&Config{Hostname: "mx.test", MaxMessageSize: 1 << 20, MaxRecipients: 10,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, nil)
	var gotDSN *DSNInfo
	var gotNotify []string
	srv.SetDeliveryHandlerWithDSN(func(from string, to []string, notify []string, dsn *DSNInfo, data []byte) error {
		gotDSN, gotNotify = dsn, notify
		return nil
	})
	done := make(chan struct{})
	go func() { srv.handleConnection(serverConn); close(done) }()
	t.Cleanup(func() { _ = clientConn.Close(); <-done })
	_ = clientConn.SetDeadline(time.Now().Add(15 * time.Second))
	c := &r85Client{conn: clientConn, r: bufio.NewReader(clientConn)}
	c.reply(t)
	c.hello(t)
	for _, step := range []struct{ line, want string }{
		{"MAIL FROM:<a@example.org> RET=HDRS ENVID=Env+2B1\r\n", "250"},
		{"RCPT TO:<b@mx.test> NOTIFY=FAILURE ORCPT=rfc822;Orig+2B@mx.test\r\n", "250"},
		{"RCPT TO:<c@mx.test>\r\n", "250"},
		{"DATA\r\n", "354"},
	} {
		if r := c.cmd(t, step.line); !strings.HasPrefix(r, step.want) {
			t.Fatalf("%q -> %q", step.line, r)
		}
	}
	if r := c.cmd(t, "Subject: x\r\n\r\nbody\r\n.\r\n"); !strings.HasPrefix(r, "250") {
		t.Fatalf("DATA end: %q", r)
	}
	want := &DSNInfo{Ret: "HDRS", EnvID: "Env+2B1", ORCPT: []string{"rfc822;Orig+2B@mx.test", ""}}
	if !reflect.DeepEqual(gotDSN, want) {
		t.Errorf("DSNInfo = %+v, want %+v", gotDSN, want)
	}
	if len(gotNotify) != 2 || gotNotify[0] != "FAILURE" {
		t.Errorf("notify = %v", gotNotify)
	}
}

// A new transaction must not inherit the previous transaction's parameters.
func TestF7310ParamsResetBetweenTransactions(t *testing.T) {
	s := &Session{}
	s.mailEnvID, s.mailFromRet, s.rcptOrcpt = "e", "HDRS", []string{"rfc822;x"}
	s.resetTransaction()
	if s.mailEnvID != "" || s.mailFromRet != "" || len(s.rcptOrcpt) != 0 {
		t.Fatalf("stale DSN params after reset: %q %q %v", s.mailEnvID, s.mailFromRet, s.rcptOrcpt)
	}
}
