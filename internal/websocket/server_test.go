package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

func TestNewServer(t *testing.T) {
	server := NewServer(nil)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.clients == nil {
		t.Error("expected clients map to be initialized")
	}
	if server.upgrader == nil {
		t.Error("expected upgrader to be initialized")
	}
	if server.notifyHub == nil {
		t.Error("expected notifyHub to be initialized")
	}
}

func TestServerSetAuthFunc(t *testing.T) {
	server := NewServer(nil)

	authFunc := func(token string) (string, bool, error) {
		return "test-user", false, nil
	}

	server.SetAuthFunc(authFunc)
	if server.authFunc == nil {
		t.Error("expected authFunc to be set")
	}
}

func TestServerHandler(t *testing.T) {
	server := NewServer(nil)
	handler := server.Handler()

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	// Test the handler
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", response["status"])
	}
}

func TestServerRegisterClient(t *testing.T) {
	server := NewServer(nil)
	client := NewClient("client1", "user1", false)

	server.RegisterClient("user1", client)

	server.clientsMu.RLock()
	registeredClient := server.clients["user1"]
	server.clientsMu.RUnlock()

	if registeredClient != client {
		t.Error("expected client to be registered")
	}
}

func TestServerUnregisterClient(t *testing.T) {
	server := NewServer(nil)
	client := NewClient("client1", "user1", false)

	server.RegisterClient("user1", client)
	server.UnregisterClient("user1", client)

	server.clientsMu.RLock()
	_, exists := server.clients["user1"]
	server.clientsMu.RUnlock()

	if exists {
		t.Error("expected client to be unregistered")
	}
}

