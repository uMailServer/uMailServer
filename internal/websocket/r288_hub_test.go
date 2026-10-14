package websocket

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// r288 fake writer: records frames, signals the first Flush (the handler has
// registered and sent "connected"), and can block Write behind a gate.
type r288Writer struct {
	hdr   http.Header
	mu    sync.Mutex
	buf   strings.Builder
	first chan struct{}
	once  sync.Once
	gate  chan struct{} // when non-nil, Write blocks until closed
	enter chan struct{} // signalled (once) when a gated Write is entered
	eonce sync.Once

	marked chan struct{} // closed when a frame containing "MARK" is written
	monce  sync.Once
}

func newR288Writer() *r288Writer {
	return &r288Writer{hdr: http.Header{}, first: make(chan struct{}), marked: make(chan struct{}), enter: make(chan struct{})}
}
func (w *r288Writer) Header() http.Header { return w.hdr }
func (w *r288Writer) WriteHeader(int)     {}
func (w *r288Writer) Write(p []byte) (int, error) {
	if w.gate != nil && w.connected() {
		w.eonce.Do(func() { close(w.enter) })
		<-w.gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.Contains(string(p), "MARK") {
		w.monce.Do(func() { close(w.marked) })
	}
	return w.buf.Write(p)
}
func (w *r288Writer) connected() bool {
	select {
	case <-w.first:
		return true
	default:
		return false
	}
}
func (w *r288Writer) Flush() { w.once.Do(func() { close(w.first) }) }
func (w *r288Writer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

type r288Conn struct {
	w      *r288Writer
	cancel context.CancelFunc
	done   chan struct{}
}

func r288Connect(t *testing.T, s *SSEServer, token string) *r288Conn {
	t.Helper()
	return r288ConnectW(t, s, token, newR288Writer())
}

func r288ConnectW(t *testing.T, s *SSEServer, token string, w *r288Writer) *r288Conn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	req.Header.Set("X-Auth-Token", token)
	c := &r288Conn{w: w, cancel: cancel, done: make(chan struct{})}
	h := s.Handler()
	go func() { defer close(c.done); h(c.w, req) }()
	select {
	case <-c.w.first:
	case <-c.done: // rejected (e.g. 503 after Close)
	}
	return c
}

func r288Server() *SSEServer {
	s := NewSSEServer(slog.New(slog.DiscardHandler))
	s.SetAuthFunc(func(tok string) (string, bool, error) {
		return strings.TrimPrefix(tok, "admin:"), strings.HasPrefix(tok, "admin:"), nil
	})
	return s
}

// HOLDS: Register(handler)/Unregister(disconnect)/Broadcast/Close from many
// goroutines never panics or races (run under -race).
func TestR288SSEConcurrentLifecycleNoPanic(t *testing.T) {
	s := r288Server()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := r288Connect(t, s, fmt.Sprintf("u%d", i%7))
			s.Broadcast("x", map[string]int{"i": i})
			_ = s.SendToUser(fmt.Sprintf("u%d", i%7), "y", 1)
			s.SendToAdmins("z", 1)
			if i%3 == 0 {
				s.Close()
			}
			c.cancel()
			<-c.done
		}(i)
	}
	wg.Wait()
	s.Close()
	s.Close()
	if n := s.GetConnectedCount(); n != 0 {
		t.Fatalf("clients leaked: %d", n)
	}
}

// HOLDS: per-user isolation incl. case/whitespace variants (keys are exact;
// an event for "alice" never reaches "Alice" or "alice ").
func TestR288SSEUserIsolation(t *testing.T) {
	s := r288Server()
	a := r288Connect(t, s, "alice")
	var others []*r288Conn
	for _, u := range []string{"Alice", "alice ", " alice", "bob"} {
		others = append(others, r288Connect(t, s, u))
	}
	if err := s.SendToUser("alice", "secret", "for-alice"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.w.String(), "for-alice") {
		t.Fatal("alice did not get her event")
	}
	for i, o := range others {
		if strings.Contains(o.w.String(), "for-alice") {
			t.Fatalf("variant %d received alice's event", i)
		}
	}
	s.Close()
	a.cancel()
	for _, o := range others {
		o.cancel()
		<-o.done
	}
	<-a.done
}

// HOLDS: at the per-user cap the OLDEST client is evicted (once), newest kept.
func TestR288SSEEvictionKeepsNewest(t *testing.T) {
	s := r288Server()
	var conns []*r288Conn
	for i := 0; i < 6; i++ {
		conns = append(conns, r288Connect(t, s, "carol"))
	}
	<-conns[0].done // evicted handler exits on its own
	if n := s.GetConnectedCount(); n != 5 {
		t.Fatalf("count=%d want 5", n)
	}
	if err := s.SendToUser("carol", "m", "marker"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conns[0].w.String(), "marker") {
		t.Fatal("evicted (oldest) client still received events")
	}
	for i := 1; i < 6; i++ {
		if !strings.Contains(conns[i].w.String(), "marker") {
			t.Fatalf("newest client %d missing event", i)
		}
	}
	s.Close()
	s.Close() // stopClient is idempotent: no double close
	for i := 1; i < 6; i++ {
		<-conns[i].done
	}
}

// HOLDS: Close then connect/Broadcast is a safe no-op (503, no panic).
func TestR288SSECloseThenUseIsNoop(t *testing.T) {
	s := r288Server()
	s.Close()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("X-Auth-Token", "dave")
	s.Handler()(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d want 503", rec.Code)
	}
	s.Broadcast("e", 1)
	if err := s.SendToUser("dave", "e", 1); err == nil {
		t.Fatal("expected not-connected error")
	}
	s.SendToAdmins("e", 1)
}

// F7620 regression: a stalled consumer (Write parked behind a gate) must not
// delay delivery to the other clients reached by Broadcast / SendToUser /
// SendToAdmins. The 5s bound only limits the failure path.
func TestR288SSEStalledClientDoesNotBlockOthers(t *testing.T) {
	for _, mode := range []string{"broadcast", "user", "admins"} {
		t.Run(mode, func(t *testing.T) {
			s := r288Server()
			tok := "u"
			if mode == "admins" {
				tok = "admin:u"
			}
			slow := newR288Writer()
			slow.gate = make(chan struct{})
			slowConn := r288ConnectW(t, s, tok, slow)
			good := r288Connect(t, s, tok)
			done := make(chan struct{})
			go func() {
				defer close(done)
				switch mode {
				case "broadcast":
					s.Broadcast("e", "MARK")
				case "user":
					_ = s.SendToUser("u", "e", "MARK")
				default:
					s.SendToAdmins("e", "MARK")
				}
			}()
			<-slow.enter
			select {
			case <-good.w.marked:
			case <-time.After(5 * time.Second):
				t.Error("good client starved behind a stalled client")
			}
			close(slow.gate)
			<-done
			s.Close()
			<-slowConn.done
			<-good.done
		})
	}
}
