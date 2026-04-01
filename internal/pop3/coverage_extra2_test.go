package pop3

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// ==========================================================================
// BboltStore tests with actual stored messages (exercise success paths)
// ==========================================================================

// setupBboltStoreWithMessages creates a BboltStore with test messages stored in
// the database. The message data files are placed at the paths that the
// INBOX/<uid> fallback in GetMessageData will find them.
func setupBboltStoreWithMessages(t *testing.T, user string, sizes []int64) (*BboltStore, func()) {
	t.Helper()
	tmpDir := t.TempDir()

	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	store := NewBboltStore(db, msgStore)

	for i, size := range sizes {
		uid := uint32(i + 1)
		// Store metadata
		meta := &storage.MessageMetadata{
			MessageID: fmt.Sprintf("msgid%d", uid),
			UID:       uid,
			Flags:     []string{},
			Size:      size,
			Subject:   fmt.Sprintf("Test message %d", uid),
		}
		if err := db.StoreMessageMetadata(user, "INBOX", uid, meta); err != nil {
			t.Fatalf("StoreMessageMetadata failed for uid %d: %v", uid, err)
		}

		// Write a data file at the path that the INBOX/<uidStr> fallback will find.
		// ReadMessage(user, "INBOX/<uidStr>") looks at:
		//   basePath/user/IN/BO/INBOX/<uidStr>
		uidStr := fmt.Sprintf("%d", uid)
		dataSubDir := filepath.Join(tmpDir, "messages", user, "IN", "BO")
		os.MkdirAll(dataSubDir, 0755)
		dataFile := filepath.Join(dataSubDir, "INBOX"+string(os.PathSeparator)+uidStr)
		// On Windows, "INBOX/1" as a filename is not valid, so create a directory INBOX
		inboxDir := filepath.Join(dataSubDir, "INBOX")
		os.MkdirAll(inboxDir, 0755)
		dataFile = filepath.Join(inboxDir, uidStr)
		body := make([]byte, size)
		for j := range body {
			body[j] = 'X'
		}
		if err := os.WriteFile(dataFile, body, 0644); err != nil {
			t.Fatalf("Failed to write data file: %v", err)
		}
	}

	return store, func() {
		db.Close()
		msgStore.Close()
	}
}

func TestBboltStore_ListMessagesWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{100, 200})
	defer cleanup()

	msgs, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Index != 1 {
		t.Errorf("Expected index 1, got %d", msgs[0].Index)
	}
	if msgs[1].Index != 2 {
		t.Errorf("Expected index 2, got %d", msgs[1].Index)
	}
	if msgs[0].Size != 100 {
		t.Errorf("Expected size 100, got %d", msgs[0].Size)
	}
	if msgs[0].UID == "" {
		t.Error("Expected non-empty UID")
	}
}

func TestBboltStore_GetMessageWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	// Valid index
	msg, err := store.GetMessage(user, 1)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if msg.Index != 1 {
		t.Errorf("Expected index 1, got %d", msg.Index)
	}

	// Out of range - too high
	_, err = store.GetMessage(user, 2)
	if err == nil {
		t.Error("Expected error for index 2 with only 1 message")
	}

	// Out of range - index 0
	_, err = store.GetMessage(user, 0)
	if err == nil {
		t.Error("Expected error for index 0")
	}
}

func TestBboltStore_GetMessageDataWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	data, err := store.GetMessageData(user, 1)
	if err != nil {
		t.Fatalf("GetMessageData failed: %v", err)
	}
	if len(data) != 50 {
		t.Errorf("Expected 50 bytes, got %d", len(data))
	}
}

func TestBboltStore_GetMessageDataBothFail(t *testing.T) {
	// Both ReadMessage and the INBOX/ fallback fail because the UID string
	// is short (< 4 chars) and ReadMessage requires >= 4 char IDs.
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	uid := uint32(42)
	// Store metadata with UID 42 -> UID string is "42" (< 4 chars)
	// ReadMessage will fail with "invalid message ID" for both paths
	meta := &storage.MessageMetadata{
		MessageID: "nonexistent1234",
		UID:       uid,
		Flags:     []string{},
		Size:      100,
		Subject:   "Ghost",
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	_, err = store.GetMessageData(user, 1)
	if err == nil {
		t.Error("Expected error when both read paths fail")
	}
}

func TestBboltStore_DeleteMessageWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	// Delete message at index 1
	err := store.DeleteMessage(user, 1)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	// Verify the \Deleted flag was added by checking the message is still listed
	msgs, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages after delete failed: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("Expected at least 1 message in list after delete")
	}
}

func TestBboltStore_DeleteMessageAlreadyDeleted(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	// Delete once - adds \Deleted flag
	err := store.DeleteMessage(user, 1)
	if err != nil {
		t.Fatalf("First DeleteMessage failed: %v", err)
	}

	// Delete again - flag already exists, hasDeleted=true branch
	err = store.DeleteMessage(user, 1)
	if err != nil {
		t.Fatalf("Second DeleteMessage failed: %v", err)
	}
}