func TestServerSendToUser(t *testing.T) {
	server := NewServer(nil)

	// Test sending to non-existent user
	msg := Message{
		Type:      TypeNewMail,
		Timestamp: time.Now(),
	}

	err := server.SendToUser("nonexistent", msg)
	if err == nil {
		t.Error("expected error when sending to non-existent user")
	}

	// Test sending to existing user
	client := NewClient("client1", "user1", false)
	server.RegisterClient("user1", client)

	// Run in goroutine to avoid blocking on send channel
	go func() {
		time.Sleep(50 * time.Millisecond)
		select {
		case <-client.send:
			// Message received
		default:
			t.Error("expected message to be sent to client")
		}
	}()

	err = server.SendToUser("user1", msg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
}

func TestServerBroadcast(t *testing.T) {
	server := NewServer(nil)

	client1 := NewClient("client1", "user1", false)
	client2 := NewClient("client2", "user2", false)

	server.RegisterClient("user1", client1)
	server.RegisterClient("user2", client2)

	msg := Message{
		Type:      TypeFolderUpdate,
		Timestamp: time.Now(),
	}

	// Run in goroutine to drain channels
	done := make(chan bool, 2)
	go func() {
		select {
		case <-client1.send:
			done <- true
		case <-time.After(100 * time.Millisecond):
			done <- false
		}
	}()
	go func() {
		select {
		case <-client2.send:
			done <- true
		case <-time.After(100 * time.Millisecond):
			done <- false
		}
	}()

	server.Broadcast(msg)

	received1 := <-done
	received2 := <-done

	if !received1 {
		t.Error("expected client1 to receive broadcast")
	}
	if !received2 {
		t.Error("expected client2 to receive broadcast")
	}
}

func TestServerBroadcastToAdmins(t *testing.T) {
	server := NewServer(nil)

	adminClient := NewClient("admin1", "admin", true)
	regularClient := NewClient("user1", "user", false)

	server.RegisterClient("admin", adminClient)
	server.RegisterClient("user", regularClient)

	msg := Message{
		Type:      TypeQueueUpdate,
		Timestamp: time.Now(),
	}

	// Run in goroutine to drain channels
	done := make(chan bool, 2)
	go func() {
		select {
		case <-adminClient.send:
			done <- true
		case <-time.After(100 * time.Millisecond):
			done <- false
		}
	}()
	go func() {
		select {
		case <-regularClient.send:
			done <- true
		case <-time.After(100 * time.Millisecond):
			done <- false
		}
	}()

	server.BroadcastToAdmins(msg)

	adminReceived := <-done
	userReceived := <-done

	if !adminReceived {
		t.Error("expected admin client to receive broadcast")
	}
	if userReceived {
		t.Error("expected regular user NOT to receive admin broadcast")
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("client1", "user1", true)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.id != "client1" {
		t.Errorf("expected id 'client1', got '%s'", client.id)
	}
	if client.user != "user1" {
		t.Errorf("expected user 'user1', got '%s'", client.user)
	}
	if !client.isAdmin {
		t.Error("expected isAdmin to be true")
	}
	if client.send == nil {
		t.Error("expected send channel to be initialized")
	}
	if client.stop == nil {
		t.Error("expected stop channel to be initialized")
	}
}

func TestClientSendMessage(t *testing.T) {
	client := NewClient("client1", "user1", false)

	msg := Message{
		Type:      TypeNewMail,
		Timestamp: time.Now(),
	}

	// Test successful send
	go func() {
		err := client.sendMessage(msg)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case data := <-client.send:
		var receivedMsg Message
		if err := json.Unmarshal(data, &receivedMsg); err != nil {
			t.Errorf("failed to unmarshal message: %v", err)
		}
		if receivedMsg.Type != TypeNewMail {
			t.Errorf("expected type %s, got %s", TypeNewMail, receivedMsg.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for message")
	}
}

func TestClientSendMessageTimeout(t *testing.T) {
	client := NewClient("client1", "user1", false)

	// Fill the channel to cause timeout
	for i := 0; i < 256; i++ {
		select {
		case client.send <- []byte("test"):
		default:
		}
	}

	msg := Message{
		Type:      TypeNewMail,
		Timestamp: time.Now(),
	}

	err := client.sendMessage(msg)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestClientClose(t *testing.T) {
	client := NewClient("client1", "user1", false)

	client.Close()

	select {
	case <-client.stop:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("expected stop channel to be closed")
	}
}

func TestBroadcastNotification(t *testing.T) {
	server := NewServer(nil)

	client := NewClient("client1", "user1", false)
	server.RegisterClient("user1", client)

	notification := imap.MailboxNotification{
		User:        "user1",
		Type:        imap.NotificationNewMessage,
		Mailbox:     "INBOX",
		MessageUID:  123,
	}

	// Drain the channel in goroutine
	go func() {
		select {
		case <-client.send:
		case <-time.After(100 * time.Millisecond):
		}
	}()

	server.broadcastNotification(notification)
	time.Sleep(50 * time.Millisecond)
}

func TestBroadcastNotificationNoClient(t *testing.T) {
	server := NewServer(nil)

	notification := imap.MailboxNotification{
		User:        "nonexistent",
		Type:        imap.NotificationNewMessage,
		Mailbox:     "INBOX",
		MessageUID:  123,
	}

	// Should not panic
	server.broadcastNotification(notification)
}

func TestMessageTypes(t *testing.T) {
	// Test that all message types are defined
	types := []MessageType{
		TypeAuth,
		TypePing,
		TypeSubscribe,
		TypeUnsubscribe,
		TypeAuthSuccess,
		TypeAuthError,
		TypePong,
		TypeNewMail,
		TypeMailUpdate,
		TypeFolderUpdate,
		TypeQueueUpdate,
		TypeNotification,
		TypeError,
	}

	for _, mt := range types {
		if mt == "" {
			t.Error("message type should not be empty")
		}
	}
}

func TestPayloadStructs(t *testing.T) {
	// Test NewMailPayload
	newMail := NewMailPayload{
		Folder:    "INBOX",
		UID:       123,
		From:      "sender@example.com",
		Subject:   "Test",
		Preview:   "Test preview",
		Timestamp: time.Now().Unix(),
	}
	if newMail.Folder != "INBOX" {
		t.Error("NewMailPayload fields not set correctly")
	}

	// Test FolderUpdatePayload
	folderUpdate := FolderUpdatePayload{
		Folder:      "INBOX",
		TotalCount:  10,
		UnreadCount: 5,
	}
	if folderUpdate.TotalCount != 10 {
		t.Error("FolderUpdatePayload fields not set correctly")
	}

	// Test QueueUpdatePayload
	queueUpdate := QueueUpdatePayload{
		Pending:   5,
		Failed:    2,
		Delivered: 100,
	}
	if queueUpdate.Pending != 5 {
		t.Error("QueueUpdatePayload fields not set correctly")
	}

	// Test AuthPayload
	auth := AuthPayload{Token: "test-token"}
	if auth.Token != "test-token" {
		t.Error("AuthPayload fields not set correctly")
	}
}
