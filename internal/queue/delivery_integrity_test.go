package queue

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// --- F4925: DKIM-Signature header field name ---

func TestRegressionF4925_SignedMessageCarriesDKIMSignatureField(t *testing.T) {
	mgr, _, database := setupManager(t)
	defer database.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := database.CreateDomain(&db.DomainData{Name: "example.com", DKIMPrivateKey: string(pemKey), DKIMSelector: "sel"}); err != nil {
		t.Fatal(err)
	}
	orig := "From: user@example.com\r\nTo: rcpt@remote.test\r\nSubject: hi\r\n\r\nHello\r\n"
	signed, err := mgr.signWithDKIM("user@example.com", []byte(orig))
	if err != nil {
		t.Fatalf("signWithDKIM: %v", err)
	}
	if !bytes.HasPrefix(signed, []byte("DKIM-Signature: v=1; ")) {
		t.Fatalf("signed message must start with a DKIM-Signature field, got %.40q", signed)
	}
	if !bytes.HasSuffix(signed, []byte(orig)) {
		t.Fatal("original message must follow the signature unchanged")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(signed))
	if err != nil {
		t.Fatalf("signed message does not parse: %v", err)
	}
	if v := msg.Header.Get("DKIM-Signature"); !strings.Contains(v, "d=example.com") || !strings.Contains(v, "s=sel") {
		t.Fatalf("DKIM-Signature header missing or incomplete: %q", v)
	}
	if msg.Header.Get("Subject") != "hi" {
		t.Fatal("original headers lost after signing")
	}
}

// --- shared fakes for F4926 / F4927 ---

type fixedMXResolver struct{}

func (fixedMXResolver) LookupMX(string) ([]string, error) { return []string{"mx.example.test"}, nil }

// countingMXPeer is a minimal SMTP server that counts accepted messages.
func countingMXPeer(conn net.Conn, accepted *int32) {
	go func() {
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250 fake")
			case cmd == "STARTTLS":
				say("502 5.5.1 not supported")
			case cmd == "DATA":
				say("354 go ahead")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
				}
				atomic.AddInt32(accepted, 1)
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
}

// newDispatchTestManager returns a manager whose workers are "busy": entries
// wait in a buffered delivery channel until drainDispatched delivers them.
func newDispatchTestManager(t *testing.T) (*Manager, *int32) {
	t.Helper()
	m, _, database := setupManager(t)
	t.Cleanup(func() { database.Close() })
	var accepted int32
	m.resolver = fixedMXResolver{}
	m.mtastsValidator = nil
	m.dialSMTP = func(string) (net.Conn, error) {
		c, s := net.Pipe()
		countingMXPeer(s, &accepted)
		return c, nil
	}
	m.deliveryChan = make(chan *db.QueueEntry, m.workerCount*2)
	return m, &accepted
}

func drainDispatched(m *Manager) int {
	n := 0
	for {
		select {
		case e := <-m.deliveryChan:
			n++
			m.deliver(context.Background(), e)
		default:
			return n
		}
	}
}

func queueStatus(t *testing.T, m *Manager, id string) string {
	t.Helper()
	e, err := m.db.GetQueueEntry(id)
	if err != nil {
		return "<missing>"
	}
	return e.Status
}

// --- F4926: entries interrupted mid-delivery are requeued on Start ---

