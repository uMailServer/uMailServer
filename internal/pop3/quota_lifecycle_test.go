package pop3

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// Regression tests for F5743-F5745: POP3 DELE+QUIT must really remove the
// message (freeing blob and quota), report the freed bytes to the quota hook,
// and a nil snapshot entry must not panic DELE.

func TestBboltStoreDeleteRemovesMessageAndBlob(t *testing.T) {
	d := t.TempDir()
	db, err := storage.OpenDatabase(d + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ms, _ := storage.NewMessageStore(d + "/m")
	st := NewBboltStore(db, ms)
	const user = "a@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	_ = db.CreateMailbox(user, "Archive")
	seed := func(mbox, body string) string {
		id, _ := ms.StoreMessage(user, []byte(body))
		uid, _ := db.GetNextUID(user, mbox)
		_ = db.StoreMessageMetadata(user, mbox, uid, &storage.MessageMetadata{MessageID: id, UID: uid, Size: int64(len(body)), Flags: []string{}})
		return id
	}
	solo := seed("INBOX", "Subject: solo\r\n\r\nx\r\n")
	shared := seed("INBOX", "Subject: shared\r\n\r\ny\r\n")
	seed("Archive", "Subject: shared\r\n\r\ny\r\n")

	if err := st.DeleteMessage(user, 1); err != nil {
		t.Fatal(err)
	}
	if ms.MessageExists(user, solo) {
		t.Error("unreferenced blob survived DELE")
	}
	if err := st.DeleteMessage(user, 1); err != nil {
		t.Fatal(err)
	}
	if !ms.MessageExists(user, shared) {
		t.Error("blob still referenced by Archive was removed")
	}
	if msgs, _ := st.ListMessages(user); len(msgs) != 0 {
		t.Errorf("INBOX still lists %d messages", len(msgs))
	}
}

type relStore struct {
	nilStore
	mu   sync.Mutex
	list []*Message
	del  []int
}

func (r *relStore) ListMessages(string) ([]*Message, error) { return r.list, nil }
func (r *relStore) GetMessage(_ string, i int) (*Message, error) {
	return r.list[i-1], nil
}
func (r *relStore) DeleteMessage(_ string, i int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i == 2 {
		return fmt.Errorf("boom")
	}
	r.del = append(r.del, i)
	return nil
}

func TestQuitReleasesQuotaForRemovedMessagesOnly(t *testing.T) {
	st := &relStore{list: []*Message{{Index: 1, UID: "1", Size: 100}, {Index: 2, UID: "2", Size: 40}, {Index: 3, UID: "3", Size: 7}}}
	srv := NewServer("127.0.0.1:0", st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthFunc(func(string, string) (bool, error) { return true, nil })
	var mu sync.Mutex
	var freed int64
	srv.SetQuotaReleaseFunc(func(_ string, n int64) { mu.Lock(); freed += n; mu.Unlock() })
	c1, c2 := net.Pipe()
	done := make(chan struct{})
	go func() { srv.handleConnection(c2); close(done) }()
	_ = c1.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c1)
	r.ReadString('\n')
	for _, cmd := range []string{"USER u", "PASS p", "DELE 1", "DELE 2", "DELE 3", "QUIT"} {
		fmt.Fprintf(c1, "%s\r\n", cmd)
		if l, _ := r.ReadString('\n'); !strings.HasPrefix(l, "+OK") && cmd != "QUIT" {
			t.Fatalf("%s: %q", cmd, l)
		}
	}
	c1.Close()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if freed != 107 { // 100 + 7; message 2 failed to delete
		t.Fatalf("freed %d, want 107", freed)
	}
}

func TestDeleNilSnapshotEntryIsErr(t *testing.T) {
	if l := r92Dele(t, 1); !strings.HasPrefix(l, "-ERR") {
		t.Fatalf("DELE of nil entry: %q", l)
	}
}

type nilStore struct{}

func (nilStore) Authenticate(string, string) (bool, error) { return true, nil }
func (nilStore) ListMessages(string) ([]*Message, error) {
	return []*Message{nil, {Index: 2, UID: "2", Size: 3}}, nil
}
func (nilStore) GetMessage(string, int) (*Message, error)   { return nil, nil }
func (nilStore) GetMessageData(string, int) ([]byte, error) { return []byte("abc"), nil }
func (nilStore) DeleteMessage(string, int) error            { return nil }
func (nilStore) GetMessageCount(string) (int, error)        { return 2, nil }
func (nilStore) GetMessageSize(string, int) (int64, error)  { return 3, nil }

func r92Dele(t *testing.T, n int) string {
	srv := NewServer("127.0.0.1:0", nilStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetAuthFunc(func(string, string) (bool, error) { return true, nil })
	c1, c2 := net.Pipe()
	go srv.handleConnection(c2)
	defer c1.Close()
	_ = c1.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c1)
	r.ReadString('\n')
	for _, c := range []string{"USER u", "PASS p"} {
		fmt.Fprintf(c1, "%s\r\n", c)
		r.ReadString('\n')
	}
	fmt.Fprintf(c1, "DELE %d\r\n", n)
	l, _ := r.ReadString('\n')
	return strings.TrimSpace(l)
}
