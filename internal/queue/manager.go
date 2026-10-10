package queue

import (
	"bytes"
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/circuitbreaker"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/store"
	"github.com/umailserver/umailserver/internal/tracing"

	"go.opentelemetry.io/otel/trace"
)

// WebhookTrigger is the interface for triggering webhook events
type WebhookTrigger interface {
	Trigger(eventType string, data interface{})
}

// Manager manages the outbound message queue.
//
// Admin API methods (GetStats, SetMaxRetries, SetMaxQueueSize, FlushQueue, RetryEntry, DropEntry)
// are implemented and exposed via /api/v1/admin/queue/* routes.
type Manager struct {
	db           *db.DB
	store        *store.MaildirStore
	dataDir      string
	queueDir     string // directory for queue message files; defaults to dataDir
	resolver     DNSResolver
	running      atomic.Bool
	shutdown     chan struct{}
	stopOnce     sync.Once
	shutMu       sync.RWMutex   // guards the shutdown channel variable
	lifeMu       sync.Mutex     // guards Start/Stop and the shutdown channel (F5904)
	stopped      bool           // Stop closed shutdown; Start must make a new one
	workers      sync.WaitGroup // delivery workers and the sweeper of the current run
	mu           sync.RWMutex
	claimMu      sync.Mutex // serializes the pending->sending claim in deliver (F4927)
	metrics      *metrics.SimpleMetrics
	logger       *slog.Logger
	maxRetries   int
	maxQueueSize int
	requireTLS   bool
	helloName    string         // EHLO name for outbound connections (F5680)
	webhook      WebhookTrigger // optional webhook trigger for delivery events

	// Worker pool settings
	workerCount  int                 // number of delivery workers (default 10)
	deliveryChan chan *db.QueueEntry // direct delivery channel

	// Per-destination-domain concurrency cap (F6020): at most maxPerDomain
	// deliveries to one recipient domain run at once so a slow domain cannot
	// occupy every worker. 0 disables the cap.
	maxPerDomain int
	domMu        sync.Mutex
	domActive    map[string]int

	// ioTimeout bounds every single read or write on an outbound SMTP
	// connection (F6021). 0 disables it.
	ioTimeout time.Duration

	// MX connection pool settings
	mxPoolSize    int           // max connections per MX host (default 10)
	mxIdleTimeout time.Duration // idle connection timeout (default 5 min)

	// MX connection pools keyed by MX host
	mxPools map[string]*mxPool

	// MTA-STS validator for TLS policy enforcement
	mtastsValidator *auth.MTASTSValidator

	// DANE validator for TLS certificate validation
	daneValidator *auth.DANEValidator

	// daneSecure reports whether TLSA answers for an MX host were DNSSEC
	// authenticated (resolver AD bit or a configured validating resolver).
	// nil means no validating resolver: DANE stays advisory (RFC 7672 8.1).
	daneSecure func(mx string) bool

	// tlsRootCAs overrides the system roots for peer verification (tests,
	// private CAs); nil uses the system pool.
	tlsRootCAs *x509.CertPool

	// TLS-RPT style failure counters (RFC 8460), keyed "domain|result-type".
	tlsrptMu       sync.Mutex
	tlsrptFailures map[string]int

	// Per-MX-host circuit breakers (keyed like mxPools, guarded by mu). One
	// breaker per host: a shared breaker let one dead destination block
	// delivery to every other domain (F5155). mxBreaker is the configuration
	// each host's breaker is created with; nil disables breaking.
	mxBreaker  *circuitbreaker.Config
	mxBreakers map[string]*circuitbreaker.CircuitBreaker

	// dialSMTP, if set, is used instead of net.DialTimeout for testing.
	// It returns a net.Conn and is used by deliverToMX.
	dialSMTP func(addr string) (net.Conn, error)

	// tracingProvider, if set, emits queue.deliver and queue.deliver.mx spans.
	tracingProvider *tracing.Provider
}

// SetTracingProvider attaches an OpenTelemetry tracing provider so each
// delivery attempt and per-MX submission emits a span. A nil provider disables
// tracing without overhead.
func (m *Manager) SetTracingProvider(provider *tracing.Provider) {
	m.tracingProvider = provider
}

// mxPool represents a connection pool for a single MX host
type mxPool struct {
	mu          sync.Mutex
	conns       []*mxConn // available connections
	addr        string    // MX host:port
	maxSize     int
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
		// net.LookupMX returns absolute names ("mx.example.com."); MTA-STS
		// patterns and TLS names are written without the root dot (F5157).
		// A null MX (RFC 7505) stays ".".
		host := mx.Host
		if host != "." {
			host = strings.TrimSuffix(host, ".")
		}
		records = append(records, host)
	}
	return records, nil
}

// realMTASTSDNSResolver implements MTASTSDNSResolver with real network calls.
type realMTASTSDNSResolver struct{}

func (r *realMTASTSDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.LookupTXT(name)
}

func (r *realMTASTSDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	// The MTA-STS validator resolves mta-sts.<domain> to refuse private
	// addresses before fetching the policy over HTTPS. A stub returning an
	// error here made every policy fetch fail, so MTA-STS never applied.
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func (r *realMTASTSDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	return net.DefaultResolver.LookupMX(ctx, domain)
}

// NewManager creates a new queue manager
const (
	// defaultMaxPerDomain leaves at least half of the default 10 workers free
	// for other domains.
	defaultMaxPerDomain = 5
	// defaultIOTimeout is the RFC 5321 §4.5.3.2 five minute command timeout.
	defaultIOTimeout = 5 * time.Minute
)

// deadlineConn refreshes the connection deadline before every Read and Write,
// so a peer that stops answering (tarpit) fails the delivery instead of
// holding a worker forever (F6021).
type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c *deadlineConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
	return c.Conn.Read(p)
}

func (c *deadlineConn) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.timeout))
	return c.Conn.Write(p)
}

// SetMaxConcurrentPerDomain caps simultaneous deliveries to one recipient
// domain; 0 disables the cap.
func (m *Manager) SetMaxConcurrentPerDomain(n int) {
	m.domMu.Lock()
	defer m.domMu.Unlock()
	m.maxPerDomain = n
}

// SetIOTimeout sets the per-operation read/write timeout of outbound SMTP
// connections; 0 disables it.
func (m *Manager) SetIOTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ioTimeout = d
}

// acquireDomainSlot reserves one delivery slot for domain.
func (m *Manager) acquireDomainSlot(domain string) bool {
	m.domMu.Lock()
	defer m.domMu.Unlock()
	if m.maxPerDomain <= 0 {
		return true
	}
	if m.domActive == nil {
		m.domActive = make(map[string]int)
	}
	if m.domActive[domain] >= m.maxPerDomain {
		return false
	}
	m.domActive[domain]++
	return true
}

func (m *Manager) releaseDomainSlot(domain string) {
	m.domMu.Lock()
	defer m.domMu.Unlock()
	if n := m.domActive[domain]; n > 1 {
		m.domActive[domain] = n - 1
	} else {
		delete(m.domActive, domain)
	}
}

