package websocket

// Regression test for the SSE writer race: sendEvent used to write the
// event/data lines and flush without any lock, while the handler goroutine
// (heartbeats, notification events) and the exported Broadcast/SendToUser/
// SendToAdmins entry points could all write the same client's
// http.ResponseWriter concurrently. net/http documents ResponseWriter as not
// safe for concurrent use, so this was a data race that also tore SSE frames
// (consecutive `event:` lines, invalid JSON). The per-client writeMu
// serializes every writer.
//
// The test uses a real httptest.NewServer (a genuinely concurrent
// ResponseWriter — httptest.NewRecorder is single-goroutine by design) and is
// meant to run under -race (Makefile test-race): any unsynchronized overlap
// trips the detector. Frame integrity is asserted functionally as well.
//
// Delivery-count note (CI flake, fixed): the original version fired the
// 300-send storm without waiting for it, slept a blind 400ms, closed the
// stream and asserted >= 50 frames. The IMAP notification hub deliberately
// DROPS notifications when a subscriber's 100-slot buffer is full
// (internal/imap/notifications.go — select/default, "buffer to prevent
// blocking"): under CI runner load the handler falls behind, drops occur and
// the received count fell below the threshold. The hub path is best-effort by
// design, so the count assertion now rests on the guaranteed path only: the
// 150 SendToUser ticks bypass the hub (direct writes, no drop path), the
// storm is awaited, and the drain polls with a deadline before closing.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/umailserver/umailserver/internal/imap"
)

func TestSSESendEventIsSafeForConcurrentWriters(t *testing.T) {
	srv := NewSSEServer(slog.Default())
	srv.SetAuthFunc(func(token string) (string, bool, error) {
		return "alice", false, nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Auth-Token", "tok")
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed via defer below; the body is read by the pump goroutine
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer resp.Body.Close()

	type sseFrame struct {
		events int
		datas  int
		data   string
	}

	frames := make(chan sseFrame, 4096)
	readerDone := make(chan struct{})
	var mu sync.Mutex
	var collected []sseFrame
	received := 0
	connectedOnce := false
	connected := make(chan struct{})

	go func() {
		defer close(readerDone)
		defer close(frames)
		cur := sseFrame{}
		r := bufio.NewReader(resp.Body)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if cur.events > 0 || cur.datas > 0 {
					frames <- cur
				}
				cur = sseFrame{}
			case strings.HasPrefix(line, "event: "):
				cur.events++
			case strings.HasPrefix(line, "data: "):
				cur.datas++
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// The single consumer: collects every frame (the only reader of the
	// channel), counts deliveries and signals the connected event once.
	go func() {
		for f := range frames {
			mu.Lock()
			collected = append(collected, f)
			received++
			if !connectedOnce && strings.Contains(f.data, "\"user\":\"alice\"") {
				connectedOnce = true
				close(connected)
			}
			mu.Unlock()
		}
	}()

	// Wait for the connected event (bounded).
	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		t.Fatal("no connected event within 10s")
	}

	// Two concurrent writers on the same client: the handler goroutine (via
	// notification-hub traffic — best-effort, may drop under backpressure)
	// and a foreign goroutine (SendToUser — direct writes, no drop path).
	var storm sync.WaitGroup
	storm.Add(1)
	go func() {
		defer storm.Done()
		for i := 0; i < 150; i++ {
			imap.GetNotificationHub().NotifyNewMessage("alice", "INBOX", uint32(i+1), uint32(i+1))
			time.Sleep(time.Millisecond)
		}
	}()
	for i := 0; i < 150; i++ {
		if err := srv.SendToUser("alice", "tick", map[string]int{"i": i}); err != nil {
			t.Fatalf("SendToUser: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	// The notification goroutine's sends must complete before any counting:
	// the original test never waited on it and closed the stream mid-storm.
	storm.Wait()

	// Bounded drain: the 150 direct-write ticks cannot be dropped, so they
	// must all be delivered once the sends are done and the handler catches
	// up. Wait for them (or fail on the deadline) instead of guessing with a
	// blind sleep that breaks under CI runner load.
	deadline := time.After(10 * time.Second)
	for {
		mu.Lock()
		n := received
		mu.Unlock()
		if n >= 150 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d frames delivered within the drain deadline — the 150 guaranteed SendToUser ticks did not arrive", n)
		case <-time.After(25 * time.Millisecond):
		}
	}

	// Close the stream and validate every frame received.
	resp.Body.Close()
	<-readerDone

	mu.Lock()
	defer mu.Unlock()
	if len(collected) < 150 {
		t.Fatalf("only %d frames collected — the 150 direct-write ticks alone should be present", len(collected))
	}
	for _, f := range collected {
		if f.events != 1 {
			t.Fatalf("torn SSE frame: %d event lines in one frame (want exactly 1): %+v", f.events, f)
		}
		if f.datas != 1 {
			t.Fatalf("torn SSE frame: %d data lines in one frame (want exactly 1): %+v", f.datas, f)
		}
		if !json.Valid([]byte(f.data)) {
			t.Fatalf("SSE frame data is not valid JSON (torn write): %q", f.data)
		}
	}
}
