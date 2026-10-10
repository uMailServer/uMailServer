package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func createHook(t *testing.T, m *Manager, url string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"url":"`+url+`","events":["*"]}`))
	m.HTTPHandler(rec, req)
	return rec.Code
}

func TestF6290_RegistryPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, "s3cret")
	if err := m.SetDataDir(dir); err != nil {
		t.Fatal(err)
	}
	if c := createHook(t, m, "https://example.com/hook"); c != http.StatusCreated {
		t.Fatalf("create=%d", c)
	}
	fi, err := os.Stat(filepath.Join(dir, "webhooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	m2 := NewManager(nil, "s3cret")
	if err := m2.SetDataDir(dir); err != nil {
		t.Fatal(err)
	}
	if len(m2.hooks) != 1 || m2.hooks[0].URL != "https://example.com/hook" {
		t.Fatalf("not reloaded: %+v", m2.hooks)
	}
	rec := httptest.NewRecorder()
	m2.HTTPHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatal("secret leaked by list")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "webhooks.json")); strings.Contains(string(b), "s3cret") {
		t.Fatal("secret written to registry")
	}
}

func TestF6290_MissingAndCorruptRegistryTolerated(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, "")
	if err := m.SetDataDir(dir); err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "webhooks.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(nil, "")
	if err := m2.SetDataDir(dir); err == nil {
		t.Fatal("corrupt file should report an error")
	}
	if len(m2.hooks) != 0 {
		t.Fatal("expected empty registry")
	}
	if c := createHook(t, m2, "https://example.com/x"); c != http.StatusCreated {
		t.Fatalf("create after corrupt=%d", c)
	}
}

func TestF6290_RegistryBounded(t *testing.T) {
	m := NewManager(nil, "")
	for i := 0; i < MaxWebhooks; i++ {
		m.hooks = append(m.hooks, &Webhook{ID: "x"})
	}
	if c := createHook(t, m, "https://example.com/x"); c != http.StatusConflict {
		t.Fatalf("got %d", c)
	}
}

func TestF6291_SignatureHeaders(t *testing.T) {
	var got http.Header
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		var b bytes.Buffer
		_, _ = b.ReadFrom(r.Body)
		body = b.Bytes()
	}))
	defer srv.Close()
	m := NewManager(nil, "key")
	m.SetAllowPrivateIP(true)
	var att int
	var ferr error
	var ok bool
	m.sendInner(&Webhook{ID: "h", URL: srv.URL}, Event{Type: "t", Timestamp: time.Now()}, &att, &ferr, &ok)
	if !ok {
		t.Fatal(ferr)
	}
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write(body)
	if got.Get("X-Webhook-Signature") != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("legacy header changed")
	}
	ts := got.Get("X-Webhook-Signature-Timestamp")
	mac = hmac.New(sha256.New, []byte("key"))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	if got.Get("X-Webhook-Signature-256") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("bad v2 signature")
	}
}

func TestF6292_ExponentialBackoffCapped(t *testing.T) {
	m := NewManager(nil, "")
	m.backoffBase, m.backoffMax = 100*time.Millisecond, 400*time.Millisecond
	for retry, max := range map[int]time.Duration{1: 100, 2: 200, 3: 400, 4: 400, 20: 400} {
		maxd := max * time.Millisecond
		for i := 0; i < 50; i++ {
			d := m.backoffDelay(retry)
			if d < maxd/2 || d > maxd {
				t.Fatalf("retry %d: %v outside [%v,%v]", retry, d, maxd/2, maxd)
			}
		}
	}
}

func TestF6293_DropsCountedWhenPoolFull(t *testing.T) {
	m := NewManager(nil, "")
	m.hooks = []*Webhook{{ID: "a", URL: "https://example.com", Events: []string{"*"}, Active: true}}
	for i := 0; i < cap(m.sem); i++ {
		m.sem <- struct{}{}
	}
	m.Trigger("x", nil)
	m.Trigger("x", nil)
	if m.DroppedDeliveries() != 2 {
		t.Fatalf("dropped=%d", m.DroppedDeliveries())
	}
}

func TestF6294_StopIdempotentAndAbortsBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()
	m := NewManager(nil, "")
	m.SetAllowPrivateIP(true)
	m.backoffBase, m.backoffMax = time.Hour, time.Hour
	done := make(chan struct{})
	go func() {
		var a int
		var e error
		var ok bool
		m.sendInner(&Webhook{ID: "h", URL: srv.URL}, Event{Type: "t"}, &a, &e, &ok)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	m.Stop()
	m.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not abort backoff")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	m.hooks = []*Webhook{{ID: "a", URL: srv.URL, Events: []string{"*"}, Active: true}}
	m.Trigger("x", nil)
	if m.DroppedDeliveries() != 1 {
		t.Fatal("trigger after stop must count as dropped")
	}
}
