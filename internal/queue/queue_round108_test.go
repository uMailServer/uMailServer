package queue

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// Regression tests for F5900-F5904 (round 108).

// F5902: headers-only returns are labelled text/rfc822-headers, oversized full
// returns are cut to headers, and the DSN is marked auto-generated.
func TestR108DSNOriginalPartAndHeaders(t *testing.T) {
	d := &DSN{ReportedDomain: "h", OriginalFrom: "a@b.test", OriginalTo: "c@d.test", Recipient: DSNRecipient{Original: "c@d.test"}, Action: "failed", Status: "5.1.1"}
	msg := []byte("Subject: x\r\n\r\nbody-secret\r\n")
	out, _ := GenerateDSN(d, msg, DSNRetHeaders)
	s := string(out)
	if !strings.Contains(s, "Content-Type: text/rfc822-headers") || strings.Contains(s, "body-secret") {
		t.Fatalf("headers-only DSN wrong:\n%s", s)
	}
	if !strings.Contains(s, "Auto-Submitted: auto-generated") {
		t.Fatal("missing Auto-Submitted")
	}
	if strings.Contains(s, "Message-ID: \r\n") {
		t.Fatal("empty Message-ID")
	}
	big := append([]byte("Subject: big\r\n\r\n"), []byte(strings.Repeat("A", maxDSNOriginalSize+1))...)
	out, _ = GenerateDSN(d, big, DSNRetFull)
	if len(out) > 8*1024 {
		t.Fatalf("oversized original returned in full: %d bytes", len(out))
	}
	out, _ = GenerateDSN(d, msg, DSNRetFull)
	if !strings.Contains(string(out), "Content-Type: message/rfc822") || !strings.Contains(string(out), "body-secret") {
		t.Fatal("small full return must stay message/rfc822")
	}
}

// F5900: a success DSN for a null-sender message is not enqueued to "".
func TestR108SuccessDSNNullSender(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	id, err := m.Enqueue("", []string{"x@y.test"}, []byte("Subject: s\r\n\r\nb\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := m.db.GetQueueEntry(id + "-0")
	m.sendSuccessDSN(e)
	n := 0
	_ = m.db.ForEach(db.BucketQueue, func(string, []byte) error { n++; return nil })
	if n != 1 {
		t.Fatalf("expected only the original entry, got %d", n)
	}
}

// F5901: every retry delay (incl. the final 48h) is used before bouncing.
func TestR108RetryScheduleUsesAllDelays(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	id, _ := m.Enqueue("a@b.test", []string{"x@y.test"}, []byte("Subject: s\r\n\r\nb\r\n"))
	e, _ := m.db.GetQueueEntry(id + "-0")
	var last time.Duration
	for i := 0; i < len(retryDelays); i++ {
		m.failDelivery(e, "451 try later", false)
		if e.Status != "pending" {
			t.Fatalf("attempt %d: status %s, want pending", i+1, e.Status)
		}
		last = time.Until(e.NextRetry)
	}
	if last < 38*time.Hour {
		t.Fatalf("last delay %v, want ~48h", last)
	}
	m.failDelivery(e, "451 try later", false)
	if e.Status != "bounced" {
		t.Fatalf("status %s, want bounced", e.Status)
	}
}

// F5903: a message larger than the advertised SIZE is rejected permanently
// without sending DATA.
func TestR108SizeExtension(t *testing.T) {
	var dataSeen int32
	dial := func(string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() {
			defer c2.Close()
			r := bufio.NewReader(c2)
			say := func(s string) { _, _ = c2.Write([]byte(s + "\r\n")) }
			say("220 fake")
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				cmd := strings.ToUpper(strings.TrimSpace(line))
				switch {
				case strings.HasPrefix(cmd, "EHLO"):
					say("250-fake")
					say("250 SIZE 100")
				case strings.HasPrefix(cmd, "STARTTLS"):
					say("502 no")
				case cmd == "DATA":
					atomic.AddInt32(&dataSeen, 1)
					say("354 go")
				case cmd == "QUIT":
					say("221 bye")
					return
				default:
					say("250 ok")
				}
			}
		}()
		return c1, nil
	}
	m := routeManager(t, routeResolver{"big.test": {"mx.big.test"}}, dial)
	id, _ := m.Enqueue("a@sender.test", []string{"u@big.test"}, []byte("Subject: x\r\n\r\n"+strings.Repeat("A", 500)+"\r\n"))
	e, _ := m.db.GetQueueEntry(id + "-0")
	m.deliver(context.Background(), e)
	e, _ = m.db.GetQueueEntry(id + "-0")
	if e.Status != "bounced" || atomic.LoadInt32(&dataSeen) != 0 {
		t.Fatalf("status=%s data=%d, want bounced without DATA (%s)", e.Status, dataSeen, e.LastError)
	}
}

// F5904: Start is idempotent under concurrency and the manager can be
// restarted after Stop (workers run again).
func TestR108StartStopRestart(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	m.workerCount = 2
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.Start(ctx) }()
	}
	wg.Wait()
	m.Stop()
	m.Stop()
	m.Start(ctx)
	defer m.Stop()
	select {
	case <-m.shutdownCh():
		t.Fatal("shutdown channel still closed after restart")
	default:
	}
	m.resolver = routeResolver{"ok.test": {"mx.ok.test"}}
	var acc int32
	m.mtastsValidator, m.daneValidator = nil, nil
	m.dialSMTP = func(string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		routePeer(c2, "250 ok", &acc)
		return c1, nil
	}
	if _, err := m.Enqueue("a@s.test", []string{"u@ok.test"}, []byte("Subject: x\r\n\r\nb\r\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&acc) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&acc) != 1 {
		t.Fatal("restarted manager did not deliver")
	}
}