// requeueLater hands a skipped entry back to the workers shortly. The entry is
// still pending, so a lost hand-off is recovered by the sweeper.
func (m *Manager) requeueLater(entry *db.QueueEntry) {
	time.AfterFunc(time.Second, func() {
		select {
		case <-m.shutdownCh():
		case m.deliveryChan <- entry:
		default:
		}
	})
}

func NewManager(db *db.DB, store *store.MaildirStore, dataDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	breakerCfg := circuitbreaker.DefaultConfig()
	return &Manager{
		db:              db,
		store:           store,
		dataDir:         dataDir,
		queueDir:        dataDir,
		resolver:        &realDNSResolver{},
		shutdown:        make(chan struct{}),
		metrics:         metrics.Get(),
		logger:          logger,
		maxRetries:      len(retryDelays) + 1, // N delays separate N+1 attempts (F5901)
		maxQueueSize:    10000,
		workerCount:     10,
		maxPerDomain:    defaultMaxPerDomain,
		ioTimeout:       defaultIOTimeout,
		domActive:       make(map[string]int),
		mxPoolSize:      10,
		mxIdleTimeout:   5 * time.Minute,
		mxPools:         make(map[string]*mxPool),
		mtastsValidator: auth.NewMTASTSValidator(&realMTASTSDNSResolver{}),
		daneValidator:   auth.NewDANEValidator(&realMTASTSDNSResolver{}),
		mxBreaker:       &breakerCfg,
		mxBreakers:      make(map[string]*circuitbreaker.CircuitBreaker),
	}
}

// shutdownCh returns the current shutdown channel; Start replaces it when the
// manager is restarted after Stop.
func (m *Manager) shutdownCh() chan struct{} {
	m.shutMu.RLock()
	defer m.shutMu.RUnlock()
	return m.shutdown
}

// Start starts the queue manager. It is safe to call concurrently and again
// after Stop (F5904).
func (m *Manager) Start(ctx context.Context) {
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if m.running.Load() {
		return
	}
	// Workers of the previous run may still be finishing an in-flight
	// delivery; requeueing "sending" entries before they end would deliver
	// those twice.
	m.workers.Wait()
	if m.stopped || m.shutdown == nil {
		m.shutMu.Lock()
		m.shutdown = make(chan struct{})
		m.shutMu.Unlock()
		m.stopOnce = sync.Once{}
		m.stopped = false
	}
	m.running.Store(true)

	// A "sending" entry at startup was in flight when the previous process
	// stopped or crashed; nothing else would ever pick it up again (F4926).
	m.requeueInterruptedEntries()

	// Create delivery channel and start worker pool. An existing channel is
	// kept on restart: Enqueue may be sending to it concurrently.
	if m.deliveryChan == nil {
		m.deliveryChan = make(chan *db.QueueEntry, m.workerCount*2)
	}
	m.workers.Add(m.workerCount + 1)
	for i := 0; i < m.workerCount; i++ {
		go func(i int) {
			defer m.workers.Done()
			m.deliveryWorker(ctx, i)
		}(i)
	}

	// Start periodic queue sweeper for retry entries
	go func() {
		defer m.workers.Done()
		m.queueSweeper(ctx)
	}()
}

// requeueInterruptedEntries makes entries left in "sending" by an interrupted
// delivery eligible again. It runs before any worker starts, so no entry can
// be in flight (bbolt allows one process per database file).
func (m *Manager) requeueInterruptedEntries() {
	if m.db == nil {
		return
	}
	var ids []string
	_ = m.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
		var entry db.QueueEntry
		if json.Unmarshal(value, &entry) == nil && entry.Status == "sending" {
			ids = append(ids, entry.ID)
		}
		return nil
	})
	for _, id := range ids {
		entry, err := m.db.GetQueueEntry(id)
		if err != nil || entry.Status != "sending" {
			continue
		}
		entry.Status = "pending"
		entry.NextRetry = time.Now()
		if err := m.db.UpdateQueueEntry(entry); err != nil {
			m.logger.Error("failed to requeue interrupted delivery", "error", err, "id", id)
		}
	}
}

// Stop stops the queue manager
func (m *Manager) Stop() {
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if !m.running.Load() {
		return
	}
	m.running.Store(false)
	m.stopped = true
	m.stopOnce.Do(func() {
		// Signal workers to stop. deliveryChan is intentionally NOT closed:
		// Enqueue (called by generateBounce on workers and by mail acceptance)
		// and sweepPendingEntries may still send — a send on a closed channel
		// panics even inside select-with-default, crashing the process during
		// graceful shutdown. Entries sent around shutdown simply stay pending
		// in the database and are retried on the next Start.
		close(m.shutdown)
	})
}

// SetMTASTSDNSResolver sets the DNS resolver for MTA-STS validation (for testing)
func (m *Manager) SetMTASTSDNSResolver(resolver MTASTSDNSResolver) {
	m.mtastsValidator = auth.NewMTASTSValidator(resolver)
}

// SetDANEDNSResolver sets the DNS resolver for DANE validation (for testing)
func (m *Manager) SetDANEDNSResolver(resolver MTASTSDNSResolver) {
	m.daneValidator = auth.NewDANEValidator(resolver)
}

// SetHelloName sets the fully qualified name announced in EHLO on outbound
// connections; it should match the server's PTR record (RFC 5321 §4.1.4).
func (m *Manager) SetHelloName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.helloName = name
}

// helloNameFor returns the EHLO name for a new outbound connection: the
// configured name, else a dotted OS hostname, else the local address literal
// (RFC 5321 §4.1.3). net/smtp's default "localhost" is refused by many MTAs
// (F5680).
func (m *Manager) helloNameFor(conn net.Conn) string {
	m.mu.RLock()
	name := m.helloName
	m.mu.RUnlock()
	if name != "" {
		return name
	}
	host, hostErr := os.Hostname()
	if hostErr == nil && strings.Contains(host, ".") {
		return host
	}
	if ta, ok := conn.LocalAddr().(*net.TCPAddr); ok && ta.IP != nil {
		if ip4 := ta.IP.To4(); ip4 != nil {
			return "[" + ip4.String() + "]"
		}
		return "[IPv6:" + ta.IP.String() + "]"
	}
	if hostErr == nil && host != "" {
		return host
	}
	return "localhost"
}

// SetWebhookTrigger sets the webhook trigger for delivery events
func (m *Manager) SetWebhookTrigger(w WebhookTrigger) {
	m.webhook = w
}

