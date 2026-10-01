package webhook

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// r22Recorder counts requests (and keeps the last body) seen by a test server.
type r22Recorder struct {
	mu   sync.Mutex
	body string
	n    int
}

func (h *r22Recorder) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	h.body = string(b)
}

func (h *r22Recorder) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

func (h *r22Recorder) lastBody() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.body
}

// r22PublicAddr is a documentation IP literal (TEST-NET-3, RFC 5737). Nothing
// binds one, and isValidWebhookURL accepts it: ParseIP succeeds and the address
// is neither loopback, private, nor link-local, so the guard allows it as the
// first hop.
const r22PublicAddr = "203.0.113.10:8080"

// r22BindPublic points r22PublicAddr at srv at the network boundary. This is
// the only faked part of these tests, because a documentation IP cannot be
// bound by a real listener. Every other address -- notably 127.0.0.1, the one
// the guard must protect -- is dialed for real.
func r22BindPublic(client *http.Client, srv *httptest.Server) {
	wire := strings.TrimPrefix(srv.URL, "http://")
	client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == r22PublicAddr {
				addr = wire
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

// TestWebhook_RedirectHopIsNotSSRFValidated pins the fix for a real SSRF
// bypass: send() calls isValidWebhookURL exactly once, for the operator
// configured URL. Without a CheckRedirect the client followed a 307 -- keeping
// the POST body -- straight to 127.0.0.1, a host the guard rejects and never
// saw, delivering the signed event payload inside the trust boundary the guard
// exists to enforce.
func TestWebhook_RedirectHopIsNotSSRFValidated(t *testing.T) {
	internal := &r22Recorder{}
	srvInternal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internal.record(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvInternal.Close()

	hop1 := &r22Recorder{}
	srvHop1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hop1.record(r)
		w.Header().Set("Location", srvInternal.URL)
		// 307 keeps the method and body, unlike 302/303.
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer srvHop1.Close()

	m := NewManager(nil, "test-secret")
	r22BindPublic(m.client, srvHop1)

	m.send(&Webhook{ID: "h1", URL: "http://" + r22PublicAddr + "/hook", Active: true},
		Event{Type: EventMailReceived, Timestamp: time.Now(),
			Data: map[string]string{"probe": "sensitive-mail-metadata"}})

	if hop1.count() == 0 {
		t.Fatalf("the configured endpoint was never reached, so the redirect was not exercised")
	}
	if internal.count() > 0 {
		t.Errorf("webhook payload was delivered to %s after a redirect (body %q); "+
			"every hop must be re-validated by isValidWebhookURL", srvInternal.URL, internal.lastBody())
	}
}

// TestWebhook_DirectDeliveryStillWorks is the control: a webhook that does not
// redirect is delivered exactly once. It passes with and without the fix, so a
// red result elsewhere cannot be blamed on the harness or on a regression in
// ordinary delivery.
func TestWebhook_DirectDeliveryStillWorks(t *testing.T) {
	direct := &r22Recorder{}
	srvDirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct.record(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvDirect.Close()

	m := NewManager(nil, "test-secret")
	r22BindPublic(m.client, srvDirect)

	m.send(&Webhook{ID: "h2", URL: "http://" + r22PublicAddr + "/hook", Active: true},
		Event{Type: EventLoginSuccess, Timestamp: time.Now(), Data: "ok"})

	if direct.count() != 1 {
		t.Fatalf("expected exactly 1 delivery to the configured endpoint, got %d", direct.count())
	}
}
