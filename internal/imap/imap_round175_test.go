package imap

import (
	"bufio"
	"compress/flate"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// F6530: COMPRESS=DEFLATE (RFC 4978) is raw DEFLATE with a sync flush after
// every response. The old gzip framing never flushed and could not be read by
// any client.
func TestF6530_CompressDeflateRoundTrip(t *testing.T) {
	client, srv := net.Pipe()
	defer client.Close()
	server := NewServer(&Config{Addr: ":0"}, &mockMailstore{})
	session := NewSession(srv, server)
	session.state = StateAuthenticated
	session.user = "u"
	session.tlsActive = true
	go session.Handle()
	defer session.Close()

	cr := bufio.NewReader(client)
	client.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := client.Write([]byte("a1 COMPRESS DEFLATE\r\n")); err != nil {
		t.Fatal(err)
	}
	l, err := cr.ReadString('\n')
	if err != nil || !strings.HasPrefix(l, "a1 OK") {
		t.Fatalf("compress: %q %v", l, err)
	}
	fw, _ := flate.NewWriter(client, flate.DefaultCompression)
	fr := bufio.NewReader(flate.NewReader(cr))
	if _, err := fw.Write([]byte("a2 NOOP\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := fw.Flush(); err != nil {
		t.Fatal(err)
	}
	l, err = fr.ReadString('\n')
	if err != nil || !strings.HasPrefix(l, "a2 OK") {
		t.Fatalf("noop over deflate: %q %v", l, err)
	}
}

// F6530: COMPRESS is an authenticated-state command; before login it would
// also let STARTTLS silently drop the compression layer.
func TestF6530_CompressRequiresAuth(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()
	go session.handleCommand("a1 COMPRESS DEFLATE")
	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Fatalf("got %q", resp)
	}
}

type panicAuthStore struct{ mockMailstore }

func (panicAuthStore) Authenticate(string, string) (bool, error) { panic("boom") }

// F6531: a panic inside a command handler left the session registered in
// Server.sessions (counted against maxConnections forever).
func TestF6531_PanicDeregistersSession(t *testing.T) {
	server := NewServer(&Config{Addr: ":0"}, &panicAuthStore{})
	server.SetAllowPlainAuth(true)
	client, srv := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { server.handleConnection(srv); close(done) }()
	cr := bufio.NewReader(client)
	client.SetDeadline(time.Now().Add(3 * time.Second))
	cr.ReadString('\n')
	client.Write([]byte("a1 LOGIN u p\r\n"))
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit")
	}
	server.sessionsMu.RLock()
	n := len(server.sessions)
	server.sessionsMu.RUnlock()
	if n != 0 {
		t.Fatalf("%d sessions leaked after panic", n)
	}
}

// F6532: RFC 3501 tag = 1*<ASTRING-CHAR except "+">. A "*" tag would make the
// tagged completion look like an untagged response.
func TestF6532_InvalidTagRejected(t *testing.T) {
	for _, tag := range []string{"*", "+x", "a(b", `a"b`, "a{b", "a%b"} {
		client, session := setupSessionWithPipe(t, StateAuthenticated, "u", nil)
		go session.handleCommand(tag + " NOOP")
		resp := drainConn(client, 150*time.Millisecond)
		client.Close()
		if strings.Contains(resp, "OK") || !strings.Contains(resp, "BAD") {
			t.Errorf("tag %q: got %q", tag, resp)
		}
	}
}

// F6533: two sessions expunging the same mailbox concurrently each scanned the
// same \Deleted set, each "deleted" every row (bbolt Delete of a missing key
// succeeds) and each announced every EXPUNGE, so IDLE sessions got duplicate
// EXPUNGE numbers and renumbered wrongly. Each message must be announced
// exactly once.
func TestF6533_ConcurrentExpungeAnnouncesOnce(t *testing.T) {
	const n = 40
	for iter := 0; iter < 20; iter++ {
		ms, err := NewBboltMailstore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		user := "u175@example.com"
		_ = ms.CreateMailbox(user, "INBOX")
		appendMsgs(t, ms, user, n)
		if err := ms.StoreFlags(user, "INBOX", "1:*", []string{"\\Deleted"}, FlagAdd); err != nil {
			t.Fatal(err)
		}
		ch := GetNotificationHub().Subscribe(user)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; _ = ms.Expunge(user, "INBOX") }()
		}
		close(start)
		wg.Wait()
		got := 0
	drain:
		for {
			select {
			case nt := <-ch:
				if nt.Type == NotificationExpunge {
					got++
				}
			default:
				break drain
			}
		}
		GetNotificationHub().Unsubscribe(user, ch)
		ms.Close()
		if got != n {
			t.Fatalf("iter %d: %d EXPUNGE announcements for %d messages", iter, got, n)
		}
	}
}

// F6534: the EXPUNGE command must report exactly the messages its own call
// removed (previously a separate SEARCH preceded the delete, so a concurrent
// expunge made both sessions report the same, then-invalid, numbers).
func TestF6534_ExpungeReportDisjoint(t *testing.T) {
	const n = 30
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	user := "u6534@example.com"
	_ = ms.CreateMailbox(user, "INBOX")
	appendMsgs(t, ms, user, n)
	if err := ms.StoreFlags(user, "INBOX", "1:*", []string{"\\Deleted"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	total := 0
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seqs, uids, err := ms.ExpungeReport(user, "INBOX")
			if err != nil || len(seqs) != len(uids) {
				t.Errorf("report: %v", err)
			}
			mu.Lock()
			total += len(seqs)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != n {
		t.Fatalf("reported %d, want %d", total, n)
	}
}