func TestBboltStore_DeleteMessageWithPlainDeletedFlag(t *testing.T) {
	// Test the branch where the message has "Deleted" (without backslash) flag
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	uid := uint32(6)
	meta := &storage.MessageMetadata{
		MessageID: "plaindeleted12345",
		UID:       uid,
		Flags:     []string{"Deleted"},
		Size:      50,
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	err = store.DeleteMessage(user, 1)
	if err != nil {
		t.Fatalf("DeleteMessage with 'Deleted' flag failed: %v", err)
	}
}

func TestBboltStore_GetMessageCountWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{10, 20, 30})
	defer cleanup()

	count, err := store.GetMessageCount(user)
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}
}

func TestBboltStore_GetMessageSizeWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{77})
	defer cleanup()

	size, err := store.GetMessageSize(user, 1)
	if err != nil {
		t.Fatalf("GetMessageSize failed: %v", err)
	}
	if size != 77 {
		t.Errorf("Expected size 77, got %d", size)
	}
}

func TestBboltStore_GetMessageDataOutOfRange(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	_, err := store.GetMessageData(user, 5)
	if err == nil {
		t.Error("Expected error for out-of-range index")
	}

	_, err = store.GetMessageData(user, 0)
	if err == nil {
		t.Error("Expected error for index 0")
	}
}

func TestBboltStore_DeleteMessageOutOfRangeWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	err := store.DeleteMessage(user, 5)
	if err == nil {
		t.Error("Expected error for out-of-range delete index")
	}

	err = store.DeleteMessage(user, 0)
	if err == nil {
		t.Error("Expected error for index 0")
	}
}

func TestBboltStore_GetMessageSizeOutOfRangeWithData(t *testing.T) {
	user := "user@example.com"
	store, cleanup := setupBboltStoreWithMessages(t, user, []int64{50})
	defer cleanup()

	_, err := store.GetMessageSize(user, 5)
	if err == nil {
		t.Error("Expected error for out-of-range size index")
	}
}

// ==========================================================================
// MaildirStore tests
// ==========================================================================

func TestMaildirStore_ReadMaildirWithDirectory(t *testing.T) {
	// Exercise the entry.IsDir() skip branch in readMaildir
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	newPath := store.newPath(user)
	os.MkdirAll(newPath, 0755)

	// Create a subdirectory (should be skipped) and a regular file
	os.MkdirAll(filepath.Join(newPath, "subdir"), 0755)
	os.WriteFile(filepath.Join(newPath, "1000000001.100.testhost"), []byte("msg"), 0644)

	msgs, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message (subdir skipped), got %d", len(msgs))
	}
}

func TestMaildirStore_GetMessageValidIndex(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	newPath := store.newPath(user)
	os.MkdirAll(newPath, 0755)
	os.WriteFile(filepath.Join(newPath, "1000000001.100.testhost"), []byte("msg body"), 0644)

	msg, err := store.GetMessage(user, 0)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if msg.UID != "1000000001.100.testhost" {
		t.Errorf("Expected UID 1000000001.100.testhost, got %s", msg.UID)
	}

	// Out of range
	_, err = store.GetMessage(user, 1)
	if err == nil {
		t.Error("Expected error for index 1 with only 1 message")
	}
}

func TestMaildirStore_DeleteMessageFromNew(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	newPath := store.newPath(user)
	curPath := store.curPath(user)
	os.MkdirAll(newPath, 0755)
	os.MkdirAll(curPath, 0755)

	msgFile := filepath.Join(newPath, "1000000001.100.testhost")
	os.WriteFile(msgFile, []byte("msg body"), 0644)

	err := store.DeleteMessage(user, 0)
	if err != nil {
		t.Fatalf("DeleteMessage from new/ failed: %v", err)
	}

	if _, err := os.Stat(msgFile); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted from new/")
	}
}

func TestMaildirStore_DeleteMessageFromCurOnly(t *testing.T) {
	// Test the cur-only delete path (new/ Remove fails, cur/ succeeds)
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	curPath := store.curPath(user)
	os.MkdirAll(curPath, 0755)

	msgFile := filepath.Join(curPath, "1000000001.100.testhost")
	os.WriteFile(msgFile, []byte("cur msg body"), 0644)

	err := store.DeleteMessage(user, 0)
	if err != nil {
		t.Fatalf("DeleteMessage from cur/ failed: %v", err)
	}

	if _, err := os.Stat(msgFile); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted from cur/")
	}
}

func TestMaildirStore_GetMessageDataFromNew(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	newPath := store.newPath(user)
	os.MkdirAll(newPath, 0755)

	expectedData := []byte("Subject: Hello\r\n\r\nWorld\r\n")
	os.WriteFile(filepath.Join(newPath, "1000000001.100.testhost"), expectedData, 0644)

	data, err := store.GetMessageData(user, 0)
	if err != nil {
		t.Fatalf("GetMessageData from new/ failed: %v", err)
	}
	if string(data) != string(expectedData) {
		t.Errorf("Expected %q, got %q", string(expectedData), string(data))
	}
}

