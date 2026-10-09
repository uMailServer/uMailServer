package queue

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// Regression tests for F5155-F5159 (MX routing, failure classification, DSN
// NOTIFY handling).

type routeResolver map[string][]string

func (r routeResolver) LookupMX(domain string) ([]string, error) {
	if v, ok := r[domain]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: domain, IsNotFound: true}
}

// routePeer is a fake SMTP server answering RCPT TO with rcptReply (lines
// separated by "\n").
func routePeer(conn net.Conn, rcptReply string, accepted *int32) {
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
			case strings.HasPrefix(cmd, "RCPT"):
				for _, l := range strings.Split(rcptReply, "\n") {
					say(l)
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

func routeManager(t *testing.T, res DNSResolver, dial func(string) (net.Conn, error)) *Manager {
	t.Helper()
	m, _, database := setupManager(t)
	t.Cleanup(func() { database.Close() })
	m.resolver = res
	m.mtastsValidator = nil
	m.daneValidator = nil
	m.dialSMTP = dial
	return m
}

func routeSend(t *testing.T, m *Manager, rcpt string) *db.QueueEntry {
	t.Helper()
	id, err := m.Enqueue("a@sender.test", []string{rcpt}, []byte("Subject: x\r\n\r\nbody\r\n"))
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

func routeFailureDSNs(m *Manager) int {
	n := 0
	_ = m.db.ForEach(db.BucketQueue, func(_ string, v []byte) error {
		var q db.QueueEntry
		if json.Unmarshal(v, &q) == nil && q.From == "" && len(q.To) == 1 && q.To[0] == "a@sender.test" {
			n++
		}
		return nil
	})
	return n
}

// routeFakeDNS points net.DefaultResolver at an in-process server answering
// every MX query with exchanges (stream framing over net.Pipe).
func routeFakeDNS(t *testing.T, exchanges ...string) {
	t.Helper()
	orig := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		c, s := net.Pipe()
		go routeServeDNS(s, exchanges)
		return c, nil
	}}
	t.Cleanup(func() { net.DefaultResolver = orig })
}

func routeServeDNS(c net.Conn, exchanges []string) {
	defer c.Close()
	for {
		var l [2]byte
		if _, err := io.ReadFull(c, l[:]); err != nil {
			return
		}
		q := make([]byte, binary.BigEndian.Uint16(l[:]))
		if _, err := io.ReadFull(c, q); err != nil {
			return
		}
		i := 12
		for q[i] != 0 {
			i += int(q[i]) + 1
		}
		qEnd := i + 5
		var ans []byte
		n := 0
		if binary.BigEndian.Uint16(q[i+1:]) == 15 { // MX
			for k, ex := range exchanges {
				rd := []byte{0, byte(10 * k)}
				for _, label := range strings.Split(strings.TrimSuffix(ex, "."), ".") {
					if label != "" {
						rd = append(append(rd, byte(len(label))), label...)
					}
				}
				rd = append(rd, 0)
				ans = append(ans, 0xC0, 12, 0, 15, 0, 1, 0, 0, 1, 0, byte(len(rd)>>8), byte(len(rd)))
				ans = append(ans, rd...)
				n++
			}
		}
		resp := append([]byte{}, q[:2]...)
		resp = append(resp, 0x81, 0x80, 0, 1, 0, byte(n), 0, 0, 0, 0)
		resp = append(resp, q[12:qEnd]...)
		resp = append(resp, ans...)
		if _, err := c.Write(append([]byte{byte(len(resp) >> 8), byte(len(resp))}, resp...)); err != nil {
			return
		}
	}
}

// F5155: one unreachable MX host must not stop delivery to other domains.
func TestRegressionF5155_DeadMXHostDoesNotBlockOtherDomains(t *testing.T) {
	var accepted, deadDials int32
	reply := "250 ok"
	m := routeManager(t, routeResolver{"dead.test": {"mx.dead.test"}, "good.test": {"mx.good.test"}}, func(addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, "mx.dead.test") {
			atomic.AddInt32(&deadDials, 1)
			return nil, errors.New("connection refused")
		}
		c, s := net.Pipe()
		routePeer(s, reply, &accepted)
		return c, nil
	})
	for i := 0; i < 5; i++ {
		routeSend(t, m, "x@dead.test")
	}
	if e := routeSend(t, m, "b@good.test"); e.Status != "delivered" {
		t.Fatalf("good.test status %q (%s), want delivered", e.Status, e.LastError)
	}
	if e := routeSend(t, m, "y@dead.test"); !strings.Contains(e.LastError, "circuit breaker") || atomic.LoadInt32(&deadDials) != 5 {
		t.Fatalf("dead host breaker did not open: dials=%d err=%q", deadDials, e.LastError)
	}
	reply = "550 5.1.1 user unknown"
	for i := 0; i < 6; i++ {
		routeSend(t, m, "nobody@good.test")
	}
	reply = "250 ok"
	if e := routeSend(t, m, "c@good.test"); e.Status != "delivered" {
		t.Fatalf("5xx rejections opened the healthy host's breaker: %q", e.LastError)
	}
}

