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

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	resp, err := http.DefaultClient.Do(req)
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

	// Wait for the connected event (bounded).
	connected := make(chan struct{})
	go func() {
		for f := range frames {
			if strings.Contains(f.data, "\"user\":\"alice\"") {
				close(connected)
				return
			}
		}
	}()
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("no connected event within 5s")
	}

	// Two concurrent writers on the same client: the handler goroutine (via
	// notification-hub traffic) and a foreign goroutine (SendToUser).
	go func() {
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

	// Drain, then close the stream and validate every frame received.
	time.Sleep(400 * time.Millisecond)
	resp.Body.Close()
	<-readerDone

	checked := 0
	for f := range frames {
		checked++
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
	if checked < 50 {
		t.Fatalf("only %d frames observed — expected dozens during the storm", checked)
	}
}
