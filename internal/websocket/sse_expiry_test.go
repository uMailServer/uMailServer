package websocket

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// F6802: once authFunc starts failing (expiry/revocation), the stream closes
// on the next heartbeat instead of living forever.
func TestSSEClosesWhenTokenInvalidated(t *testing.T) {
	s := NewSSEServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	var revoked atomic.Bool
	s.SetAuthFunc(func(tok string) (string, bool, error) {
		if revoked.Load() {
			return "", false, errors.New("expired")
		}
		return "u@x.com", false, nil
	})
	s.SetHeartbeatInterval(20 * time.Millisecond)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	req.Header.Set("X-Auth-Token", "t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	time.Sleep(60 * time.Millisecond) // a few valid heartbeats
	revoked.Store(true)
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stream still open after token invalidated")
	}
	if n := s.GetConnectedCount(); n != 0 {
		t.Fatalf("client not removed: %d", n)
	}
}
