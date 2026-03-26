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

// SSEServer provides Server-Sent Events for real-time updates
type SSEServer struct {
	logger    *slog.Logger
	clients   map[string]*SSEClient // user -> client
	clientsMu sync.RWMutex
	authFunc  func(token string) (user string, isAdmin bool, err error)
}

// SSEClient represents an SSE connection
type SSEClient struct {
	user      string
	isAdmin   bool
	writer    http.ResponseWriter
	flusher   http.Flusher
	stop      chan struct{}
	subscribe chan imap.MailboxNotification
}

// NewSSEServer creates a new SSE server
func NewSSEServer(logger *slog.Logger) *SSEServer {
	s := &SSEServer{
		logger:  logger,
		clients: make(map[string]*SSEClient),
	}

	return s
}

// SetAuthFunc sets the authentication function
func (s *SSEServer) SetAuthFunc(fn func(token string) (user string, isAdmin bool, err error)) {
	s.authFunc = fn
}

// Handler returns the HTTP handler for SSE connections
func (s *SSEServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate the request
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("X-Auth-Token")
		}

		var user string
		var isAdmin bool

		if s.authFunc != nil && token != "" {
			var err error
			user, isAdmin, err = s.authFunc(token)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			// For development, allow unauthenticated connections
			user = r.URL.Query().Get("user")
			if user == "" {
				http.Error(w, "Missing user parameter", http.StatusBadRequest)
				return
			}
		}

		// Set up SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Create client
		client := &SSEClient{
			user:      user,
			isAdmin:   isAdmin,
			writer:    w,
			flusher:   flusher,
			stop:      make(chan struct{}),
			subscribe: make(chan imap.MailboxNotification, 100),
		}

		// Register client
		s.clientsMu.Lock()
		if oldClient, exists := s.clients[user]; exists {
			close(oldClient.stop)
		}
		s.clients[user] = client
		s.clientsMu.Unlock()

		s.logger.Info("SSE client connected", "user", user, "is_admin", isAdmin)

		// Subscribe to IMAP notifications
		notifyChan := imap.GetNotificationHub().Subscribe(user)
		defer imap.GetNotificationHub().Unsubscribe(user, notifyChan)

		// Send initial connection event
		s.sendEvent(client, "connected", map[string]interface{}{
			"user":     user,
			"is_admin": isAdmin,
			"timestamp": time.Now().Unix(),
		})

		// Keep connection alive and send events
		ticker := time.NewTicker(30 * time.Second) // Heartbeat
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				s.clientsMu.Lock()
				delete(s.clients, user)
				s.clientsMu.Unlock()
				s.logger.Info("SSE client disconnected", "user", user)
				return

			case <-client.stop:
				s.clientsMu.Lock()
				delete(s.clients, user)
				s.clientsMu.Unlock()
				return

			case notification := <-notifyChan:
				s.handleNotification(client, notification)

			case <-ticker.C:
				s.sendEvent(client, "heartbeat", map[string]interface{}{
					"timestamp": time.Now().Unix(),
				})
			}
		}
	}
}

// handleNotification processes IMAP notifications and sends SSE events
func (s *SSEServer) handleNotification(client *SSEClient, notification imap.MailboxNotification) {
	switch notification.Type {
	case imap.NotificationNewMessage:
		s.sendEvent(client, "new_mail", map[string]interface{}{
			"folder":     notification.Mailbox,
			"uid":        notification.MessageUID,
			"seq_num":    notification.SeqNum,
			"timestamp":  time.Now().Unix(),
		})

	case imap.NotificationExpunge:
		s.sendEvent(client, "expunge", map[string]interface{}{
			"folder":    notification.Mailbox,
			"seq_num":   notification.SeqNum,
			"timestamp": time.Now().Unix(),
		})

	case imap.NotificationFlagsChanged:
		s.sendEvent(client, "flags_changed", map[string]interface{}{
			"folder":    notification.Mailbox,
			"uid":       notification.MessageUID,
			"seq_num":   notification.SeqNum,
			"flags":     notification.Flags,
			"timestamp": time.Now().Unix(),
		})

	case imap.NotificationMailboxUpdate:
		s.sendEvent(client, "folder_update", map[string]interface{}{
			"folder":    notification.Mailbox,
			"timestamp": time.Now().Unix(),
		})
	}
}

// sendEvent sends an SSE event to a client
func (s *SSEServer) sendEvent(client *SSEClient, event string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		s.logger.Error("Failed to marshal SSE event", "error", err)
		return
	}

	fmt.Fprintf(client.writer, "event: %s\n", event)
	fmt.Fprintf(client.writer, "data: %s\n\n", payload)
	client.flusher.Flush()
}

// Broadcast sends an event to all connected clients
func (s *SSEServer) Broadcast(event string, data interface{}) {
	s.clientsMu.RLock()
	clients := make([]*SSEClient, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		s.sendEvent(client, event, data)
	}
}

// SendToUser sends an event to a specific user
func (s *SSEServer) SendToUser(user, event string, data interface{}) error {
	s.clientsMu.RLock()
	client := s.clients[user]
	s.clientsMu.RUnlock()

	if client == nil {
		return fmt.Errorf("user not connected")
	}

	s.sendEvent(client, event, data)
	return nil
}

// SendToAdmins sends an event to all admin clients
func (s *SSEServer) SendToAdmins(event string, data interface{}) {
	s.clientsMu.RLock()
	clients := make([]*SSEClient, 0)
	for _, c := range s.clients {
		if c.isAdmin {
			clients = append(clients, c)
		}
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		s.sendEvent(client, event, data)
	}
}

// GetConnectedUsers returns a list of connected users
func (s *SSEServer) GetConnectedUsers() []string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	users := make([]string, 0, len(s.clients))
	for user := range s.clients {
		users = append(users, user)
	}
	return users
}

// GetConnectedCount returns the number of connected clients
func (s *SSEServer) GetConnectedCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}
