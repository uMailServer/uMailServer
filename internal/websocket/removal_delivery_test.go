package websocket

import (
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

type gatedRemovalPayload struct{ entered, release chan struct{} }

func (d gatedRemovalPayload) MarshalJSON() ([]byte, error) {
	close(d.entered)
	<-d.release
	return []byte(`{"fixture":"ordinary"}`), nil
}
func TestRemovedSSEClientRejectsStaleBroadcast(t *testing.T) {
	s := NewSSEServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	client := &SSEClient{user: "alice", writer: w, flusher: w, stop: make(chan struct{})}
	s.clients["alice"] = []*SSEClient{client}
	s.Broadcast("control", "active")
	before := w.Body.String()
	if before == "" {
		t.Fatal("CONTROL FAILED")
	}
	fmt.Println("CONTROL EXPECTED: active broadcast writes | ACTUAL: written")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { defer close(done); s.Broadcast("stale", gatedRemovalPayload{entered, release}) }()
	<-entered // Broadcast has captured its client snapshot, then pauses in marshaling.
	s.removeClient("alice", client)
	close(release)
	<-done
	after := w.Body.String()
	fmt.Printf("EXPECTED: removed client unchanged | ACTUAL: unchanged=%v\n", after == before)
	if after != before {
		fmt.Println("PROBLEM CONFIRMED")
		t.FailNow()
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
	s.removeClient("alice", client)
	s.sendEvent(client, "after-close", "direct")
	if w.Body.String() != before {
		t.Fatal("repeated removal or direct delivery wrote to retired client")
	}
	if err := s.SendToUser("alice", "absent", nil); err == nil {
		t.Fatal("removed user still connected")
	}
	replacementWriter := httptest.NewRecorder()
	replacement := &SSEClient{user: "alice", writer: replacementWriter, flusher: replacementWriter, stop: make(chan struct{})}
	s.clients["alice"] = []*SSEClient{replacement}
	s.removeClient("alice", client) // A stale owner's cleanup must preserve the replacement.
	if err := s.SendToUser("alice", "replacement", "active"); err != nil {
		t.Fatal(err)
	}
	if replacementWriter.Body.Len() == 0 || w.Body.String() != before || s.GetConnectedCount() != 1 {
		t.Fatal("replacement affected by stale owner")
	}
	fmt.Println("FIX VERIFIED")
}