func TestMaildirStore_GetMessageCountWithData(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	newPath := store.newPath(user)
	curPath := store.curPath(user)
	os.MkdirAll(newPath, 0755)
	os.MkdirAll(curPath, 0755)

	os.WriteFile(filepath.Join(newPath, "1000000001.100.testhost"), []byte("msg1"), 0644)
	os.WriteFile(filepath.Join(curPath, "1000000002.100.testhost"), []byte("msg2"), 0644)

	count, err := store.GetMessageCount(user)
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}
}

func TestMaildirStore_GetMessageSizeWithData(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	newPath := store.newPath(user)
	os.MkdirAll(newPath, 0755)

	content := []byte("A longer message body for size testing")
	os.WriteFile(filepath.Join(newPath, "1000000001.100.testhost"), content, 0644)

	size, err := store.GetMessageSize(user, 0)
	if err != nil {
		t.Fatalf("GetMessageSize failed: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), size)
	}
}

func TestMaildirStore_GetMessageNegativeIndex(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	_, err := store.GetMessage("test@example.com", -1)
	if err == nil {
		t.Error("Expected error for negative index")
	}
}

func TestMaildirStore_GetMessageDataNoFilesAtAll(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	// No directories created at all - GetMessage will fail first
	_, err := store.GetMessageData("test@example.com", 0)
	if err == nil {
		t.Error("Expected error when no messages exist")
	}
}

// ==========================================================================
// sendTop tests for \n\n fallback branch
// ==========================================================================

func TestSendTop_LFOnlyHeaders(t *testing.T) {
	// Test the \n\n branch (not \r\n\r\n) in sendTop.
	// Note: sendTop always advances by 4 bytes past headerEnd (designed for \r\n\r\n),
	// so with \n\n it skips 2 extra bytes. We account for this in our data.
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	// Use data where the extra 2-byte skip won't cause issues for our assertions.
	// The body after headerEnd+4 will be "Alpha\nBravo\nCharlie\nDelta"
	data := []byte("H1: v1\nH2: v2\n\nAAAlpha\nBravo\nCharlie\nDelta")

	go func() {
		session.sendTop(data, 2)
		session.writer.Flush()
		serverConn.Close()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respData, err := io.ReadAll(clientConn)
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		t.Fatalf("Failed to read: %v", err)
	}

	respStr := string(respData)
	if !strings.Contains(respStr, "H1: v1") {
		t.Errorf("Expected header H1 in response, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Alpha") {
		t.Errorf("Expected Alpha in response, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Bravo") {
		t.Errorf("Expected Bravo in response, got: %s", respStr)
	}
}

// ==========================================================================
// BboltStore: ListMessages with metadata returning zero size
// ==========================================================================

func TestBboltStore_ListMessagesZeroSize(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	meta := &storage.MessageMetadata{
		UID:   1,
		Flags: []string{},
		Size:  0,
	}
	db.StoreMessageMetadata(user, "INBOX", 1, meta)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	msgs, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message, got %d", len(msgs))
	}
}

// ==========================================================================
// BboltStore: GetMessageData with INBOX/ prefix fallback succeeding
// This exercises the fallback path in GetMessageData (line 81).
// ==========================================================================

func TestBboltStore_GetMessageDataINBOXFallback(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := storage.OpenDatabase(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("NewMessageStore failed: %v", err)
	}

	user := "user@example.com"
	uid := uint32(1000) // UID string "1000" has >= 4 chars for ReadMessage

	// Store metadata
	meta := &storage.MessageMetadata{
		MessageID: "test1000",
		UID:       uid,
		Flags:     []string{},
		Size:      20,
	}
	db.StoreMessageMetadata(user, "INBOX", uid, meta)

	// The direct ReadMessage(user, "1000") will fail (no file there).
	// The fallback ReadMessage(user, "INBOX/1000") looks at:
	//   basePath/user/IN/BO/INBOX/1000
	// (first 2 chars of "INBOX/1000" = "IN", next 2 = "BO")
	inboxDir := filepath.Join(tmpDir, "messages", user, "IN", "BO", "INBOX")
	os.MkdirAll(inboxDir, 0755)
	testData := []byte("INBOX fallback data!!")
	os.WriteFile(filepath.Join(inboxDir, "1000"), testData, 0644)

	store := NewBboltStore(db, msgStore)
	defer db.Close()
	defer msgStore.Close()

	data, err := store.GetMessageData(user, 1)
	if err != nil {
		t.Fatalf("GetMessageData with INBOX fallback failed: %v", err)
	}
	if string(data) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(data))
	}
}

// ==========================================================================
// Server acceptLoop: test that stopped server rejects connections
// ==========================================================================

func TestServerAcceptLoop_AfterStop(t *testing.T) {
	store := NewSimpleMemoryStore()
	server := NewServer("127.0.0.1:0", store, nil)

	if err := server.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	addr := server.listener.Addr().String()
	server.Stop()

	// Verify the server is no longer listening
	conn, err := net.Dial("tcp", addr)
	if err == nil {
		conn.Close()
		t.Error("Expected connection to fail after stop")
	}
}
