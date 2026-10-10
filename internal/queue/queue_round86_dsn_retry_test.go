package queue

// Regression tests for F5680-F5684 (outbound EHLO, return path, opportunistic TLS, claim vs NextRetry, DSN diagnostics).

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

type r86Peer struct {
	mu        sync.Mutex
	ehlo      []string
	mailFrom  []string
	accepted  int32
	tlsOK     int32
	rcptReply string // "" = 250 ok; lines separated by \n
	startTLS  string // "" = refuse with 502; "tls" = accept and handshake
	cert      tls.Certificate
	dials     int32
}

func r86SelfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mx.remote.test"},
		DNSNames: []string{"mx.remote.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
}

func (p *r86Peer) start(t *testing.T) func(string) (net.Conn, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return func(string) (net.Conn, error) {
		atomic.AddInt32(&p.dials, 1)
		return net.Dial("tcp", ln.Addr().String())
	}
}

func (p *r86Peer) serve(conn net.Conn) {
	defer func() { conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		raw := strings.TrimSpace(line)
		cmd := strings.ToUpper(raw)
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			p.mu.Lock()
			p.ehlo = append(p.ehlo, strings.TrimSpace(raw[4:]))
			p.mu.Unlock()
			say("250 fake")
		case cmd == "STARTTLS":
			if p.startTLS != "tls" {
				say("502 5.5.1 not supported")
				continue
			}
			say("220 go ahead")
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{p.cert}})
			if err := tc.Handshake(); err != nil {
				return
			}
			atomic.AddInt32(&p.tlsOK, 1)
			conn = tc
			r = bufio.NewReader(tc)
			w = bufio.NewWriter(tc)
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			a := raw[len("MAIL FROM:"):]
			if i := strings.Index(a, ">"); i >= 0 {
				a = a[:i]
			}
			p.mu.Lock()
			p.mailFrom = append(p.mailFrom, strings.TrimPrefix(a, "<"))
			p.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT"):
			if p.rcptReply == "" {
				say("250 ok")
			} else {
				for _, l := range strings.Split(p.rcptReply, "\n") {
					say(l)
				}
			}
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
			atomic.AddInt32(&p.accepted, 1)
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func r86Manager(t *testing.T, p *r86Peer) *Manager {
	t.Helper()
	m, _, database := setupManager(t)
	t.Cleanup(func() { database.Close() })
	m.resolver = routeResolver{"remote.test": {"mx.remote.test"}}
	m.mtastsValidator = nil
	m.daneValidator = nil
	m.dialSMTP = p.start(t)
	return m
}

