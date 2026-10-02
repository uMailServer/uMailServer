package pop3

// Regression test for the RETR retrieval defect: the session passed a
// 0-BASED index (`index-1`) to mailstore.GetMessageData, while the real
// BboltStore resolves that index through GetMessage with 1-BASED semantics
// (`index < 1` rejected; `messages[index-1]`). Through the production
// BboltStore this breaks RETR twice over:
//   - RETR 1 -> GetMessageData(user, 0) -> GetMessage(0) -> "message index
//     out of range" -> "-ERR Failed to read message": the FIRST message is
//     never retrievable.
//   - RETR 2 -> GetMessage(1) -> messages[0] -> message 1's metadata: the
//     wrong message is resolved (RFC 1939 §3 forbids renumbering; POP3
//     message numbers are 1-based and stable for the session).
// Additionally, BboltStore.GetMessageData addressed raw bodies by the
// decimal UID string, which isValidMessageID (>= 4 chars, hash-layout
// paths) can never resolve — raw bodies are content-hash-addressed and the
// blob ID lives in the message metadata (MessageID). The fix: the session
// passes the 1-based POP3 message number, and the store resolves data via
// the metadata's blob ID.
//
// Hermetic: real temp storage + real MessageStore + the real BboltStore as
// the session's mailstore; no external network.

import (
	"bufio"
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func startRealStoreSession(t *testing.T) (*Session, net.Conn, []byte, []byte) {
	t.Helper()

	storageDB, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storageDB.Close() })

	msgStore, err := storage.NewMessageStore(t.TempDir() + "/blobs")
	if err != nil {
		t.Fatalf("open message store: %v", err)
	}
	t.Cleanup(func() { _ = msgStore.Close() })

	const user = "alice@example.com"
	if err := storageDB.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	body1 := []byte("Subject: one\r\n\r\nbody one\r\n")
	body2 := []byte("Subject: two\r\n\r\nbody two\r\n")
	blob1, err := msgStore.StoreMessage(user, body1)
	if err != nil {
		t.Fatalf("store blob 1: %v", err)
	}
	blob2, err := msgStore.StoreMessage(user, body2)
	if err != nil {
		t.Fatalf("store blob 2: %v", err)
	}
	if err := storageDB.StoreMessageMetadata(user, "INBOX", 1, &storage.MessageMetadata{
		UID: 1, MessageID: blob1, Subject: "one", Flags: []string{},
	}); err != nil {
		t.Fatalf("store metadata 1: %v", err)
	}
	if err := storageDB.StoreMessageMetadata(user, "INBOX", 2, &storage.MessageMetadata{
		UID: 2, MessageID: blob2, Subject: "two", Flags: []string{},
	}); err != nil {
		t.Fatalf("store metadata 2: %v", err)
	}

	bstore := NewBboltStore(storageDB, msgStore)
	server := NewServer("127.0.0.1:0", bstore, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverConnCh := make(chan net.Conn, 1)
	go func() {
		c, _ := listener.Accept()
		serverConnCh <- c
	}()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	serverConn := <-serverConnCh
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	session := NewSession(serverConn, server)
	// Mirror the AUTH transition (server.go: load the maildrop snapshot via
	// ListMessages and enter TRANSACTION state). The session has no mutex;
	// in-package direct field access is the established pattern.
	session.user = user
	session.state = StateTransaction
	session.messages, err = bstore.ListMessages(user)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(session.messages) != 2 {
		t.Fatalf("FAIL: harness maildrop has %d messages, want 2", len(session.messages))
	}

	return session, clientConn, body1, body2
}

// retr reads the RETR reply: the status line, then raw octets up to the
// CRLF.CRLF terminator (dot-stuffing is identity for the dot-free fixtures).
func retr(t *testing.T, session *Session, conn net.Conn, n int) (int, []byte) {
	t.Helper()
	if err := session.handleCommand("RETR " + strconv.Itoa(n)); err != nil {
		t.Fatalf("FAIL: RETR %d handler: %v", n, err)
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("FAIL: read RETR %d status: %v", n, err)
	}
	status := strings.TrimRight(statusLine, "\r\n")
	if strings.HasPrefix(status, "-ERR") {
		return -1, []byte(status)
	}
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("FAIL: RETR %d unexpected status %q", n, status)
	}
	raw, err := readUntilTerminator(reader)
	if err != nil {
		t.Fatalf("FAIL: read RETR %d data: %v", n, err)
	}
	return 1, unstuff(raw)
}

func readUntilTerminator(reader *bufio.Reader) ([]byte, error) {
	var out bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == ".\r\n" || line == ".\n" {
			return out.Bytes(), nil
		}
		out.WriteString(line)
	}
}

// unstuff reverses RFC 1939 §3.1 dot-stuffing on the collected octets.
func unstuff(data []byte) []byte {
	if bytes.HasPrefix(data, []byte("..\r\n")) || !bytes.Contains(data, []byte("\r\n..")) {
		if bytes.HasPrefix(data, []byte(".")) {
			cp := append([]byte(nil), data[1:]...)
			return cp
		}
	}
	return bytes.ReplaceAll(data, []byte("\r\n.."), []byte("\r\n."))
}

func TestRETRRetrievesFirstMessage(t *testing.T) {
	session, conn, body1, _ := startRealStoreSession(t)

	code, data := retr(t, session, conn, 1)
	if code != 1 {
		t.Fatalf("FAIL: RETR 1 failed: %s — the first message is never retrievable (session passed a 0-based index to the 1-based store)", data)
	}
	if !bytes.Equal(data, body1) {
		t.Fatalf("FAIL: RETR 1 delivered %q, want the first message %q", string(data), string(body1))
	}
}

func TestRETRRetrievesSecondMessage(t *testing.T) {
	session, conn, _, body2 := startRealStoreSession(t)

	code, data := retr(t, session, conn, 2)
	if code != 1 {
		t.Fatalf("FAIL: RETR 2 failed: %s — wrong-message resolution (off-by-one against the 1-based store)", data)
	}
	if !bytes.Equal(data, body2) {
		t.Fatalf("FAIL: RETR 2 delivered %q, want the second message %q", string(data), string(body2))
	}
}
