package queue

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/rand"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/metrics"
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
	mu          sync.RWMutex
	metrics     *metrics.SimpleMetrics
	maxRetries  int
	maxQueueSize int

	// dialSMTP, if set, is used instead of net.DialTimeout for testing.
	// It returns a net.Conn and is used by deliverToMX.
	dialSMTP func(addr string) (net.Conn, error)
}

// QueueStats holds queue statistics
type QueueStats struct {
	Pending   int
	Sending   int
	Failed    int
	Delivered int
	Bounced   int
	Total     int
}

// Resolver handles DNS resolution for MX records
type Resolver struct{}

// NewManager creates a new queue manager
func NewManager(db *db.DB, store *store.MaildirStore, dataDir string) *Manager {
	return &Manager{
		db:           db,
		store:        store,
		dataDir:      dataDir,
		resolver:     &Resolver{},
		shutdown:     make(chan struct{}),
		metrics:      metrics.Get(),
		maxRetries:   len(retryDelays),
		maxQueueSize: 10000,
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
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check queue size limit
	stats, err := m.getStats()
	if err != nil {
		return "", fmt.Errorf("failed to get queue stats: %w", err)
	}
	if stats.Total >= m.maxQueueSize {
		return "", fmt.Errorf("queue is full (max %d entries)", m.maxQueueSize)
	}

	// Generate unique message ID
	id := generateID()

	// Create queue directory if not exists
	queueDir := filepath.Join(m.dataDir, "queue")
	if err := os.MkdirAll(queueDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create queue directory: %w", err)
	}

	// Store message on disk
	messagePath := filepath.Join(queueDir, id+".msg")
	if err := writeFile(messagePath, message); err != nil {
		return "", fmt.Errorf("failed to store message: %w", err)
	}

	// Create queue entries for each recipient
	now := time.Now()
	baseID := id

	for i, recipient := range to {
		// Unique ID per recipient
		entryID := fmt.Sprintf("%s-%d", baseID, i)

		entry := &db.QueueEntry{
			ID:          entryID,
			From:        from,
			To:          []string{recipient},
			MessagePath: messagePath,
			CreatedAt:   now,
			NextRetry:   now,
			RetryCount:  0,
			Status:      "pending",
		}

		if err := m.db.Enqueue(entry); err != nil {
			// Clean up message file on failure
			deleteFile(messagePath)
			return "", fmt.Errorf("failed to enqueue: %w", err)
		}
	}

	// Track metric
	if m.metrics != nil {
		// Queue enqueue metric would go here
	}

	return baseID, nil
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
	// Sign message with DKIM if possible
	signedMsg, err := m.signWithDKIM(from, message)
	if err == nil && len(signedMsg) > 0 {
		message = signedMsg
	}

	// Use VERP-encoded envelope sender for bounce tracking
	envelopeSender := from
	if at := strings.LastIndex(from, "@"); at >= 0 {
		senderDomain := from[at+1:]
		verpSender := EncodeVERP(senderDomain, to)
		if verpSender != "" {
			envelopeSender = verpSender
		}
	}

	// Connect to MX server
	addr := mx + ":25"
	var conn net.Conn
	if m.dialSMTP != nil {
		conn, err = m.dialSMTP(addr)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 30*time.Second)
	}
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

	// Attempt STARTTLS
	tlsConfig := &tls.Config{
		ServerName: mx,
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		// STARTTLS failed — continue with plaintext
		// Some servers don't support TLS
	}

	// Set sender (VERP-encoded for bounce tracking)
	if err := client.Mail(envelopeSender); err != nil {
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

// handleDeliveryFailure handles delivery failure with exponential backoff and jitter
func (m *Manager) handleDeliveryFailure(entry *db.QueueEntry, errorMsg string) {
	entry.LastError = errorMsg
	entry.RetryCount++

	// Check if max retries reached
	if entry.RetryCount >= m.maxRetries {
		// Generate bounce
		entry.Status = "bounced"
		m.generateBounce(entry)
	} else {
		// Calculate retry delay with jitter (±20%)
		baseDelay := retryDelays[entry.RetryCount-1]
		jitter := time.Duration(float64(baseDelay) * (0.8 + 0.4*rand.Float64()))
		entry.NextRetry = time.Now().Add(jitter)
		entry.Status = "pending"
	}

	m.db.UpdateQueueEntry(entry)

	// Track metric
	if m.metrics != nil {
		m.metrics.DeliveryFailed()
	}
}

// generateBounce generates a bounce message and delivers it back to the sender
func (m *Manager) generateBounce(entry *db.QueueEntry) {
	// Read original message
	originalMsg, err := readFile(entry.MessagePath)
	if err != nil {
		return
	}

	// Create bounce message
	bounceMsg := fmt.Sprintf(
		"From: MAILER-DAEMON@umailserver\r\n"+
			"To: %s\r\n"+
			"Subject: Delivery Status Notification (Failure)\r\n"+
			"Content-Type: multipart/report; report-type=delivery-status; boundary=boundary\r\n"+
			"Date: %s\r\n"+
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
			"Reporting-MTA: dns; umailserver\r\n"+
			"Arrival-Date: %s\r\n"+
			"\r\n"+
			"Final-Recipient: rfc822; %s\r\n"+
			"Action: failed\r\n"+
			"Status: 5.0.0\r\n"+
			"Diagnostic-Code: smtp; %s\r\n"+
			"\r\n"+
			"--boundary\r\n"+
			"Content-Type: message/rfc822\r\n"+
			"\r\n"+
			"%s"+
			"\r\n--boundary--\r\n",
		entry.From,
		time.Now().Format(time.RFC1123Z),
		entry.To[0],
		entry.LastError,
		entry.CreatedAt.Format(time.RFC1123Z),
		entry.To[0],
		entry.LastError,
		string(originalMsg),
	)

	// Enqueue bounce as a new message back to the sender
	if m.db != nil {
		if _, enqueueErr := m.Enqueue("MAILER-DAEMON@umailserver", []string{entry.From}, []byte(bounceMsg)); enqueueErr != nil {
			fmt.Printf("failed to enqueue bounce message: %v\n", enqueueErr)
		}
	}

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

// GetStats returns queue statistics
func (m *Manager) GetStats() (*QueueStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getStats()
}

// getStats is the internal version without locking
func (m *Manager) getStats() (*QueueStats, error) {
	// In a real implementation, this would query the database
	// For now, return empty stats
	return &QueueStats{}, nil
}

// SetMaxRetries sets the maximum number of retry attempts
func (m *Manager) SetMaxRetries(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxRetries = max
}

// SetMaxQueueSize sets the maximum queue size
func (m *Manager) SetMaxQueueSize(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxQueueSize = max
}

func generateID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Intn(10000))
}

func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write to temp file first, then rename for atomicity
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func deleteFile(path string) {
	os.Remove(path)
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

// signWithDKIM signs an outgoing message with DKIM if the sender's domain has a DKIM key configured.
func (m *Manager) signWithDKIM(from string, message []byte) ([]byte, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Extract sender domain
	senderDomain := extractDomain(from)
	if senderDomain == "" {
		return nil, fmt.Errorf("cannot extract domain from sender: %s", from)
	}

	// Look up domain's DKIM key
	domain, err := m.db.GetDomain(senderDomain)
	if err != nil {
		return nil, fmt.Errorf("domain %s not found: %w", senderDomain, err)
	}
	if domain.DKIMPrivateKey == "" || domain.DKIMSelector == "" {
		return nil, fmt.Errorf("no DKIM key configured for domain %s", senderDomain)
	}

	// Parse private key
	block, _ := pem.Decode([]byte(domain.DKIMPrivateKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode DKIM private key PEM")
	}

	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 as fallback
		key, err8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err8 != nil {
			return nil, fmt.Errorf("failed to parse DKIM private key: %w (pkcs1: %w)", err8, err)
		}
		var ok bool
		rsaKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("DKIM private key is not RSA")
		}
	}

	// Parse message headers and body
	headers := parseMessageHeaders(message)
	body := extractMessageBody(message)

	// Create signer and sign
	dnsResolver := &dkimDNSResolver{}
	signer := auth.NewDKIMSigner(dnsResolver, rsaKey, senderDomain, domain.DKIMSelector)
	signature, err := signer.Sign(headers, body)
	if err != nil {
		return nil, fmt.Errorf("DKIM signing failed: %w", err)
	}

	// Prepend DKIM-Signature header to the message
	dkimHeader := signature + "\r\n"
	signedMessage := append([]byte(dkimHeader), message...)
	return signedMessage, nil
}

// parseMessageHeaders parses the headers from a raw email message into a map
func parseMessageHeaders(message []byte) map[string][]string {
	headers := make(map[string][]string)
	reader := bytes.NewReader(message)
	msg, err := mail.ReadMessage(reader)
	if err != nil {
		return headers
	}
	return msg.Header
}

// extractMessageBody extracts the body portion after the header separator
func extractMessageBody(message []byte) []byte {
	// Find the blank line separating headers from body
	idx := bytes.Index(message, []byte("\r\n\r\n"))
	if idx >= 0 {
		return message[idx+4:]
	}
	idx = bytes.Index(message, []byte("\n\n"))
	if idx >= 0 {
		return message[idx+2:]
	}
	return nil
}

// dkimDNSResolver implements auth.DNSResolver for the queue package
type dkimDNSResolver struct{}

func (r *dkimDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.LookupTXT(name)
}

func (r *dkimDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.LookupIP(host)
}

func (r *dkimDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	return net.LookupMX(domain)
}
