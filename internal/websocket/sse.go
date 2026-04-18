package websocket

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

// SSEServer provides Server-Sent Events for real-time updates
type SSEServer struct {
	logger     *slog.Logger
	clients    map[string][]*SSEClient // user -> list of clients
	clientsMu  sync.RWMutex
	authFunc   func(token string) (user string, isAdmin bool, err error)
	corsOrigin string
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
		clients: make(map[string][]*SSEClient),
	}

	return s
}

// SetAuthFunc sets the authentication function
func (s *SSEServer) SetAuthFunc(fn func(token string) (user string, isAdmin bool, err error)) {
	s.authFunc = fn
}

// SetCorsOrigin sets the allowed CORS origin(s). Multiple origins are comma-separated.
// If empty, defaults to "*" (allow all).
func (s *SSEServer) SetCorsOrigin(origin string) {
	s.corsOrigin = origin
}

// Handler returns the HTTP handler for SSE connections
func (s *SSEServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate the request - token from X-Auth-Token header or Authorization Bearer
		token := r.Header.Get("X-Auth-Token")
		if token == "" {
			if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
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
		} else if s.authFunc != nil {
			// authFunc is set but no token provided
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		} else {
			// authFunc is not configured - this is a misconfiguration
			// In production, SetAuthFunc must always be called
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Set up SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Only set CORS if explicitly configured; empty means no CORS headers (secure default)
		if s.corsOrigin != "" {
			origin := s.corsOrigin
			if reqOrigin := r.Header.Get("Origin"); origin != "*" && reqOrigin != "" {
				for _, o := range strings.Split(origin, ",") {
					if strings.TrimSpace(o) == reqOrigin {
						origin = reqOrigin
						break
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

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

		// Register client (enforce per-user connection limit)
		s.clientsMu.Lock()
		const maxClientsPerUser = 5
		if len(s.clients[user]) >= maxClientsPerUser {
			oldest := s.clients[user][0]
			s.clients[user] = s.clients[user][1:]
			close(oldest.stop)
		}
		s.clients[user] = append(s.clients[user], client)
		s.clientsMu.Unlock()

		s.logger.Info("SSE client connected", "user", user, "is_admin", isAdmin)

		// Subscribe to IMAP notifications
		notifyChan := imap.GetNotificationHub().Subscribe(user)
		defer imap.GetNotificationHub().Unsubscribe(user, notifyChan)

		// Send initial connection event
		s.sendEvent(client, "connected", map[string]interface{}{
			"user":      user,
			"is_admin":  isAdmin,
			"timestamp": time.Now().Unix(),
		})

		// Keep connection alive and send events
		ticker := time.NewTicker(30 * time.Second) // Heartbeat
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				s.removeClient(user, client)
				s.logger.Info("SSE client disconnected", "user", user)
				return

			case <-client.stop:
				s.removeClient(user, client)
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
			"folder":    notification.Mailbox,
			"uid":       notification.MessageUID,
			"seq_num":   notification.SeqNum,
			"timestamp": time.Now().Unix(),
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

	if _, err := fmt.Fprintf(client.writer, "event: %s\n", event); err != nil {
		s.logger.Debug("failed to write SSE event", "error", err)
		return
	}
	if _, err := fmt.Fprintf(client.writer, "data: %s\n\n", payload); err != nil {
		s.logger.Debug("failed to write SSE data", "error", err)
		return
	}
	client.flusher.Flush()
}

// Broadcast sends an event to all connected clients
func (s *SSEServer) Broadcast(event string, data interface{}) {
	s.clientsMu.RLock()
	var clients []*SSEClient
	for _, list := range s.clients {
		clients = append(clients, list...)
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		s.sendEvent(client, event, data)
	}
}

// SendToUser sends an event to all connections of a specific user
func (s *SSEServer) SendToUser(user, event string, data interface{}) error {
	s.clientsMu.RLock()
	clients := s.clients[user]
	s.clientsMu.RUnlock()

	if len(clients) == 0 {
		return fmt.Errorf("user not connected")
	}

	for _, client := range clients {
		s.sendEvent(client, event, data)
	}
	return nil
}

// SendToAdmins sends an event to all admin clients
func (s *SSEServer) SendToAdmins(event string, data interface{}) {
	s.clientsMu.RLock()
	var clients []*SSEClient
	for _, list := range s.clients {
		for _, c := range list {
			if c.isAdmin {
				clients = append(clients, c)
			}
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
	count := 0
	for _, list := range s.clients {
		count += len(list)
	}
	return count
}

// removeClient removes a specific client from a user's client list.
func (s *SSEServer) removeClient(user string, client *SSEClient) {
	s.clientsMu.Lock()
	clients := s.clients[user]
	for i, c := range clients {
		if c == client {
			s.clients[user] = append(clients[:i], clients[i+1:]...)
			break
		}
	}
	if len(s.clients[user]) == 0 {
		delete(s.clients, user)
	}
	s.clientsMu.Unlock()
}
