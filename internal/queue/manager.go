package queue

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"path/filepath"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/store"
)

// Manager manages the outbound message queue
type Manager struct {
	db          *db.DB
	store       *store.MaildirStore
	dataDir     string
	resolver    *Resolver
	running     bool
	shutdown    chan struct{}
}

// Resolver handles DNS resolution for MX records
type Resolver struct{}

// NewManager creates a new queue manager
func NewManager(db *db.DB, store *store.MaildirStore, dataDir string) *Manager {
	return &Manager{
		db:       db,
		store:    store,
		dataDir:  dataDir,
		resolver: &Resolver{},
		shutdown: make(chan struct{}),
	}
}

// Start starts the queue manager
func (m *Manager) Start(ctx context.Context) {
	if m.running {
		return
	}
	m.running = true

	// Start queue processor
	go m.processQueue(ctx)
}

// Stop stops the queue manager
func (m *Manager) Stop() {
	if !m.running {
		return
	}
	m.running = false
	close(m.shutdown)
}

// Enqueue adds a message to the outbound queue
func (m *Manager) Enqueue(from string, to []string, message []byte) (string, error) {
	// Generate unique message ID
	id := fmt.Sprintf("%d-%s", time.Now().UnixNano(), generateID())

	// Store message on disk
	messagePath := filepath.Join(m.dataDir, "queue", id+".msg")
	if err := writeFile(messagePath, message); err != nil {
		return "", fmt.Errorf("failed to store message: %w", err)
	}

	// Create queue entries for each recipient
	now := time.Now()
	for _, recipient := range to {
		entry := &db.QueueEntry{
			ID:          id + "-" + recipient,
			From:        from,
			To:          []string{recipient},
			MessagePath: messagePath,
			CreatedAt:   now,
			NextRetry:   now,
			RetryCount:  0,
			Status:      "pending",
		}

		if err := m.db.Enqueue(entry); err != nil {
			return "", fmt.Errorf("failed to enqueue: %w", err)
		}
	}

	return id, nil
}

// GetQueueEntry retrieves a queue entry by ID
func (m *Manager) GetQueueEntry(id string) (*db.QueueEntry, error) {
	return m.db.GetQueueEntry(id)
}

// GetPendingEntries returns all pending queue entries
func (m *Manager) GetPendingEntries() ([]*db.QueueEntry, error) {
	return m.db.GetPendingQueue(time.Now().Add(time.Hour))
}

// RetryEntry schedules an entry for immediate retry
func (m *Manager) RetryEntry(id string) error {
	entry, err := m.db.GetQueueEntry(id)
	if err != nil {
		return err
	}

	entry.Status = "pending"
	entry.NextRetry = time.Now()
	entry.RetryCount = 0
	entry.LastError = ""

	return m.db.UpdateQueueEntry(entry)
}

// DropEntry removes an entry from the queue
func (m *Manager) DropEntry(id string) error {
	return m.db.Dequeue(id)
}

// FlushQueue retries all failed entries
func (m *Manager) FlushQueue() error {
	// Get all entries and retry them
	entries, err := m.GetPendingEntries()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Status == "failed" {
			if err := m.RetryEntry(entry.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

// processQueue processes the queue
func (m *Manager) processQueue(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.shutdown:
			return
		case <-ticker.C:
			m.processPendingEntries()
		}
	}
}

// processPendingEntries processes pending queue entries
func (m *Manager) processPendingEntries() {
	entries, err := m.db.GetPendingQueue(time.Now())
	if err != nil {
		return
	}

	for _, entry := range entries {
		go m.deliver(entry)
	}
}

// deliver attempts to deliver a message
func (m *Manager) deliver(entry *db.QueueEntry) {
	// Update status to sending
	entry.Status = "sending"
	m.db.UpdateQueueEntry(entry)

	// Read message from disk
	message, err := readFile(entry.MessagePath)
	if err != nil {
		m.handleDeliveryFailure(entry, fmt.Sprintf("failed to read message: %v", err))
		return
	}

	// Get recipient domain
	domain := extractDomain(entry.To[0])
	if domain == "" {
		m.handleDeliveryFailure(entry, "invalid recipient domain")
		return
	}

	// Look up MX records
	mxRecords, err := m.resolver.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		// Fall back to A record
		mxRecords = []string{domain}
	}

	// Try each MX server
	delivered := false
	var lastErr string

	for _, mx := range mxRecords {
		if err := m.deliverToMX(entry.From, entry.To[0], message, mx); err != nil {
			lastErr = err.Error()
			continue
		}

		delivered = true
		break
	}

	if delivered {
		m.handleDeliverySuccess(entry)
	} else {
		m.handleDeliveryFailure(entry, lastErr)
	}
}

