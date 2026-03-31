package pop3

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errorMailstore wraps SimpleMemoryStore but returns errors from ListMessages
type errorMailstore struct {
	*SimpleMemoryStore
	listErr error
}

func (e *errorMailstore) ListMessages(user string) ([]*Message, error) {
	if e.listErr != nil {
		return nil, e.listErr
	}
	return e.SimpleMemoryStore.ListMessages(user)
}

// --- WriteDataLine coverage: test the "." dot-escaping branch ---

func TestWriteDataLine_DotEscaping(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	reader := bufio.NewReader(clientConn)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Write a line starting with "." - should be escaped to ".."
		session.WriteDataLine(".something")
		session.writer.Flush()
		serverConn.Close()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read data line: %v", err)
	}
	if !strings.HasPrefix(line, "..") {
		t.Errorf("Expected dot-escaped line starting with '..', got %q", line)
	}
	if line != "..something\r\n" {
		t.Errorf("Expected '..something\\r\\n', got %q", line)
	}
}

func TestWriteDataLine_NormalLine(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteDataLine("normal line")
		session.writer.Flush()
		serverConn.Close()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read data line: %v", err)
	}
	if line != "normal line\r\n" {
		t.Errorf("Expected 'normal line\\r\\n', got %q", line)
	}
}

// --- handleCommand coverage: empty parts (whitespace-only input) ---
// The Handle() loop skips empty lines (after TrimRight), but handleCommand
// checks len(parts)==0 separately. We test this by calling handleCommand directly.

func TestHandleCommand_EmptyParts(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	reader := bufio.NewReader(clientConn)

	go func() {
		// handleCommand with a line that has only spaces/tabs (Fields returns empty)
		session.handleCommand("   \t  ")
		session.writer.Flush()
		serverConn.Close()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}
	if !strings.HasPrefix(resp, "-ERR Invalid command") {
		t.Errorf("Expected '-ERR Invalid command', got %q", resp)
	}
}

// --- handleCommand coverage: StateUpdate case ---

func TestHandleCommand_StateUpdatePath(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	reader := bufio.NewReader(clientConn)

	go func() {
		// Force session into Update state
		session.state = StateUpdate
		session.user = "testuser"
		// handleCommand should route to handleUpdateCommand
		session.handleCommand("QUIT")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK' from update state, got %q", resp)
	}
}

// --- handleAuthorizationCommand: USER without args ---

