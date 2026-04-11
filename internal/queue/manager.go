package queue

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/store"
)

// Manager manages the outbound message queue.
//
// Admin API methods (GetStats, SetMaxRetries, SetMaxQueueSize, FlushQueue, RetryEntry, DropEntry)
// are implemented and exposed via /api/v1/admin/queue/* routes.
type Manager struct {
	db           *db.DB
	store        *store.MaildirStore
	dataDir      string
	resolver     DNSResolver
	running      atomic.Bool
	shutdown     chan struct{}
	stopOnce     sync.Once
	mu           sync.RWMutex
	metrics      *metrics.SimpleMetrics
	logger       *slog.Logger
	maxRetries   int
	maxQueueSize int
	requireTLS   bool

	// MX connection pool settings
	mxPoolSize     int           // max connections per MX host (default 10)
	mxIdleTimeout  time.Duration // idle connection timeout (default 5 min)

	// MX connection pools keyed by MX host
	mxPools map[string]*mxPool

	// MTA-STS validator for TLS policy enforcement
	mtastsValidator *auth.MTASTSValidator

	// DANE validator for TLS certificate validation
	daneValidator *auth.DANEValidator

	// dialSMTP, if set, is used instead of net.DialTimeout for testing.
	// It returns a net.Conn and is used by deliverToMX.
	dialSMTP func(addr string) (net.Conn, error)
}

// mxPool represents a connection pool for a single MX host
type mxPool struct {
	mu       sync.Mutex
	conns    []*mxConn // available connections
	addr     string    // MX host:port
	maxSize  int
	idleTimeout time.Duration
}

// mxConn wraps an SMTP client connection with metadata
type mxConn struct {
	client   *smtp.Client
	lastUsed time.Time
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

// DNSResolver handles DNS resolution for email delivery
type DNSResolver interface {
	LookupMX(domain string) ([]string, error)
}

// MTASTSDNSResolver handles DNS resolution for MTA-STS validation
type MTASTSDNSResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
	LookupMX(ctx context.Context, domain string) ([]*net.MX, error)
}

// realDNSResolver implements DNSResolver with real network calls
type realDNSResolver struct{}

func (r *realDNSResolver) LookupMX(domain string) ([]string, error) {
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

// realMTASTSDNSResolver implements MTASTSDNSResolver with real network calls.
// Note: LookupIP and LookupMX are stubs since MTA-STS and DANE validators
// only use LookupTXT. These exist only to satisfy the auth.DNSResolver interface.
type realMTASTSDNSResolver struct{}

func (r *realMTASTSDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.LookupTXT(name)
}

func (r *realMTASTSDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	// MTA-STS and DANE validators do not use LookupIP
	// This stub exists only to satisfy the auth.DNSResolver interface
	return nil, nil
}

func (r *realMTASTSDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	// MTA-STS and DANE validators do not use LookupMX
	// This stub exists only to satisfy the auth.DNSResolver interface
	return nil, nil
}

// NewManager creates a new queue manager
func NewManager(db *db.DB, store *store.MaildirStore, dataDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		db:              db,
		store:           store,
		dataDir:         dataDir,
		resolver:        &realDNSResolver{},
		shutdown:        make(chan struct{}),
		metrics:         metrics.Get(),
		logger:          logger,
		maxRetries:      len(retryDelays),
		maxQueueSize:    10000,
		mxPoolSize:      10,
		mxIdleTimeout:   5 * time.Minute,
		mxPools:         make(map[string]*mxPool),
		mtastsValidator: auth.NewMTASTSValidator(&realMTASTSDNSResolver{}),
		daneValidator:   auth.NewDANEValidator(&realMTASTSDNSResolver{}),
	}
}

// Start starts the queue manager
func (m *Manager) Start(ctx context.Context) {
	if m.running.Load() {
		return
	}
	m.running.Store(true)

	// Start queue processor
	go m.processQueue(ctx)
}

// Stop stops the queue manager
func (m *Manager) Stop() {
	if !m.running.Load() {
		return
	}
	m.running.Store(false)
	m.stopOnce.Do(func() { close(m.shutdown) })
}

// SetMTASTSDNSResolver sets the DNS resolver for MTA-STS validation (for testing)
func (m *Manager) SetMTASTSDNSResolver(resolver MTASTSDNSResolver) {
	m.mtastsValidator = auth.NewMTASTSValidator(resolver)
}