// Enqueue adds a message to the outbound queue
// EnqueueWithNotify enqueues a message with per-recipient DSN notify preferences.
// Each element of notify corresponds to the same index in to. An empty string
// means the sender has no preference (bounce on permanent failure per RFC 3461).
func (m *Manager) EnqueueWithNotify(from string, to []string, notify []string, message []byte) (string, error) {
	if len(to) == 0 {
		return "", fmt.Errorf("cannot enqueue message without recipients")
	}
	id := generateID()
	queueDir := m.queueDir
	if queueDir == "" {
		queueDir = "."
	}
	messagePath := filepath.Join(queueDir, id+".msg")
	// Atomic temp+rename write: a failed write (disk full) must not leave a
	// truncated .msg behind that a later delivery would send (F6023).
	if err := writeFile(messagePath, message); err != nil {
		return "", fmt.Errorf("failed to write message file: %w", err)
	}
	baseID := id

	// Build per-recipient entries with their notify flags.
	// If notify is shorter than to, missing entries default to 0 (sender has no preference).
	entries := make([]*db.QueueEntry, len(to))
	for i, recipient := range to {
		var dsnNotify DSNNotify
		if i < len(notify) && notify[i] != "" {
			dsnNotify = ParseDSNNotify(notify[i])
		}
		entries[i] = &db.QueueEntry{
			ID:          fmt.Sprintf("%s-%d", baseID, i),
			From:        from,
			To:          []string{recipient},
			MessagePath: messagePath,
			CreatedAt:   time.Now(),
			NextRetry:   time.Now(),
			RetryCount:  0,
			Status:      "pending",
			Notify:      db.DSNNotify(dsnNotify),
		}
	}

	now := time.Now()
	for i, entry := range entries {
		entry.CreatedAt = now
		entry.NextRetry = now
		if err := m.db.EnqueueWithLimit(entry, m.maxQueueSize); err != nil {
			for j := 0; j < i; j++ {
				rollbackID := fmt.Sprintf("%s-%d", baseID, j)
				_ = m.db.Dequeue(rollbackID)
			}
			deleteFile(messagePath)
			return "", fmt.Errorf("failed to enqueue: %w", err)
		}
	}
	// Dispatch only once every recipient is stored: a worker that delivered
	// recipient 0 before recipient 1 hit the queue limit would deliver to a
	// recipient whose enqueue was reported as failed, and the caller's retry
	// would duplicate it (F6024).
	m.dispatchEntries(entries)

	return baseID, nil
}

// dispatchEntries hands stored entries to the workers; a full channel leaves
// them to the sweeper.
func (m *Manager) dispatchEntries(entries []*db.QueueEntry) {
	for _, entry := range entries {
		select {
		case m.deliveryChan <- entry:
		default:
			m.logger.Warn("delivery channel full, entry will be retried by sweeper", "id", entry.ID, "to", entry.To)
		}
	}
}

func (m *Manager) Enqueue(from string, to []string, message []byte) (string, error) {
	if len(to) == 0 {
		return "", fmt.Errorf("cannot enqueue message without recipients")
	}
	// Generate unique message ID and write to disk outside the lock
	id := generateID()

	queueDir := filepath.Join(m.dataDir, "queue")
	if err := os.MkdirAll(queueDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create queue directory: %w", err)
	}

	messagePath := filepath.Join(queueDir, id+".msg")
	if err := writeFile(messagePath, message); err != nil {
		return "", fmt.Errorf("failed to store message: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	baseID := id

	entries := make([]*db.QueueEntry, 0, len(to))
	for i, recipient := range to {
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
		entries = append(entries, entry)

		// EnqueueWithLimit performs an atomic check-and-set inside a single
		// bbolt transaction, eliminating the race between getStats and Enqueue.
		if err := m.db.EnqueueWithLimit(entry, m.maxQueueSize); err != nil {
			for j := 0; j < i; j++ {
				rollbackID := fmt.Sprintf("%s-%d", baseID, j)
				_ = m.db.Dequeue(rollbackID)
			}
			deleteFile(messagePath)
			return "", fmt.Errorf("failed to enqueue: %w", err)
		}
	}
	// Dispatch only once every recipient is stored: a worker that delivered
	// recipient 0 before recipient 1 hit the queue limit would deliver to a
	// recipient whose enqueue was reported as failed, and the caller's retry
	// would duplicate it (F6024).
	m.dispatchEntries(entries)

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

	if err := m.db.UpdateQueueEntry(entry); err != nil {
		return err
	}

	// Send to delivery channel for immediate retry
	select {
	case m.deliveryChan <- entry:
	default:
		// Channel full, sweeper will handle it
	}

	return nil
}

// DropEntry removes an entry from the queue
func (m *Manager) DropEntry(id string) error {
	entry, err := m.db.GetQueueEntry(id)
	if err != nil {
		return m.db.Dequeue(id)
	}
	if err := m.db.Dequeue(id); err != nil {
		return err
	}
	m.deleteMessageFileIfUnreferenced(entry.MessagePath)
	return nil
}

// FlushQueue retries all failed entries
func (m *Manager) FlushQueue() error {
	// Collect failed IDs before retrying so writes happen outside the read transaction.
	var failedIDs []string
	err := m.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
		var entry db.QueueEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return err
		}
		if entry.Status == "failed" {
			failedIDs = append(failedIDs, entry.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, id := range failedIDs {
		if err := m.RetryEntry(id); err != nil {
			return err
		}
	}

	return nil
}

// queueSweeper periodically picks up entries that need retry
// and sends them to the delivery channel for processing.
func (m *Manager) queueSweeper(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.shutdownCh():
			return
		case <-ticker.C:
			m.sweepPendingEntries()
		}
	}
}

// sweepPendingEntries picks up entries ready for delivery and sends them to workers
func (m *Manager) sweepPendingEntries() {
	entries, err := m.db.GetPendingQueue(time.Now())
	if err != nil {
		return
	}

	for _, entry := range entries {
		select {
		case <-m.shutdownCh():
			return
		case m.deliveryChan <- entry:
			// Sent to worker
		default:
			// Channel full, try next entry - we'll get them on next sweep
		}
	}
}

// deliveryWorker is a persistent worker that processes entries from the delivery channel
func (m *Manager) deliveryWorker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.shutdownCh():
			return
		case entry, ok := <-m.deliveryChan:
			if !ok {
				return
			}
			m.deliver(ctx, entry)
		}
	}
}

// processQueue is a backward-compatible method for testing.
// It starts the queue sweeper in the background and returns.
func (m *Manager) processQueue(ctx context.Context) {
	go m.queueSweeper(ctx)
}

// processPendingEntries is a backward-compatible method for testing.
// It performs a one-time sweep of pending entries.
func (m *Manager) processPendingEntries() {
	m.sweepPendingEntries()
}

// claimEntry moves the stored entry from "pending" to "sending" and returns
// the stored copy. It fails when the entry is gone or already claimed,
// delivered or bounced: the same entry can be dispatched more than once
// (Enqueue/RetryEntry and the sweeper), but only one copy may be delivered (F4927).
func (m *Manager) claimEntry(id string) (*db.QueueEntry, bool) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	cur, err := m.db.GetQueueEntry(id)
	// A stale duplicate dispatched while the entry is deferred must not
	// retry it early and burn an attempt (F5683).
	if err != nil || cur.Status != "pending" || cur.NextRetry.After(time.Now()) {
		return nil, false
	}
	cur.Status = "sending"
	if err := m.db.UpdateQueueEntry(cur); err != nil {
		return nil, false
	}
	return cur, true
}

