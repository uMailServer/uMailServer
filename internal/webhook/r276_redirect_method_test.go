package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// F7500: a 301/302/303 answer to the signed POST makes net/http re-issue it
// as a body-less GET; its 2xx used to be counted as a delivery although the
// payload never reached a receiver.

type r276Seen struct {
	mu     sync.Mutex
	method string
	body   string
	n      int
}

func (s *r276Seen) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.method, s.body = r.Method, string(b)
	s.n++
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func r276Send(t *testing.T, url string) (delivered, failed uint64) {
	t.Helper()
	m := NewManager(nil, "s")
	m.SetAllowPrivateIP(true)
	m.backoffBase, m.backoffMax = time.Millisecond, time.Millisecond
	m.send(&Webhook{ID: "wh_1", URL: url, Active: true}, Event{Type: "mail.received", Timestamp: time.Now(), Data: "x"})
	d, f, _ := m.DeliveryStats()
	return d, f
}

func TestR276_RedirectDowngradingPostIsNotDelivery(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		final := &r276Seen{}
		fs := httptest.NewServer(http.HandlerFunc(final.handler))
		front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, fs.URL, code)
		}))
		d, f := r276Send(t, front.URL)
		front.Close()
		fs.Close()
		if d != 0 || f != 1 {
			t.Fatalf("status %d: delivered=%d failed=%d, want 0/1 (payload was dropped by redirect)", code, d, f)
		}
		if final.n != 0 {
			t.Fatalf("status %d: final host was contacted %d times (%s)", code, final.n, final.method)
		}
	}
}

func TestR276_RedirectPreservingPostStillDelivers(t *testing.T) {
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		final := &r276Seen{}
		fs := httptest.NewServer(http.HandlerFunc(final.handler))
		front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, fs.URL, code)
		}))
		d, _ := r276Send(t, front.URL)
		front.Close()
		fs.Close()
		if d != 1 || final.method != "POST" || final.body == "" {
			t.Fatalf("status %d: delivered=%d method=%q body=%q", code, d, final.method, final.body)
		}
	}
}

func TestR276_DirectDeliveryUnaffected(t *testing.T) {
	s := &r276Seen{}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	if d, _ := r276Send(t, srv.URL); d != 1 || s.method != "POST" || s.body == "" {
		t.Fatalf("delivered=%d method=%q body=%q", d, s.method, s.body)
	}
}