// SetDANEDNSResolver sets the DNS resolver for DANE validation (for testing)
func (m *Manager) SetDANEDNSResolver(resolver MTASTSDNSResolver) {
	m.daneValidator = auth.NewDANEValidator(resolver)
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
			// Roll back entries already created for this message
			for j := 0; j < i; j++ {
				rollbackID := fmt.Sprintf("%s-%d", baseID, j)
				m.db.Dequeue(rollbackID)
			}
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

// processPendingEntries processes pending queue entries with bounded concurrency
func (m *Manager) processPendingEntries() {
	entries, err := m.db.GetPendingQueue(time.Now())
	if err != nil {
		return
	}

	// Limit concurrent deliveries
	sem := make(chan struct{}, 20)
	var wg sync.WaitGroup

	for _, entry := range entries {
		select {
		case <-m.shutdown:
			wg.Wait()
			return
		default:
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(e *db.QueueEntry) {
			defer func() {
				<-sem
				wg.Done()
			}()
			m.deliver(e)
		}(entry)
	}
	wg.Wait()
}

// deliver attempts to deliver a message
func (m *Manager) deliver(entry *db.QueueEntry) {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("panic in delivery", "error", r, "to", entry.To)
		}
	}()

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

// getMXPool gets or creates a connection pool for the given MX host
func (m *Manager) getMXPool(mx string) *mxPool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool, ok := m.mxPools[mx]; ok {
		return pool
	}
	pool := &mxPool{
		addr:        mx,
		maxSize:     m.mxPoolSize,
		idleTimeout: m.mxIdleTimeout,
		conns:       make([]*mxConn, 0),
	}
	m.mxPools[mx] = pool
	return pool
}

// acquireMXConn acquires a connection from the pool or creates a new one.
// Returns (client, fromPool, error).
func (m *Manager) acquireMXConn(mx string) (*smtp.Client, bool, error) {
	pool := m.getMXPool(mx)

	pool.mu.Lock()
	defer pool.mu.Unlock()

	now := time.Now()

	// Try to find an idle connection that's still valid
	for i := len(pool.conns) - 1; i >= 0; i-- {
		conn := pool.conns[i]
		if now.Sub(conn.lastUsed) > pool.idleTimeout {
			// Connection expired, remove it
			conn.client.Close()
			pool.conns = append(pool.conns[:i], pool.conns[i+1:]...)
			continue
		}
		// Check if connection is still alive by doing a no-op
		if err := conn.client.Noop(); err == nil {
			// Valid connection found
			pool.conns = append(pool.conns[:i], pool.conns[i+1:]...)
			return conn.client, true, nil
		}
		// Connection dead, remove it
		conn.client.Close()
		pool.conns = append(pool.conns[:i], pool.conns[i+1:]...)
	}

	// No valid connection in pool, need to create new
	return nil, false, nil
}

// createMXConn creates a new SMTP connection to the given MX host
func (m *Manager) createMXConn(mx string) (*smtp.Client, error) {
	addr := mx + ":25"
	var conn net.Conn
	var err error
	if m.dialSMTP != nil {
		conn, err = m.dialSMTP(addr)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 30*time.Second)
	}
	if err != nil {
		return nil, err
	}

	client, err := smtp.NewClient(conn, mx)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return client, nil
}

// releaseMXConn returns a connection to the pool
func (m *Manager) releaseMXConn(mx string, client *smtp.Client, valid bool) {
	if client == nil {
		return
	}

	pool := m.getMXPool(mx)
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if !valid {
		// Connection is dead, close it
		client.Close()
		return
	}

	// Return to pool if not at capacity
	if len(pool.conns) < pool.maxSize {
		pool.conns = append(pool.conns, &mxConn{
			client:   client,
			lastUsed: time.Now(),
		})
	} else {
		// Pool full, close the connection
		client.Close()
	}
}