// deliver attempts to deliver a message
func (m *Manager) deliver(ctx context.Context, entry *db.QueueEntry) {
	// Per-domain cap (F6020): over the cap, leave the entry pending and try
	// again shortly rather than parking a worker on a slow domain.
	if len(entry.To) > 0 {
		dom := strings.ToLower(extractDomain(entry.To[0]))
		if !m.acquireDomainSlot(dom) {
			m.requeueLater(entry)
			return
		}
		defer m.releaseDomainSlot(dom)
	}
	claimed, ok := m.claimEntry(entry.ID)
	if !ok {
		return
	}
	entry = claimed

	if m.tracingProvider != nil && m.tracingProvider.IsEnabled() {
		var span = m.startDeliverSpan(ctx, entry)
		defer span.End()
	}

	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("panic in delivery", "error", r, "to", entry.To)
			m.handleDeliveryFailure(entry, fmt.Sprintf("panic during delivery: %v", r))
		}
	}()

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
	if err != nil && !isMXNotFound(err) {
		// RFC 5321 §5.1: only a domain with no MX records (NXDOMAIN/NODATA)
		// falls back to its address record. A temporary DNS failure
		// (SERVFAIL, timeout) defers the message (F5390).
		m.handleDeliveryFailure(entry, fmt.Sprintf("451 4.4.3 MX lookup for %s failed: %v", domain, err))
		return
	}
	if len(mxRecords) == 0 {
		// Fall back to A record
		mxRecords = []string{domain}
	}

	// RFC 7505: a null MX means the domain accepts no mail; fail permanently
	// without connecting instead of retrying for days (F5158).
	if len(mxRecords) == 1 && (mxRecords[0] == "." || mxRecords[0] == "") {
		m.failDelivery(entry, "556 5.1.10 recipient domain "+domain+" does not accept mail (null MX)", true)
		return
	}

	// Try each MX server
	delivered := false
	permanent := false
	var lastErr string

	for _, mx := range mxRecords {
		mx = strings.TrimSuffix(mx, ".")
		if mx == "" {
			continue
		}
		if err := m.deliverToMXTraced(ctx, entry.From, entry.To[0], message, mx); err != nil {
			lastErr = tagMX(err.Error(), mx)
			// A 5yz reply is final (RFC 5321 §4.2.1); other MXs of the same
			// domain are not asked and the entry is not retried (F5156).
			var perm *permanentSMTPError
			if errors.As(err, &perm) {
				permanent = true
				break
			}
			continue
		}

		delivered = true
		break
	}

	if delivered {
		if err := m.handleDeliverySuccess(entry); err != nil {
			m.logger.Error("delivery marked success but queue entry update failed", "error", err, "id", entry.ID)
		}
	} else {
		m.failDelivery(entry, lastErr, permanent)
	}
}

// permanentSMTPError is a 5yz reply to MAIL, RCPT or DATA: the remote MTA
// rejected the transaction and retrying cannot change that (F5156).
type permanentSMTPError struct{ err error }

func (e *permanentSMTPError) Error() string { return e.err.Error() }
func (e *permanentSMTPError) Unwrap() error { return e.err }

// classifySMTPReply marks a 5yz reply error from the SMTP transaction as
// permanent; everything else (4yz, I/O errors) stays temporary.
func classifySMTPReply(err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) && tp.Code >= 500 && tp.Code <= 599 {
		return &permanentSMTPError{err: err}
	}
	return err
}

// isMXNotFound reports whether an MX lookup error means the domain has no MX
// records (NXDOMAIN or NODATA) rather than a temporary resolution failure.
func isMXNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// enhancedStatusRe matches a 5yz reply carrying an RFC 3463 enhanced status
// code, e.g. `550 5.1.1 user unknown` or, as textproto.Error renders it,
// `550 "5.1.1 user unknown"`.
var enhancedStatusRe = regexp.MustCompile(`^5\d\d[ -]"?(5\.\d{1,3}\.\d{1,3})(?:[\s"]|$)`)

// bounceStatus returns the RFC 3464 Status for a failure DSN: the enhanced
// status code of the remote 5yz reply when present, else 5.0.0 (F5392).
func bounceStatus(lastError string) string {
	if m := enhancedStatusRe.FindStringSubmatch(lastError); m != nil {
		return m[1]
	}
	return "5.0.0"
}

// mxTagRe matches the " (mx: host)" suffix tagMX appends to a delivery error.
var mxTagRe = regexp.MustCompile(` \(mx: ([^\s()]+)\)$`)

// tagMX records which MX host produced a delivery error so the failure DSN
// can name it in Remote-MTA (RFC 3464 §2.3.5, F5684).
func tagMX(msg, mx string) string { return msg + " (mx: " + mx + ")" }

// splitMXTag splits a LastError into its text and the tagged MX host.
func splitMXTag(lastError string) (text, mx string) {
	if m := mxTagRe.FindStringSubmatchIndex(lastError); m != nil {
		return lastError[:m[0]], lastError[m[2]:m[3]]
	}
	return lastError, ""
}

// startDeliverSpan starts a queue.deliver span carrying envelope attributes.
// Caller is responsible for ending the returned span.
func (m *Manager) startDeliverSpan(ctx context.Context, entry *db.QueueEntry) trace.Span {
	_, span := m.tracingProvider.StartSpanWithKind(ctx, "queue.deliver", tracing.SpanKindInternal)
	tracing.SetStringAttribute(span, "queue.from", entry.From)
	if len(entry.To) > 0 {
		tracing.SetStringAttribute(span, "queue.to", entry.To[0])
	}
	tracing.SetStringAttribute(span, "queue.id", entry.ID)
	tracing.SetIntAttribute(span, "queue.retry_count", entry.RetryCount)
	return span
}