func TestRegressionF4926_InterruptedDeliveryRequeuedOnRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "q.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	msgPath := filepath.Join(dir, "m.msg")
	if err := os.WriteFile(msgPath, []byte("Subject: x\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mk := func(id, status string) *db.QueueEntry {
		e := &db.QueueEntry{ID: id, From: "a@sender.test", To: []string{"b@example.test"}, MessagePath: msgPath,
			Status: status, CreatedAt: time.Now(), NextRetry: time.Now().Add(-time.Minute)}
		if err := database.Enqueue(e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	inflight := mk("inflight-0", "pending")
	mk("done-0", "delivered")
	mk("bounced-0", "bounced")

	m1 := NewManager(database, nil, dir, nil)
	m1.resolver = fixedMXResolver{}
	m1.mtastsValidator = nil
	m1.mxBreaker = nil
	dialing := make(chan struct{})
	release := make(chan struct{})
	m1.dialSMTP = func(string) (net.Conn, error) {
		close(dialing)
		<-release
		return nil, errors.New("connection refused")
	}
	done := make(chan struct{})
	go func() { m1.deliver(context.Background(), inflight); close(done) }()
	<-dialing // claimed and talking to the MX
	m1.Stop()
	_ = database.Close() // the server closes the DB after queue.Stop
	close(release)
	<-done // the final status write fails: the entry stays "sending" on disk

	database2, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database2.Close()
	m2 := NewManager(database2, nil, dir, nil)
	m2.workerCount = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m2.Start(ctx)
	defer m2.Stop()

	if s := queueStatus(t, m2, "inflight-0"); s != "pending" {
		t.Fatalf("interrupted entry status after restart = %q, want pending", s)
	}
	pending, err := database2.GetPendingQueue(time.Now().Add(time.Second))
	if err != nil || len(pending) != 1 || pending[0].ID != "inflight-0" {
		t.Fatalf("pending after restart = %v (err %v), want only inflight-0", pending, err)
	}
	if s := queueStatus(t, m2, "done-0"); s != "delivered" {
		t.Fatalf("delivered entry changed to %q", s)
	}
	if s := queueStatus(t, m2, "bounced-0"); s != "bounced" {
		t.Fatalf("bounced entry changed to %q", s)
	}
}

// --- F4927: an entry dispatched twice is delivered once ---

func TestRegressionF4927_SweepOfBufferedEntryDoesNotRedeliver(t *testing.T) {
	m, accepted := newDispatchTestManager(t)
	id, err := m.Enqueue("a@sender.test", []string{"b@example.test"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.sweepPendingEntries() // entry still buffered, so still "pending": dispatched again
	if n := drainDispatched(m); n != 2 {
		t.Fatalf("dispatched %d copies, want 2 (precondition)", n)
	}
	if got := atomic.LoadInt32(accepted); got != 1 {
		t.Fatalf("remote MTA accepted %d copies, want 1", got)
	}
	if s := queueStatus(t, m, id+"-0"); s != "delivered" {
		t.Fatalf("entry status = %q, want delivered (stale copy must not resurrect it)", s)
	}
}

func TestRegressionF4927_DroppedEntryNotDelivered(t *testing.T) {
	m, accepted := newDispatchTestManager(t)
	id, err := m.Enqueue("a@sender.test", []string{"b@example.test"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DropEntry(id + "-0"); err != nil {
		t.Fatal(err)
	}
	drainDispatched(m)
	if got := atomic.LoadInt32(accepted); got != 0 {
		t.Fatalf("dropped entry was delivered %d times", got)
	}
}

func TestRegressionF4927_RetryEntryRedelivers(t *testing.T) {
	m, accepted := newDispatchTestManager(t)
	id, err := m.Enqueue("a@sender.test", []string{"b@example.test", "c@example.test"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	drainDispatched(m)
	if got := atomic.LoadInt32(accepted); got != 2 {
		t.Fatalf("accepted %d, want 2", got)
	}
	// A failed entry retried by the admin is claimable again.
	e, err := m.db.GetQueueEntry(id + "-1")
	if err != nil {
		t.Fatal(err)
	}
	e.Status = "failed"
	if err := m.db.UpdateQueueEntry(e); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.MessagePath, []byte("Subject: x\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.RetryEntry(id + "-1"); err != nil {
		t.Fatal(err)
	}
	drainDispatched(m)
	if got := atomic.LoadInt32(accepted); got != 3 {
		t.Fatalf("accepted %d after retry, want 3", got)
	}
}