// deliverToMX delivers a message to a specific MX server
func (m *Manager) deliverToMX(from, to string, message []byte, mx string) error {
	// Check MTA-STS policy for recipient domain
	domain := extractDomain(to)
	if m.mtastsValidator != nil && domain != "" {
		allowed, policy, err := m.mtastsValidator.CheckPolicy(context.Background(), domain, mx)
		if err != nil {
			m.logger.Debug("MTA-STS check failed", "domain", domain, "mx", mx, "error", err)
		}
		if policy != nil && policy.Mode == auth.MTASTSModeEnforce && !allowed {
			return fmt.Errorf("MTA-STS policy violation: MX %s not allowed for domain %s", mx, domain)
		}
		if policy != nil && policy.Mode == auth.MTASTSModeEnforce {
			m.logger.Debug("MTA-STS policy enforced", "domain", domain, "mx", mx)
		}
	}

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

	// Connect to MX server (try pool first, then create new)
	client, fromPool, err := m.acquireMXConn(mx)
	if err != nil {
		return err
	}
	if fromPool {
		// Got pooled connection - verify it's still good with RSET (Reset)
		if err := client.Reset(); err != nil {
			// Connection bad, release it and create new
			m.releaseMXConn(mx, client, false)
			client, err = m.createMXConn(mx)
			if err != nil {
				return err
			}
		}
	} else {
		// Fresh connection, created inline
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

		newClient, err := smtp.NewClient(conn, mx)
		if err != nil {
			return err
		}
		client = newClient
	}

	// Attempt STARTTLS
	tlsConfig := &tls.Config{
		ServerName: mx,
		MinVersion: tls.VersionTLS12,
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		if m.requireTLS {
			return fmt.Errorf("STARTTLS required but failed: %w", err)
		}
		// STARTTLS failed — continue with plaintext only if not required
	} else {
		// STARTTLS succeeded — validate with DANE if available
		if m.daneValidator != nil {
			if state, ok := client.TLSConnectionState(); ok {
				result, daneErr := m.daneValidator.Validate(mx, 25, &state)
				if daneErr != nil {
					m.logger.Debug("DANE validation error", "mx", mx, "error", daneErr)
				} else if result == auth.DANEValidated {
					m.logger.Debug("DANE validation successful", "mx", mx)
				} else if result == auth.DANEFailed {
					m.logger.Warn("DANE validation failed", "mx", mx)
					// If DANE is configured but validation failed, reject the connection
					return fmt.Errorf("DANE validation failed for %s", mx)
				}
			}
		}
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
		// Return bad connection to pool (will be closed)
		m.releaseMXConn(mx, client, false)
		return err
	}

	// Successful delivery - return connection to pool for reuse
	// Skip QUIT since we're keeping the connection alive
	m.releaseMXConn(mx, client, true)
	return nil
}

// handleDeliverySuccess handles successful delivery
func (m *Manager) handleDeliverySuccess(entry *db.QueueEntry) {
	entry.Status = "delivered"
	m.db.UpdateQueueEntry(entry)

	// Send DSN if requested (NOTIFY includes SUCCESS)
	if entry.Notify != 0 && int(entry.Notify)&int(DSNNotifySuccess) != 0 {
		m.sendSuccessDSN(entry)
	}

	// Only delete message file when no other entries reference it
	m.deleteMessageFileIfUnreferenced(entry.MessagePath)

	// Track metric
	if m.metrics != nil {
		m.metrics.DeliverySuccess()
	}
}

// sendSuccessDSN sends a DSN success notification
func (m *Manager) sendSuccessDSN(entry *db.QueueEntry) {
	// Read original message for headers if needed (DSNRetFull = 0, DSNRetHeaders = 1)
	var originalMsg []byte
	if int(entry.Ret) == 0 { // DSNRetFull
		originalMsg, _ = readFile(entry.MessagePath)
	}

	dsn := &DSN{
		ReportedDomain: "umailserver",
		ReportedName:   "umailserver",
		ArrivalDate:    entry.CreatedAt,
		OriginalFrom:   entry.From,
		OriginalTo:     entry.To[0],
		Recipient: DSNRecipient{
			Original: entry.To[0],
			Notify:   DSNNotify(entry.Notify),
			Ret:      DSNRet(entry.Ret),
		},
		Action:    "delivered",
		Status:    "2.0.0",
		RemoteMTA: "unknown",
		FinalMTA:  "umailserver",
		MessageID: GenerateMessageID(),
	}

	dsnMsg, err := GenerateDSN(dsn, originalMsg, DSNRet(entry.Ret))
	if err != nil {
		m.logger.Error("failed to generate DSN", "error", err)
		return
	}

	// Enqueue DSN back to sender
	if _, err := m.Enqueue("MAILER-DAEMON@umailserver", []string{entry.From}, dsnMsg); err != nil {
		m.logger.Error("failed to enqueue DSN", "error", err)
	}
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
		idx := entry.RetryCount - 1
		if idx >= len(retryDelays) {
			idx = len(retryDelays) - 1 // Use last delay if we've exceeded the array
		}
		baseDelay := retryDelays[idx]
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
	// Check if we should send DSN (NOTIFY never means no bounce)
	if entry.Notify != 0 && int(entry.Notify)&int(DSNNotifyNever) != 0 {
		// NOTIFY=NEVER - don't send anything
		return
	}

	// Read original message
	originalMsg, err := readFile(entry.MessagePath)
	if err != nil {
		return
	}

	// Determine what to include based on RET parameter (DSNRetFull=0, DSNRetHeaders=1)
	var ret DSNRet
	if int(entry.Ret)&1 != 0 {
		ret = DSNRetHeaders
	} else {
		ret = DSNRetFull
	}

	dsn := &DSN{
		ReportedDomain: "umailserver",
		ReportedName:   "umailserver",
		ArrivalDate:    entry.CreatedAt,
		OriginalFrom:   entry.From,
		OriginalTo:     entry.To[0],
		Recipient: DSNRecipient{
			Original: entry.To[0],
			Notify:   DSNNotify(entry.Notify),
			Ret:      ret,
		},
		Action:         "failed",
		Status:         "5.0.0",
		DiagnosticCode: "smtp; " + entry.LastError,
		RemoteMTA:      "unknown",
		FinalMTA:       "umailserver",
		MessageID:      GenerateMessageID(),
	}

	// Generate proper DSN bounce message
	bounceMsg, err := GenerateDSN(dsn, originalMsg, ret)
	if err != nil {
		m.logger.Error("failed to generate DSN bounce", "error", err)
		// Fall back to old-style bounce
		bounceMsg = m.createFallbackBounce(entry, originalMsg)
	}

	// Enqueue bounce as a new message back to the sender
	if m.db != nil {
		if _, enqueueErr := m.Enqueue("MAILER-DAEMON@umailserver", []string{entry.From}, bounceMsg); enqueueErr != nil {
			m.logger.Error("failed to enqueue bounce message", "error", enqueueErr)
		}
	}

	// Only delete message file when no other entries reference it
	if m.db != nil {
		m.deleteMessageFileIfUnreferenced(entry.MessagePath)
	} else {
		deleteFile(entry.MessagePath)
	}
}

