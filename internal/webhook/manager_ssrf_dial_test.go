package webhook

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ssrfDNS is a minimal in-process DNS server. answers(n) gives the A record
// for the n-th A query (1-based); AAAA queries get an empty answer.
type ssrfDNS struct {
	mu      sync.Mutex
	aCount  int
	answers func(n int) net.IP
}

func (d *ssrfDNS) reply(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	i := 12
	for i < len(q) && q[i] != 0 {
		i += int(q[i]) + 1
	}
	if i+5 > len(q) {
		return nil
	}
	qend := i + 5
	qtype := binary.BigEndian.Uint16(q[i+1 : i+3])
	resp := append([]byte{q[0], q[1], 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0}, q[12:qend]...)
	if qtype == 1 {
		d.mu.Lock()
		d.aCount++
		ip := d.answers(d.aCount).To4()
		d.mu.Unlock()
		resp[7] = 1
		resp = append(resp, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
		resp = append(resp, ip...)
	}
	return resp
}

func ssrfInstallDNS(t *testing.T, d *ssrfDNS) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if r := d.reply(append([]byte(nil), buf[:n]...)); r != nil {
				_, _ = pc.WriteTo(r, addr)
			}
		}
	}()
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dl net.Dialer
		return dl.DialContext(ctx, "udp", pc.LocalAddr().String())
	}}
	t.Cleanup(func() { net.DefaultResolver = old; _ = pc.Close() })
}

// ssrfLocalServer returns the port of a loopback server and a channel that
// receives one value per request it handles.
func ssrfLocalServer(t *testing.T) (string, chan struct{}) {
	t.Helper()
	got := make(chan struct{}, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL[strings.LastIndex(srv.URL, ":")+1:], got
}

// TestWebhook_UnspecifiedAddressBlocked pins F5175: 0.0.0.0 and [::] reach
// the local host but passed the guard because IsUnspecified was not checked.
func TestWebhook_UnspecifiedAddressBlocked(t *testing.T) {
	port, got := ssrfLocalServer(t)
	m := NewManager(nil, "s")
	for _, host := range []string{"0.0.0.0", "[::]"} {
		u := "http://" + host + ":" + port + "/hook"
		if valid, _ := m.isValidWebhookURL(u); valid {
			t.Errorf("isValidWebhookURL(%s) = true, want false", u)
		}
		m.send(&Webhook{ID: "u", URL: u, Active: true}, Event{Type: EventMailReceived, Timestamp: time.Now()})
	}
	select {
	case <-got:
		t.Fatal("webhook delivered to the local host via the unspecified address")
	default:
	}
}

// TestWebhook_DNSRebindingBlockedAtDial pins F5176: the guard validated the
// first DNS answer, but the client dialed whatever a second lookup returned.
// The transport must re-check the address it actually connects to.
func TestWebhook_DNSRebindingBlockedAtDial(t *testing.T) {
	d := &ssrfDNS{answers: func(n int) net.IP {
		if n == 1 {
			return net.IPv4(203, 0, 113, 10)
		}
		return net.IPv4(127, 0, 0, 1)
	}}
	ssrfInstallDNS(t, d)
	port, got := ssrfLocalServer(t)
	m := NewManager(nil, "s")
	u := "http://rebind.webhook-test.test:" + port + "/hook"
	if valid, _ := m.isValidWebhookURL(u); !valid {
		t.Fatal("precondition: first (public) answer should pass the URL guard")
	}
	resp, err := m.client.Post(u, "application/json", strings.NewReader("{}"))
	if err == nil {
		_ = resp.Body.Close()
	}
	select {
	case <-got:
		t.Fatal("webhook client connected to loopback after DNS rebinding")
	default:
	}
	if err == nil {
		t.Fatal("expected the dial to the rebound loopback address to fail")
	}

	// Test mode still reaches local endpoints.
	m.SetAllowPrivateIP(true)
	resp, err = m.client.Post(u, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("allowPrivateIP mode: %v", err)
	}
	_ = resp.Body.Close()
	<-got
}

// TestWebhook_ConcurrentDeliveriesDoNotMutateHook pins F5177: sendInner
// wrote hook.ResolvedIP on the *Webhook shared by every delivery goroutine,
// a data race under -race when one hook receives several events.
func TestWebhook_ConcurrentDeliveriesDoNotMutateHook(t *testing.T) {
	const events = 4
	got := make(chan struct{}, events)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		got <- struct{}{}
	}))
	defer srv.Close()
	m := NewManager(nil, "s")
	m.SetAllowPrivateIP(true)
	m.mu.Lock()
	m.hooks = append(m.hooks, &Webhook{ID: "h", URL: srv.URL + "/hook", Events: []string{"*"}, Active: true})
	m.mu.Unlock()
	for i := 0; i < events; i++ {
		m.Trigger(EventMailReceived, i)
	}
	for i := 0; i < events; i++ {
		<-got
	}
	for i := 0; i < cap(m.sem); i++ { // all delivery goroutines have finished
		m.sem <- struct{}{}
	}
}