func r86Send(t *testing.T, m *Manager, from string) *db.QueueEntry {
	t.Helper()
	id, err := m.Enqueue(from, []string{"x@remote.test"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := m.db.GetQueueEntry(id + "-0")
	if err != nil {
		t.Fatal(err)
	}
	m.deliver(context.Background(), e)
	e, err = m.db.GetQueueEntry(id + "-0")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// ---- F5680: outbound EHLO name ----

func TestRegressionF5680Control(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	if e := r86Send(t, m, "a@sender.test"); e.Status != "delivered" || len(p.ehlo) == 0 {
		t.Fatalf("INVALID control: status=%q ehlo=%v", e.Status, p.ehlo)
	}
}

func TestRegressionF5680EHLO(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	r86Send(t, m, "a@sender.test")
	if len(p.ehlo) == 0 {
		t.Fatal("INVALID no EHLO seen")
	}
	if p.ehlo[0] == "localhost" {
		t.Fatalf("DEFECT F5680\nEXPECTED: EHLO <FQDN or address literal>\nACTUAL: EHLO %s", p.ehlo[0])
	}
}

// ---- F5681: VERP envelope sender with no decoder ----

func TestRegressionF5681Control(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	if e := r86Send(t, m, ""); e.Status != "delivered" || len(p.mailFrom) != 1 || p.mailFrom[0] != "" {
		t.Fatalf("INVALID control: status=%q mail=%q", e.Status, p.mailFrom)
	}
}

func TestRegressionF5681VERP(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	r86Send(t, m, "a@sender.test")
	if len(p.mailFrom) != 1 {
		t.Fatalf("INVALID mail=%q", p.mailFrom)
	}
	if p.mailFrom[0] != "a@sender.test" {
		t.Fatalf("DEFECT F5681\nEXPECTED: MAIL FROM:<a@sender.test>\nACTUAL: MAIL FROM:<%s>", p.mailFrom[0])
	}
}

// ---- F5682: opportunistic STARTTLS against a self-signed certificate ----

func TestRegressionF5682Control(t *testing.T) {
	p := &r86Peer{} // refuses STARTTLS (502): plaintext fallback must work
	m := r86Manager(t, p)
	if e := r86Send(t, m, "a@sender.test"); e.Status != "delivered" {
		t.Fatalf("INVALID control: status=%q err=%q", e.Status, e.LastError)
	}
}

func TestRegressionF5682SelfSigned(t *testing.T) {
	p := &r86Peer{startTLS: "tls", cert: r86SelfSigned(t)}
	m := r86Manager(t, p)
	e := r86Send(t, m, "a@sender.test")
	if e.Status != "delivered" {
		t.Fatalf("DEFECT F5682\nEXPECTED: delivered (opportunistic TLS)\nACTUAL: status=%q retry=%d err=%q", e.Status, e.RetryCount, e.LastError)
	}
}

// ---- F5683: claim ignores NextRetry ----

func r86PendingEntry(t *testing.T, m *Manager, next time.Time) *db.QueueEntry {
	t.Helper()
	path := m.dataDir + "/r86.msg"
	if err := os.WriteFile(path, []byte("Subject: x\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &db.QueueEntry{ID: "r86-0", From: "a@sender.test", To: []string{"x@remote.test"}, MessagePath: path,
		CreatedAt: time.Now(), NextRetry: next, RetryCount: 2, Status: "pending"}
	if err := m.db.EnqueueWithLimit(e, 100); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRegressionF5683Control(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	e := r86PendingEntry(t, m, time.Now().Add(-time.Minute))
	m.deliver(context.Background(), e)
	if g, _ := m.db.GetQueueEntry(e.ID); g.Status != "delivered" {
		t.Fatalf("INVALID control: %q", g.Status)
	}
}

func TestRegressionF5683NextRetry(t *testing.T) {
	p := &r86Peer{rcptReply: "451 4.2.0 try later"}
	m := r86Manager(t, p)
	e := r86PendingEntry(t, m, time.Now().Add(time.Hour)) // deferred by a prior failure
	m.deliver(context.Background(), e)                    // stale duplicate dispatched early
	g, _ := m.db.GetQueueEntry(e.ID)
	if atomic.LoadInt32(&p.dials) != 0 || g.RetryCount != 2 {
		t.Fatalf("DEFECT F5683\nEXPECTED: not attempted before NextRetry (dials=0 retry=2)\nACTUAL: dials=%d retry=%d", p.dials, g.RetryCount)
	}
}

// ---- F5684: DSN Diagnostic-Code / Remote-MTA ----

func r86Bounce(t *testing.T, reply string) string {
	t.Helper()
	p := &r86Peer{rcptReply: reply}
	m := r86Manager(t, p)
	e := r86Send(t, m, "a@sender.test")
	if e.Status != "bounced" {
		t.Fatalf("INVALID: status=%q err=%q", e.Status, e.LastError)
	}
	var msg string
	_ = m.db.ForEach(db.BucketQueue, func(_ string, v []byte) error {
		var q db.QueueEntry
		if json.Unmarshal(v, &q) == nil && q.From == "" {
			b, _ := os.ReadFile(q.MessagePath)
			msg = string(b)
		}
		return nil
	})
	if msg == "" {
		t.Fatal("INVALID: no DSN enqueued")
	}
	return msg
}

func TestRegressionF5684Control(t *testing.T) {
	if msg := r86Bounce(t, "550 5.1.1 user unknown"); !strings.Contains(msg, "Status: 5.1.1\r\n") {
		t.Fatalf("INVALID control: %q", msg)
	}
}

func TestRegressionF5684DSN(t *testing.T) {
	msg := r86Bounce(t, "550 5.1.1 user unknown")
	wantDiag := "Diagnostic-Code: smtp; 550 5.1.1 user unknown\r\n"
	wantMTA := "Remote-MTA: dns; mx.remote.test\r\n"
	if !strings.Contains(msg, wantDiag) || !strings.Contains(msg, wantMTA) {
		var got []string
		for _, l := range strings.Split(msg, "\r\n") {
			if strings.HasPrefix(l, "Diagnostic-Code") || strings.HasPrefix(l, "Remote-MTA") {
				got = append(got, l)
			}
		}
		t.Fatalf("DEFECT F5684\nEXPECTED: %q + %q\nACTUAL: %q", strings.TrimSpace(wantDiag), strings.TrimSpace(wantMTA), got)
	}
}

// ---- edge cases ----

func TestRegressionF5680_ConfiguredAndInvalidName(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	m.SetHelloName("mail.sender.test")
	if e := r86Send(t, m, "a@sender.test"); e.Status != "delivered" || p.ehlo[0] != "mail.sender.test" {
		t.Fatalf("status=%q ehlo=%v", e.Status, p.ehlo)
	}
	p2 := &r86Peer{}
	m2 := r86Manager(t, p2)
	m2.SetHelloName("bad\r\nname") // rejected by net/smtp: must not break delivery
	if e := r86Send(t, m2, "a@sender.test"); e.Status != "delivered" || strings.Contains(p2.ehlo[0], "bad") {
		t.Fatalf("status=%q ehlo=%v", e.Status, p2.ehlo)
	}
}

func TestRegressionF5682_TLSUsedAndRequireTLSVerifies(t *testing.T) {
	p := &r86Peer{startTLS: "tls", cert: r86SelfSigned(t)}
	m := r86Manager(t, p)
	if e := r86Send(t, m, "a@sender.test"); e.Status != "delivered" || atomic.LoadInt32(&p.tlsOK) != 1 {
		t.Fatalf("status=%q tlsOK=%d", e.Status, p.tlsOK)
	}
	p2 := &r86Peer{startTLS: "tls", cert: r86SelfSigned(t)}
	m2 := r86Manager(t, p2)
	m2.SetRequireTLS(true)
	if e := r86Send(t, m2, "a@sender.test"); e.Status != "pending" || atomic.LoadInt32(&p2.accepted) != 0 {
		t.Fatalf("requireTLS accepted an unverifiable certificate: status=%q accepted=%d", e.Status, p2.accepted)
	}
}

func TestRegressionF5683_RetryEntryStillDelivers(t *testing.T) {
	p := &r86Peer{}
	m := r86Manager(t, p)
	e := r86PendingEntry(t, m, time.Now().Add(time.Hour))
	if err := m.RetryEntry(e.ID); err != nil {
		t.Fatal(err)
	}
	cur, _ := m.db.GetQueueEntry(e.ID)
	m.deliver(context.Background(), cur)
	if g, _ := m.db.GetQueueEntry(e.ID); g.Status != "delivered" {
		t.Fatalf("status=%q", g.Status)
	}
}

func TestRegressionF5684_MultilineReplyStaysOneField(t *testing.T) {
	msg := r86Bounce(t, "550-5.2.2 mailbox full\n550 5.2.2 over quota")
	lines := strings.Split(msg, "\r\n")
	n := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "Diagnostic-Code:") {
			n++
			if l != "Diagnostic-Code: smtp; 550 5.2.2 mailbox full 5.2.2 over quota" || lines[i+1] != "" {
				t.Fatalf("diagnostic %q next %q", l, lines[i+1])
			}
		}
	}
	if n != 1 {
		t.Fatalf("Diagnostic-Code fields: %d", n)
	}
}

func TestRegressionF5684_DiagnosticHelpers(t *testing.T) {
	cases := map[string]string{
		`550 "5.1.1 user unknown"`:      "550 5.1.1 user unknown",
		"smtp; 550 5.0.0 local failure": "550 5.0.0 local failure",
		"dial tcp: refused\r\nX: y":     "dial tcp: refused X: y",
		"":                              "",
	}
	for in, want := range cases {
		if got := diagnosticText(in); got != want {
			t.Errorf("diagnosticText(%q)=%q want %q", in, got, want)
		}
	}
	if text, mx := splitMXTag(tagMX("550 x", "mx.a.test")); text != "550 x" || mx != "mx.a.test" {
		t.Errorf("split: %q %q", text, mx)
	}
	if text, mx := splitMXTag("plain error"); text != "plain error" || mx != "" {
		t.Errorf("split untagged: %q %q", text, mx)
	}
	if bounceStatus(tagMX(`550 "5.1.1 user unknown"`, "mx.a.test")) != "5.1.1" {
		t.Error("bounceStatus must survive the MX tag")
	}
	out, _ := GenerateFailureDSN(&DSN{ReportedDomain: "h", OriginalFrom: "a@b", OriginalTo: "c@d", Recipient: DSNRecipient{Original: "c@d"}, RemoteMTA: "unknown"}, []byte("S: x\r\n\r\nb"), DSNRetHeaders, "smtp; 550 5.0.0 x")
	if strings.Contains(string(out), "Remote-MTA") || strings.Contains(string(out), "smtp; smtp;") {
		t.Errorf("DSN: %s", out)
	}
}