// deliverToMXTraced wraps deliverToMX in a tracing span when enabled.
func (m *Manager) deliverToMXTraced(ctx context.Context, from, to string, message []byte, mx string) error {
	if m.tracingProvider == nil || !m.tracingProvider.IsEnabled() {
		return m.deliverToMX(ctx, from, to, message, mx)
	}
	spanCtx, span := m.tracingProvider.StartSpanWithKind(ctx, "queue.deliver.mx", tracing.SpanKindClient)
	defer span.End()
	tracing.SetStringAttribute(span, "queue.mx", mx)
	tracing.SetStringAttribute(span, "queue.from", from)
	tracing.SetStringAttribute(span, "queue.to", to)
	err := m.deliverToMX(spanCtx, from, to, message, mx)
	if err != nil {
		tracing.RecordError(span, err)
		tracing.SetStatus(span, tracing.StatusError, err.Error())
	} else {
		tracing.SetStatus(span, tracing.StatusOk, "")
	}
	return err
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

	now := time.Now()

	for {
		var conn *mxConn
		pool.mu.Lock()
		// Find the first non-expired connection from the end
		for i := len(pool.conns) - 1; i >= 0; i-- {
			c := pool.conns[i]
			if now.Sub(c.lastUsed) > pool.idleTimeout {
				_ = c.client.Close()
				pool.conns = append(pool.conns[:i], pool.conns[i+1:]...)
				continue
			}
			conn = c
			pool.conns = append(pool.conns[:i], pool.conns[i+1:]...)
			break
		}
		pool.mu.Unlock()

		if conn == nil {
			// No valid connection in pool, need to create new
			return nil, false, nil
		}

		// Check connection health without holding the pool lock
		if err := conn.client.Reset(); err == nil {
			return conn.client, true, nil
		}
		_ = conn.client.Close()
		// Connection was dead, loop to try the next one
	}
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

	m.mu.RLock()
	ioTimeout := m.ioTimeout
	m.mu.RUnlock()
	if ioTimeout > 0 {
		conn = &deadlineConn{Conn: conn, timeout: ioTimeout}
	}
	client, err := smtp.NewClient(conn, mx)
	if err != nil {
		_ = conn.Close() // Best-effort
		return nil, err
	}
	// Hello must precede any other command; an invalid configured name keeps
	// net/smtp's default.
	if herr := client.Hello(m.helloNameFor(conn)); herr != nil {
		m.logger.Debug("outbound EHLO name rejected", "error", herr)
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
		_ = client.Close() // Best-effort
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
		_ = client.Close() // Best-effort
	}
}

// mxBreakerFor returns the circuit breaker of one MX host, or nil when
// breaking is disabled.
func (m *Manager) mxBreakerFor(mx string) *circuitbreaker.CircuitBreaker {
	if m.mxBreaker == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mxBreakers == nil {
		m.mxBreakers = make(map[string]*circuitbreaker.CircuitBreaker)
	}
	cb, ok := m.mxBreakers[mx]
	if !ok {
		cb = circuitbreaker.New(*m.mxBreaker)
		m.mxBreakers[mx] = cb
	}
	return cb
}

// deliverToMX delivers a message to a specific MX server
func (m *Manager) deliverToMX(ctx context.Context, from, to string, message []byte, mx string) error {
	// Use a per-host circuit breaker so a failing MX host only stops
	// deliveries to itself (F5155).
	cb := m.mxBreakerFor(mx)
	if cb == nil {
		return m.doDeliverToMX(ctx, from, to, message, mx)
	}
	var rejected error
	err := cb.Execute(func() error {
		err := m.doDeliverToMX(ctx, from, to, message, mx)
		var perm *permanentSMTPError
		var pol *tlsPolicyError
		if errors.As(err, &perm) || errors.As(err, &pol) {
			// The host answered: a 5yz rejection of one recipient is not
			// a host failure and must not open the breaker.
			rejected = err
			return nil
		}
		return err
	})
	if rejected != nil {
		return rejected
	}
	return err
}

// tlsPolicyError is a delivery refusal caused by the recipient domain's TLS
// policy (MTA-STS enforce, required TLS) rather than a failing host: it is
// retried on the next MX but must not open that host's circuit breaker for
// other domains.
type tlsPolicyError struct{ err error }

func (e *tlsPolicyError) Error() string { return e.err.Error() }
func (e *tlsPolicyError) Unwrap() error { return e.err }

// withMXConn acquires an MX connection, calls fn, and guarantees release.
// It recovers from panics inside fn and returns them as errors.
func (m *Manager) withMXConn(mx string, fn func(*smtp.Client) error) (err error) {
	return m.withMXConnPolicy(mx, false, fn)
}

// tlsUnverified reports whether client runs TLS without a verified peer
// certificate (opportunistic STARTTLS skips verification).
func tlsUnverified(client *smtp.Client) bool {
	st, ok := client.TLSConnectionState()
	return ok && len(st.VerifiedChains) == 0
}

// withMXConnPolicy is withMXConn; strict (requireTLS or an MTA-STS enforce
// policy) refuses a pooled connection whose TLS session was never certificate
// verified. Pools are keyed by MX host, and a session that an unauthenticated
// opportunistic delivery negotiated must not carry a delivery that demands an
// authenticated one (F6022).
func (m *Manager) withMXConnPolicy(mx string, strict bool, fn func(*smtp.Client) error) (err error) {
	client, fromPool, err := m.acquireMXConn(mx)
	if err != nil {
		return err
	}
	for fromPool && strict && tlsUnverified(client) {
		_ = client.Close()
		client, fromPool, err = m.acquireMXConn(mx)
		if err != nil {
			return err
		}
	}
	if !fromPool && client == nil {
		client, err = m.createMXConn(mx)
		if err != nil {
			return err
		}
	}

	// For pooled connections: verify with RSET, replacing a dead one
	if fromPool {
		if rerr := client.Reset(); rerr != nil {
			m.releaseMXConn(mx, client, false)
			client, err = m.createMXConn(mx)
			if err != nil {
				return err
			}
		}
	}

	// withMXConn is the only owner of the release: fn must not release client.
	// A successful delivery returns the connection to the pool; any error or
	// panic closes it.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during MX delivery: %v", r)
		}
		m.releaseMXConn(mx, client, err == nil)
	}()

	return fn(client)
}