// createFallbackBounce creates a simple bounce message when DSN generation fails
func (m *Manager) createFallbackBounce(entry *db.QueueEntry, originalMsg []byte) []byte {
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
	return []byte(bounceMsg)
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
	stats := &QueueStats{}

	_, err := m.db.GetPendingQueue(time.Now().Add(24 * time.Hour))
	if err != nil {
		return stats, err
	}

	// Count all queue entries by status
	m.db.ForEach(db.BucketQueue, func(key string, value []byte) error {
		var entry db.QueueEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil // skip malformed entries
		}
		stats.Total++
		switch entry.Status {
		case "pending":
			stats.Pending++
		case "sending":
			stats.Sending++
		case "failed":
			stats.Failed++
		case "delivered":
			stats.Delivered++
		case "bounced":
			stats.Bounced++
		}
		return nil
	})

	return stats, nil
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

// SetRequireTLS enforces TLS for outbound deliveries.
func (m *Manager) SetRequireTLS(require bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requireTLS = require
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), b)
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

	// Sync temp file before rename to ensure data durability
	f, err := os.OpenFile(tmpPath, os.O_RDWR, 0)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	f.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Sync parent directory to ensure the rename is durable.
	// Directory sync may fail on Windows; file sync above is the critical part.
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	_ = dirFile.Sync()
	dirFile.Close()
	return nil
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func deleteFile(path string) {
	os.Remove(path)
}

// countMessageRefs counts how many queue entries still reference the given
// message file path. Used to avoid deleting a shared .msg file while other
// recipients still need it. Must be called with mu held for read.
func (m *Manager) countMessageRefs(messagePath string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	m.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
		var entry db.QueueEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil
		}
		if entry.MessagePath == messagePath {
			// Final-state entries (delivered/bounced) no longer need the file
			if entry.Status != "delivered" && entry.Status != "bounced" {
				count++
			}
		}
		return nil
	})
	return count
}

// deleteMessageFileIfUnreferenced removes the message file only when no queue
// entries reference it anymore. Returns true if the file was deleted.
func (m *Manager) deleteMessageFileIfUnreferenced(messagePath string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check count under write lock for atomic check-and-delete
	if m.countMessageRefsUnsafe(messagePath) == 0 {
		os.Remove(messagePath)
		return true
	}
	return false
}

// countMessageRefsUnsafe counts references without locking. Caller must hold mu.
func (m *Manager) countMessageRefsUnsafe(messagePath string) int {
	count := 0
	m.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
		var entry db.QueueEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil
		}
		if entry.MessagePath == messagePath {
			if entry.Status != "delivered" && entry.Status != "bounced" {
				count++
			}
		}
		return nil
	})
	return count
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
