package pop3

// Regression tests for the POP3 UPDATE deletion semantics defect:
// handleUpdateCommand resolved mailstore.DeleteMessage with the 0-based index
// into the login-time snapshot while BboltStore resolves that index 1-based
// against the CURRENT maildrop, and only flagged \Deleted without the flag
// ever taking effect in the POP3 view. Net effect: DELE 1 + QUIT silently did
// nothing, DELE 2 + QUIT flagged message 1, and "deleted" mail stayed fully
// visible in later sessions (RFC 1939 §4 requires removal at UPDATE).
// The fix tracks deletions by UID, resolves them against a fresh listing at
// UPDATE, and excludes \Deleted messages from the maildrop view.

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// updateStore models the fixed BboltStore: ListMessages excludes \Deleted,
// DeleteMessage flags \Deleted by resolving the 1-based index.
type updateStore struct {
	msgs    []*Message
	deleted map[uint32]bool
}

func (u *updateStore) Authenticate(string, string) (bool, error) { return true, nil }

func (u *updateStore) visible() []*Message {
	var out []*Message
	for _, m := range u.msgs {
		if !u.deleted[updateUID(m.UID)] {
			out = append(out, m)
		}
	}
	return out
}

func updateUID(s string) uint32 {
	var uid uint32
	for _, c := range s {
		if c >= '0' && c <= '9' {
			uid = uid*10 + uint32(c-'0')
		}
	}
	return uid
}

func (u *updateStore) ListMessages(string) ([]*Message, error) { return u.visible(), nil }

func (u *updateStore) GetMessage(_ string, i int) (*Message, error) {
	vis := u.visible()
	if i < 1 || i > len(vis) {
		return nil, fmt.Errorf("no such message")
	}
	return vis[i-1], nil
}

func (u *updateStore) GetMessageData(_ string, i int) ([]byte, error) {
	return []byte("Subject: x\r\n\r\nbody\r\n"), nil
}

func (u *updateStore) DeleteMessage(_ string, i int) error {
	m, err := u.GetMessage("", i)
	if err != nil {
		return err
	}
	u.deleted[updateUID(m.UID)] = true
	return nil
}

func (u *updateStore) GetMessageCount(string) (int, error) { return len(u.visible()), nil }

func (u *updateStore) GetMessageSize(_ string, i int) (int64, error) {
	m, err := u.GetMessage("", i)
	if err != nil {
		return 0, err
	}
	return m.Size, nil
}

// runPOP3Session drives one full session through Handle() and returns the
// server responses in order. Commands that produce multi-line responses
// (LIST/UIDL without an argument) are read until the terminating ".".
func runPOP3Session(t *testing.T, srv *Server, commands ...string) []string {
	t.Helper()
	c1, c2 := net.Pipe()
	defer c2.Close()

	session := NewSession(c1, srv)
	go func() {
		session.Handle()
		c1.Close()
	}()

	var responses []string
	rd := bufio.NewReader(c2)
	for _, cmd := range commands {
		if _, err := c2.Write([]byte(cmd + "\r\n")); err != nil {
			t.Fatalf("write %q: %v", cmd, err)
		}
		multiLine := (strings.HasPrefix(cmd, "LIST") || strings.HasPrefix(cmd, "UIDL")) &&
			!strings.Contains(cmd, " ")
		for {
			c2.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatalf("read after %q: %v", cmd, err)
			}
			responses = append(responses, strings.TrimRight(line, "\r\n"))
			if !multiLine || strings.TrimRight(line, "\r\n") == "." {
				break
			}
		}
	}
	c2.Close()
	return responses
}

func newUpdateServer(t *testing.T) (*Server, *updateStore) {
	t.Helper()
	store := &updateStore{
		msgs: []*Message{
			{UID: "11", Size: 20},
			{UID: "22", Size: 21},
			{UID: "33", Size: 22},
		},
		deleted: map[uint32]bool{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := &Server{logger: logger}
	srv.mailstore = store
	srv.SetAuthFunc(func(username, password string) (bool, error) {
		return username == "alice" && password == "pw", nil
	})
	return srv, store
}

func TestUpdateRemovesOnlyDeletedMessage(t *testing.T) {
	srv, store := newUpdateServer(t)

	// Session 1: DELE message 2 (UID 22), QUIT (UPDATE).
	responses := runPOP3Session(t, srv, "USER alice", "PASS pw", "DELE 2", "QUIT")
	for i, resp := range responses {
		if !strings.HasPrefix(resp, "+OK") {
			t.Fatalf("CONTROL FAILED (harness): response %d = %q, want +OK", i, resp)
		}
	}

	// The UPDATE must flag exactly UID 22 — the off-by-one used to skip UID 11
	// and flag the wrong message instead.
	if !store.deleted[22] {
		t.Fatalf("FAIL: DELE 2 + QUIT did not flag UID 22; flagged=%v", store.deleted)
	}
	if store.deleted[11] || store.deleted[33] {
		t.Fatalf("FAIL: UPDATE flagged the wrong messages: %v (only 22 was deleted)", store.deleted)
	}

	// Session 2: UIDL must show only UIDs 11 and 33.
	responses = runPOP3Session(t, srv, "USER alice", "PASS pw", "UIDL")
	joined := strings.Join(responses, "|")
	if strings.Contains(joined, " 22") {
		t.Fatalf("FAIL: deleted message still visible in a later session: %v", responses)
	}
	if !strings.Contains(joined, " 11") || !strings.Contains(joined, " 33") {
		t.Fatalf("FAIL: surviving messages missing from UIDL: %v", responses)
	}
}

func TestRSETUnmarksDeletions(t *testing.T) {
	srv, store := newUpdateServer(t)

	responses := runPOP3Session(t, srv, "USER alice", "PASS pw", "DELE 2", "RSET", "UIDL", "QUIT")
	if len(store.deleted) != 0 {
		t.Fatalf("FAIL: RSET did not unmark deletions; flagged=%v", store.deleted)
	}
	joined := strings.Join(responses, "|")
	if !strings.Contains(joined, " 11") || !strings.Contains(joined, " 22") || !strings.Contains(joined, " 33") {
		t.Fatalf("FAIL: RSET lost messages: %v", responses)
	}
}

func TestDELEdMessageHiddenInSession(t *testing.T) {
	srv, _ := newUpdateServer(t)

	responses := runPOP3Session(t, srv, "USER alice", "PASS pw", "DELE 2", "UIDL", "RETR 2")
	joined := strings.Join(responses, "|")
	if strings.Contains(joined, " 22") {
		t.Fatalf("FAIL: DELEted message still listed by UIDL in the same session: %v", responses)
	}
	last := responses[len(responses)-1]
	if !strings.HasPrefix(last, "-ERR") {
		t.Fatalf("FAIL: RETR 2 of a DELEted message did not return -ERR: %v", responses)
	}
}