// doDeliverToMX performs the actual MX delivery
func (m *Manager) doDeliverToMX(ctx context.Context, from, to string, message []byte, mx string) error {
	// Check MTA-STS policy for recipient domain
	domain := extractDomain(to)
	enforceTLS := false
	m.mu.RLock()
	requireTLS := m.requireTLS
	m.mu.RUnlock()
	testingMode := false
	if m.mtastsValidator != nil && domain != "" {
		allowed, policy, err := m.mtastsValidator.CheckPolicy(ctx, domain, mx)
		if err != nil {
			// RFC 8461 3.3: with no usable policy delivery is opportunistic, but
			// the operator must be able to see that a policy could not be
			// obtained (a cached policy is kept by the validator, so reaching
			// here means none is known).
			m.logger.Warn("MTA-STS policy unavailable, delivering without policy", "domain", domain, "mx", mx, "error", err)
		}
		if policy != nil && policy.Mode == auth.MTASTSModeEnforce {
			if !allowed {
				m.recordTLSFailure(domain, mx, "sts-policy-invalid")
				return &tlsPolicyError{fmt.Errorf("MTA-STS policy violation: MX %s not allowed for domain %s", mx, domain)}
			}
			enforceTLS = true
			m.logger.Debug("MTA-STS policy enforced", "domain", domain, "mx", mx)
		}
		if policy != nil && policy.Mode == auth.MTASTSModeTesting {
			testingMode = true
			if !allowed {
				m.recordTLSFailure(domain, mx, "sts-policy-invalid")
			}
		}
	}

	// Normalise (CRLF, no Bcc/Return-Path) before signing, always.
	message = prepareOutbound(message)

	// Sign with DKIM if possible. Policy: a signing failure never blocks
	// delivery; the message goes out unsigned and the reason is logged
	// (error text never contains key material).
	signedMsg, err := m.signWithDKIM(from, message)
	if err == nil && len(signedMsg) > 0 {
		message = signedMsg
	} else if err != nil && m.logger != nil && m.db != nil {
		m.logger.Warn("DKIM signing skipped, delivering unsigned", "from", from, "error", err.Error())
	}

	// The real envelope sender is used: a VERP address (bounce-user=dom@sender)
	// has no decoder or mailbox on the inbound side, so asynchronous bounces to
	// it were rejected and never reached the sender (F5681).
	envelopeSender := from

	return m.withMXConnPolicy(mx, requireTLS || enforceTLS, func(client *smtp.Client) error {
		// Attempt STARTTLS
		// Opportunistic TLS (neither requireTLS nor an MTA-STS enforce policy)
		// is encryption without authentication: an unverifiable certificate
		// must not fail the delivery, because a failed handshake leaves the
		// connection unusable (F5682).
		tlsConfig := &tls.Config{
			ServerName:         mx,
			MinVersion:         tls.VersionTLS12,
			RootCAs:            m.tlsRootCAs,
			InsecureSkipVerify: !requireTLS && !enforceTLS, //nolint:gosec // opportunistic TLS, RFC 7435
		}
		// A reused pooled connection may already be TLS; STARTTLS again is a
		// protocol error that remote MTAs reject.
		var tlsErr error
		if _, isTLS := client.TLSConnectionState(); !isTLS {
			tlsErr = client.StartTLS(tlsConfig)
		}
		if err := tlsErr; err != nil {
			// requireTLS and an MTA-STS enforce policy (RFC 8461 4.2) must never
			// fall back to plaintext: STARTTLS refused, stripped (502/454/4xx),
			// or failed means this MX is not usable.
			if requireTLS || enforceTLS {
				if enforceTLS {
					m.recordTLSFailure(domain, mx, "starttls-not-supported")
				}
				return &tlsPolicyError{fmt.Errorf("STARTTLS required but failed: %w", err)}
			}
			if testingMode {
				m.recordTLSFailure(domain, mx, "starttls-not-supported")
			}
			// A non-reply error is a failed handshake: the connection is
			// unusable and cannot fall back to plaintext (F5682).
			var tp *textproto.Error
			if !errors.As(err, &tp) {
				return fmt.Errorf("STARTTLS handshake failed: %w", err)
			}
			// STARTTLS refused by the server — continue with plaintext
		} else {
			if st, ok := client.TLSConnectionState(); ok {
				if testingMode && len(st.VerifiedChains) == 0 {
					// Opportunistic session in testing mode: still check the
					// certificate so TLS-RPT style failures are recorded.
					if verr := verifyPeerForHost(st, mx, m.tlsRootCAs); verr != nil {
						m.recordTLSFailure(domain, mx, "certificate-not-trusted")
						m.logger.Warn("MTA-STS testing mode: certificate validation failed", "domain", domain, "mx", mx, "error", verr)
					}
				}
				if derr := m.checkDANE(mx, st); derr != nil {
					return derr
				}
			}
		}

		// Set sender (VERP-encoded for bounce tracking)
		if err := client.Mail(envelopeSender); err != nil {
			return classifySMTPReply(err)
		}

		// Set recipient
		if err := client.Rcpt(to); err != nil {
			return classifySMTPReply(err)
		}

		// RFC 1870: do not transmit a message the server already declared too
		// large; the 552 would be final anyway (F5903).
		if ok, param := client.Extension("SIZE"); ok {
			if limit, perr := strconv.ParseInt(strings.TrimSpace(param), 10, 64); perr == nil && limit > 0 && int64(len(message)) > limit {
				return &permanentSMTPError{err: &textproto.Error{Code: 552, Msg: "5.3.4 message size exceeds remote limit of " + param + " bytes"}}
			}
		}

		// Send data
		w, err := client.Data()
		if err != nil {
			return classifySMTPReply(err)
		}

		_, err = w.Write(message)
		if err != nil {
			return err
		}

		// withMXConn pools the connection on success and closes it on error;
		// QUIT is skipped since we're keeping the connection alive
		return classifySMTPReply(w.Close())
	})
}

// recordTLSFailure counts a TLS-RPT (RFC 8460) style failure for a policy
// domain and logs it.
func (m *Manager) recordTLSFailure(domain, mx, resultType string) {
	m.tlsrptMu.Lock()
	if m.tlsrptFailures == nil {
		m.tlsrptFailures = make(map[string]int)
	}
	m.tlsrptFailures[domain+"|"+resultType]++
	m.tlsrptMu.Unlock()
	if m.logger != nil {
		m.logger.Warn("TLS-RPT failure", "domain", domain, "mx", mx, "result-type", resultType)
	}
}

// TLSFailureCounts returns a copy of the TLS-RPT style failure counters keyed
// "domain|result-type".
func (m *Manager) TLSFailureCounts() map[string]int {
	m.tlsrptMu.Lock()
	defer m.tlsrptMu.Unlock()
	out := make(map[string]int, len(m.tlsrptFailures))
	for k, v := range m.tlsrptFailures {
		out[k] = v
	}
	return out
}

// verifyPeerForHost verifies the presented certificate chain for host against
// the system roots.
func verifyPeerForHost(st tls.ConnectionState, host string, roots *x509.CertPool) error {
	if len(st.PeerCertificates) == 0 {
		return errors.New("no peer certificate")
	}
	inter := x509.NewCertPool()
	for _, c := range st.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	_, err := st.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: host, Roots: roots, Intermediates: inter})
	return err
}

// SetDANESecureHook installs a callback reporting whether TLSA answers for an
// MX host are DNSSEC authenticated (resolver AD bit, or a configured
// validating resolver). Without it DANE is advisory only: the stock resolver
// cannot validate DNSSEC, so TLSA data is never trusted (RFC 7672 8.1).
func (m *Manager) SetDANESecureHook(fn func(mx string) bool) {
	m.mu.Lock()
	m.daneSecure = fn
	m.mu.Unlock()
}

// checkDANE validates the negotiated TLS session against TLSA records when
// they exist and are DNSSEC authenticated; otherwise DANE stays opportunistic.
func (m *Manager) checkDANE(mx string, st tls.ConnectionState) error {
	if m.daneValidator == nil {
		return nil
	}
	tlsa, err := m.daneValidator.LookupTLSA(mx, 25)
	if err != nil {
		m.logger.Debug("TLSA lookup failed", "mx", mx, "error", err)
		return nil
	}
	if len(tlsa) == 0 {
		return nil
	}
	m.mu.RLock()
	secure := m.daneSecure
	m.mu.RUnlock()
	if secure == nil || !secure(mx) {
		// RFC 7672 8.1: without DNSSEC, DANE cannot provide security.
		m.logger.Warn("DANE TLSA records present but DNSSEC unavailable, skipping DANE validation (RFC 7672 8.1)",
			"mx", mx, "records", len(tlsa))
		return nil
	}
	res, verr := m.daneValidator.ValidateWithDNSSEC(mx, 25, &st, auth.DNSSECSecured)
	if res == auth.DANEFailed {
		m.recordTLSFailure(mx, mx, "tlsa-invalid")
		return fmt.Errorf("DANE validation failed for %s: %v", mx, verr)
	}
	return nil
}