func TestUSER_NoArgs(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	fmt.Fprintf(clientConn, "USER\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR for USER without args, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleAuthorizationCommand: PASS without args ---

func TestPASS_NoArgs(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Send USER first to set s.user
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')

	// Send PASS without args
	fmt.Fprintf(clientConn, "PASS\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR for PASS without args, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: CAPA in transaction state ---

func TestCAPA_InTransactionState(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Send CAPA in transaction state
	fmt.Fprintf(clientConn, "CAPA\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK Capability list follows") {
		t.Errorf("Expected '+OK Capability list follows', got %s", resp)
	}

	// Read capability lines until "."
	for {
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) == "." {
			break
		}
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: UIDL with specific index ---

func TestUIDL_SpecificIndex(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "unique-id-123",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// UIDL for specific message
	fmt.Fprintf(clientConn, "UIDL 1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK 1 unique-id-123") {
		t.Errorf("Expected '+OK 1 unique-id-123', got %s", resp)
	}

	// UIDL for invalid index
	fmt.Fprintf(clientConn, "UIDL 999\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR for UIDL invalid index, got %s", resp)
	}

	// UIDL for non-numeric index
	fmt.Fprintf(clientConn, "UIDL abc\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR for UIDL non-numeric, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- UIDL for deleted message ---

func TestUIDL_DeletedMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete message first
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	reader.ReadString('\n')

	// UIDL for deleted message
	fmt.Fprintf(clientConn, "UIDL 1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Message deleted") {
		t.Errorf("Expected '-ERR Message deleted', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: DELE already deleted message ---

func TestDELE_AlreadyDeleted(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete message first time
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK first delete, got %s", resp)
	}

	// Try to delete again
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Message already deleted") {
		t.Errorf("Expected '-ERR Message already deleted', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: TOP with missing args ---

func TestTOP_MissingArgs(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// TOP with only one arg (missing lines)
	fmt.Fprintf(clientConn, "TOP 1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Usage: TOP") {
		t.Errorf("Expected '-ERR Usage: TOP', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: TOP with invalid index ---

func TestTOP_InvalidIndex(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// TOP with invalid index
	fmt.Fprintf(clientConn, "TOP 999 0\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR No such message") {
		t.Errorf("Expected '-ERR No such message', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: TOP with invalid line count ---

func TestTOP_InvalidLineCount(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// TOP with negative line count
	fmt.Fprintf(clientConn, "TOP 1 -1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Invalid line count") {
		t.Errorf("Expected '-ERR Invalid line count', got %s", resp)
	}

	// TOP with non-numeric line count
	fmt.Fprintf(clientConn, "TOP 1 abc\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Invalid line count") {
		t.Errorf("Expected '-ERR Invalid line count' for non-numeric, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: TOP with deleted message ---

func TestTOP_DeletedMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete message first
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	reader.ReadString('\n')

	// TOP on deleted message
	fmt.Fprintf(clientConn, "TOP 1 0\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Message deleted") {
		t.Errorf("Expected '-ERR Message deleted', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- RETR with message that has nil Data (forces GetMessageData call) ---

func TestRETR_NilDataMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	// Add a message with nil Data - this forces the RETR handler to call
	// mailstore.GetMessageData() to load the data
	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  nil, // explicitly nil
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// RETR will find msg.Data == nil and call GetMessageData
	fmt.Fprintf(clientConn, "RETR 1\r\n")
	resp, _ := reader.ReadString('\n')
	// The message data is nil and GetMessageData for SimpleMemoryStore with
	// nil data returns the nil Data field, so len(msg.Data) will be 0
	if !strings.HasPrefix(resp, "+OK 0 octets") {
		t.Errorf("Expected '+OK 0 octets' for nil-data message, got %s", resp)
	}

	// Read until terminator
	for {
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) == "." {
			break
		}
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleUpdateCommand with a deleted message (nil entry) ---

func TestUpdateState_DeletesMarkedMessages(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete message
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	reader.ReadString('\n')

	// QUIT - enters UPDATE state, should delete marked messages
	fmt.Fprintf(clientConn, "QUIT\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK' on QUIT with deletion, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: TOP with message that has nil Data ---

func TestTOP_NilDataMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	// Add message with nil Data to exercise the "if msg.Data == nil" branch in TOP
	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  nil,
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// TOP with nil Data message
	fmt.Fprintf(clientConn, "TOP 1 5\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK' for TOP with nil-data message, got %s", resp)
	}

	// Read until terminator
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "." {
			break
		}
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- handleTransactionCommand: LIST with nil messages in the list ---

func TestLIST_MixedNilMessages(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	// Add two messages
	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  100,
		Data:  []byte("msg1"),
	})
	store.AddMessage("test", &Message{
		Index: 2,
		UID:   "uid2",
		Size:  200,
		Data:  []byte("msg2"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete first message
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	reader.ReadString('\n')

	// LIST all - should skip the nil (deleted) message
	fmt.Fprintf(clientConn, "LIST\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK' for LIST, got %s", resp)
	}

	// Should only see message 2
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "2 200") {
		t.Errorf("Expected '2 200', got %s", line)
	}

	// Read terminator
	line, _ = reader.ReadString('\n')
	if strings.TrimSpace(line) != "." {
		t.Errorf("Expected '.', got %s", line)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- STAT with nil messages in the list (verifies nil check) ---

func TestSTAT_WithDeletedMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  100,
		Data:  []byte("msg1"),
	})
	store.AddMessage("test", &Message{
		Index: 2,
		UID:   "uid2",
		Size:  200,
		Data:  []byte("msg2"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete first message
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	reader.ReadString('\n')

	// STAT - should count len(messages) which includes nil entries
	// totalSize should only count non-nil messages
	fmt.Fprintf(clientConn, "STAT\r\n")
	resp, _ := reader.ReadString('\n')
	// len(s.messages) is 2 (nil entry still counts), totalSize is 200
	if !strings.HasPrefix(resp, "+OK 2 200") {
		t.Errorf("Expected '+OK 2 200' for STAT with one deleted, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- UIDL listing with nil messages ---

func TestUIDL_ListWithNilMessages(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  100,
		Data:  []byte("msg1"),
	})
	store.AddMessage("test", &Message{
		Index: 2,
		UID:   "uid2",
		Size:  200,
		Data:  []byte("msg2"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Delete first message
	fmt.Fprintf(clientConn, "DELE 1\r\n")
	reader.ReadString('\n')

	// UIDL all - should skip nil messages
	fmt.Fprintf(clientConn, "UIDL\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK' for UIDL, got %s", resp)
	}

	// Should only see message 2's UID
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "2 uid2") {
		t.Errorf("Expected '2 uid2', got %s", line)
	}

	// Read terminator
	line, _ = reader.ReadString('\n')
	if strings.TrimSpace(line) != "." {
		t.Errorf("Expected '.', got %s", line)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- TOP with non-numeric index ---

func TestTOP_NonNumericIndex(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// TOP with non-numeric index
	fmt.Fprintf(clientConn, "TOP abc 5\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR No such message") {
		t.Errorf("Expected '-ERR No such message' for non-numeric index, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- BboltStore: ListMessages with GetMessageUIDs error path ---
// Since storage.Database is a stub and always returns empty results,
// we can only test the empty/error-free path. The existing tests already
// cover the "no messages" case. Let's verify edge cases.

func TestBboltStoreListMessages_EmptyUser(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	msgs, err := store.ListMessages("")
	if err != nil {
		t.Fatalf("ListMessages('') failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages for empty user, got %d", len(msgs))
	}
}

func TestBboltStoreGetMessage_ExactLowerBound(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	// index=1 is the lower bound for valid indices
	_, err := store.GetMessage("user@test.com", 1)
	if err == nil {
		t.Error("Expected error for GetMessage(1) on empty mailbox")
	}
}

func TestBboltStoreGetMessageData_ZeroIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageData("user@test.com", 0)
	if err == nil {
		t.Error("Expected error for GetMessageData(0) on empty mailbox")
	}
}

func TestBboltStoreDeleteMessage_ZeroIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	err := store.DeleteMessage("user@test.com", 0)
	if err == nil {
		t.Error("Expected error for DeleteMessage(0) on empty mailbox")
	}
}

func TestBboltStoreGetMessageSize_ZeroIndex(t *testing.T) {
	store, cleanup := setupBboltStoreTest(t)
	defer cleanup()

	_, err := store.GetMessageSize("user@test.com", 0)
	if err == nil {
		t.Error("Expected error for GetMessageSize(0) on empty mailbox")
	}
}

// --- Test RETR with message data not ending with newline ---
// This exercises the "if !strings.HasSuffix" branch that adds "\r\n"

func TestRETR_MessageDataNoNewline(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	// Message data without trailing newline
	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  11,
		Data:  []byte("hello world"), // no newline
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// RETR the message
	fmt.Fprintf(clientConn, "RETR 1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK', got %s", resp)
	}

	// Read until terminator
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "." {
			break
		}
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- Test RETR with message data ending with newline ---
// This exercises the branch where HasSuffix returns true

func TestRETR_MessageDataWithNewline(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	// Message data WITH trailing newline
	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  20,
		Data:  []byte("Subject: Test\n\nHello\n"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// RETR the message
	fmt.Fprintf(clientConn, "RETR 1\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK', got %s", resp)
	}

	// Read until terminator
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "." {
			break
		}
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- Test PASS with authFunc returning error ---

func TestPASS_AuthFuncError(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return false, fmt.Errorf("auth backend error")
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Send USER
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')

	// PASS should get auth error
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Authentication failed") {
		t.Errorf("Expected '-ERR Authentication failed', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- Test PASS without authFunc (bypasses authentication) ---

func TestPASS_NoAuthFunc(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	// Don't set authFunc - should skip auth and proceed directly to loading messages

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Send USER
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')

	// PASS without authFunc should succeed
	fmt.Fprintf(clientConn, "PASS anything\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected '+OK' for PASS without authFunc, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- Test Stop with active sessions ---

func TestStop_WithActiveSessions(t *testing.T) {
	store := NewSimpleMemoryStore()
	server := NewServer("127.0.0.1:0", store, nil)

	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Connect a client
	conn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Give time for the session to be registered
	time.Sleep(50 * time.Millisecond)

	// Stop should close all sessions
	err = server.Stop()
	if err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}
}

// --- Test PASS when ListMessages returns an error ---

func TestPASS_ListMessagesError(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	inner := NewSimpleMemoryStore()
	store := &errorMailstore{
		SimpleMemoryStore: inner,
		listErr:           errors.New("database error"),
	}

	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Send USER
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')

	// PASS should fail because ListMessages returns error
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Unable to load messages") {
		t.Errorf("Expected '-ERR Unable to load messages', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- Test RSET when ListMessages returns an error ---

func TestRSET_ListMessagesError(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	inner := NewSimpleMemoryStore()
	inner.AddMessage("test", &Message{
		Index: 1,
		UID:   "uid1",
		Size:  50,
		Data:  []byte("test"),
	})

	store := &errorMailstore{
		SimpleMemoryStore: inner,
		// listErr is nil initially so PASS succeeds, then we set it
	}

	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 ready")
		session.Handle()
	}()

	reader.ReadString('\n') // greeting

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Now make ListMessages return error
	store.listErr = errors.New("database error")

	// RSET should fail
	fmt.Fprintf(clientConn, "RSET\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR Unable to reset") {
		t.Errorf("Expected '-ERR Unable to reset', got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

// --- Test Start() with a port that's already bound ---

func TestStart_PortAlreadyBound(t *testing.T) {
	store := NewSimpleMemoryStore()

	// Listen on a port first
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create first listener: %v", err)
	}
	defer listener.Close()

	// Try to start server on the same port
	server := NewServer(listener.Addr().String(), store, nil)
	err = server.Start()
	if err == nil {
		t.Error("Expected error when starting server on already-bound port")
		server.Stop()
	}
}

// --- MaildirStore: DeleteMessage from cur/ directory ---

func TestMaildirStore_DeleteMessageFromCur(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)

	// Create maildir structure
	for _, sub := range []string{"new", "cur", "tmp"} {
		os.MkdirAll(filepath.Join(userDir, sub), 0755)
	}

	// Place a message in cur/
	msgContent := []byte("From: test@test.com\r\nSubject: Test\r\n\r\nHello\r\n")
	msgFile := filepath.Join(userDir, "cur", "1234567890.1234.testhost:2,S")
	os.WriteFile(msgFile, msgContent, 0644)

	// Delete it
	err := store.DeleteMessage(user, 0)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(msgFile); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted from cur/")
	}
}

// --- MaildirStore: DeleteMessage non-existent ---

func TestMaildirStore_DeleteMessageNonExistent(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)

	// Create maildir structure with no messages
	for _, sub := range []string{"new", "cur", "tmp"} {
		os.MkdirAll(filepath.Join(userDir, sub), 0755)
	}

	// Try deleting from empty store
	err := store.DeleteMessage(user, 0)
	if err == nil {
		t.Error("Expected error for deleting non-existent message")
	}
}

// --- MaildirStore: GetMessageData from cur/ ---

func TestMaildirStore_GetMessageDataFromCur(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)

	for _, sub := range []string{"new", "cur", "tmp"} {
		os.MkdirAll(filepath.Join(userDir, sub), 0755)
	}

	// Place a message in cur/ (use filename without colon for Windows compatibility)
	expectedData := []byte("From: test@test.com\r\nSubject: Test\r\n\r\nBody content\r\n")
	msgName := "1234567890.M123456.testhost"
	os.WriteFile(filepath.Join(userDir, "cur", msgName), expectedData, 0644)

	// GetMessageData should find it
	data, err := store.GetMessageData(user, 0)
	if err != nil {
		t.Fatalf("GetMessageData failed: %v", err)
	}
	if string(data) != string(expectedData) {
		t.Errorf("Expected %q, got %q", string(expectedData), string(data))
	}
}

// --- MaildirStore: GetMessageData non-existent ---

func TestMaildirStore_GetMessageDataNonExistent(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)

	for _, sub := range []string{"new", "cur", "tmp"} {
		os.MkdirAll(filepath.Join(userDir, sub), 0755)
	}

	_, err := store.GetMessageData(user, 99)
	if err == nil {
		t.Error("Expected error for non-existent message data")
	}
}

// --- MaildirStore: ListMessages with both new and cur ---

func TestMaildirStore_ListMessagesBothDirs(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)

	for _, sub := range []string{"new", "cur", "tmp"} {
		os.MkdirAll(filepath.Join(userDir, sub), 0755)
	}

	// Place messages in both new/ and cur/
	os.WriteFile(filepath.Join(userDir, "new", "1000000001.100.testhost"), []byte("new msg"), 0644)
	os.WriteFile(filepath.Join(userDir, "cur", "1000000002.100.testhost:2,S"), []byte("cur msg"), 0644)

	messages, err := store.ListMessages(user)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}
}

// --- MaildirStore: Authenticate with existing user ---

func TestMaildirStore_AuthenticateExisting(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)
	os.MkdirAll(userDir, 0755)

	ok, err := store.Authenticate(user, "pass")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if !ok {
		t.Error("Expected auth success for existing user directory")
	}
}

// --- MaildirStore: Authenticate with non-existent user ---

func TestMaildirStore_AuthenticateNonExistent(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	ok, err := store.Authenticate("noone@example.com", "pass")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if ok {
		t.Error("Expected auth failure for non-existent user directory")
	}
}

// --- MaildirStore: GetMessageCount and GetMessageSize ---

func TestMaildirStore_MessageCountAndSize(t *testing.T) {
	baseDir := t.TempDir()
	store := NewMaildirStore(baseDir)

	user := "test@example.com"
	safeUser := strings.ReplaceAll(user, "@", "_")
	userDir := filepath.Join(baseDir, safeUser)

	for _, sub := range []string{"new", "cur", "tmp"} {
		os.MkdirAll(filepath.Join(userDir, sub), 0755)
	}

	// Place a message
	msgData := []byte("Subject: Test\r\n\r\nHello World\r\n")
	os.WriteFile(filepath.Join(userDir, "new", "1000.100.testhost"), msgData, 0644)

	count, err := store.GetMessageCount(user)
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 message, got %d", count)
	}

	size, err := store.GetMessageSize(user, 0)
	if err != nil {
		t.Fatalf("GetMessageSize failed: %v", err)
	}
	if size != int64(len(msgData)) {
		t.Errorf("Expected size %d, got %d", len(msgData), size)
	}
}

// --- SimpleMemoryStore: DeleteMessage marks as nil ---

func TestSimpleMemoryStore_DeleteMessageMarksNil(t *testing.T) {
	store := NewSimpleMemoryStore()
	msg := &Message{
		Index: 0,
		UID:   "1",
		Size:  100,
		Data:  []byte("test message data"),
	}
	store.AddMessage("testuser", msg)

	err := store.DeleteMessage("testuser", 0)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	msgs, _ := store.ListMessages("testuser")
	if len(msgs) == 0 || msgs[0] != nil {
		t.Error("Expected message to be nil after deletion")
	}
}

// --- SimpleMemoryStore: GetMessageData for valid message ---

func TestSimpleMemoryStore_GetMessageDataValid(t *testing.T) {
	store := NewSimpleMemoryStore()
	expectedData := []byte("From: test@test.com\r\nSubject: Hello\r\n\r\nWorld\r\n")
	msg := &Message{
		Index: 0,
		UID:   "1",
		Size:  int64(len(expectedData)),
		Data:  expectedData,
	}
	store.AddMessage("testuser", msg)

	data, err := store.GetMessageData("testuser", 0)
	if err != nil {
		t.Fatalf("GetMessageData failed: %v", err)
	}
	if string(data) != string(expectedData) {
		t.Errorf("Expected %q, got %q", string(expectedData), string(data))
	}
}
// --- SimpleMemoryStore: GetMessageSize for valid message ---

func TestSimpleMemoryStore_GetMessageSizeValid(t *testing.T) {
	store := NewSimpleMemoryStore()
	msg := &Message{
		Index: 0,
		UID:   "1",
		Size:  42,
		Data:  []byte("test"),
	}
	store.AddMessage("testuser", msg)

	size, err := store.GetMessageSize("testuser", 0)
	if err != nil {
		t.Fatalf("GetMessageSize failed: %v", err)
	}
	if size != 42 {
		t.Errorf("Expected size 42, got %d", size)
	}
}