// deliverToMX delivers a message to a specific MX server
func (m *Manager) deliverToMX(from, to string, message []byte, mx string) error {
	// Connect to MX server
	addr := mx + ":25"
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send message using SMTP
	client, err := smtp.NewClient(conn, mx)
	if err != nil {
		return err
	}
	defer client.Close()

	// Set sender
	if err := client.Mail(from); err != nil {
		return err
	}

	// Set recipient
	if err := client.Rcpt(to); err != nil {
		return err
	}

	// Send data
	w, err := client.Data()
	if err != nil {
		return err
	}

	_, err = w.Write(message)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	// Quit
	return client.Quit()
}

// handleDeliverySuccess handles successful delivery
func (m *Manager) handleDeliverySuccess(entry *db.QueueEntry) {
	entry.Status = "delivered"
	m.db.UpdateQueueEntry(entry)

	// Clean up message file
	deleteFile(entry.MessagePath)
}

// handleDeliveryFailure handles delivery failure
func (m *Manager) handleDeliveryFailure(entry *db.QueueEntry, errorMsg string) {
	entry.LastError = errorMsg
	entry.RetryCount++

	// Check if max retries reached
	if entry.RetryCount >= len(retryDelays) {
		// Generate bounce
		entry.Status = "bounced"
		m.generateBounce(entry)
	} else {
		// Schedule retry
		delay := retryDelays[entry.RetryCount-1]
		entry.NextRetry = time.Now().Add(delay)
		entry.Status = "pending"
	}

	m.db.UpdateQueueEntry(entry)
}

// generateBounce generates a bounce message
func (m *Manager) generateBounce(entry *db.QueueEntry) {
	// Read original message
	_, err := readFile(entry.MessagePath)
	if err != nil {
		return
	}

	// Create bounce message
	bounceMsg := fmt.Sprintf(
		"From: MAILER-DAEMON@%s\r\n"+
		"To: %s\r\n"+
		"Subject: Delivery Status Notification (Failure)\r\n"+
		"Content-Type: multipart/report; report-type=delivery-status; boundary=boundary\r\n"+
		"\r\n"+
		"--boundary\r\n"+
		"Content-Type: text/plain\r\n"+
		"\r\n"+
		"Your message could not be delivered to: %s\r\n"+
		"Error: %s\r\n"+
		"\r\n"+
		"--boundary\r\n"+
		"Content-Type: message/delivery-status\r\n"+
		"\r\n"+
		"Action: failed\r\n"+
		"Status: 5.0.0\r\n"+
		"\r\n"+
		"--boundary--\r\n",
		"umailserver",
		entry.From,
		entry.To[0],
		entry.LastError,
	)

	// Store bounce in sender's inbox (simplified)
	// In real implementation, this would deliver to the local mailbox
	_ = bounceMsg

	// Clean up
	deleteFile(entry.MessagePath)
}

// Retry delays for exponential backoff
// Schedule: 5m, 15m, 30m, 1h, 2h, 4h, 8h, 16h, 24h, 48h
var retryDelays = []time.Duration{
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	2 * time.Hour,
	4 * time.Hour,
	8 * time.Hour,
	16 * time.Hour,
	24 * time.Hour,
	48 * time.Hour,
}

// Helper functions

func generateID() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func writeFile(path string, data []byte) error {
	// Simplified - in production use proper file operations
	return nil
}

func readFile(path string) ([]byte, error) {
	// Simplified - in production use proper file operations
	return []byte("test message"), nil
}

func deleteFile(path string) {
	// Simplified
}

// LookupMX looks up MX records for a domain
func (r *Resolver) LookupMX(domain string) ([]string, error) {
	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		return nil, err
	}

	var records []string
	for _, mx := range mxRecords {
		records = append(records, mx.Host)
	}

	return records, nil
}
