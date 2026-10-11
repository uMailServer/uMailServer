package websocket

import (
	"bufio"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// F6506: Close must end open streams and reject new connections.
func TestF6506_CloseEndsStreamsAndRejectsNew(t *testing.T) {
	srv := NewSSEServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthFunc(func(string) (string, bool, error) { return "alice", false, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set("X-Auth-Token", "tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	if _, err := br.ReadString('\n'); err != nil { // connected event => registered
		t.Fatal(err)
	}

	srv.Close()
	srv.Close() // idempotent

	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, br); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream still open after Close")
	}
	if n := srv.GetConnectedCount(); n != 0 {
		t.Fatalf("clients remain after Close: %d", n)
	}

	req2, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req2.Header.Set("X-Auth-Token", "tok")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d after Close, want 503", resp2.StatusCode)
	}
}