// F5156: a 5yz reply bounces at once; a 4yz reply is retried.
func TestRegressionF5156_PermanentReplyBouncesImmediately(t *testing.T) {
	for _, c := range []struct{ reply, want string }{
		{"550 5.1.1 user unknown", "bounced"},
		{"550-5.1.1 no such user\n550 5.1.1 check the address", "bounced"},
		{"451 4.7.1 greylisted", "pending"},
	} {
		var accepted, dials int32
		m := routeManager(t, routeResolver{"remote.test": {"mx1.remote.test", "mx2.remote.test"}}, func(string) (net.Conn, error) {
			atomic.AddInt32(&dials, 1)
			c2, s := net.Pipe()
			routePeer(s, c.reply, &accepted)
			return c2, nil
		})
		e := routeSend(t, m, "b@remote.test")
		if e.Status != c.want {
			t.Fatalf("reply %q: status %q, want %q", c.reply, e.Status, c.want)
		}
		if c.want == "bounced" && (dials != 1 || routeFailureDSNs(m) != 1) {
			t.Fatalf("reply %q: dials=%d DSNs=%d, want 1/1", c.reply, dials, routeFailureDSNs(m))
		}
	}
}

// F5157: MX hosts are returned without the root dot so MTA-STS patterns and
// TLS names match; F5158: a null MX is kept as ".".
func TestRegressionF5157_RealResolverStripsRootDot(t *testing.T) {
	routeFakeDNS(t, "mx.example.test", "alt1.mx.example.test")
	hosts, err := (&realDNSResolver{}).LookupMX("example.test")
	if err != nil || len(hosts) != 2 || hosts[0] != "mx.example.test" || hosts[1] != "alt1.mx.example.test" {
		t.Fatalf("LookupMX = %v, %v", hosts, err)
	}
	routeFakeDNS(t, ".")
	hosts, err = (&realDNSResolver{}).LookupMX("nomail.test")
	if err != nil || len(hosts) != 1 || hosts[0] != "." {
		t.Fatalf("null MX LookupMX = %v, %v", hosts, err)
	}
}

// F5158: a null MX (RFC 7505) fails permanently without connecting.
func TestRegressionF5158_NullMXBouncesWithoutDialing(t *testing.T) {
	routeFakeDNS(t, ".")
	var dials int32
	m := routeManager(t, &realDNSResolver{}, func(string) (net.Conn, error) {
		atomic.AddInt32(&dials, 1)
		return nil, errors.New("unexpected dial")
	})
	e := routeSend(t, m, "b@nomail.test")
	if e.Status != "bounced" || dials != 0 || !strings.Contains(e.LastError, "5.1.10") {
		t.Fatalf("status=%q dials=%d err=%q", e.Status, dials, e.LastError)
	}
	if n := routeFailureDSNs(m); n != 1 {
		t.Fatalf("%d failure DSNs, want 1", n)
	}
}

// F5159: a failure DSN is sent only when NOTIFY is absent or lists FAILURE.
func TestRegressionF5159_FailureDSNHonoursNotify(t *testing.T) {
	for notify, want := range map[string]int{"": 1, "FAILURE": 1, "SUCCESS,FAILURE": 1, "SUCCESS": 0, "DELAY": 0, "NEVER": 0} {
		m := routeManager(t, routeResolver{"remote.test": {"mx.remote.test"}}, func(string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		})
		m.maxRetries = 1
		id, err := m.EnqueueWithNotify("a@sender.test", []string{"b@remote.test"}, []string{notify}, []byte("Subject: x\r\n\r\nbody\r\n"))
		if err != nil {
			t.Fatal(err)
		}
		e, _ := m.db.GetQueueEntry(id + "-0")
		m.deliver(context.Background(), e)
		if n := routeFailureDSNs(m); n != want {
			t.Fatalf("NOTIFY=%q: %d failure DSNs, want %d", notify, n, want)
		}
	}
}