// handleDeliverySuccess handles successful delivery. Returns error if the queue
// entry could not be persisted — callers must not delete message files on error.
func (m *Manager) handleDeliverySuccess(entry *db.QueueEntry) error {
	entry.Status = "delivered"
	if err := m.db.UpdateQueueEntry(entry); err != nil {
		m.logger.Error("failed to update queue entry after delivery success", "error", err)
		return err
	}

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

	// Trigger webhook for successful delivery
	if m.webhook != nil {
		m.webhook.Trigger("delivery.success", map[string]interface{}{
			"message_id": entry.ID,
			"from":       entry.From,
			"to":         entry.To,
			"domain":     extractDomain(entry.To[0]),
		})
	}

	return nil
}

// sendSuccessDSN sends a DSN success notification
func (m *Manager) sendSuccessDSN(entry *db.QueueEntry) {
	// A null reverse-path has nobody to notify (RFC 5321 §4.5.5); enqueueing
	// to an empty recipient only creates an undeliverable entry (F5900).
	if entry.From == "" {
		return
	}
	// GenerateDSN extracts the headers when RET requests headers only.
	originalMsg, _ := readFile(entry.MessagePath)

	dsn := &DSN{
		ReportedDomain: "umailserver",
		ReportedName:   "umailserver",
		ArrivalDate:    entry.CreatedAt,
		OriginalFrom:   entry.From,
		OriginalTo:     entry.To[0],
		Recipient: DSNRecipient{
			Original: entry.To[0],
			Notify:   DSNNotifyNever,
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
	// RFC 3461 §6.2: a DSN is sent with a null reverse-path so that a failing
	// DSN is never bounced to our own MAILER-DAEMON address (F5391).
	if _, err := m.Enqueue("", []string{entry.From}, dsnMsg); err != nil {
		m.logger.Error("failed to enqueue DSN", "error", err)
	}
}

// handleDeliveryFailure handles a temporary delivery failure with exponential
// backoff and jitter.
func (m *Manager) handleDeliveryFailure(entry *db.QueueEntry, errorMsg string) {
	m.failDelivery(entry, errorMsg, false)
}

// failDelivery records a failed attempt. A permanent failure bounces at once;
// a temporary one is retried until maxRetries is reached.
func (m *Manager) failDelivery(entry *db.QueueEntry, errorMsg string, permanent bool) {
	entry.LastError = errorMsg
	entry.RetryCount++

	// Check if max retries reached
	if permanent || entry.RetryCount >= m.maxRetries {
		// Generate bounce
		entry.Status = "bounced"
	} else {
		// Calculate retry delay with jitter (±20%)
		idx := entry.RetryCount - 1
		if idx >= len(retryDelays) {
			idx = len(retryDelays) - 1 // Use last delay if we've exceeded the array
		}
		baseDelay := retryDelays[idx]
		var n uint64
		if err := binary.Read(crand.Reader, binary.BigEndian, &n); err != nil {
			// Fallback to deterministic jitter if crypto/rand fails (extremely rare)
			n = uint64(time.Now().UnixNano())
		}
		jitter := time.Duration(float64(baseDelay) * (0.8 + 0.4*(float64(n)/float64(math.MaxUint64))))
		entry.NextRetry = time.Now().Add(jitter)
		entry.Status = "pending"
	}

	if err := m.db.UpdateQueueEntry(entry); err != nil {
		m.logger.Error("failed to update queue entry after delivery failure", "error", err)
		return
	}

	if entry.Status == "bounced" {
		m.generateBounce(entry)
		m.deleteMessageFileIfUnreferenced(entry.MessagePath)
	}

	// Track metric
	if m.metrics != nil {
		m.metrics.DeliveryFailed()
	}

	// Trigger webhook for failed delivery
	if m.webhook != nil {
		m.webhook.Trigger("delivery.failed", map[string]interface{}{
			"message_id":  entry.ID,
			"from":        entry.From,
			"to":          entry.To,
			"domain":      extractDomain(entry.To[0]),
			"error":       errorMsg,
			"retry_count": entry.RetryCount,
			"max_retries": m.maxRetries,
		})
	}
}

// generateBounce generates a bounce message and delivers it back to the sender
func (m *Manager) generateBounce(entry *db.QueueEntry) {
	// RFC 3461 §4.1: without NOTIFY the default is FAILURE; with NOTIFY a
	// failure DSN is sent only when FAILURE is listed (NEVER excludes it).
	// NOTIFY=SUCCESS or DELAY alone must not produce a failure DSN (F5159).
	if entry.Notify != 0 && (int(entry.Notify)&int(DSNNotifyNever) != 0 || int(entry.Notify)&int(DSNNotifyFailure) == 0) {
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
			Notify:   DSNNotifyNever,
			Ret:      ret,
		},
		Action:         "failed",
		Status:         bounceStatus(entry.LastError),
		DiagnosticCode: entry.LastError,
		FinalMTA:       "umailserver",
		MessageID:      GenerateMessageID(),
	}
	// Name the MX host that rejected the message (F5684).
	dsn.DiagnosticCode, dsn.RemoteMTA = splitMXTag(entry.LastError)

	// Generate proper DSN bounce message
	bounceMsg, err := GenerateDSN(dsn, originalMsg, ret)
	if err != nil {
		m.logger.Error("failed to generate DSN bounce", "error", err)
		// Fall back to old-style bounce
		bounceMsg = m.createFallbackBounce(entry, originalMsg)
	}

	// Enqueue bounce as a new message back to the sender
	if m.db != nil {
		if entry.From == "" {
			m.logger.Warn("cannot send bounce: original message had null sender (MAIL FROM:<>), message lost", "entry_id", entry.ID)
		} else if _, enqueueErr := m.Enqueue("", []string{entry.From}, bounceMsg); enqueueErr != nil {
			// RFC 5321 §4.5.5: failure notifications MUST use a null return
			// path. A failing bounce is then dropped by the null-sender guard
			// above instead of generating another bounce to ourselves.
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

	// Count all queue entries by status
	err := m.db.ForEach(db.BucketQueue, func(key string, value []byte) error {
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

	return stats, err
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
	if _, err := crand.Read(b); err != nil {
		// Fallback to partial random if crypto/rand fails (extremely rare)
		b[0] = byte(time.Now().UnixNano() & 0xff)
		b[1] = byte((time.Now().UnixNano() >> 8) & 0xff)
	}
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
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	// Write to temp file first, then rename for atomicity
	tmpPath := path + ".tmp"
	if err := os.WriteFile(filepath.Clean(tmpPath), data, 0o600); err != nil {
		return err
	}

	// Sync temp file before rename to ensure data durability
	f, err := os.OpenFile(filepath.Clean(tmpPath), os.O_RDWR, 0)
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() // Best-effort
		_ = os.Remove(tmpPath)
		return err
	}
	_ = f.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// Sync parent directory to ensure the rename is durable.
	// Directory sync may fail on Windows; file sync above is the critical part.
	dirFile, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	_ = dirFile.Sync()
	_ = dirFile.Close()
	return nil
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path))
}

func deleteFile(path string) {
	_ = os.Remove(path)
}

// countMessageRefs counts how many queue entries still reference the given
// message file path. Used to avoid deleting a shared .msg file while other
// recipients still need it. Must be called with mu held for read.
func (m *Manager) countMessageRefs(messagePath string) int {
	// Lock must be held by caller

	count := 0
	_ = m.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
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
		_ = os.Remove(messagePath)
		return true
	}
	return false
}

// countMessageRefsUnsafe counts references without locking. Caller must hold mu.
func (m *Manager) countMessageRefsUnsafe(messagePath string) int {
	count := 0
	_ = m.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
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

// prepareOutbound normalises a message for remote delivery (F6203, F6205):
//   - bare LF / bare CR line endings become CRLF, so the bytes signed are the
//     bytes the receiver sees (the SMTP DATA writer converts them anyway, which
//     would invalidate a "simple" body hash computed over LF text);
//   - Return-Path is removed (it is added by the final delivery agent; a copy
//     here would be duplicated or spoofed);
//   - Bcc is removed so blind recipients never reach remote servers in the
//     header (the envelope already carries them).
func prepareOutbound(message []byte) []byte {
	// Normalise line endings.
	if bytes.IndexByte(message, '\n') >= 0 || bytes.IndexByte(message, '\r') >= 0 {
		out := make([]byte, 0, len(message)+len(message)/64)
		for i := 0; i < len(message); i++ {
			c := message[i]
			switch c {
			case '\r':
				if i+1 < len(message) && message[i+1] == '\n' {
					i++
				}
				out = append(out, '\r', '\n')
			case '\n':
				out = append(out, '\r', '\n')
			default:
				out = append(out, c)
			}
		}
		message = out
	}

	hdrEnd := bytes.Index(message, []byte("\r\n\r\n"))
	var hdr, rest []byte
	if hdrEnd >= 0 {
		hdr, rest = message[:hdrEnd+2], message[hdrEnd+2:]
	} else {
		hdr, rest = message, nil
	}
	var out []byte
	changed := false
	skipping := false
	for len(hdr) > 0 {
		i := bytes.Index(hdr, []byte("\r\n"))
		var line []byte
		if i < 0 {
			line, hdr = hdr, nil
		} else {
			line, hdr = hdr[:i+2], hdr[i+2:]
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if skipping {
				continue
			}
		} else {
			name := line
			if c := bytes.IndexByte(line, ':'); c >= 0 {
				name = line[:c]
			}
			n := strings.ToLower(strings.TrimSpace(string(name)))
			skipping = n == "bcc" || n == "return-path"
			if skipping {
				changed = true
				continue
			}
		}
		out = append(out, line...)
	}
	if !changed {
		// Nothing removed: reuse the (possibly line-normalised) message.
		return message
	}
	return append(out, rest...)
}

// parseDKIMKey parses a PEM private key (PKCS#1 RSA, PKCS#8 RSA or Ed25519).
func parseDKIMKey(pemData string) (*rsa.PrivateKey, ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, nil, fmt.Errorf("failed to decode DKIM private key PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil, nil
	} else {
		key, err8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err8 != nil {
			// Never include key material in the error.
			return nil, nil, fmt.Errorf("failed to parse DKIM private key: %w (pkcs1: %w)", err8, err)
		}
		switch kk := key.(type) {
		case *rsa.PrivateKey:
			return kk, nil, nil
		case ed25519.PrivateKey:
			return nil, kk, nil
		}
		return nil, nil, fmt.Errorf("DKIM private key is neither RSA nor Ed25519")
	}
}

// signWithDKIM signs an outgoing message with DKIM. The signing domain is the
// From-header domain when we own it and it has a key (DMARC alignment), else
// the envelope sender's domain. Domains not present in the database are never
// signed for. A returned error means the message must go out unsigned (policy:
// deliver unsigned rather than fail; the caller logs it).
func (m *Manager) signWithDKIM(from string, message []byte) ([]byte, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	message = prepareOutbound(message)

	envDomain := extractDomain(from)
	var candidates []string
	if hd := fromHeaderDomain(message); hd != "" {
		candidates = append(candidates, hd)
	}
	if envDomain != "" && (len(candidates) == 0 || !strings.EqualFold(candidates[0], envDomain)) {
		candidates = append(candidates, envDomain)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("cannot extract domain from sender: %s", from)
	}

	var lastErr error
	for _, d := range candidates {
		domain, err := m.db.GetDomain(strings.ToLower(d))
		if err != nil {
			lastErr = fmt.Errorf("domain %s not found: %w", d, err)
			continue
		}
		if domain.DKIMPrivateKey == "" || domain.DKIMSelector == "" {
			lastErr = fmt.Errorf("no DKIM key configured for domain %s", d)
			continue
		}
		signed, err := m.signAs(strings.ToLower(d), domain.DKIMSelector, domain.DKIMPrivateKey, message)
		if err != nil {
			lastErr = err
			continue
		}
		return signed, nil
	}
	return nil, lastErr
}

func (m *Manager) signAs(signDomain, selector, keyPEM string, message []byte) ([]byte, error) {
	rsaKey, edKey, err := parseDKIMKey(keyPEM)
	if err != nil {
		return nil, err
	}

	headers := parseMessageHeaders(message)
	if len(headers) == 0 {
		return nil, fmt.Errorf("cannot parse message headers for DKIM")
	}
	body := extractMessageBody(message)

	var signer *auth.DKIMSigner
	dnsResolver := &dkimDNSResolver{}
	if edKey != nil {
		signer = auth.NewDKIMSignerEd25519(dnsResolver, edKey, signDomain, selector)
	} else {
		signer = auth.NewDKIMSigner(dnsResolver, rsaKey, signDomain, selector)
	}
	signature, err := signer.Sign(headers, body)
	if err != nil {
		return nil, fmt.Errorf("DKIM signing failed: %w", err)
	}

	// Sign returns only the field value; the field name is required (F4925).
	dkimHeader := "DKIM-Signature: " + signature + "\r\n"
	signedMessage := make([]byte, 0, len(dkimHeader)+len(message))
	signedMessage = append(signedMessage, dkimHeader...)
	signedMessage = append(signedMessage, message...)
	return signedMessage, nil
}

// fromHeaderDomain returns the domain of the first From header address.
func fromHeaderDomain(message []byte) string {
	h := parseMessageHeaders(message)
	v := h["From"]
	if len(v) == 0 {
		return ""
	}
	addr, err := mail.ParseAddress(v[0])
	if err != nil {
		return ""
	}
	return extractDomain(addr.Address)
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
