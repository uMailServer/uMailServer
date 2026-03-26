package websocket

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

// Message types for WebSocket communication
type MessageType string

const (
	// Client -> Server messages
	TypeAuth       MessageType = "auth"
	TypePing       MessageType = "ping"
	TypeSubscribe  MessageType = "subscribe"
	TypeUnsubscribe MessageType = "unsubscribe"

	// Server -> Client messages
	TypeAuthSuccess   MessageType = "auth_success"
	TypeAuthError     MessageType = "auth_error"
	TypePong          MessageType = "pong"
	TypeNewMail       MessageType = "new_mail"
	TypeMailUpdate    MessageType = "mail_update"
	TypeFolderUpdate  MessageType = "folder_update"
	TypeQueueUpdate   MessageType = "queue_update"
	TypeNotification  MessageType = "notification"
	TypeError         MessageType = "error"
)

// Message represents a WebSocket message
type Message struct {
	Type      MessageType     `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// NewMailPayload is sent when new mail arrives
type NewMailPayload struct {
	Folder    string `json:"folder"`
	UID       uint32 `json:"uid"`
	From      string `json:"from"`
	Subject   string `json:"subject"`
	Preview   string `json:"preview"`
	Timestamp int64  `json:"timestamp"`
}

// FolderUpdatePayload is sent when folder counts change
type FolderUpdatePayload struct {
	Folder      string `json:"folder"`
	TotalCount  int    `json:"total_count"`
	UnreadCount int    `json:"unread_count"`
}

// QueueUpdatePayload is sent for queue status changes (admin only)
type QueueUpdatePayload struct {
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Delivered int `json:"delivered"`
}

// AuthPayload for authentication
type AuthPayload struct {
	Token string `json:"token"`
}

// Server represents the WebSocket server
type Server struct {
	upgrader    *Upgrader
	logger      *slog.Logger
	clients     map[string]*Client // user -> client
	clientsMu   sync.RWMutex
	authFunc    func(token string) (user string, isAdmin bool, err error)
	notifyHub   *imap.NotificationHub
}

// Client represents a connected WebSocket client
type Client struct {
	id       string
	user     string
	isAdmin  bool
	conn     *Conn
	server   *Server
	send     chan []byte
	stop     chan struct{}
	mu       sync.RWMutex
}

// Upgrader handles WebSocket upgrades
type Upgrader struct {
	CheckOrigin func(r *http.Request) bool
}

// Conn represents a WebSocket connection (simplified interface)
type Conn struct {
	writeMu sync.Mutex
}

// NewServer creates a new WebSocket server
func NewServer(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		upgrader: &Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins in development, should be configurable
				return true
			},
		},
		logger:    logger,
		clients:   make(map[string]*Client),
		notifyHub: imap.GetNotificationHub(),
	}

	// Start the notification listener
	go s.notificationListener()

	return s
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(token string) (user string, isAdmin bool, err error)) {
	s.authFunc = fn
}

// Handler returns the HTTP handler for WebSocket connections
func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For now, use a simple HTTP-based polling fallback
		// Full WebSocket upgrade would require gorilla/websocket or similar
		s.handleHTTPConnection(w, r)
	}
}

// handleHTTPConnection handles WebSocket-over-HTTP for now (SSE fallback)
func (s *Server) handleHTTPConnection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Return SSE endpoint info
	response := map[string]interface{}{
		"status": "ok",
		"message": "WebSocket endpoint - use /ws/stream for SSE events",
		"supported_events": []string{
			"new_mail",
			"folder_update",
			"mail_update",
		},
	}

	json.NewEncoder(w).Encode(response)
}

// notificationListener listens for IMAP notifications and broadcasts to clients
func (s *Server) notificationListener() {
	// Create a global notification channel
	ch := make(chan imap.MailboxNotification, 1000)

	// Subscribe to all users (we'll filter per-client)
	// Note: In a real implementation, we'd have a better subscription model

	s.logger.Info("WebSocket notification listener started")

	for notification := range ch {
		s.broadcastNotification(notification)
	}
}

// broadcastNotification broadcasts a notification to relevant clients
func (s *Server) broadcastNotification(notification imap.MailboxNotification) {
	s.clientsMu.RLock()
	client := s.clients[notification.User]
	s.clientsMu.RUnlock()

	if client == nil {
		return // User not connected
	}

	var msg Message
	msg.Timestamp = time.Now()

	switch notification.Type {
	case imap.NotificationNewMessage:
		msg.Type = TypeNewMail
		payload := NewMailPayload{
			Folder:    notification.Mailbox,
			UID:       notification.MessageUID,
			Timestamp: time.Now().Unix(),
		}
		data, _ := json.Marshal(payload)
		msg.Payload = data

	case imap.NotificationFlagsChanged, imap.NotificationMailboxUpdate:
		msg.Type = TypeFolderUpdate
		// Would need to get actual counts from storage
		payload := FolderUpdatePayload{
			Folder: notification.Mailbox,
		}
		data, _ := json.Marshal(payload)
		msg.Payload = data

	default:
		return // Ignore other notification types
	}

	client.sendMessage(msg)
}

// RegisterClient registers a new client connection
func (s *Server) RegisterClient(user string, client *Client) {
	s.clientsMu.Lock()
	s.clients[user] = client
	s.clientsMu.Unlock()

	s.logger.Info("WebSocket client registered", "user", user, "client_id", client.id)
}

// UnregisterClient removes a client connection
func (s *Server) UnregisterClient(user string, client *Client) {
	s.clientsMu.Lock()
	if s.clients[user] == client {
		delete(s.clients, user)
	}
	s.clientsMu.Unlock()

	s.logger.Info("WebSocket client unregistered", "user", user, "client_id", client.id)
}

// SendToUser sends a message to a specific user
func (s *Server) SendToUser(user string, msg Message) error {
	s.clientsMu.RLock()
	client := s.clients[user]
	s.clientsMu.RUnlock()

	if client == nil {
		return fmt.Errorf("user not connected")
	}

	return client.sendMessage(msg)
}

// Broadcast sends a message to all connected clients
func (s *Server) Broadcast(msg Message) {
	s.clientsMu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		client.sendMessage(msg)
	}
}

// BroadcastToAdmins sends a message to all admin clients
func (s *Server) BroadcastToAdmins(msg Message) {
	s.clientsMu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.isAdmin {
			clients = append(clients, c)
		}
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		client.sendMessage(msg)
	}
}

// Client methods

// sendMessage sends a message to the client
func (c *Client) sendMessage(msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case c.send <- data:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("send timeout")
	}
}

// Close closes the client connection
func (c *Client) Close() {
	close(c.stop)
}

// NewClient creates a new client
func NewClient(id, user string, isAdmin bool) *Client {
	return &Client{
		id:      id,
		user:    user,
		isAdmin: isAdmin,
		send:    make(chan []byte, 256),
		stop:    make(chan struct{}),
	}
}
